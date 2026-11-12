import assert from 'node:assert/strict'
import { before, after, describe, it, TestContext } from 'node:test'
import { EventEmitter } from 'events'
import { mkdir, mkdtemp, rm, writeFile, readFile, rename } from 'fs/promises'
import { join, resolve } from 'path'
import { exec } from 'dugite'
import { ipcRenderer } from 'electron'
import type { AppRuntime } from '../../src/ui/app-runtime'
import { Account } from '../../src/models/account'
import { Repository } from '../../src/models/repository'
import { PullRequest, PullRequestRef } from '../../src/models/pull-request'
import { forkPullRequestRemoteName } from '../../src/models/remote'
import { PopupType } from '../../src/models/popup'
import { Banner, BannerType } from '../../src/models/banner'
import {
  RepositorySectionTab,
  ChangesSelectionKind,
  SelectionType,
} from '../../src/lib/app-state'
import { AppMenuController } from '../../src/ui/app-menu-controller'
import { FoldoutType } from '../../src/lib/app-state'
import { TipState } from '../../src/models/tip'
import {
  parseNativeWorkspaceCommand,
  NativeWorkspaceAction,
} from '../../src/native-kernel/workspace-protocol'
import {
  executeNativeWorkspaceCommand,
  buildWorkspaceMenu,
} from '../../src/native-kernel/workspace-presenter'
import {
  projectNativeWorkspace,
  visibleWorkingDirectoryFiles,
} from '../../src/native-kernel/workspace-projection'
import { projectDiffSelection } from '../../src/native-kernel/line-selection'
import { closeKernelPopup } from '../../src/native-kernel/popup-presenter'
import { repositoryDialogs } from '../../src/native-kernel/repository-dialogs'
import { DesktopPresenter } from '../../src/native-kernel/desktop-presenter'
import { projectKernelState } from '../../src/native-kernel/state-projection'
import { executeKernelCommand } from '../../src/native-kernel/execute-command'
import { parseKernelCommand } from '../../src/native-kernel/command-validation'
import { captureClipboardWrites } from '../helpers/ui/electron'
import { hasRepositoryGitOperations } from '../../src/lib/git/repository-operation'

// The standard unit-platform seam only. Git/AppStore/Dispatcher and their
// conflict/stash/checkout contracts are real and share one original runtime.
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})

describe(
  'native workspace original kernel behavior',
  // Budget the full series of real Git workflows, including Windows process
  // startup. The former aggregate deadline cancelled later recovery cases.
  { timeout: 600000 },
  () => {
    let runtime: AppRuntime
    before(async () => {
      localStorage.clear()
      localStorage.setItem('stats-opt-out', '1')
      localStorage.setItem('has-sent-stats-opt-in-ping', '1')
      const { getAppRuntime } = await import('../../src/ui/app-runtime')
      runtime = getAppRuntime()
    })
    after(() => window.close())

    async function until(predicate: () => boolean) {
      if (predicate()) {
        return
      }
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => {
          subscription.dispose()
          reject(new Error('Original workspace state did not settle'))
        }, 15000)
        const subscription = runtime.appStore.onDidUpdate(() => {
          if (predicate()) {
            clearTimeout(timer)
            subscription.dispose()
            resolve()
          }
        })
      })
    }

    async function fixture(t: TestContext) {
      const root = resolve('_temp/native-workspace-tests')
      await mkdir(root, { recursive: true })
      const path = await mkdtemp(join(root, 'repo-'))
      const git = async (args: string[], code = 0) => {
        const result = await exec(args, path)
        assert.equal(result.exitCode, code, result.stderr)
        return result.stdout.trim()
      }
      await git(['init', '-b', 'main'])
      await git(['config', 'core.autocrlf', 'false'])
      await git(['config', 'user.name', 'Workspace Fixture'])
      await git(['config', 'user.email', 'workspace@example.invalid'])
      await writeFile(join(path, 'file.txt'), 'base\n')
      await git(['add', '.'])
      await git(['commit', '-m', 'base'])
      const [repository] = await runtime.dispatcher.addRepositories([path])
      assert.ok(repository instanceof Repository)
      await runtime.dispatcher.selectRepository(repository)
      await runtime.dispatcher.refreshRepository(repository)
      const act = (action: NativeWorkspaceAction) =>
        executeNativeWorkspaceCommand(runtime, repository, action)
      const state = () => runtime.repositoryStateManager.get(repository)
      const view = () => projectNativeWorkspace(runtime.appStore.getState())
      t.after(async () => {
        runtime.dispatcher.clearBanner()
        while (runtime.appStore.getState().currentPopup !== null) {
          runtime.dispatcher.closePopupById(
            runtime.appStore.getState().currentPopup!.id!
          )
        }
        if (
          runtime.appStore
            .getState()
            .repositories.some(r => r.id === repository.id)
        ) {
          await runtime.dispatcher.removeRepository(repository, false)
        }
        await rm(path, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 50,
        })
      })
      return { repository, path, git, act, state, view }
    }

    it('projects the original unpushed commits and repository list state', async t => {
      const f = await fixture(t)
      const base = await f.git(['rev-parse', 'HEAD'])
      await writeFile(join(f.path, 'file.txt'), 'changed\n')
      await runtime.dispatcher.refreshRepository(f.repository)
      // Without a remote every commit is local in the original GitStore.
      await until(() => f.view().localCommitSHAs.includes(base))
      assert.deepEqual(f.view().tagsToPush, [])
      await until(
        () =>
          runtime.appStore
            .getState()
            .localRepositoryStateLookup.get(f.repository.id)
            ?.changedFilesCount === 1
      )
      const listed = projectKernelState(
        runtime.appStore.getState(),
        1
      ).repositories.find(r => r.id === f.repository.id)
      assert.deepEqual(listed?.local, {
        ahead: null,
        behind: null,
        changedFiles: 1,
      })
    })

    it('projects real submodule commits and opens only the selected original submodule', async t => {
      const paths: string[] = []
      const refreshes: Promise<void>[] = []
      const refresh = runtime.appStore._refreshRepository.bind(runtime.appStore)
      t.mock.method(
        runtime.appStore,
        '_refreshRepository',
        (repository: Repository) => {
          const pending = refresh(repository)
          refreshes.push(pending)
          return pending
        }
      )
      // Repository selection launches real background refreshes. Keep every
      // fixture directory until all nested repositories are unregistered and
      // those reads have settled; t.after hooks run in registration order.
      t.after(async () => {
        const owned = runtime.appStore
          .getState()
          .repositories.filter(r => paths.includes(r.path))
          .sort((a, b) => b.path.length - a.path.length)
        for (const repository of owned) {
          await runtime.dispatcher.removeRepository(repository, false)
          await until(
            () =>
              !runtime.appStore
                .getState()
                .repositories.some(r => r.id === repository.id)
          )
        }
        await Promise.all(refreshes)
      })
      const child = await fixture(t)
      paths.push(child.path)
      const parent = await fixture(t)
      paths.push(parent.path)
      await parent.git([
        '-c',
        'protocol.file.allow=always',
        'submodule',
        'add',
        child.path,
        'module',
      ])
      await parent.git(['commit', '-am', 'add module'])
      const modulePath = join(parent.path, 'module')
      paths.push(modulePath)
      const childGit = async (args: string[]) => {
        const result = await exec(args, modulePath)
        assert.equal(result.exitCode, 0, result.stderr)
        return result.stdout.trim()
      }
      const oldSHA = await childGit(['rev-parse', 'HEAD'])
      await childGit(['config', 'user.name', 'Module Fixture'])
      await childGit(['config', 'user.email', 'module@example.invalid'])
      await writeFile(join(modulePath, 'file.txt'), 'new module version\n')
      await childGit(['commit', '-am', 'module change'])
      const newSHA = await childGit(['rev-parse', 'HEAD'])
      await runtime.dispatcher.refreshRepository(parent.repository)
      const file = parent
        .state()
        .changesState.workingDirectory.files.find(f => f.path === 'module')!
      assert.ok(file)
      await runtime.dispatcher.selectWorkingDirectoryFiles(parent.repository, [
        file,
      ])
      await until(
        () =>
          projectKernelState(runtime.appStore.getState(), 1).selected?.diff
            ?.kind === 'submodule'
      )
      const diff = projectKernelState(runtime.appStore.getState(), 1).selected!
        .diff!
      assert.deepEqual(
        [diff.submodule?.oldSHA, diff.submodule?.newSHA],
        [oldSHA, newSHA]
      )
      assert.equal(diff.submodule?.path, 'module')
      await assert.rejects(
        parent.act({ type: 'submodule.open', fileID: 'stale-file' }),
        /submodule changed/
      )
      // Opening an unregistered submodule retains the original confirmation.
      const cancelled = parent.act({ type: 'submodule.open', fileID: file.id })
      await until(
        () =>
          runtime.appStore.getState().currentPopup?.type ===
          PopupType.AddRepository
      )
      await closeKernelPopup(
        runtime,
        runtime.appStore.getState().currentPopup!.id!
      )
      await cancelled
      assert.equal(
        runtime.appStore.getState().selectedState?.repository.path,
        parent.path
      )
      const opening = parent.act({ type: 'submodule.open', fileID: file.id })
      await until(
        () =>
          runtime.appStore.getState().currentPopup?.type ===
          PopupType.AddRepository
      )
      const popup = runtime.appStore.getState().currentPopup!
      assert.ok(popup.type === PopupType.AddRepository)
      assert.equal(popup.path, modulePath)
      await repositoryDialogs.reply(runtime, popup, 'confirm', {
        path: modulePath,
      })
      await opening
      await until(
        () =>
          runtime.appStore.getState().selectedState?.repository.path ===
          modulePath
      )
      const selected = runtime.appStore.getState().selectedState!.repository
      assert.ok(selected instanceof Repository)
      await Promise.all(refreshes)
      const tip = runtime.repositoryStateManager.get(selected).branchesState.tip
      assert.ok(tip.kind === TipState.Valid)
      assert.equal(tip.branch.tip.sha, newSHA)
    })

    it('lets the native Push entry continue to the original unpublished repository dialog', async t => {
      const f = await fixture(t)
      const presenter = new DesktopPresenter(runtime)
      t.after(() => presenter.dispose())
      await presenter.push({ id: f.repository.id, path: f.repository.path })
      const popup = runtime.appStore.getState().currentPopup
      assert.equal(popup?.type, PopupType.PublishRepository)
      assert.ok(popup?.type === PopupType.PublishRepository)
      assert.equal(popup.repository.path, f.repository.path)
    })

    it('uses the original recommended force-push confirmation without writing the remote', async t => {
      const f = await fixture(t)
      const remote = `${f.path}-remote.git`
      t.after(() =>
        rm(remote, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 50,
        })
      )
      await f.git(['clone', '--bare', f.path, remote])
      await f.git(['remote', 'add', 'origin', remote])
      await f.git(['push', '--set-upstream', 'origin', 'main'])
      await runtime.dispatcher.refreshRepository(f.repository)
      const state = f.state()
      assert.ok(state.branchesState.tip.kind === TipState.Valid)
      const tip = state.branchesState.tip
      // This is the original cache shape produced by a published amend/rebase;
      // only the frontend entry is under test. Git and confirmation are real.
      runtime.repositoryStateManager.updateBranchesState(f.repository, () => ({
        forcePushBranches: new Map([
          [tip.branch.nameWithoutRemote, tip.branch.tip.sha],
        ]),
      }))
      runtime.repositoryStateManager.update(f.repository, () => ({
        aheadBehind: { ahead: 1, behind: 1 },
      }))
      runtime.dispatcher.setConfirmForcePushSetting(true)
      const presenter = new DesktopPresenter(runtime)
      t.after(() => presenter.dispose())
      const before = await f.git(['ls-remote', 'origin', 'refs/heads/main'])
      assert.equal(
        projectKernelState(runtime.appStore.getState(), 1).selected
          ?.forcePushRecommended,
        true
      )
      await presenter.push({ id: f.repository.id, path: f.repository.path })
      assert.equal(
        runtime.appStore.getState().currentPopup?.type,
        PopupType.ConfirmForcePush
      )
      assert.equal(
        await f.git(['ls-remote', 'origin', 'refs/heads/main']),
        before
      )
    })

    it('presents the original missing repository and locates it without replacing its identity', async t => {
      const f = await fixture(t)
      const moved = `${f.path}-moved`
      const presenter = new DesktopPresenter(runtime)
      t.after(() => presenter.dispose())
      t.after(() =>
        rm(moved, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 50,
        })
      )
      // Move the repository while Desktop is quiet. Selecting it starts
      // background reads; on Windows, renaming the directory under a running
      // Git process fails that read with Permission denied, which would be a
      // different scenario from an externally moved idle repository.
      for (let quiet = 0; quiet < 3; ) {
        await new Promise(resolve => setTimeout(resolve, 50))
        quiet = hasRepositoryGitOperations(f.path) ? 0 : quiet + 1
      }
      await rename(f.path, moved)
      await runtime.dispatcher.refreshRepository(f.repository)
      await until(
        () =>
          runtime.appStore.getState().selectedState?.type ===
          SelectionType.MissingRepository
      )
      assert.equal(
        runtime.appStore.getState().selectedState?.type,
        SelectionType.MissingRepository
      )
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => {
          subscription.dispose()
          reject(new Error('Original path inspection did not finish'))
        }, 15000)
        const subscription = presenter.onDidUpdate(() => {
          if (presenter.getMissingRepositoryState()?.checking === false) {
            clearTimeout(timer)
            subscription.dispose()
            resolve()
          }
        })
        presenter.getMissingRepositoryState()
      })
      const missing = presenter.getMissingRepositoryState()!
      assert.equal(missing.repository.id, f.repository.id)
      assert.equal(missing.repository.path, f.path)
      assert.equal(missing.canClone, false)
      const projection = projectKernelState(
        runtime.appStore.getState(),
        1,
        null,
        null,
        undefined,
        missing
      )
      assert.equal(projection.selected, null)
      assert.equal(projection.missingRepository?.repository.id, f.repository.id)
      await assert.rejects(
        presenter.recoverMissingRepository(
          { id: f.repository.id, path: f.path },
          'clone'
        )
      )
      const invoke = ipcRenderer.invoke.bind(ipcRenderer)
      t.mock.method(
        ipcRenderer,
        'invoke',
        async (channel: string, ...args: unknown[]) =>
          channel === 'show-open-dialog' ? moved : invoke(channel, ...args)
      )
      await presenter.recoverMissingRepository(
        { id: f.repository.id, path: f.path },
        'locate'
      )
      // RepositoriesStore publishes its completed database update on the next
      // asynchronous emission, just as the original React view observes it.
      await until(() =>
        runtime.appStore
          .getState()
          .repositories.some(r => r.id === f.repository.id && r.path === moved)
      )
      const relocated = runtime.appStore
        .getState()
        .repositories.find(r => r.id === f.repository.id)
      assert.ok(relocated instanceof Repository)
      assert.equal(relocated.path, moved)
      assert.equal(relocated.id, f.repository.id)
      await runtime.dispatcher.selectRepository(relocated)
      await runtime.dispatcher.refreshRepository(relocated)
      assert.equal(
        runtime.appStore.getState().selectedState?.type,
        SelectionType.Repository
      )
      assert.equal(presenter.getMissingRepositoryState(), null)
      await assert.rejects(
        presenter.recoverMissingRepository(
          { id: relocated.id, path: f.path },
          'trust'
        )
      )
    })

    it('strictly parses only the explicit union and rejects tokens/reflection/stale-shape fields', () => {
      const repository = { id: 1, path: 'fixture' }
      assert.equal(parseNativeWorkspaceCommand({ name: 'state' }), null)
      const command = {
        name: 'workspace.action',
        repository,
        action: { type: 'branch.checkout', ref: 'refs/heads/main' },
      }
      assert.deepEqual(parseNativeWorkspaceCommand(command), command)
      assert.equal(
        parseNativeWorkspaceCommand({
          name: 'workspace.action',
          repository: null,
          action: { type: 'banner', id: 1, action: 'dismiss' },
        })?.repository,
        null
      )
      assert.throws(() =>
        parseNativeWorkspaceCommand({ ...command, repository: null })
      )
      for (const action of [
        { type: 'dispatcher.deleteWorktree', path: 'anything' },
        { type: 'worktree.select', path: 'fixture', force: true },
        { type: 'stash.discard', sha: 'a', token: 'sentinel' },
        {
          type: 'diff.discard',
          identity: 'a',
          fileID: 'M+file',
          lineIDs: [-1],
        },
        { type: 'history.select', shas: ['a', 'a'] },
        { type: 'banner', id: 1, action: 'cancel' },
        {
          type: 'menu',
          target: { kind: 'branch', id: 'a' },
          lease: 'a',
          path: [0],
          callback: 'run',
        },
      ]) {
        assert.throws(() =>
          parseNativeWorkspaceCommand({
            name: 'workspace.action',
            repository,
            action,
          })
        )
      }
    })

    it('projects and constructs every offered menu without mutating the original section, draft or selection', async t => {
      const f = await fixture(t)
      await writeFile(
        join(f.path, 'file.txt'),
        'read-only projection fixture\n'
      )
      await runtime.dispatcher.refreshRepository(f.repository)
      await runtime.dispatcher.setCommitMessage(f.repository, {
        summary: 'Keep this draft',
        description: null,
        timestamp: 1,
      })
      const state = f.state()
      const app = runtime.appStore.getState()
      let updates = 0
      const subscription = runtime.appStore.onDidUpdate(() => updates++)
      try {
        for (let i = 0; i < 10; i++) {
          const projection = projectNativeWorkspace(app)
          for (const menu of projection.menus) {
            buildWorkspaceMenu(app, f.repository, state, menu.target, runtime)
          }
        }
        assert.equal(updates, 0)
        assert.equal(f.state(), state)
        assert.equal(f.state().selectedSection, RepositorySectionTab.Changes)
        assert.equal(
          f.state().changesState.commitMessage.summary,
          'Keep this draft'
        )
      } finally {
        subscription.dispose()
      }
      assert.equal(await f.git(['rev-list', '--count', 'HEAD']), '1')
      assert.equal(await f.git(['diff', '--cached', '--name-only']), '')
    })

    it('presents original App menu foldouts and resets their unmounted filter sessions', async t => {
      const f = await fixture(t)
      await f.git(['branch', 'feature'])
      await runtime.dispatcher.refreshRepository(f.repository)
      const menu = new AppMenuController(
        runtime.dispatcher,
        () => runtime.appStore.getState(),
        {
          perform: action => {
            throw new Error(`Unexpected toolkit action: ${action}`)
          },
          showTestUI: () => {
            throw new Error('Unexpected test dialog')
          },
        }
      )
      await menu.handleEvent('show-branches')
      assert.equal(f.view().navigation, 'branches')
      await f.act({
        type: 'navigation.filter',
        scope: 'branches',
        text: 'feature',
      })
      assert.deepEqual(
        f
          .view()
          .branches.flatMap(group => group.items)
          .map(branch => branch.name),
        ['feature']
      )
      await menu.handleEvent('show-branches')
      assert.equal(f.view().navigation, '')
      await menu.handleEvent('show-branches')
      assert.equal(f.view().filters.branches, '')
      assert.ok(
        f
          .view()
          .branches.flatMap(group => group.items)
          .some(branch => branch.name === 'main')
      )
      await f.act({ type: 'navigation.close', target: 'branches' })
      assert.equal(runtime.appStore.getState().currentFoldout, null)
      await f.act({ type: 'navigation.open', target: 'worktrees' })
      assert.equal(
        runtime.appStore.getState().currentFoldout?.type,
        FoldoutType.Worktree
      )
      assert.equal(f.view().navigation, 'worktrees')
      await f.act({ type: 'navigation.close', target: 'branches' })
      assert.equal(f.view().navigation, 'worktrees')
      await f.act({ type: 'navigation.close', target: 'worktrees' })
      assert.equal(f.view().navigation, '')
      assert.equal(f.state().selectedSection, RepositorySectionTab.Changes)
      await menu.handleEvent('compare-to-branch')
      assert.equal(f.state().selectedSection, RepositorySectionTab.History)
      assert.equal(f.view().compare.showBranchList, true)
      await f.act({ type: 'compare.list', visible: false })
      assert.equal(f.view().compare.showBranchList, false)
    })

    it('projects real local/remote/recent branches and checks out original refs', async t => {
      const f = await fixture(t)
      await f.git(['branch', 'feature'])
      await f.git(['branch', 'remote-feature'])
      const remote = `${f.path}-branches.git`
      t.after(() =>
        rm(remote, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 50,
        })
      )
      await f.git(['clone', '--bare', f.path, remote])
      await f.git(['branch', '-D', 'remote-feature'])
      await f.git(['remote', 'add', 'origin', remote])
      await f.git(['fetch', 'origin'])
      await runtime.dispatcher.refreshRepository(f.repository)
      const branches = f.view().branches.flatMap(group => group.items)
      assert.ok(
        branches.some(
          b => b.ref === 'refs/remotes/origin/remote-feature' && b.remote
        )
      )
      await f.act({
        type: 'branch.checkout',
        ref: 'refs/remotes/origin/remote-feature',
      })
      assert.equal(await f.git(['branch', '--show-current']), 'remote-feature')
      assert.equal(
        await f.git(['config', 'branch.remote-feature.remote']),
        'origin'
      )
      await f.act({ type: 'branch.checkout', ref: 'refs/heads/feature' })
      assert.equal(await f.git(['branch', '--show-current']), 'feature')
      await f.act({ type: 'branch.checkout', ref: 'refs/heads/main' })
      assert.equal(await f.git(['branch', '--show-current']), 'main')
      assert.ok(
        f.state().branchesState.recentBranches.some(b => b.name === 'feature')
      )
      await assert.rejects(
        f.act({ type: 'branch.checkout', ref: 'refs/heads/not-loaded' }),
        /branch changed/
      )
    })

    it('checks out a projected already-fetched PR through the original branch flow', async t => {
      const f = await fixture(t)
      await f.git(['checkout', '-b', 'pr-topic'])
      await writeFile(join(f.path, 'file.txt'), 'pull request content\n')
      await f.git(['commit', '-am', 'pull request fixture'])
      const prSHA = await f.git(['rev-parse', 'HEAD'])
      const remote = `${f.path}-remote.git`
      t.after(() =>
        rm(remote, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 50,
        })
      )
      await f.git(['clone', '--bare', f.path, remote])
      await f.git(['checkout', 'main'])
      await f.git(['branch', '-D', 'pr-topic'])
      const forkRemote = forkPullRequestRemoteName('fixture')
      await f.git([
        'fetch',
        remote,
        `refs/heads/pr-topic:refs/remotes/${forkRemote}/pr-topic`,
      ])
      await runtime.dispatcher.refreshRepository(f.repository)
      const cloneURL = 'https://workspace.example.test/fixture/project.git'
      // Git's own repository-local URL rewrite keeps the complete original
      // remote/fetch path real without an account, secret or network dependency.
      await f.git([
        'config',
        `url.${remote.replace(/\\/g, '/')}.insteadOf`,
        cloneURL,
      ])
      const gh = await runtime.repositoriesStore.upsertGitHubRepository(
        'https://api.workspace.example.test',
        {
          clone_url: cloneURL,
          ssh_url: 'git@workspace.example.test:fixture/project.git',
          html_url: 'https://workspace.example.test/fixture/project',
          name: 'project',
          owner: {
            id: 901,
            html_url: 'https://workspace.example.test/fixture',
            login: 'fixture',
            avatar_url: '',
            type: 'User',
          },
          private: false,
          fork: false,
          default_branch: 'main',
          pushed_at: '2024-01-01T00:00:00Z',
          has_issues: false,
          archived: false,
          permissions: { pull: true, push: true, admin: false },
          parent: undefined,
        }
      )
      const repository = await runtime.repositoriesStore.setGitHubRepository(
        f.repository,
        gh,
        null
      )
      await runtime.dispatcher.selectRepository(repository)
      const baseSHA = await f.git(['rev-parse', 'HEAD'])
      const pr = new PullRequest(
        new Date(0),
        'Original PR fixture',
        42,
        new PullRequestRef('pr-topic', prSHA, gh),
        new PullRequestRef('main', baseSHA, gh),
        'fixture-author',
        false,
        ''
      )
      // The original immutable API model is test input. No Dispatcher/Git
      // behavior is stubbed and no callback is placed in the native DTO.
      runtime.repositoryStateManager.updateBranchesState(repository, () => ({
        openPullRequests: [pr],
      }))
      const projection = projectNativeWorkspace(runtime.appStore.getState())
      assert.equal(projection.pullRequestsAvailable, true)
      assert.equal(projection.pullRequests[0].number, 42)
      // Selecting a GitHub repository starts the original background fetch.
      // Its documented network-operation guard skips another in-flight fetch;
      // wait for that real state instead of racing PR checkout against startup.
      await until(
        () =>
          !runtime.repositoryStateManager.get(repository)
            .isPushPullFetchInProgress
      )
      const errors: string[] = []
      const errorSubscription = runtime.appStore.onDidError(error =>
        errors.push(error.message)
      )
      t.after(() => errorSubscription.dispose())
      await executeNativeWorkspaceCommand(runtime, repository, {
        type: 'pullRequest.checkout',
        number: 42,
      })
      assert.equal(
        await f.git(['rev-parse', 'HEAD']),
        prSHA,
        JSON.stringify({
          errors,
          remotes: await f.git(['remote', '-v']),
          refs: await f.git(['for-each-ref', '--format=%(refname)']),
          popup: runtime.appStore.getState().currentPopup?.type,
        })
      )
      assert.equal(
        await readFile(join(f.path, 'file.txt'), 'utf8'),
        'pull request content\n'
      )
      assert.ok((await f.git(['remote'])).includes(forkRemote))
    })

    it('switches real worktrees and retains locked/missing removal confirmation', async t => {
      const f = await fixture(t)
      const linked = `${f.path}-linked`
      t.after(() =>
        rm(linked, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 50,
        })
      )
      await f.git(['worktree', 'add', '-b', 'linked-branch', linked])
      await runtime.dispatcher.refreshRepository(f.repository)
      await f.act({ type: 'worktree.select', path: linked })
      assert.equal(
        runtime.appStore.getState().selectedState?.repository.path,
        linked
      )
      await f.act({ type: 'worktree.select', path: f.path })
      assert.equal(
        runtime.appStore.getState().selectedState?.repository.path,
        f.path
      )
      await f.git(['worktree', 'lock', linked])
      await runtime.dispatcher.refreshRepository(f.repository)
      const lockedMenu = f
        .view()
        .menus.find(
          menu => menu.target.kind === 'worktree' && menu.target.id === linked
        )!
      assert.equal(
        lockedMenu.items.find(item => item.labelKey === 'workspace.delete')
          ?.enabled,
        false
      )
      await f.git(['worktree', 'unlock', linked])
      await rm(linked, { recursive: true, force: true })
      await runtime.dispatcher.refreshRepository(f.repository)
      assert.equal(
        f.view().worktrees.find(tree => tree.path === linked)?.missing,
        true
      )
      await f.act({ type: 'worktree.select', path: linked })
      const popup = runtime.appStore.getState().currentPopup
      assert.ok(
        popup?.type === PopupType.DeleteWorktree && popup.id !== undefined
      )
      assert.match(
        await f.git(['worktree', 'list', '--porcelain']),
        /linked-branch/
      )
      await closeKernelPopup(runtime, popup.id)
      assert.match(
        await f.git(['worktree', 'list', '--porcelain']),
        /linked-branch/
      )
    })

    it('retains original history comparison and computes contiguous multi-selection from displayed order', async t => {
      const f = await fixture(t)
      await f.git(['branch', 'older'])
      for (let i = 0; i < 3; i++) {
        await writeFile(join(f.path, 'file.txt'), `version ${i}\n`)
        await f.git(['commit', '-am', `version ${i}`])
      }
      await runtime.dispatcher.refreshRepository(f.repository)
      await runtime.dispatcher.changeRepositorySection(
        f.repository,
        RepositorySectionTab.History
      )
      await runtime.dispatcher.loadNextCommitBatch(f.repository)
      const shas = f.state().compareState.commitSHAs
      assert.ok(shas.length >= 4)
      await f.act({ type: 'history.select', shas: [shas[0], shas[2]] })
      assert.equal(f.state().commitSelection.isContiguous, false)
      await f.act({ type: 'history.select', shas: [shas[2], shas[0], shas[1]] })
      assert.deepEqual(f.state().commitSelection.shas, shas.slice(0, 3))
      assert.equal(f.state().commitSelection.isContiguous, true)
      await f.act({ type: 'history.reorder', shas: [shas[0]], beforeSHA: null })
      assert.equal(await f.git(['rev-parse', 'HEAD']), shas[0])
      assert.equal(f.state().multiCommitOperationState, null)
      await f.act({
        type: 'compare.branch',
        ref: 'refs/heads/older',
        mode: 'ahead',
      })
      assert.equal(f.view().compare.mode, 'ahead')
      assert.equal(f.view().compare.ref, 'refs/heads/older')
      await f.act({ type: 'compare.operation', operation: 'rebase' })
      assert.equal(f.view().compare.previewStatus, 'clean')
      assert.equal(f.view().compare.rebaseCommits, 3)
      await f.act({ type: 'compare.history' })
      assert.equal(f.view().compare.mode, 'history')
    })

    it('reads history row statistics without changing the selected commit', async t => {
      const f = await fixture(t)
      const older = await f.git(['rev-parse', 'HEAD'])
      await f.git(['mv', 'file.txt', 'renamed.txt'])
      await writeFile(join(f.path, 'binary.bin'), Buffer.from([0, 1, 2]))
      await f.git(['add', '.'])
      await f.git(['commit', '-m', 'Rename and add binary'])
      const renamed = await f.git(['rev-parse', 'HEAD'])
      await f.git(['commit', '--allow-empty', '-m', 'Empty change'])
      const empty = await f.git(['rev-parse', 'HEAD'])
      await runtime.dispatcher.refreshRepository(f.repository)
      await runtime.dispatcher.changeRepositorySection(
        f.repository,
        RepositorySectionTab.History
      )
      await runtime.dispatcher.loadNextCommitBatch(f.repository)
      await f.act({ type: 'history.select', shas: [older] })
      const before = f.state().commitSelection
      const presenter = new DesktopPresenter(runtime)
      t.after(() => presenter.dispose())
      const result = await executeKernelCommand(
        presenter,
        parseKernelCommand({
          name: 'history.summaries',
          repository: { id: f.repository.id, path: f.path },
          shas: [older, renamed, empty],
        })
      )
      assert.deepEqual(
        'historySummaries' in result ? result.historySummaries : undefined,
        [
          { sha: older, fileCount: 1, linesAdded: 1, linesDeleted: 0 },
          { sha: renamed, fileCount: 2, linesAdded: 0, linesDeleted: 0 },
          { sha: empty, fileCount: 0, linesAdded: 0, linesDeleted: 0 },
        ]
      )
      assert.equal(f.state().commitSelection, before)
    })

    it('copies the complete selected non-HEAD SHA through the original menu and rejects a stale lease', async t => {
      const f = await fixture(t)
      const older = await f.git(['rev-parse', 'HEAD'])
      await f.git(['commit', '--allow-empty', '-m', 'Newer version'])
      const head = await f.git(['rev-parse', 'HEAD'])
      await runtime.dispatcher.refreshRepository(f.repository)
      await runtime.dispatcher.changeRepositorySection(
        f.repository,
        RepositorySectionTab.History
      )
      await runtime.dispatcher.loadNextCommitBatch(f.repository)
      await f.act({ type: 'history.select', shas: [older] })
      const menu = f
        .view()
        .menus.find(m => m.target.kind === 'commit' && m.target.id === older)!
      assert.ok(menu)
      const index = menu.items.findIndex(
        item => item.labelKey === 'workspace.copySHA'
      )
      assert.ok(index >= 0)
      const clipboard = captureClipboardWrites()
      t.after(clipboard.restore)
      const action = {
        type: 'menu' as const,
        target: menu.target,
        lease: menu.lease,
        path: [index],
      }
      await f.act(action)
      assert.equal(older.length, 40)
      assert.deepEqual(clipboard.writes, [older])
      assert.equal(await f.git(['rev-parse', 'HEAD']), head)
      await f.act({ type: 'history.select', shas: [head] })
      await assert.rejects(f.act(action))
      assert.deepEqual(clipboard.writes, [older])

      // The always-visible row copy action remains available while comparing
      // several commits and must not collapse or retarget that selection.
      await f.act({ type: 'history.select', shas: [head, older] })
      const selection = f.state().commitSelection
      for (const sha of [head, older]) {
        const rowMenu = f
          .view()
          .menus.find(m => m.target.kind === 'commit' && m.target.id === sha)!
        const copyIndex = rowMenu.items.findIndex(
          item => item.labelKey === 'workspace.copySHA'
        )
        assert.ok(copyIndex >= 0, 'multi-selection hides the row copy action')
        await f.act({
          type: 'menu',
          target: rowMenu.target,
          lease: rowMenu.lease,
          path: [copyIndex],
        })
        assert.equal(f.state().commitSelection, selection)
      }
      assert.deepEqual(clipboard.writes, [older, head, older])
    })

    it('uses original stash creation/load/file-selection/confirmation/cancellation and restore', async t => {
      const f = await fixture(t)
      await writeFile(join(f.path, 'file.txt'), 'stashed version\n')
      await runtime.dispatcher.refreshRepository(f.repository)
      await f.act({ type: 'stash.create' })
      const stash = f.state().changesState.stashEntry
      assert.ok(stash !== null)
      assert.equal(await readFile(join(f.path, 'file.txt'), 'utf8'), 'base\n')
      await f.act({ type: 'stash.view' })
      await until(() => f.view().stash?.loadState === 'Loaded')
      const stashView = f.view().stash!
      assert.equal(stashView.selected, true)
      assert.equal(stashView.loadState, 'Loaded')
      assert.equal(stashView.files.length, 1)
      await f.act({
        type: 'stash.file',
        sha: stash.stashSha,
        fileID: stashView.files[0].id,
      })
      assert.equal(f.view().stash?.fileID, stashView.files[0].id)
      await assert.rejects(
        f.act({ type: 'stash.discard', sha: 'stale' }),
        /stash changed/
      )
      await f.act({ type: 'stash.discard', sha: stash.stashSha })
      const popup = runtime.appStore.getState().currentPopup
      assert.ok(
        popup?.type === PopupType.ConfirmDiscardStash && popup.id !== undefined
      )
      await closeKernelPopup(runtime, popup.id)
      assert.equal(f.state().changesState.stashEntry?.stashSha, stash.stashSha)
      await f.act({ type: 'stash.restore', sha: stash.stashSha })
      assert.equal(
        await readFile(join(f.path, 'file.txt'), 'utf8'),
        'stashed version\n'
      )
      assert.equal(f.state().changesState.stashEntry, null)
    })

    it('uses shared fuzzy/status/inclusion filters and leases original file-menu actions', async t => {
      const f = await fixture(t)
      await mkdir(join(f.path, 'source'), { recursive: true })
      await writeFile(join(f.path, 'source', 'native-controller.ts'), 'new\n')
      await writeFile(join(f.path, 'other.txt'), 'other\n')
      await runtime.dispatcher.refreshRepository(f.repository)
      await f.act({
        type: 'files.filter',
        filter: {
          filterText: 'snc',
          isIncludedInCommit: false,
          isExcludedFromCommit: false,
          isNewFile: true,
          isModifiedFile: false,
          isDeletedFile: false,
        },
      })
      assert.deepEqual(
        visibleWorkingDirectoryFiles(f.state(), true).map(file => file.path),
        ['source/native-controller.ts']
      )
      assert.equal(visibleWorkingDirectoryFiles(f.state(), false).length, 2)
      const file = f
        .state()
        .changesState.workingDirectory.files.find(f => f.path === 'other.txt')!
      await runtime.dispatcher.selectWorkingDirectoryFiles(f.repository, [file])
      // Menus derive from state regardless of whether a hidden file is selected.
      await f.act({
        type: 'files.filter',
        filter: { ...f.state().changesState.fileListFilter, filterText: '' },
      })
      const menu = f
        .view()
        .menus.find(m => m.target.kind === 'file' && m.target.id === file.id)!
      assert.ok(menu)
      const ignore = menu.items.findIndex(
        item => item.labelKey === 'workspace.ignoreFile'
      )
      await f.act({
        type: 'menu',
        target: menu.target,
        lease: menu.lease,
        path: [ignore],
      })
      assert.match(
        await readFile(join(f.path, '.gitignore'), 'utf8'),
        /other\.txt/
      )
      await runtime.dispatcher.refreshRepository(f.repository)
      await assert.rejects(
        f.act({
          type: 'menu',
          target: menu.target,
          lease: menu.lease,
          path: [ignore],
        }),
        /changed/
      )
    })

    it('resolves real original line IDs and opens the exact discard-selection confirmation', async t => {
      const f = await fixture(t)
      const originalLines = Array.from({ length: 40 }, (_, i) => `context-${i}`)
      await writeFile(join(f.path, 'file.txt'), originalLines.join('\n') + '\n')
      await f.git(['commit', '-am', 'long file'])
      const editedLines = [...originalLines]
      editedLines[20] = 'edited'
      const edited = editedLines.join('\n') + '\n'
      await writeFile(join(f.path, 'file.txt'), edited)
      await runtime.dispatcher.refreshRepository(f.repository)
      const file = f.state().changesState.workingDirectory.files[0]
      await runtime.dispatcher.selectWorkingDirectoryFiles(f.repository, [file])
      await until(() => projectDiffSelection(f.state(), true) !== null)
      const diff = projectDiffSelection(f.state(), true)
      assert.ok(diff !== null && diff.lines.length > 0)
      const textDiff = f.view().textDiff!
      await f.act({
        type: 'diff.prepare',
        fileID: textDiff.fileID,
        identity: textDiff.identity,
      })
      assert.equal(f.view().textDiff?.canExpand, true)
      await f.act({
        type: 'diff.expand',
        fileID: textDiff.fileID,
        identity: textDiff.identity,
      })
      const expanded = f.view().textDiff!
      assert.match(expanded.expandedText!, /context-0/)
      assert.match(expanded.expandedText!, /context-39/)
      assert.deepEqual(
        expanded.selection?.lines.map(line => line.id),
        diff.lines.map(line => line.id)
      )
      assert.equal(expanded.selection?.identity, diff.identity)
      assert.equal(
        projectDiffSelection(f.state(), true)?.identity,
        diff.identity
      )
      await f.act({
        type: 'diff.collapse',
        fileID: textDiff.fileID,
        identity: textDiff.identity,
      })
      assert.equal(f.view().textDiff?.expandedText, null)
      await assert.rejects(
        f.act({
          type: 'diff.discard',
          fileID: file.id,
          identity: 'stale',
          lineIDs: [diff.lines[0].id],
        }),
        /diff changed/
      )
      await f.act({
        type: 'diff.discard',
        fileID: file.id,
        identity: diff.identity,
        lineIDs: diff.lines.map(line => line.id),
      })
      const popup = runtime.appStore.getState().currentPopup
      assert.ok(
        popup?.type === PopupType.ConfirmDiscardSelection &&
          popup.id !== undefined
      )
      assert.equal(
        popup.file,
        f
          .state()
          .changesState.workingDirectory.files.find(
            current => current.id === file.id
          )
      )
      const selection = f.state().changesState.selection
      assert.ok(selection.kind === ChangesSelectionKind.WorkingDirectory)
      assert.equal(popup.diff, selection.diff)
      await closeKernelPopup(runtime, popup.id)
      assert.equal(await readFile(join(f.path, 'file.txt'), 'utf8'), edited)
    })

    for (const operation of ['merge', 'rebase', 'cherry-pick'] as const) {
      it(`recovers an externally started ${operation} without relying on a banner`, async t => {
        const f = await fixture(t)
        await f.git(['checkout', '-b', 'conflict-topic'])
        await writeFile(join(f.path, 'file.txt'), 'topic\n')
        await f.git(['commit', '-am', 'topic change'])
        const topicSHA = await f.git(['rev-parse', 'HEAD'])
        await f.git(['checkout', 'main'])
        await writeFile(join(f.path, 'file.txt'), 'main\n')
        await f.git(['commit', '-am', 'main change'])
        const originalSHA = await f.git(['rev-parse', 'HEAD'])
        await f.git(
          [
            operation,
            operation === 'cherry-pick' ? topicSHA : 'conflict-topic',
          ],
          1
        )
        await runtime.dispatcher.refreshRepository(f.repository)
        await until(
          () =>
            f.state().multiCommitOperationState !== null &&
            f.view().conflict?.canAbort === true
        )
        runtime.dispatcher.closePopup(PopupType.MultiCommitOperation)
        runtime.dispatcher.clearBanner()
        const recovery = f.view().conflict!
        assert.equal(recovery.remaining, 1)
        assert.equal(recovery.canContinue, false)
        await f.act({ type: 'conflicts.open', identity: recovery.identity })
        assert.equal(
          runtime.appStore.getState().currentPopup?.type,
          PopupType.MultiCommitOperation
        )
        await assert.rejects(
          f.act({ type: 'conflicts.continue', identity: recovery.identity }),
          /Resolve all conflicts/
        )
        assert.match(
          await readFile(join(f.path, 'file.txt'), 'utf8'),
          /<<<<<<< HEAD/
        )
        if (operation === 'rebase') {
          await writeFile(join(f.path, 'file.txt'), 'resolved\n')
          await runtime.dispatcher.refreshRepository(f.repository)
          assert.equal(f.view().conflict?.canContinue, true)
          await f.act({
            type: 'conflicts.continue',
            identity: f.view().conflict!.identity,
          })
          await until(() => f.state().changesState.conflictState === null)
          assert.equal(await f.git(['branch', '--show-current']), 'main')
          assert.notEqual(await f.git(['rev-parse', 'HEAD']), originalSHA)
          assert.equal(
            await readFile(join(f.path, 'file.txt'), 'utf8'),
            'resolved\n'
          )
        } else {
          await f.act({
            type: 'conflicts.abort',
            identity: f.view().conflict!.identity,
          })
          assert.equal(f.state().changesState.conflictState, null)
          assert.equal(await f.git(['rev-parse', 'HEAD']), originalSHA)
        }
      })
    }

    it('projects all 15 banner variants safely and rejects replaced callback leases', async t => {
      const f = await fixture(t)
      const withAccount = projectNativeWorkspace({
        ...runtime.appStore.getState(),
        accounts: [
          new Account(
            'fixture-account',
            'https://api.example.invalid',
            'WORKSPACE-PRIVATE-TOKEN-SENTINEL',
            [],
            '',
            91,
            'Fixture',
            'free'
          ),
        ],
      })
      assert.equal(
        JSON.stringify(withAccount).includes(
          'WORKSPACE-PRIVATE-TOKEN-SENTINEL'
        ),
        false
      )
      assert.equal(Object.hasOwn(withAccount, 'accounts'), false)
      let called = 0
      const callback = () => {
        called++
        assert.equal(runtime.appStore.getState().currentBanner, null)
      }
      const banners: Banner[] = [
        {
          type: BannerType.SuccessfulMerge,
          ourBranch: 'main',
          theirBranch: 'topic',
        },
        {
          type: BannerType.MergeConflictsFound,
          ourBranch: 'main',
          popup: { type: PopupType.About },
        },
        { type: BannerType.SuccessfulRebase, targetBranch: 'main' },
        {
          type: BannerType.RebaseConflictsFound,
          targetBranch: 'main',
          onOpenDialog: callback,
        },
        { type: BannerType.BranchAlreadyUpToDate, ourBranch: 'main' },
        {
          type: BannerType.SuccessfulCherryPick,
          targetBranchName: 'main',
          count: 2,
          onUndo: callback,
        },
        {
          type: BannerType.CherryPickConflictsFound,
          targetBranchName: 'main',
          onOpenConflictsDialog: callback,
        },
        {
          type: BannerType.CherryPickUndone,
          targetBranchName: 'main',
          countCherryPicked: 2,
        },
        { type: BannerType.SquashUndone, commitsCount: 2 },
        { type: BannerType.ReorderUndone, commitsCount: 2 },
        {
          type: BannerType.OpenThankYouCard,
          emoji: new Map(),
          onOpenCard: () => {
            called++
          },
          onThrowCardAway: callback,
        },
        { type: BannerType.SuccessfulSquash, count: 2, onUndo: callback },
        { type: BannerType.SuccessfulReorder, count: 2, onUndo: callback },
        {
          type: BannerType.ConflictsFound,
          operationDescription: 'DESCRIPTION-NOT-SERIALIZED',
          onOpenConflictsDialog: callback,
        },
        { type: BannerType.OSVersionNoLongerSupported },
      ]
      for (const banner of banners) {
        runtime.dispatcher.setBanner(banner)
        const projected = f.view().banner!
        assert.equal(projected.type, banner.type)
        assert.equal(JSON.stringify(projected).includes('onUndo'), false)
        assert.equal(
          JSON.stringify(projected).includes('DESCRIPTION-NOT-SERIALIZED'),
          false
        )
        if (projected.actions.includes('undo')) {
          await f.act({ type: 'banner', id: projected.id, action: 'undo' })
        } else if (!projected.dismissable) {
          await assert.rejects(
            f.act({ type: 'banner', id: projected.id, action: 'dismiss' }),
            /conflict dialog/
          )
          await f.act({ type: 'banner', id: projected.id, action: 'open' })
          while (runtime.appStore.getState().currentPopup !== null) {
            runtime.dispatcher.closePopupById(
              runtime.appStore.getState().currentPopup!.id!
            )
          }
        }
      }
      assert.equal(called, 6)
      runtime.dispatcher.setBanner({
        type: BannerType.SuccessfulSquash,
        count: 1,
        onUndo: callback,
      })
      const old = f.view().banner!
      runtime.dispatcher.setBanner({
        type: BannerType.SuccessfulSquash,
        count: 1,
        onUndo: callback,
      })
      await assert.rejects(
        f.act({ type: 'banner', id: old.id, action: 'undo' }),
        /replaced/
      )
      assert.equal(f.state().branchesState.tip.kind, TipState.Valid)
    })
  }
)

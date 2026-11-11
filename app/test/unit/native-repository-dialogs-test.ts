import assert from 'node:assert/strict'
import { after, before, beforeEach, describe, it } from 'node:test'
import { EventEmitter } from 'events'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { join, resolve, normalize } from 'path'
import { exec } from 'dugite'
import { ipcRenderer } from 'electron'
import {
  repositoryDialogs,
  mountRepositoryDialog,
} from '../../src/native-kernel/repository-dialogs'
import type { AppRuntime } from '../../src/ui/app-runtime'
import { Popup, PopupDetail, PopupType } from '../../src/models/popup'
import { Branch, BranchType } from '../../src/models/branch'
import {
  Repository,
  isRepositoryWithForkedGitHubRepository,
} from '../../src/models/repository'
import { Account } from '../../src/models/account'
import { Owner } from '../../src/models/owner'
import { GitHubRepository } from '../../src/models/github-repository'
import { listWorktrees } from '../../src/lib/git'
import {
  validateRepositoryClonePath,
  resolveRepositoryCloneInfo,
} from '../../src/ui/lib/repository-clone'
import { CloneRepositoryTab } from '../../src/models/clone-repository-tab'
import { DesktopPresenter } from '../../src/native-kernel/desktop-presenter'
import { SelectionType } from '../../src/lib/app-state'
import { projectKernelState } from '../../src/native-kernel/state-projection'
import { repositoryFullPath } from '../../src/ui/lib/repository-path-policy'
import { getDefaultDir, setDefaultDir } from '../../src/ui/lib/default-dir'
import { AppMenuController } from '../../src/ui/app-menu-controller'
import {
  executeRepositoryMenu,
  projectRepositoryMenus,
} from '../../src/native-kernel/repository-menu'

// Complete only Electron IPC. The real AppStore/Dispatcher/DB/Git and extracted
// upstream controllers perform every local operation against this fixture root.
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
let fixture: string
let runtime: AppRuntime
const previousGlobal = process.env.GIT_CONFIG_GLOBAL
const previousCeiling = process.env.GIT_CEILING_DIRECTORIES
const previousNoSystem = process.env.GIT_CONFIG_NOSYSTEM
before(async () => {
  const scratch = resolve('_temp/native-full-ui')
  await mkdir(scratch, { recursive: true })
  fixture = await mkdtemp(join(scratch, 'repository-dialogs-'))
  const config = join(fixture, 'global-config')
  process.env.GIT_CONFIG_GLOBAL = config
  process.env.GIT_CONFIG_NOSYSTEM = '1'
  // Stop Git's ancestor discovery before it can see the user's working repo.
  process.env.GIT_CEILING_DIRECTORIES = fixture
  await writeFile(
    config,
    '[user]\n name = Fixture Author\n email = fixture@example.invalid\n[init]\n defaultBranch = main\n'
  )
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  localStorage.setItem('last-clone-location', fixture)
  const { getAppRuntime } = await import('../../src/ui/app-runtime')
  runtime = getAppRuntime()
})
beforeEach(() => {
  for (const popup of runtime.appStore.getState().allPopups) {
    if (popup.id !== undefined) {
      runtime.dispatcher.closePopupById(popup.id)
    }
  }
})
after(async () => {
  // Stop new selection notifications and wait for real background refreshes.
  // A fixed delay can delete a fixture while its history diff still uses it.
  runtime.repositoriesStore['emitter'].dispose()
  await runtime.appStore._selectRepository(null)
  await Promise.all(
    [...runtime.appStore['repositoryRefreshes'].values()].flatMap(pending => [
      ...pending,
    ])
  )
  window.close()
  if (previousGlobal === undefined) {
    delete process.env.GIT_CONFIG_GLOBAL
  } else {
    process.env.GIT_CONFIG_GLOBAL = previousGlobal
  }
  if (previousNoSystem === undefined) {
    delete process.env.GIT_CONFIG_NOSYSTEM
  } else {
    process.env.GIT_CONFIG_NOSYSTEM = previousNoSystem
  }
  if (previousCeiling === undefined) {
    delete process.env.GIT_CEILING_DIRECTORIES
  } else {
    process.env.GIT_CEILING_DIRECTORIES = previousCeiling
  }
  await rm(fixture, {
    recursive: true,
    force: true,
    maxRetries: 10,
    retryDelay: 100,
  })
})
async function git(args: string[], path: string) {
  assert.ok(
    normalize(path).startsWith(normalize(fixture)),
    'Git is confined to temporary fixtures'
  )
  const result = await exec(args, path)
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout.trim()
}
async function show(detail: PopupDetail): Promise<Popup> {
  await runtime.dispatcher.showPopup(detail)
  const popup = runtime.appStore.getState().currentPopup
  assert.ok(popup && popup.type === detail.type)
  return popup
}
async function confirm(
  detail: PopupDetail,
  values: Record<string, unknown> = {}
) {
  const popup = await show(detail)
  await repositoryDialogs.reply(runtime, popup, 'confirm', values)
  return popup
}
async function init(name: string) {
  const path = join(fixture, name)
  await mkdir(path)
  await git(['init', '-b', 'main'], path)
  await writeFile(join(path, 'existing.txt'), 'existing fixture\n')
  await git(['add', '.'], path)
  await git(['commit', '-m', 'fixture initial'], path)
  const [repository] = await runtime.dispatcher.addRepositories([path])
  assert.ok(repository)
  await runtime.dispatcher.selectRepository(repository)
  return repository
}
function selectedRepository() {
  const selected = runtime.appStore.getState().selectedState
  assert.ok(selected && 'state' in selected)
  return selected.repository as Repository
}

async function waitForRepositories(check: () => boolean) {
  if (check()) return
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      subscription.dispose()
      reject(new Error('Repository update did not reach AppStore'))
    }, 5000)
    const subscription = runtime.appStore.onDidUpdate(() => {
      if (check()) {
        clearTimeout(timer)
        subscription.dispose()
        resolve()
      }
    })
  })
}

describe('native repository dialogs with the original kernel', () => {
  it('preserves the original checked account no-op and routes another account to the clicked project', async t => {
    const endpoint = 'https://api.example.invalid'
    const accounts = ['current', 'other'].map(
      (login, i) => new Account(login, endpoint, '', [], '', i + 1, login)
    )
    const repository = new Repository(
      join(fixture, 'account-menu'),
      912,
      new GitHubRepository('menu', new Owner('owner', endpoint, 1), 912),
      false,
      null,
      {},
      false,
      undefined,
      undefined,
      'current'
    )
    const state = {
      ...runtime.appStore.getState(),
      repositories: [repository],
      accounts,
    }
    const controller = new AppMenuController(runtime.dispatcher, () => state, {
      perform() {},
      showTestUI() {},
    })
    const update = t.mock.method(
      runtime.dispatcher,
      'updateRepositoryAccount',
      async () => {}
    )
    const menu = projectRepositoryMenus(state)[0]
    const accountPath = menu.items.findIndex(
      i => i.labelKey === 'workspace.repositoryAccount'
    )
    await executeRepositoryMenu(
      state,
      controller,
      menu.repository,
      menu.lease,
      [accountPath, 0]
    )
    assert.equal(update.mock.callCount(), 0)
    await executeRepositoryMenu(
      state,
      controller,
      menu.repository,
      menu.lease,
      [accountPath, 1]
    )
    assert.equal(update.mock.callCount(), 1)
    assert.deepEqual(update.mock.calls[0].arguments, [repository, 'other'])
    await assert.rejects(
      executeRepositoryMenu(state, controller, menu.repository, menu.lease, [
        accountPath,
      ]),
      /not an action/
    )
    const signedOut = { ...state, accounts: [accounts[1]] }
    const newMenu = projectRepositoryMenus(signedOut)[0]
    await assert.rejects(
      executeRepositoryMenu(
        signedOut,
        controller,
        newMenu.repository,
        newMenu.lease,
        [accountPath, 0]
      ),
      /not available/
    )
    await assert.rejects(
      executeRepositoryMenu(
        signedOut,
        controller,
        menu.repository,
        menu.lease,
        [accountPath, 1]
      ),
      /changed/
    )
  })

  it('targets an unselected repository menu without changing selection and rejects stale actions', async () => {
    const a = await init('menu-selected')
    const b = await init('menu-target')
    await git(
      ['remote', 'add', 'origin', 'https://example.invalid/menu-target.git'],
      b.path
    )
    await runtime.dispatcher.selectRepository(a)
    const presenter = new DesktopPresenter(runtime)
    try {
      const menu = presenter
        .getRepositoryMenus()
        .find(m => m.repository.id === b.id)!
      assert.ok(menu, 'every listed project has its original context menu')
      assert.equal(selectedRepository().id, a.id)
      const settings = menu.items.findIndex(
        i => i.labelKey === 'workspace.repositorySettings'
      )
      await presenter.executeRepositoryMenu(menu.repository, menu.lease, [
        settings,
      ])
      const popup = runtime.appStore.getState().currentPopup
      assert.ok(popup?.type === PopupType.RepositorySettings)
      assert.equal(popup.repository.path, b.path)
      assert.equal(selectedRepository().id, a.id)
      assert.equal(
        runtime.repositoryStateManager.get(b).remote?.url,
        'https://example.invalid/menu-target.git'
      )
      if (popup.id !== undefined) runtime.dispatcher.closePopupById(popup.id)
      await runtime.dispatcher.changeRepositoryAlias(b, 'Renamed target')
      await waitForRepositories(() =>
        runtime.appStore
          .getState()
          .repositories.some(
            r =>
              r.id === b.id &&
              r instanceof Repository &&
              r.alias === 'Renamed target'
          )
      )
      const alias = menu.items.findIndex(
        i => i.labelKey === 'workspace.createAlias'
      )
      await assert.rejects(
        presenter.executeRepositoryMenu(menu.repository, menu.lease, [alias]),
        /changed/
      )
      const updated = presenter
        .getRepositoryMenus()
        .find(m => m.repository.id === b.id)!
      await presenter.executeRepositoryMenu(updated.repository, updated.lease, [
        updated.items.findIndex(i => i.labelKey === 'workspace.changeAlias'),
      ])
      const aliasPopup = runtime.appStore.getState().currentPopup
      assert.ok(aliasPopup?.type === PopupType.ChangeRepositoryAlias)
      assert.equal(aliasPopup.repository.path, b.path)
      assert.equal(selectedRepository().id, a.id)
      if (aliasPopup.id !== undefined)
        runtime.dispatcher.closePopupById(aliasPopup.id)
      await runtime.dispatcher.removeRepository(b, false)
      await waitForRepositories(
        () =>
          !runtime.appStore
            .getState()
            .repositories.some(r => r.id === b.id && r.path === b.path)
      )
      await assert.rejects(
        presenter.executeRepositoryMenu(updated.repository, updated.lease, [0]),
        /listed/
      )
      assert.equal(selectedRepository().id, a.id)
    } finally {
      presenter.dispose()
    }
  })

  it('creates README, attributes, description and initial commit through the shared upstream controller, then adds an existing repository', async () => {
    await confirm(
      { type: PopupType.CreateRepository },
      {
        name: 'created-repo',
        path: fixture,
        description: 'Fixture description 中英',
        readme: true,
        gitIgnore: 'None',
        license: 'None',
      }
    )
    const repository = selectedRepository()
    assert.equal(repository.path, join(fixture, 'created-repo'))
    assert.match(
      await readFile(join(repository.path, 'README.md'), 'utf8'),
      /created-repo/
    )
    assert.match(
      await readFile(join(repository.path, '.gitattributes'), 'utf8'),
      /auto/
    )
    assert.equal(
      await readFile(join(repository.path, '.git', 'description'), 'utf8').then(
        s => s.trim()
      ),
      'Fixture description 中英'
    )
    assert.equal(
      await git(['log', '-1', '--format=%s'], repository.path),
      'Initial commit'
    )
    assert.equal(await git(['status', '--porcelain'], repository.path), '')
    await assert.rejects(
      confirm(
        { type: PopupType.CreateRepository, path: repository.path },
        {
          name: 'created-repo',
          path: fixture,
        }
      ),
      /already contains/
    )
    runtime.dispatcher.closePopup()
    await confirm({
      type: PopupType.AddRepository,
      path: `"${repository.path}"`,
    })
    assert.equal(selectedRepository().id, repository.id)
    assert.equal(runtime.appStore.getState().currentPopup, null)
  })

  it('preserves non-Git/bare validation and create-here/add-instead continuations without touching another path', async () => {
    const missing = join(fixture, 'empty-folder')
    await mkdir(missing)
    const popup = await show({ type: PopupType.AddRepository, path: missing })
    await assert.rejects(
      repositoryDialogs.reply(runtime, popup, 'confirm', {}),
      /does not appear/
    )
    assert.equal(runtime.appStore.getState().currentPopup?.id, popup.id)
    await repositoryDialogs.reply(runtime, popup, 'createHere', {})
    assert.equal(
      runtime.appStore.getState().currentPopup?.type,
      PopupType.CreateRepository
    )
    assert.equal(
      (
        runtime.appStore.getState().currentPopup as Extract<
          Popup,
          { type: PopupType.CreateRepository }
        >
      ).path,
      missing
    )
    runtime.dispatcher.closePopup()
    const bare = join(fixture, 'bare.git')
    await mkdir(bare)
    await git(['init', '--bare'], bare)
    await assert.rejects(
      confirm({ type: PopupType.AddRepository, path: bare }),
      /Bare repositories/
    )
    runtime.dispatcher.closePopup()
    await confirm({
      type: PopupType.AddRepository,
      path: join(fixture, 'created-repo'),
    })
  })

  it('renames, creates and deletes branches/tags using original objects, targets and compare reset', async () => {
    const repository = await init('branches')
    const sha = await git(['rev-parse', 'HEAD'], repository.path)
    await confirm(
      {
        type: PopupType.CreateBranch,
        repository,
        targetCommit: { sha, summary: 'Existing commit' },
      },
      { name: 'feature new' }
    )
    assert.equal(
      await git(['branch', '--show-current'], repository.path),
      'feature-new'
    )
    await runtime.dispatcher.refreshRepository(repository)
    const branch = runtime.repositoryStateManager
      .get(repository)
      .branchesState.allBranches.find(b => b.name === 'feature-new')!
    assert.ok(branch)
    await confirm(
      { type: PopupType.RenameBranch, repository, branch },
      { name: 'feature-renamed' }
    )
    assert.equal(
      await git(['branch', '--show-current'], repository.path),
      'feature-renamed'
    )
    await confirm(
      {
        type: PopupType.CreateTag,
        repository,
        targetCommitSha: sha,
        localTags: new Map(),
      },
      { name: 'v1.0' }
    )
    assert.equal(await git(['rev-parse', 'v1.0^{}'], repository.path), sha)
    const duplicate = await show({
      type: PopupType.CreateTag,
      repository,
      targetCommitSha: sha,
      localTags: new Map([['v1.0', sha]]),
    })
    await assert.rejects(
      repositoryDialogs.reply(runtime, duplicate, 'confirm', { name: 'v1.0' }),
      /already exists/
    )
    await assert.rejects(
      repositoryDialogs.reply(runtime, duplicate, 'confirm', {
        name: 'x'.repeat(246),
      }),
      /245/
    )
    await repositoryDialogs.cancel(runtime, duplicate)
    await confirm({ type: PopupType.DeleteTag, repository, tagName: 'v1.0' })
    assert.equal(await git(['tag', '--list'], repository.path), '')
    const renamed = new Branch(
      'feature-renamed',
      null,
      { sha },
      BranchType.Local,
      'refs/heads/feature-renamed'
    )
    await confirm({
      type: PopupType.DeleteBranch,
      repository,
      branch: renamed,
      existsOnRemote: false,
    })
    assert.equal(
      await git(['branch', '--show-current'], repository.path),
      'main'
    )
    assert.equal(
      runtime.repositoryStateManager.get(repository).compareState.formState
        .kind,
      'History'
    )
    assert.equal(
      await git(['branch', '--list', 'feature-renamed'], repository.path),
      ''
    )
    await confirm(
      { type: PopupType.ChangeRepositoryAlias, repository },
      { alias: 'Long alias 中文 '.repeat(80) }
    )
    assert.equal(
      (await runtime.repositoriesStore.getAll()).find(
        r => r.id === repository.id
      )?.alias,
      'Long alias 中文 '.repeat(80)
    )
  })

  it('deletes a remote-only branch against an isolated local bare remote, never a network account', async () => {
    const repository = await init('remote-branch')
    const bare = join(fixture, 'remote-branch.git')
    await mkdir(bare)
    await git(['init', '--bare'], bare)
    await git(['remote', 'add', 'origin', bare], repository.path)
    await git(['push', 'origin', 'main:remote-only'], repository.path)
    await git(['fetch', 'origin'], repository.path)
    await runtime.dispatcher.refreshRepository(repository)
    const branch = runtime.repositoryStateManager
      .get(repository)
      .branchesState.allBranches.find(b => b.name === 'origin/remote-only')
    assert.ok(branch?.type === BranchType.Remote)
    await confirm({ type: PopupType.DeleteRemoteBranch, repository, branch })
    assert.equal(await git(['branch', '--list', 'remote-only'], bare), '')
    assert.equal(
      runtime.repositoryStateManager.get(repository).compareState.formState
        .kind,
      'History'
    )
  })

  it('adds and renames a worktree via real UI continuations and keeps a locked delete recoverable', async () => {
    const repository = await init('worktrees')
    await confirm(
      {
        type: PopupType.AddWorktree,
        repository,
        initialWorktreeName: 'linked',
        initialBranchName: 'linked-branch',
      },
      {
        name: 'linked',
        path: fixture,
        branch: 'linked-branch',
      }
    )
    const linkedRepository = selectedRepository()
    assert.equal(linkedRepository.path, join(fixture, 'linked'))
    const main = (await listWorktrees(linkedRepository)).find(
      w => w.type === 'main'
    )!
    await runtime.dispatcher.switchWorktree(linkedRepository, main)
    const mainRepository = selectedRepository()
    const renamePopup = await show({
      type: PopupType.RenameWorktree,
      repository: mainRepository,
      worktreePath: linkedRepository.path,
    })
    const path = join(fixture, 'linked-renamed')
    let renamedWorktree
    // Windows can reject a move while a just-switched worktree's background
    // reader still holds its cwd. Exercise the original visible error + user
    // retry, not a hidden backend retry or an invented successful rename.
    for (let attempt = 0; attempt < 5; attempt++) {
      await repositoryDialogs.reply(runtime, renamePopup, 'confirm', {
        name: 'linked-renamed',
      })
      renamedWorktree = (await listWorktrees(mainRepository)).find(
        w => w.path === path
      )
      if (renamedWorktree) {
        break
      }
      const error = runtime.appStore.getState().currentPopup
      assert.ok(
        error?.type === PopupType.Error,
        'a failed rename must show its original error'
      )
      assert.ok(error.error.message.length > 0)
      runtime.dispatcher.closePopupById(error.id!)
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    assert.ok(renamedWorktree, 'the original visible rename retry must succeed')
    await runtime.dispatcher.switchWorktree(mainRepository, renamedWorktree)
    const renamed = selectedRepository()
    assert.equal(renamed.path, path)
    await git(['worktree', 'lock', path], path)
    await runtime.dispatcher.refreshRepository(renamed)
    const popup = await show({
      type: PopupType.DeleteWorktree,
      repository: renamed,
      worktreePath: path,
      options: {},
    })
    const view = repositoryDialogs.project(popup, runtime.appStore.getState())
    assert.equal(view.fields.find(f => f.id === 'locked')?.value, true)
    await assert.rejects(
      repositoryDialogs.reply(runtime, popup, 'confirm', {
        worktreePath: join(fixture, 'not-target'),
      }),
      /Invalid native dialog fields/
    )
    await repositoryDialogs.reply(runtime, popup, 'confirm', {
      doNotAskAgain: false,
    })
    const failed = runtime.appStore.getState().currentPopup
    assert.equal(failed?.type, PopupType.DeleteWorktreeFailed)
    assert.ok(
      (await listWorktrees(selectedRepository())).some(
        w => w.path === path && w.isLocked
      )
    )
    assert.ok(failed && failed.type === PopupType.DeleteWorktreeFailed)
    assert.equal(failed.originalWorktree?.path, path)
    await repositoryDialogs.cancel(runtime, failed)
    assert.equal(
      selectedRepository().path,
      path,
      'cancel restores the original worktree before finishing'
    )
    await git(['worktree', 'unlock', path], path)
    await writeFile(join(path, 'dirty.txt'), 'do not silently discard\n')
    const deletePopup = await show({
      type: PopupType.DeleteWorktree,
      repository: selectedRepository(),
      worktreePath: path,
      options: {},
    })
    await repositoryDialogs.reply(runtime, deletePopup, 'confirm', {
      doNotAskAgain: true,
    })
    const dirtyFailed = runtime.appStore.getState().currentPopup
    assert.ok(dirtyFailed?.type === PopupType.DeleteWorktreeFailed)
    assert.equal(
      runtime.appStore.getState().askForConfirmationOnWorktreeRemoval,
      false
    )
    await repositoryDialogs.reply(runtime, dirtyFailed, 'confirm', {})
    assert.equal(
      (await listWorktrees(selectedRepository())).some(w => w.path === path),
      false
    )
  })

  it('removes a missing worktree with the original deferred checkout even when confirmation is disabled', async () => {
    const repository = selectedRepository()
    const path = join(fixture, 'prunable')
    await git(
      ['worktree', 'add', '-b', 'prunable-branch', path],
      repository.path
    )
    await rm(path, { recursive: true, force: true })
    await runtime.dispatcher.refreshRepository(repository)
    const branch = runtime.repositoryStateManager
      .get(repository)
      .branchesState.allBranches.find(b => b.name === 'prunable-branch')!
    assert.ok(branch)
    runtime.dispatcher.requestDeleteWorktree(repository, path, { branch })
    const popup = runtime.appStore.getState().currentPopup
    assert.ok(popup?.type === PopupType.DeleteWorktree)
    assert.equal(popup.options.isMissing, true)
    const view = repositoryDialogs.project(popup, runtime.appStore.getState())
    assert.equal(
      view.fields.some(f => f.id === 'doNotAskAgain'),
      false
    )
    assert.equal(view.fields.find(f => f.id === 'missing')?.value, true)
    await repositoryDialogs.reply(runtime, popup, 'confirm', {})
    assert.equal(
      (await listWorktrees(selectedRepository())).some(w => w.path === path),
      false
    )
    assert.equal(
      await git(['branch', '--show-current'], selectedRepository().path),
      branch.name
    )
  })

  it('selects an original in-progress clone and rejects its stale identity after removal', async () => {
    let release!: () => void
    const held = new Promise<undefined>(resolve => {
      release = () => resolve(undefined)
    })
    const cloning = runtime.cloningRepositoriesStore.clone(
      'https://example.invalid/project.git',
      join(fixture, 'pending-clone'),
      {},
      () => held
    )
    const repository = runtime.cloningRepositoriesStore.repositories[0]
    assert.ok(repository)
    const presenter = new DesktopPresenter(runtime)
    try {
      await presenter.selectRepository({
        id: repository.id,
        path: repository.path,
      })
      assert.equal(
        runtime.appStore.getState().selectedState?.type,
        SelectionType.CloningRepository
      )
      assert.equal(
        projectKernelState(runtime.appStore.getState(), 1).cloningRepository
          ?.progress.value,
        0
      )
      assert.throws(
        () => presenter.getRepository(repository),
        /still being cloned/
      )
      release()
      assert.equal(await cloning, false)
      assert.throws(
        () => presenter.selectRepository(repository),
        /no longer available/
      )
    } finally {
      release()
      await cloning
      presenter.dispose()
    }
  })

  it('runs original clone validation and local clone without account/network lookup, and removes only registration by default', async () => {
    const source = await init('clone-source')
    const destination = join(fixture, 'cloned')
    assert.equal(await validateRepositoryClonePath(destination), null)
    assert.match(
      (await validateRepositoryClonePath(source.path))?.message ?? '',
      /contains files/
    )
    assert.deepEqual(
      await resolveRepositoryCloneInfo(
        {
          tab: CloneRepositoryTab.Generic,
          url: source.path,
          path: destination,
          lastParsedIdentifier: null,
          account: null,
        },
        []
      ),
      { url: source.path }
    )
    const popup = await show({
      type: PopupType.CloneRepository,
      initialURL: source.path,
    })
    await repositoryDialogs.reply(runtime, popup, 'edit', {
      tab: String(CloneRepositoryTab.Generic),
    })
    await repositoryDialogs.reply(runtime, popup, 'confirm', {
      url: source.path,
      path: destination,
    })
    // Dispatcher.clone schedules the original CloningRepositoriesStore task.
    for (let i = 0; i < 200; i++) {
      if (
        (await readFile(join(destination, '.git', 'HEAD'), 'utf8').catch(
          () => ''
        )) &&
        (await runtime.repositoriesStore.getAll()).some(
          r => r.path === destination
        )
      ) {
        break
      }
      await new Promise(resolve => setTimeout(resolve, 20))
    }
    assert.equal(
      await git(['log', '-1', '--format=%s'], destination),
      'fixture initial'
    )
    const cloned = (await runtime.repositoriesStore.getAll()).find(
      r => r.path === destination
    )
    assert.ok(cloned)
    // A newly cloned repository has legitimate background refresh work. Remove
    // a separately added (not selected) fixture so this test does not race the
    // upstream stash-metric refresh against deletion of its DB row.
    const removalPath = join(fixture, 'remove-registration')
    await mkdir(removalPath)
    await git(['init', '-b', 'main'], removalPath)
    await writeFile(join(removalPath, 'keep.txt'), 'keep on disk\n')
    const [removable] = await runtime.dispatcher.addRepositories([removalPath])
    assert.ok(removable)
    await confirm({ type: PopupType.RemoveRepository, repository: removable })
    assert.equal(
      (await runtime.repositoriesStore.getAll()).some(
        r => r.id === removable.id
      ),
      false
    )
    assert.equal(
      await readFile(join(removalPath, 'keep.txt'), 'utf8'),
      'keep on disk\n'
    )
  })
})

describe('repository forms whitelist and existing long data', () => {
  it('projects all 19 owned types, never raw account/token/error metadata, and supplies every translated key', async () => {
    const state = runtime.appStore.getState()
    const repository = selectedRepository()
    const sha = await git(['rev-parse', 'HEAD'], repository.path)
    const account = new Account(
      'fixture-user',
      'https://api.example.invalid',
      'TOKEN-SENTINEL',
      [],
      '',
      1,
      'Fixture'
    )
    const owner = new Owner('fixture-owner', account.endpoint, 1)
    const parent = new GitHubRepository(
      'parent',
      owner,
      1,
      false,
      null,
      'https://example.invalid/parent.git'
    )
    const gh = new GitHubRepository(
      'fork',
      owner,
      2,
      false,
      null,
      null,
      null,
      null,
      null,
      parent
    )
    const fork = new Repository(repository.path, 998, gh, false)
    assert.ok(isRepositoryWithForkedGitHubRepository(fork))
    const branch = new Branch(
      'long-branch-'.repeat(100),
      'origin/long',
      { sha },
      BranchType.Local,
      'refs/heads/long'
    )
    const worktreePath = join(fixture, 'worktree long 中文 '.repeat(6))
    const popups: Popup[] = [
      { type: PopupType.RenameBranch, repository, branch },
      {
        type: PopupType.DeleteBranch,
        repository,
        branch,
        existsOnRemote: true,
      },
      { type: PopupType.DeleteRemoteBranch, repository, branch },
      { type: PopupType.CreateBranch, repository },
      { type: PopupType.AddRepository, path: repository.path },
      { type: PopupType.CreateRepository, path: repository.path },
      {
        type: PopupType.CloneRepository,
        initialURL: 'https://example.invalid/' + 'long'.repeat(200),
      },
      { type: PopupType.PublishRepository, repository },
      { type: PopupType.RemoveRepository, repository },
      {
        type: PopupType.CreateTag,
        repository,
        targetCommitSha: sha,
        initialName: 'tag',
        localTags: new Map([['long-tag'.repeat(100), sha]]),
      },
      {
        type: PopupType.DeleteTag,
        repository,
        tagName: 'long-tag'.repeat(100),
      },
      { type: PopupType.ChangeRepositoryAlias, repository },
      { type: PopupType.AddWorktree, repository },
      { type: PopupType.RenameWorktree, repository, worktreePath },
      { type: PopupType.DeleteWorktree, repository, worktreePath, options: {} },
      {
        type: PopupType.DeleteWorktreeFailed,
        repository,
        worktreePath,
        options: {},
        originalWorktree: null,
        error: Object.assign(new Error('Visible failure 中文 '.repeat(200)), {
          token: 'ERROR-METADATA-SENTINEL',
        }),
      },
      { type: PopupType.ChooseForkSettings, repository: fork },
      { type: PopupType.CreateFork, repository: fork, account },
      {
        type: PopupType.UpstreamAlreadyExists,
        repository: fork,
        existingRemote: {
          name: 'upstream',
          url: 'https://example.invalid/other.git',
        },
      },
    ]
    assert.deepEqual(
      new Set(popups.map(p => p.type)),
      new Set(repositoryDialogs.types)
    )
    const labels = JSON.parse(
      await readFile(
        resolve('references/native-ui-repository-labels.json'),
        'utf8'
      )
    )
    for (const popup of popups) {
      const view = repositoryDialogs.project(
        Object.assign(popup, { secret: 'EXTRA-SENTINEL' }),
        { ...state, accounts: [account] }
      )
      assert.equal(view.supported, true)
      assert.equal(view.layout, 'form')
      assert.doesNotMatch(
        JSON.stringify(view),
        /SENTINEL|onComplete|originalWorktree|cachedRepoRulesets/
      )
      for (const action of view.actions) {
        assert.ok(
          Array.isArray(action.fields),
          `${popup.type}/${action.id} needs a field contract`
        )
        assert.ok(
          action.fields.every(id =>
            view.fields.some(field => field.id === id && !field.readOnly)
          )
        )
        if (action.id === 'cancel') {
          assert.deepEqual(action.fields, [])
        }
      }
      for (const field of view.fields) {
        if (field.changeAction) {
          assert.ok(
            view.actions
              .find(action => action.id === field.changeAction)
              ?.fields?.includes(field.id)
          )
        }
      }
      for (const key of [
        view.titleKey,
        view.descriptionKey,
        ...view.fields.map(f => f.labelKey),
        ...view.actions.map(a => a.labelKey),
      ].filter(Boolean)) {
        assert.ok(labels[key!], `missing ${key}`)
        for (const language of ['en', 'zh-Hans', 'zh-Hant']) {
          assert.ok(labels[key!][language])
        }
      }
      await assert.rejects(
        repositoryDialogs.reply(runtime, popup, 'not-an-action', {}),
        /Invalid repository dialog action/
      )
      await assert.rejects(
        repositoryDialogs.reply(runtime, popup, 'cancel', { force: true }),
        /Invalid native dialog fields/
      )
      await assert.rejects(
        repositoryDialogs.reply(runtime, popup, 'cancel', null),
        /Invalid native dialog fields/
      )
    }
    const view = repositoryDialogs.project(popups[0], state)
    assert.equal(view.fields.find(f => f.id === 'name')?.value, branch.name)
    assert.equal(
      repositoryFullPath('nested/repo', fixture),
      join(fixture, 'nested', 'repo')
    )
  })

  it('loads defaults once on mount and keeps credential-bearing clone URLs display-only', async () => {
    const url =
      'https://fixture:PASSWORD-SENTINEL@example.invalid/owner/repo.git?token=QUERY-SENTINEL#oauth=OAUTH-SENTINEL'
    const popup = await show({
      type: PopupType.CloneRepository,
      initialURL: url,
    })
    await repositoryDialogs.reply(runtime, popup, 'edit', {
      tab: String(CloneRepositoryTab.Generic),
    })
    let updates = 0
    const handle = mountRepositoryDialog(runtime, popup, () => updates++)
    try {
      for (let i = 0; i < 100; i++) {
        const view = repositoryDialogs.project(
          popup,
          runtime.appStore.getState()
        )
        if (
          view.fields.find(f => f.id === 'path')?.value &&
          !view.actions.some(a => a.disabled && a.id === 'load')
        ) {
          break
        }
        await new Promise(resolve => setTimeout(resolve, 10))
      }
      const view = repositoryDialogs.project(popup, runtime.appStore.getState())
      assert.ok(
        !view.actions.some(a => a.id === 'load'),
        'Completed automatic loading must not leave a setup step in the form'
      )
      assert.equal(
        view.fields.find(f => f.id === 'url')?.value,
        'https://example.invalid/owner/repo.git'
      )
      assert.doesNotMatch(JSON.stringify(view), /SENTINEL/)
      assert.ok(
        updates >= 2,
        'Mount/default completion requires explicit UI notifications'
      )
      assert.ok(
        'initialURL' in popup && popup.initialURL === url,
        'Display redaction changed original continuation input'
      )
      await repositoryDialogs.reply(runtime, popup, 'edit', {
        url: 'https://example.invalid/owner/repo.git',
      })
      assert.doesNotMatch(
        JSON.stringify(
          repositoryDialogs.project(popup, runtime.appStore.getState())
        ),
        /SENTINEL/
      )
      handle.dispose()
      const before = updates
      await repositoryDialogs.reply(runtime, popup, 'edit', {
        url: 'owner/other',
      })
      assert.equal(
        updates,
        before,
        'Disposed observer received a late draft update'
      )
    } finally {
      handle.dispose()
    }
    await repositoryDialogs.cancel(runtime, popup)
  })

  it('previews original sanitization/README overwrite, keeps clone tab drafts separate, and rejects overlapping replies', async () => {
    const repository = await init('preview-forms')
    await writeFile(
      join(repository.path, 'README.md'),
      'Existing preview README\n'
    )
    const create = await show({
      type: PopupType.CreateRepository,
      path: repository.path,
    })
    await repositoryDialogs.reply(runtime, create, 'edit', { readme: true })
    assert.ok(
      repositoryDialogs
        .project(create, runtime.appStore.getState())
        .fields.some(f => f.id === 'readmeWarning')
    )
    await repositoryDialogs.cancel(runtime, create)
    const previousCloneDirectory = await getDefaultDir()
    setDefaultDir(fixture)
    const clone = await show({
      type: PopupType.CloneRepository,
      initialURL: null,
    })
    await repositoryDialogs.reply(runtime, clone, 'edit', {
      tab: String(CloneRepositoryTab.Generic),
    })
    await repositoryDialogs.reply(runtime, clone, 'load', {})
    await repositoryDialogs.reply(runtime, clone, 'edit', {
      url: 'fixture-user/example',
    })
    await repositoryDialogs.reply(runtime, clone, 'edit', {
      url: 'fixture-user/second',
    })
    const suggested = repositoryDialogs
      .project(clone, runtime.appStore.getState())
      .fields.find(f => f.id === 'path')?.value
    assert.equal(suggested, join(fixture, 'second'))
    await repositoryDialogs.reply(runtime, clone, 'edit', {
      tab: String(CloneRepositoryTab.GitHub),
    })
    await repositoryDialogs.reply(runtime, clone, 'edit', {
      tab: String(CloneRepositoryTab.Generic),
    })
    assert.equal(
      repositoryDialogs
        .project(clone, runtime.appStore.getState())
        .fields.find(f => f.id === 'url')?.value,
      'fixture-user/second'
    )
    assert.equal(
      repositoryDialogs
        .project(clone, runtime.appStore.getState())
        .fields.find(f => f.id === 'path')?.value,
      suggested
    )
    await repositoryDialogs.cancel(runtime, clone)
    setDefaultDir(previousCloneDirectory)
    const tag = await show({
      type: PopupType.CreateTag,
      repository,
      targetCommitSha: await git(['rev-parse', 'HEAD'], repository.path),
      localTags: new Map(),
    })
    await repositoryDialogs.reply(runtime, tag, 'edit', { name: 'preview tag' })
    assert.equal(
      repositoryDialogs
        .project(tag, runtime.appStore.getState())
        .fields.find(f => f.id === 'effectiveName')?.value,
      'preview-tag'
    )
    const pending = repositoryDialogs.reply(runtime, tag, 'confirm', {})
    await assert.rejects(
      repositoryDialogs.reply(runtime, tag, 'confirm', {}),
      /busy/
    )
    await pending
    assert.equal(
      await git(['tag', '--list', 'preview-tag'], repository.path),
      'preview-tag'
    )
  })

  it('rejects extra fields, wrong choices, wrong booleans, NUL text and forged existing-object paths before mutation', async () => {
    // The rejection checks own a clean repository; earlier preview tests may
    // deliberately leave untracked files in the globally selected repository.
    const repository = await init('invalid-dialog-inputs')
    const popup = await show({ type: PopupType.AddWorktree, repository })
    for (const values of [
      { name: 'x\0' },
      { name: 12 },
      { branch: {} },
      { force: true },
      { repository: { id: 1 } },
    ]) {
      await assert.rejects(
        repositoryDialogs.reply(runtime, popup, 'confirm', values),
        /Invalid native dialog/
      )
    }
    await repositoryDialogs.cancel(runtime, popup)
    const remove = await show({ type: PopupType.RemoveRepository, repository })
    await assert.rejects(
      repositoryDialogs.reply(runtime, remove, 'confirm', {
        deleteFromDisk: 'true',
      }),
      /Invalid native dialog choice/
    )
    await repositoryDialogs.cancel(runtime, remove)
    const forkSettings = await show({
      type: PopupType.CloneRepository,
      initialURL: null,
    })
    await assert.rejects(
      repositoryDialogs.reply(runtime, forkSettings, 'edit', { tab: '100' }),
      /Invalid repository dialog selection/
    )
    await repositoryDialogs.cancel(runtime, forkSettings)
    assert.equal(await git(['status', '--porcelain'], repository.path), '')
  })
})

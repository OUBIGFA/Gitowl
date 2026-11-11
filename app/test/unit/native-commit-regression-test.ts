import { listenOnLoopback } from '../helpers/http-server'
import assert from 'node:assert/strict'
import { after, before, describe, it, mock } from 'node:test'
import { EventEmitter } from 'events'
import { createServer, Server, ServerResponse } from 'http'
import { AddressInfo } from 'net'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { exec } from 'dugite'
import { ipcRenderer } from 'electron'
import type { AppRuntime } from '../../src/ui/app-runtime'
import type { Repository } from '../../src/models/repository'
import type { Account } from '../../src/models/account'
import type { NativeCommitPresenter } from '../../src/native-kernel/commit-presenter'

// Only OS secrets and Electron IPC are substituted. API, author provider, all
// original stores/Dispatcher and Git are real, including first-click preparation.
const secrets = new Map<string, string>()
mock.module('keytar', {
  namedExports: {
    setPassword: async (service: string, key: string, value: string) => {
      secrets.set(`${service}:${key}`, value)
    },
    getPassword: async (service: string, key: string) =>
      secrets.get(`${service}:${key}`) ?? null,
    deletePassword: async (service: string, key: string) =>
      secrets.delete(`${service}:${key}`),
  },
})
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
let runtime: AppRuntime
let Presenter: typeof NativeCommitPresenter
let presenter: NativeCommitPresenter
let root: string
const servers: Server[] = []
const endpoints: string[] = []
const accounts: Account[] = []
const repos: Repository[] = []
const requests: { endpoint: number; url: string; authorization?: string }[] = []
const held: ServerResponse[] = []
let holdUser = false
let enteredUser: (() => void) | undefined
let module: typeof import('../../src/native-kernel/operation-dialogs')
let PopupType: typeof import('../../src/models/popup').PopupType
const priorEndpoint = process.env.DESKTOP_GITHUB_DOTCOM_API_ENDPOINT
const priorGlobal = process.env.GIT_CONFIG_GLOBAL
const priorSystem = process.env.GIT_CONFIG_NOSYSTEM

async function git(repository: Repository | string, args: string[]) {
  const result = await exec(
    args,
    typeof repository === 'string' ? repository : repository.path
  )
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout.trim()
}
async function until(predicate: () => boolean, message: string) {
  const deadline = Date.now() + 15000
  while (!predicate() && Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 10))
  }
  assert.ok(predicate(), message)
}
function state(repo = repos[0]) {
  return runtime.repositoryStateManager.get(repo)
}
function closePopups() {
  for (const popup of runtime.appStore.getState().allPopups) {
    runtime.dispatcher.closePopupById(popup.id!)
  }
}
async function select(repo: Repository) {
  closePopups()
  await runtime.appStore._selectRepository(repo)
  await runtime.dispatcher.refreshRepository(repo)
  presenter.observe(runtime.appStore.getState())
  const { isRepositoryGitMutationInProgress } = await import(
    '../../src/lib/git/repository-operation'
  )
  await until(
    () => !isRepositoryGitMutationInProgress(repo.path),
    'Original background Git mutations did not settle'
  )
}

before(async () => {
  await mkdir(resolve('_temp/native-full-ui'), { recursive: true })
  root = await mkdtemp(
    join(resolve('_temp/native-full-ui'), 'commit-regression-')
  )
  process.env.GIT_CONFIG_GLOBAL = join(root, 'global-config')
  process.env.GIT_CONFIG_NOSYSTEM = '1'
  await writeFile(process.env.GIT_CONFIG_GLOBAL, '')
  for (const endpoint of [0, 1]) {
    const server = createServer((req, res) => {
      const url = req.url ?? ''
      requests.push({ endpoint, url, authorization: req.headers.authorization })
      res.setHeader('Content-Type', 'application/json')
      const user = (login: string) => ({
        id: endpoint + 20,
        login,
        name: `Author ${endpoint}`,
        email: `${login}-${endpoint}@example.test`,
        type: 'User',
        avatar_url: '',
      })
      if (url.endsWith('/mentionables/users')) {
        res.end(JSON.stringify([user('same')]))
        return
      }
      if (url.startsWith('/users/')) {
        if (holdUser) {
          held.push(res)
          enteredUser?.()
          return
        }
        if (url === '/users/missing') {
          res.statusCode = 404
          res.end('{}')
          return
        }
        res.end(JSON.stringify(user(url.substring('/users/'.length))))
        return
      }
      // Unrelated refresh endpoints have no objects in this local fixture.
      res.statusCode = 404
      res.end('{}')
    })
    await listenOnLoopback(server)
    servers.push(server)
    endpoints.push(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)
  }
  process.env.DESKTOP_GITHUB_DOTCOM_API_ENDPOINT = endpoints[0]
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  runtime = (await import('../../src/ui/app-runtime')).getAppRuntime()
  const { AppStore } = await import('../../src/lib/stores/app-store')
  const { Dispatcher } = await import('../../src/ui/dispatcher')
  assert.ok(runtime.appStore instanceof AppStore)
  assert.ok(runtime.dispatcher instanceof Dispatcher)
  runtime.appStore.onDidError(error => {
    void runtime.dispatcher.postError(error)
  })
  Presenter = (await import('../../src/native-kernel/commit-presenter'))
    .NativeCommitPresenter
  PopupType = (await import('../../src/models/popup')).PopupType
  module = await import('../../src/native-kernel/operation-dialogs')
  presenter = new Presenter(runtime, s =>
    s.changesState.workingDirectory.files.filter(f => f.path === 'visible.txt')
  )
  const { Account } = await import('../../src/models/account')
  for (const index of [0, 1]) {
    const account = await runtime.accountsStore.addAccount(
      new Account(
        'signed-in',
        endpoints[index],
        `loopback-token-${index}`,
        [],
        '',
        index + 1,
        'Fixture'
      )
    )
    assert.ok(account)
    accounts.push(account)
    await until(
      () =>
        runtime.appStore
          .getState()
          .accounts.some(
            current =>
              current.endpoint === account.endpoint &&
              current.token === account.token
          ),
      'Account reconciliation not complete'
    )
    const path = join(root, `repo-${index}`)
    await mkdir(path)
    await git(path, ['init', '--template=', '--initial-branch=main'])
    await git(path, ['config', 'user.name', 'Fixture Author'])
    await git(path, ['config', 'user.email', 'fixture@example.test'])
    await git(path, ['config', 'commit.gpgSign', 'false'])
    await git(path, ['config', 'core.hooksPath', join(root, 'no-hooks')])
    await writeFile(join(path, 'file.txt'), 'base\n')
    await git(path, ['add', '.'])
    await git(path, ['commit', '-m', 'base'])
    const [local] = await runtime.dispatcher.addRepositories([path])
    const github =
      await runtime.repositoriesStore.upsertGitHubRepositoryFromMatch({
        account,
        owner: 'owner',
        name: `repo-${index}`,
      })
    const repo = await runtime.repositoriesStore.setGitHubRepository(
      local,
      github,
      account.login
    )
    repos.push(repo)
    await runtime.gitHubUserStore.updateMentionables(github, account)
  }
})
after(async () => {
  presenter?.dispose()
  held.forEach(response => response.destroy())
  servers.forEach(server => {
    server.closeAllConnections()
    server.close()
  })
  await runtime?.appStore._selectRepository(null)
  window.close()
  for (const [key, value] of [
    ['DESKTOP_GITHUB_DOTCOM_API_ENDPOINT', priorEndpoint],
    ['GIT_CONFIG_GLOBAL', priorGlobal],
    ['GIT_CONFIG_NOSYSTEM', priorSystem],
  ] as const) {
    if (value === undefined) {
      delete process.env[key]
    } else {
      process.env[key] = value
    }
  }
  await rm(root, {
    recursive: true,
    force: true,
    maxRetries: 20,
    retryDelay: 50,
  })
})

describe(
  'native commit input with original APIs, AppStore, Dispatcher and Git',
  { timeout: 240000 },
  () => {
    it('keeps identical usernames on two real API endpoints distinct and refuses old target/hit IDs', async () => {
      await select(repos[0])
      await presenter.searchAuthors(repos[0], 'same')
      const first = presenter
        .getState()!
        .authorHits.find(hit => hit.username === 'same')!
      assert.equal(first.email, 'same-0@example.test')
      const { Repository } = await import('../../src/models/repository')
      const { GitHubRepository } = await import(
        '../../src/models/github-repository'
      )
      const { Owner } = await import('../../src/models/owner')
      const source = repos[0],
        gh = source.gitHubRepository!
      const otherHost = new GitHubRepository(
        gh.name,
        new Owner(gh.owner.login, endpoints[1], gh.owner.id),
        gh.dbID,
        gh.isPrivate,
        gh.htmlURL,
        gh.cloneURL,
        gh.issuesEnabled,
        gh.isArchived,
        gh.permissions,
        gh.parent
      )
      const staleHost = new Repository(
        source.path,
        source.id,
        otherHost,
        source.missing,
        source.alias,
        source.workflowPreferences,
        source.isTutorialRepository,
        source.gitDir,
        source.mainWorktreePath,
        source.login
      )
      // The original GitHubRepository hash omits endpoint; host equality must
      // be explicit even when every identity/hash otherwise matches.
      assert.equal(staleHost.hash, source.hash)
      await assert.rejects(
        presenter.searchAuthors(staleHost, 'same'),
        /previous/
      )
      await select(repos[1])
      await assert.rejects(
        presenter.addAuthor(repos[1], first.id),
        /suggestion/
      )
      await assert.rejects(
        presenter.searchAuthors(repos[0], 'same'),
        /previous/
      )
      await runtime.gitHubUserStore.updateMentionables(
        repos[1].gitHubRepository!,
        accounts[1]
      )
      await presenter.searchAuthors(repos[1], 'same')
      const second = presenter
        .getState()!
        .authorHits.find(hit => hit.username === 'same')!
      assert.equal(second.email, 'same-1@example.test')
      await presenter.addAuthor(repos[1], second.id)
      assert.deepEqual(state(repos[1]).changesState.coAuthors, [
        {
          kind: 'known',
          username: 'same',
          name: 'Author 1',
          email: 'same-1@example.test',
        },
      ])
      assert.equal(state(repos[0]).changesState.coAuthors.length, 0)
      for (const endpoint of [0, 1]) {
        assert.ok(
          requests.some(
            request =>
              request.endpoint === endpoint &&
              request.url.endsWith('/mentionables/users') &&
              request.authorization?.includes(`loopback-token-${endpoint}`)
          )
        )
      }
      const author = presenter.getState()!.coAuthors[0]
      presenter.removeAuthor(repos[1], author.id)
      assert.equal(state(repos[1]).changesState.coAuthors.length, 0)
    })

    it('resolves unknown users through API.fromAccount and ignores late responses after repository switch or removal', async () => {
      await select(repos[0])
      await presenter.searchAuthors(repos[0], 'exact')
      await presenter.addAuthor(
        repos[0],
        presenter.getState()!.authorHits[0].id
      )
      assert.equal(
        presenter.getState()!.coAuthors[0].email,
        'exact-0@example.test'
      )
      await presenter.searchAuthors(repos[0], 'missing')
      await presenter.addAuthor(
        repos[0],
        presenter.getState()!.authorHits[0].id
      )
      assert.equal(presenter.getState()!.coAuthors[1].state, 'error')
      for (const transition of ['remove', 'repository'] as const) {
        holdUser = true
        const entered = new Promise<void>(resolve => {
          enteredUser = resolve
        })
        await presenter.searchAuthors(repos[0], `late-${transition}`)
        const adding = presenter.addAuthor(
          repos[0],
          presenter.getState()!.authorHits[0].id
        )
        await entered
        if (transition === 'remove') {
          presenter.removeAuthor(
            repos[0],
            presenter.getState()!.coAuthors.at(-1)!.id
          )
        } else {
          await select(repos[1])
        }
        held.shift()!.end(
          JSON.stringify({
            id: 99,
            login: `late-${transition}`,
            name: 'LATE',
            email: 'late@example.test',
            type: 'User',
            avatar_url: '',
          })
        )
        await adding
        assert.ok(
          !state(repos[0]).changesState.coAuthors.some(
            author =>
              author.kind === 'known' && author.email === 'late@example.test'
          )
        )
        assert.equal(state(repos[1]).changesState.coAuthors.length, 0)
        holdUser = false
      }
      runtime.dispatcher.setCoAuthors(repos[0], [])
    })

    it('rejects cached suggestions after the real assigned account is removed', async () => {
      await select(repos[1])
      await presenter.searchAuthors(repos[1], 'same')
      const id = presenter.getState()!.authorHits[0].id
      await runtime.accountsStore.removeAccount(accounts[1])
      await assert.rejects(presenter.addAuthor(repos[1], id), /suggestion/)
      assert.equal(state(repos[1]).changesState.coAuthors.length, 0)
    })

    it('uses actual Git trailers, default single-file summary, signoff and amend without pushing', async () => {
      const repo = repos[0]
      await writeFile(join(repo.path, 'file.txt'), 'manual change\n')
      await select(repo)
      runtime.dispatcher.setCoAuthors(repo, [
        {
          kind: 'known',
          username: 'same',
          name: 'Same',
          email: 'same-0@example.test',
        },
      ])
      runtime.dispatcher.updateCommitOptions(repo, { signOffCommits: true })
      assert.equal(presenter.getState()!.defaultSummary, 'Update file.txt')
      assert.equal(
        await presenter.createManual(repo, {
          summary: '',
          description: 'body',
        }),
        true
      )
      const message = await git(repo, ['log', '-1', '--format=%B'])
      assert.match(message, /^Update file.txt\n\nbody/)
      assert.match(message, /Co-Authored-By: Same <same-0@example.test>/i)
      const identity = (
        await git(repo, ['var', 'GIT_COMMITTER_IDENT'])
      ).replace(/>.*$/, '>')
      assert.ok(message.includes(`Signed-off-by: ${identity}`))
      await runtime.dispatcher.refreshRepository(repo)
      await presenter.amend(repo, true)
      await until(
        () =>
          state(repo).commitToAmend !== null ||
          runtime.appStore.getState().currentPopup !== null,
        'Original amend did not settle'
      )
      if (state(repo).commitToAmend === null) {
        const warning = runtime.appStore.getState().currentPopup!
        assert.equal(warning.type, PopupType.WarnForcePush)
        await module.operationDialogs.reply(runtime, warning, 'submit', {
          confirm: true,
        })
        await until(
          () => state(repo).commitToAmend !== null,
          'Confirmed amend did not settle'
        )
      }
      assert.equal(state(repo).commitToAmend?.summary, 'Update file.txt')
      const count = await git(repo, ['rev-list', '--count', 'HEAD'])
      assert.equal(
        await presenter.createManual(repo, {
          summary: 'Amended summary',
          description: '',
        }),
        true
      )
      assert.equal(await git(repo, ['rev-list', '--count', 'HEAD']), count)
      assert.equal(
        await git(repo, ['log', '-1', '--format=%s']),
        'Amended summary'
      )
      assert.equal(await git(repo, ['remote']), '')
      runtime.dispatcher.setCoAuthors(repo, [])
    })

    it('retains selected hidden files behind the original confirmation and show-files clears only the filter', async () => {
      const repo = repos[0]
      await writeFile(join(repo.path, 'visible.txt'), 'visible\n')
      await writeFile(join(repo.path, 'hidden.txt'), 'hidden\n')
      await select(repo)
      runtime.dispatcher.setConfirmCommitFilteredChanges(true)
      runtime.dispatcher.setChangesListFilterText(repo, 'visible')
      const head = await git(repo, ['rev-parse', 'HEAD'])
      assert.equal(
        await presenter.createManual(repo, {
          summary: 'filtered',
          description: '',
        }),
        false
      )
      const popup = runtime.appStore.getState().currentPopup!
      assert.equal(popup.type, PopupType.ConfirmCommitFilteredChanges)
      await module.operationDialogs.reply(runtime, popup, 'showFiles', {})
      assert.equal(state(repo).changesState.fileListFilter.filterText, '')
      assert.equal(await git(repo, ['rev-parse', 'HEAD']), head)
      assert.equal(await git(repo, ['diff', '--cached', '--name-only']), '')
    })

    it('freezes original prepared intent and rejects option/dispose/repository changes across its real asynchronous reads', async () => {
      const repo = repos[0]
      await select(repo)
      const files = state(repo).changesState.workingDirectory.files
      const prepared =
        await runtime.dispatcher.prepareCopilotAssistedCommitRequest(repo, {
          files,
          trailers: [],
          signOffCommits: false,
          skipCommitHooks: false,
          allowEmptyCommit: false,
        })
      assert.ok(
        Object.isFrozen(prepared) &&
          Object.isFrozen(prepared.files) &&
          Object.isFrozen(prepared.files[0]) &&
          Object.isFrozen(prepared.trailers)
      )
      assert.ok(prepared.intent)
      const head = await git(repo, ['rev-parse', 'HEAD'])
      const index = await readFile(join(repo.path, '.git', 'index'))
      for (const transition of ['options', 'dispose', 'repository'] as const) {
        await select(repo)
        const p = new Presenter(runtime)
        const creating = p.createAssisted(repo)
        // prepare() has captured its original request and is awaiting real lstat /
        // selection-reader work here. No planner, store, freeze or assert is mocked.
        if (transition === 'options') {
          runtime.dispatcher.updateCommitOptions(repo, {
            signOffCommits: !state(repo).signOffCommits,
          })
        }
        if (transition === 'dispose') {
          p.dispose()
        }
        if (transition === 'repository') {
          await runtime.appStore._selectRepository(null)
        }
        await assert.rejects(creating, /changed/)
        assert.equal(state(repo).changesState.assistedCommit.kind, 'idle')
        assert.equal(runtime.appStore.getState().currentPopup, null)
        p.dispose()
      }
      assert.equal(await git(repo, ['rev-parse', 'HEAD']), head)
      assert.deepEqual(await readFile(join(repo.path, '.git', 'index')), index)
    })

    it('rejects a late original unknown-author continuation after leaving the assisted page', async () => {
      const repo = repos[0]
      await select(repo)
      runtime.dispatcher.setCoAuthors(repo, [
        { kind: 'unknown', username: 'unresolved', state: 'error' },
      ])
      const head = await git(repo, ['rev-parse', 'HEAD'])
      assert.equal((await presenter.createAssisted(repo)).kind, 'warning')
      const popup = runtime.appStore.getState().currentPopup!
      assert.equal(popup.type, PopupType.UnknownAuthors)
      await runtime.appStore._selectRepository(null)
      if (popup.type !== PopupType.UnknownAuthors) {
        assert.fail('Missing original author warning')
      }
      popup.onCommit()
      await until(
        () =>
          runtime.appStore.getState().currentPopup?.type === PopupType.Error,
        'Stale continuation did not report its original error'
      )
      assert.equal(state(repo).changesState.assistedCommit.kind, 'idle')
      assert.equal(await git(repo, ['rev-parse', 'HEAD']), head)
      runtime.dispatcher.setCoAuthors(repo, [])
      closePopups()
    })

    it('rejects author hits and assisted warning continuations after an actual linked-worktree switch', async () => {
      const repo = repos[0]
      await select(repo)
      const { listWorktrees } = await import('../../src/lib/git/worktree')
      const linkedPath = join(root, 'linked-worktree')
      await git(repo, ['worktree', 'add', '-b', 'linked-fixture', linkedPath])
      const trees = await listWorktrees(repo)
      const linked = trees.find(
        tree => resolve(tree.path) === resolve(linkedPath)
      )!
      const main = trees.find(
        tree => resolve(tree.path) === resolve(repo.path)
      )!
      assert.ok(linked && main)
      runtime.dispatcher.setCoAuthors(repo, [
        { kind: 'unknown', username: 'unresolved', state: 'error' },
      ])
      await presenter.searchAuthors(repo, 'same')
      const hit = presenter.getState()!.authorHits[0].id
      const head = await git(repo, ['rev-parse', 'HEAD'])
      assert.equal((await presenter.createAssisted(repo)).kind, 'warning')
      const warning = runtime.appStore.getState().currentPopup!
      await runtime.dispatcher.switchWorktree(repo, linked)
      const { SelectionType } = await import('../../src/lib/app-state')
      const selected = runtime.appStore.getState().selectedState
      assert.ok(selected?.type === SelectionType.Repository)
      assert.equal(selected.repository.id, repo.id)
      assert.equal(resolve(selected.repository.path), resolve(linkedPath))
      await assert.rejects(
        presenter.addAuthor(selected.repository, hit),
        /suggestion/
      )
      if (warning.type !== PopupType.UnknownAuthors) {
        assert.fail('Missing author warning')
      }
      warning.onCommit()
      await until(
        () =>
          runtime.appStore.getState().currentPopup?.type === PopupType.Error,
        'Old worktree continuation was not rejected'
      )
      assert.equal(await git(repo, ['rev-parse', 'HEAD']), head)
      assert.equal(state(repo).changesState.assistedCommit.kind, 'idle')
      closePopups()
      await runtime.dispatcher.switchWorktree(selected.repository, main)
      const restored = runtime.appStore.getState().selectedState
      assert.ok(restored?.type === SelectionType.Repository)
      repos[0] = restored.repository
      runtime.dispatcher.setCoAuthors(repos[0], [])
    })

    it('rechecks default summaries after selection changes and enforces original rules on modal drafts', async () => {
      const repo = repos[0]
      await select(repo)
      const { RepoRulesInfo } = await import('../../src/models/repo-rules')
      const { parseRepoRules, useRepoRulesLogic } = await import(
        '../../src/lib/helpers/repo-rules'
      )
      const { APIRepoRuleType, APIRepoRuleMetadataOperator } = await import(
        '../../src/lib/api'
      )
      assert.equal(useRepoRulesLogic(accounts[0], repo), true)
      const info = await parseRepoRules(
        [
          {
            ruleset_id: 1,
            type: APIRepoRuleType.CommitMessagePattern,
            parameters: {
              name: 'Fixture rule',
              negate: false,
              operator: APIRepoRuleMetadataOperator.StartsWith,
              pattern: 'Create visible',
            },
          },
        ],
        new Map([[1, { id: 1, current_user_can_bypass: 'never' as const }]]),
        repo
      )
      runtime.repositoryStateManager.updateChangesState(repo, () => ({
        currentRepoRulesInfo: info,
      }))
      runtime.dispatcher.setCoAuthors(repo, [])
      runtime.dispatcher.setCommitMessage(repo, {
        summary: '',
        description: '',
        timestamp: Date.now(),
      })
      const files = state(repo).changesState.workingDirectory.files
      await runtime.dispatcher.changeFileIncluded(repo, files, false)
      await runtime.dispatcher.changeFileIncluded(
        repo,
        files.filter(file => file.path === 'visible.txt'),
        true
      )
      await until(() => !presenter.getState()!.rulesPending, 'Rules pending')
      assert.equal(presenter.getState()!.defaultSummary, 'Create visible.txt')
      assert.equal(presenter.getState()!.rulesBlocked, false)
      await runtime.dispatcher.changeFileIncluded(
        repo,
        files.filter(file => file.path === 'visible.txt'),
        false
      )
      await runtime.dispatcher.changeFileIncluded(
        repo,
        files.filter(file => file.path === 'hidden.txt'),
        true
      )
      await until(
        () => !presenter.getState()!.rulesPending,
        'Rules pending after selected file changes'
      )
      assert.equal(presenter.getState()!.rulesBlocked, true)
      let calls = 0
      await runtime.dispatcher.showPopup({
        type: PopupType.CommitMessage,
        repository: repo,
        commitMessage: {
          summary: 'blocked',
          description: '',
          timestamp: Date.now(),
        },
        coAuthors: [],
        showCoAuthoredBy: false,
        dialogTitle: 'Rules',
        dialogButtonText: 'Use',
        prepopulateCommitSummary: false,
        onSubmitCommitMessage: async () => {
          calls++
          return false
        },
      })
      const popup = runtime.appStore.getState().currentPopup!
      const handle = module.mountOperationDialog(runtime, popup, () => {})
      const view = () =>
        module.operationDialogs.project(popup, runtime.appStore.getState())
      const reply = (action: string, values: unknown) =>
        module.operationDialogs.reply(runtime, popup, action, values)
      try {
        await until(
          () =>
            view().fields.find(field => field.id === 'rules')?.labelKey ===
            'operation.rulesBlocked',
          'Modal rule gate not projected'
        )
        await assert.rejects(
          reply('submit', {
            summary: 'blocked',
            description: '',
            showCoAuthors: false,
            skipCommitHooks: false,
            signOffCommits: false,
          }),
          /action/
        )
        assert.equal(calls, 0)
        await reply('change', { summary: 'Create visible change' })
        await until(
          () =>
            !view().actions.find(action => action.id === 'submit')!.disabled,
          'Edited modal rule check did not settle'
        )
        await reply('submit', {
          summary: 'Create visible change',
          description: '',
          showCoAuthors: false,
          skipCommitHooks: false,
          signOffCommits: false,
        })
        assert.equal(calls, 1)
        await reply('cancel', {})
      } finally {
        handle.dispose()
        runtime.repositoryStateManager.updateChangesState(repo, () => ({
          currentRepoRulesInfo: new RepoRulesInfo(),
        }))
      }
    })

    it('keeps late modal author resolution pending while the original callback is busy', async () => {
      const repo = repos[0]
      await select(repo)
      let finish!: (value: boolean) => void
      const pending = new Promise<boolean>(resolve => {
        finish = resolve
      })
      await runtime.dispatcher.showPopup({
        type: PopupType.CommitMessage,
        repository: repo,
        commitMessage: {
          summary: 'Busy fixture',
          description: '',
          timestamp: 1,
        },
        coAuthors: [],
        showCoAuthoredBy: true,
        dialogTitle: 'Busy author',
        dialogButtonText: 'Use',
        prepopulateCommitSummary: false,
        onSubmitCommitMessage: () => pending,
      })
      const popup = runtime.appStore.getState().currentPopup!
      const handle = module.mountOperationDialog(runtime, popup, () => {})
      const view = () =>
        module.operationDialogs.project(popup, runtime.appStore.getState())
      const reply = (action: string, values: unknown) =>
        module.operationDialogs.reply(runtime, popup, action, values)
      holdUser = true
      const entered = new Promise<void>(resolve => {
        enteredUser = resolve
      })
      try {
        await reply('searchAuthors', { authorQuery: 'busy-author' })
        const hit = view().fields.find(field => field.id === 'authorHit')!
          .choices![0].id
        const adding = reply('addAuthor', { authorHit: hit })
        await entered
        await reply('searchAuthors', { authorQuery: 'same' })
        const cachedHit = view().fields.find(field => field.id === 'authorHit')!
          .choices![0].id
        await until(
          () =>
            !view().actions.find(action => action.id === 'submit')!.disabled,
          'Rules did not settle before modal submission'
        )
        await reply('submit', {
          summary: 'Busy fixture',
          description: '',
          showCoAuthors: true,
          skipCommitHooks: false,
          signOffCommits: false,
        })
        const warning = runtime.appStore.getState().currentPopup!
        assert.equal(warning.type, PopupType.UnknownAuthors)
        await module.operationDialogs.reply(runtime, warning, 'submit', {})
        assert.ok(view().actions.every(action => action.disabled))
        assert.equal(
          view().fields.find(field => field.id === 'authorHit')!.choices!
            .length,
          0
        )
        held.shift()!.end(
          JSON.stringify({
            id: 80,
            login: 'busy-author',
            name: 'Busy Author',
            email: 'busy@example.test',
            type: 'User',
            avatar_url: '',
          })
        )
        await adding
        assert.equal(
          view().fields.find(field => field.id === 'authors')!.text,
          'busy-author'
        )
        assert.ok(
          view().fields.some(
            field => field.labelKey === 'operation.authorSearching'
          )
        )
        await assert.rejects(reply('cancel', {}))
        finish(false)
        await until(
          () =>
            view().fields.find(field => field.id === 'author')!.choices![0]
              .label === 'Busy Author <busy@example.test>',
          'Pending author was not delivered after the original callback settled'
        )
        await assert.rejects(reply('addAuthor', { authorHit: cachedHit }))
        assert.equal(state(repo).changesState.coAuthors.length, 0)
        await reply('cancel', {})
      } finally {
        finish(false)
        holdUser = false
        handle.dispose()
      }
    })

    it('edits modal co-authors with typed controls, local ownership and the original callback context', async () => {
      const repo = repos[0]
      await select(repo)
      runtime.dispatcher.setShowCommitLengthWarning(true)
      const contexts: unknown[] = []
      await runtime.dispatcher.showPopup({
        type: PopupType.CommitMessage,
        repository: repo,
        commitMessage: null,
        coAuthors: [],
        showCoAuthoredBy: true,
        dialogTitle: 'Fixture',
        dialogButtonText: 'Use',
        prepopulateCommitSummary: false,
        onSubmitCommitMessage: async context => {
          contexts.push(context)
          return false
        },
      })
      const popup = runtime.appStore.getState().currentPopup!
      const handle = module.mountOperationDialog(runtime, popup, () => {})
      const view = () =>
        module.operationDialogs.project(popup, runtime.appStore.getState())
      const reply = (action: string, values: unknown) =>
        module.operationDialogs.reply(runtime, popup, action, values)
      try {
        assert.equal(
          view().fields.find(field => field.id === 'authorQuery')?.changeAction,
          'searchAuthors'
        )
        await reply('change', {
          summary: 'a'.repeat(51),
          description: 'draft only',
        })
        assert.ok(view().fields.some(field => field.id === 'summaryLength'))
        await reply('searchAuthors', { authorQuery: 'same' })
        const hit = view().fields.find(field => field.id === 'authorHit')!
          .choices![0].id
        await assert.rejects(
          reply('addAuthor', { authorHit: hit, email: 'forged@example.test' })
        )
        await reply('addAuthor', { authorHit: hit })
        assert.equal(state(repo).changesState.coAuthors.length, 0)
        await until(
          () =>
            !view().actions.find(action => action.id === 'submit')!.disabled,
          'Modal rules did not finish'
        )
        const head = await git(repo, ['rev-parse', 'HEAD'])
        await reply('submit', {
          summary: 'reviewed',
          description: 'body',
          showCoAuthors: true,
          skipCommitHooks: false,
          signOffCommits: false,
        })
        assert.deepEqual(contexts, [
          {
            summary: 'reviewed',
            description: 'body',
            amend: false,
            messageGeneratedByCopilot: false,
            trailers: [
              {
                token: 'Co-Authored-By',
                value: 'Author 0 <same-0@example.test>',
              },
            ],
          },
        ])
        assert.equal(await git(repo, ['rev-parse', 'HEAD']), head)
        const author = view().fields.find(field => field.id === 'author')!
          .choices![0].id
        await reply('removeAuthor', { author })
        assert.equal(
          view().fields.find(field => field.id === 'author')!.choices!.length,
          0
        )
        await reply('cancel', {})
      } finally {
        handle.dispose()
      }
    })
  }
)

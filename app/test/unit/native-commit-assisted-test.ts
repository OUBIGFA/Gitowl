import { listenOnLoopback } from '../helpers/http-server'
import assert from 'node:assert/strict'
import { after, before, describe, it, mock } from 'node:test'
import { EventEmitter } from 'events'
import { createServer } from 'http'
import { AddressInfo } from 'net'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { exec } from 'dugite'
import { ipcRenderer } from 'electron'
import type { AppRuntime } from '../../src/ui/app-runtime'
import type { NativeCommitPresenter } from '../../src/native-kernel/commit-presenter'
import type { Repository } from '../../src/models/repository'
import { getCopilotRuntimeLaunch } from '../../src/lib/copilot-runtime'

// OS-only fixture boundaries. The executable locator points at the repository's
// SDK subprocess fixture, not a mocked CopilotStore/client/planner. Tests stop at
// consent, or commit an empty selection (which never invokes any model).
mock.module('../../src/lib/copilot-runtime', {
  namedExports: {
    getCopilotRuntimeLaunch,
    getCopilotRuntimePath: () =>
      resolve('app/test/native-service/copilot-fixture-runtime.js'),
  },
})
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
const server = createServer((_req, res) => {
  res.statusCode = 404
  res.end('{}')
})
let root: string
let runtime: AppRuntime
let presenter: NativeCommitPresenter
let repo: Repository
let PopupType: typeof import('../../src/models/popup').PopupType
let operations: typeof import('../../src/native-kernel/operation-dialogs').operationDialogs
const previous = new Map<string, string | undefined>()
function env(key: string, value: string) {
  previous.set(key, process.env[key])
  process.env[key] = value
}
async function git(args: string[]) {
  const result = await exec(args, repo.path)
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout.trim()
}
async function until(predicate: () => boolean) {
  const deadline = Date.now() + 20000
  while (!predicate() && Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 10))
  }
  assert.ok(
    predicate(),
    'Original assisted state did not reach the expected boundary'
  )
}
const state = () => runtime.repositoryStateManager.get(repo)
before(async () => {
  await mkdir(resolve('_temp/native-full-ui'), { recursive: true })
  root = await mkdtemp(
    join(resolve('_temp/native-full-ui'), 'assisted-presenter-')
  )
  await listenOnLoopback(server)
  const endpoint = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
  env('DESKTOP_GITHUB_DOTCOM_API_ENDPOINT', endpoint)
  env('GITHUB_DESKTOP_PREVIEW_FEATURES', '1')
  env('COPILOT_FIXTURE_TRACE', join(root, 'sdk-trace.jsonl'))
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  runtime = (await import('../../src/ui/app-runtime')).getAppRuntime()
  runtime.appStore.onDidError(error => {
    void runtime.dispatcher.postError(error)
  })
  const { Account } = await import('../../src/models/account')
  await runtime.accountsStore.addAccount(
    new Account(
      'fixture',
      endpoint,
      'fixture-token',
      [],
      '',
      1,
      'Fixture',
      'free',
      undefined,
      true,
      ['desktop_copilot_generate_commit_message'],
      'PRO'
    )
  )
  const { Repository } = await import('../../src/models/repository')
  repo = new Repository(root, -1, null, false)
  await git(['init', '--template=', '--initial-branch=main'])
  await git(['config', 'user.name', 'Fixture'])
  await git(['config', 'user.email', 'fixture@example.test'])
  await git(['config', 'commit.gpgSign', 'false'])
  await git(['config', 'core.hooksPath', join(root, 'no-hooks')])
  await writeFile(join(root, 'file.txt'), 'base\n')
  await git(['add', '.'])
  await git(['commit', '-m', 'base'])
  ;[repo] = await runtime.dispatcher.addRepositories([root])
  await runtime.appStore._selectRepository(repo)
  await runtime.dispatcher.refreshRepository(repo)
  const { NativeCommitPresenter } = await import(
    '../../src/native-kernel/commit-presenter'
  )
  presenter = new NativeCommitPresenter(runtime)
  PopupType = (await import('../../src/models/popup')).PopupType
  operations = (await import('../../src/native-kernel/operation-dialogs'))
    .operationDialogs
})
after(async () => {
  presenter?.dispose()
  await runtime?.appStore._selectRepository(null)
  server.closeAllConnections()
  server.close()
  window.close()
  for (const [key, value] of previous) {
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
  'native assisted presenter with original coordinator and Git',
  { timeout: 120000 },
  () => {
    async function ready() {
      for (const popup of runtime.appStore.getState().allPopups) {
        runtime.dispatcher.closePopupById(popup.id!)
      }
      await runtime.dispatcher.refreshRepository(repo)
      const { isRepositoryGitMutationInProgress } = await import(
        '../../src/lib/git/repository-operation'
      )
      await until(() => !isRepositoryGitMutationInProgress(repo.path))
      runtime.dispatcher.setCommitMode(repo, 'copilot')
    }

    it('rejects preparation when the original fallback generation account signs out of a local repository', async () => {
      await writeFile(join(root, 'file.txt'), 'selected\n')
      await ready()
      const account = runtime.appStore.getState().accounts[0]
      const head = await git(['rev-parse', 'HEAD'])
      const creating = presenter.createAssisted(repo)
      const rejected = assert.rejects(creating, /changed/)
      await runtime.accountsStore.removeAccount(account)
      await rejected
      assert.equal(state().changesState.assistedCommit.kind, 'idle')
      assert.equal(await git(['rev-parse', 'HEAD']), head)
      assert.equal(runtime.appStore.getState().currentPopup, null)
      await runtime.accountsStore.addAccount(account)
    })

    it('cancels original consent without staging/committing, ignores stale consent and retains selections', async () => {
      await writeFile(join(root, 'file.txt'), 'selected\n')
      await ready()
      const head = await git(['rev-parse', 'HEAD'])
      const index = await readFile(join(root, '.git', 'index'))
      const creating = presenter.createAssisted(repo)
      await until(
        () => state().changesState.assistedCommit.kind === 'awaiting-consent'
      )
      const popup = runtime.appStore.getState().currentPopup!
      assert.equal(popup.type, PopupType.AssistedCommitDisclaimer)
      assert.equal(presenter.getState()!.assisted.canCancel, true)
      const runID = presenter.getState()!.assisted.runID!
      runtime.dispatcher.cancelCopilotAssistedCommits(repo, 'stale-run-id')
      assert.equal(state().changesState.assistedCommit.kind, 'awaiting-consent')
      runtime.dispatcher.cancelCopilotAssistedCommits(repo, runID)
      const receipt = await creating
      assert.equal(receipt.kind, 'cancelled')
      assert.equal(state().changesState.assistedCommit.kind, 'idle')
      if (popup.type !== PopupType.AssistedCommitDisclaimer) {
        assert.fail('Missing consent')
      }
      popup.onAccepted()
      assert.equal(
        localStorage.getItem('assisted-commit-disclaimer-last-seen'),
        null
      )
      assert.equal(await git(['rev-parse', 'HEAD']), head)
      assert.deepEqual(await readFile(join(root, '.git', 'index')), index)
      const { DiffSelectionType } = await import('../../src/models/diff')
      assert.equal(
        state().changesState.workingDirectory.files[0].selection.getSelectionType(),
        DiffSelectionType.All
      )
    })

    it('closes original consent as declined and preserves input rather than generating a message', async () => {
      await ready()
      const creating = presenter.createAssisted(repo)
      await until(
        () => state().changesState.assistedCommit.kind === 'awaiting-consent'
      )
      await operations.cancel(
        runtime,
        runtime.appStore.getState().currentPopup!
      )
      assert.equal((await creating).kind, 'declined')
      assert.equal(state().changesState.assistedCommit.kind, 'idle')
      assert.equal(state().changesState.commitMessage.summary, '')
    })

    it('settles an unavailable unpackaged hook executable as an original error without accepting history', async () => {
      await git(['checkout', '--', 'file.txt'])
      const hooks = join(root, '.git', 'fixture-hooks')
      await mkdir(hooks)
      await writeFile(
        join(hooks, 'pre-commit'),
        '#!/bin/sh\necho fixture-hook-rejection >&2\nexit 1\n',
        { mode: 0o755 }
      )
      await git(['config', 'core.hooksPath', hooks])
      await ready()
      runtime.dispatcher.updateCommitOptions(repo, {
        allowEmptyCommit: true,
        skipCommitHooks: false,
      })
      const head = await git(['rev-parse', 'HEAD'])
      const index = await readFile(join(root, '.git', 'index'))
      // Unit tests execute source TS, not a packaged hooks directory. Exercise
      // that real OS failure and recovery; do not pretend a HookFailed dialog ran.
      const receipt = await presenter.createAssisted(repo)
      const failure = state().changesState.assistedCommit
      assert.ok(failure.kind === 'error')
      const { assistedCommitErrorCauses } = await import(
        '../../src/lib/assisted-commit-run'
      )
      assert.ok(
        assistedCommitErrorCauses(failure.error).some(error =>
          String(error).includes('process-proxy')
        )
      )
      assert.equal(receipt.kind, 'error')
      assert.equal(await git(['rev-parse', 'HEAD']), head)
      assert.deepEqual(await readFile(join(root, '.git', 'index')), index)
      const error = state().changesState.assistedCommit
      assert.equal(error.kind, 'error')
      assert.equal(presenter.getState()!.assisted.busy, false)
      if (error.kind !== 'error') {
        assert.fail('Missing original failed-hook outcome')
      }
      assert.equal(error.retry, null)
      runtime.dispatcher.dismissCopilotAssistedCommitError(repo, error.runId)
      assert.equal(state().changesState.assistedCommit.kind, 'idle')
      await git(['config', 'core.hooksPath', join(root, 'no-hooks')])
    })

    it('accepts a real empty local commit only after explicit creation, with no planner or push', async () => {
      await git(['checkout', '--', 'file.txt'])
      await ready()
      runtime.dispatcher.updateCommitOptions(repo, {
        allowEmptyCommit: true,
        signOffCommits: true,
      })
      const count = Number(await git(['rev-list', '--count', 'HEAD']))
      const receipt = await presenter.createAssisted(repo)
      assert.equal(receipt.kind, 'local-ready')
      assert.equal(receipt.commits?.length, 1)
      assert.equal(receipt.headSHA, await git(['rev-parse', 'HEAD']))
      assert.equal(
        Number(await git(['rev-list', '--count', 'HEAD'])),
        count + 1
      )
      assert.equal(await git(['log', '-1', '--format=%s']), 'Empty commit')
      assert.match(await git(['log', '-1', '--format=%B']), /Signed-off-by:/)
      assert.equal(await git(['remote']), '')
      assert.equal(state().changesState.assistedCommit.kind, 'idle')
    })
  }
)

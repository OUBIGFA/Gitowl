import '../../src/native-service/environment'
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { spawn } from 'child_process'
import { once } from 'events'
import { createServer, request as httpRequest, Server } from 'http'
import { AddressInfo } from 'net'
import { join } from 'path'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { exec } from 'dugite'
import { Repository } from '../../src/models/repository'
import {
  RemoteOperations,
  RemoteOperationError,
  RemoteOperationErrorCode,
} from '../../src/native-service/remote-operations'
import { getRepositoryType } from '../../src/lib/git/rev-parse'
import { getStatus } from '../../src/lib/git/status'
import {
  acquireAssistedCommitGitLease,
  hasRepositoryGitOperationAccess,
  isRepositoryGitMutationInProgress,
} from '../../src/lib/git/repository-operation'
import {
  envForProxy,
  envForRemoteOperation,
} from '../../src/lib/git/environment'
import { getHTMLURL, getDotComAPIEndpoint } from '../../src/lib/api-endpoints'
import { GitLFSProgressParser } from '../../src/lib/progress/lfs'
import { HookProgress, TerminalOutput } from '../../src/lib/git/core'

// This file is bundled and executed by remote-operations-test.ts, never imported
// into the jsdom test realm. Real Node storage is the production preferences API.
assert.equal(typeof window, 'undefined')
assert.equal(typeof document, 'undefined')
assert.equal(typeof location, 'undefined')
assert.equal(process.versions.electron, undefined)
localStorage.setItem('git-hooks-env-enabled', '1')

const scratch = process.env.GITOWL_REMOTE_TEST_DIRECTORY!
const noNetworkProxy = async () => undefined

async function rawGit(path: string, args: string[]) {
  const result = await exec(args, path)
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout
}

async function configure(path: string) {
  await rawGit(path, ['config', 'user.name', 'Remote Operations Tests'])
  await rawGit(path, [
    'config',
    'user.email',
    'remote-operations@example.invalid',
  ])
  await rawGit(path, ['config', 'commit.gpgsign', 'false'])
  await rawGit(path, ['config', 'core.autocrlf', 'false'])
  await rawGit(path, ['config', 'core.hooksPath', join(path, '.git', 'hooks')])
  await rawGit(path, ['config', 'pull.rebase', 'false'])
}

async function openRepository(path: string) {
  const type = await getRepositoryType(path)
  assert.equal(type.kind, 'regular')
  assert.ok(type.kind === 'regular')
  return new Repository(
    type.topLevelWorkingDirectory,
    1,
    null,
    false,
    null,
    {},
    false,
    type.gitDir
  )
}

async function tip(path: string, ref = 'HEAD') {
  return (await rawGit(path, ['rev-parse', '--verify', ref])).trim()
}

async function commitFiles(
  path: string,
  files: Record<string, string | Buffer>,
  message: string
) {
  for (const [name, contents] of Object.entries(files)) {
    await writeFile(join(path, name), contents)
  }
  await rawGit(path, ['add', '--', ...Object.keys(files)])
  await rawGit(path, ['commit', '-m', message])
}

interface IFixture {
  readonly root: string
  readonly remote: string
  readonly source: string
  readonly repository: Repository
}

async function withFixture(run: (fixture: IFixture) => Promise<void>) {
  const root = await mkdtemp(join(scratch, 'repository-'))
  const source = join(root, 'source')
  const remote = join(root, 'remote.git')
  const local = join(root, 'local')
  try {
    await rawGit(root, ['init', '--initial-branch=main', source])
    await configure(source)
    await commitFiles(
      source,
      { 'file.txt': 'base\n', 'keep.txt': 'keep\n' },
      'Initial'
    )
    await rawGit(root, ['init', '--bare', '--initial-branch=main', remote])
    await rawGit(source, ['remote', 'add', 'origin', remote])
    await rawGit(source, ['push', '--set-upstream', 'origin', 'main'])
    await rawGit(root, ['clone', '--', remote, local])
    await configure(local)
    await run({ root, remote, source, repository: await openRepository(local) })
  } finally {
    // AbortSignal rejects execFile before Windows has closed every descendant's
    // handle. Cleanup waits for that real OS settlement, not an invented rollback.
    await rm(root, {
      recursive: true,
      force: true,
      maxRetries: 20,
      retryDelay: 50,
    })
  }
}

async function rejected(
  repository: Repository,
  run: () => Promise<unknown>,
  code: RemoteOperationErrorCode
) {
  await assert.rejects(
    async () => run(),
    (error: unknown) => {
      assert.ok(error instanceof RemoteOperationError)
      assert.equal(error.code, code, error.message)
      return true
    }
  )
  assert.equal(
    isRepositoryGitMutationInProgress(repository.path),
    false,
    'failed operation retained Git admission'
  )
  // An acquired lease proves no pending operation was left behind to drain.
  const lease = await acquireAssistedCommitGitLease(repository.path)
  lease.release()
}

async function close(server: Server) {
  await new Promise<void>((resolve, reject) => {
    server.close(error => (error ? reject(error) : resolve()))
    server.closeAllConnections()
  })
}

async function listen(server: Server) {
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`
}

/**
 * Real full Git acts as the loopback server; the locked minGit client deliberately
 * omits http-backend. This is fixture hosting, never a service execution fallback.
 */
async function gitHTTP(root: string, requireAuthentication = false) {
  let requests = 0
  const server = createServer((req, res) => {
    requests++
    const respond = async () => {
      if (requireAuthentication) {
        res.writeHead(401, {
          'WWW-Authenticate': 'Basic realm="remote-fixture"',
        })
        res.end('Fixture requires authentication')
        return
      }
      const url = new URL(req.url!, 'http://fixture.invalid')
      assert.ok(url.pathname.startsWith('/remote.git/'))
      const chunks: Buffer[] = []
      for await (const chunk of req) {
        chunks.push(Buffer.from(chunk))
      }
      const body = Buffer.concat(chunks)
      const backend = spawn('git', ['http-backend'], {
        cwd: root,
        env: {
          ...process.env,
          GIT_PROJECT_ROOT: root,
          GIT_HTTP_EXPORT_ALL: '1',
          PATH_INFO: url.pathname,
          QUERY_STRING: url.search.slice(1),
          REQUEST_METHOD: req.method!,
          CONTENT_TYPE: req.headers['content-type'] ?? '',
          CONTENT_LENGTH: `${body.length}`,
          HTTP_CONTENT_ENCODING: req.headers['content-encoding'] ?? '',
        },
      })
      const stdout: Buffer[] = []
      const stderr: Buffer[] = []
      backend.stdout.on('data', chunk => stdout.push(chunk))
      backend.stderr.on('data', chunk => stderr.push(chunk))
      backend.stdin.end(body)
      const [code] = await once(backend, 'close')
      assert.equal(code, 0, Buffer.concat(stderr).toString())
      const result = { stdout: Buffer.concat(stdout) }
      const separator = result.stdout.indexOf('\r\n\r\n')
      assert.notEqual(separator, -1)
      const headers: Record<string, string> = {}
      let status = 200
      for (const line of result.stdout
        .subarray(0, separator)
        .toString()
        .split('\r\n')) {
        const colon = line.indexOf(':')
        const key = line.slice(0, colon)
        const value = line.slice(colon + 1).trim()
        if (key.toLowerCase() === 'status') {
          status = parseInt(value, 10)
        } else {
          headers[key] = value
        }
      }
      res.writeHead(status, headers)
      res.end(result.stdout.subarray(separator + 4))
    }
    respond().catch(error => {
      log.error('Loopback Git fixture failed', error)
      res.writeHead(500)
      res.end(String(error))
    })
  })
  const origin = await listen(server)
  return {
    url: `${origin}/remote.git`,
    requests: () => requests,
    close: () => close(server),
  }
}

/** A loopback forwarding proxy, not a resolver or Git execution mock. */
async function forwardingProxy(targetURL: string) {
  let requests = 0
  const targetOrigin = new URL(targetURL).origin
  const server = createServer((req, res) => {
    requests++
    const url = new URL(req.url!)
    assert.equal(
      url.origin,
      targetOrigin,
      'fixture proxy must never contact another origin'
    )
    const upstream = httpRequest(
      url,
      { method: req.method, headers: req.headers },
      response => {
        res.writeHead(response.statusCode!, response.headers)
        response.pipe(res)
      }
    )
    upstream.on('error', error => {
      res.writeHead(502)
      res.end(String(error))
    })
    req.pipe(upstream)
  })
  const url = await listen(server)
  return { url, requests: () => requests, close: () => close(server) }
}

async function waitForFile(path: string) {
  const deadline = Date.now() + 10000
  for (;;) {
    try {
      return await readFile(path)
    } catch (error) {
      if (error.code !== 'ENOENT' || Date.now() >= deadline) {
        throw error
      }
      await new Promise(resolve => setTimeout(resolve, 20))
    }
  }
}

function outputText(output: TerminalOutput) {
  return Array.isArray(output)
    ? Buffer.concat(output).toString()
    : output.toString()
}

function hookScript(name: string, exitCode: number, readInput = false) {
  return [
    '#!/bin/sh',
    'test "$GITHUB_DESKTOP" = 1 || { echo missing-real-hook-interception >&2; exit 99; }',
    `printf "${name}\\n" >> .git/remote-hook-runs`,
    ...(readInput ? ['cat >> .git/remote-hook-input'] : []),
    `echo ${name}-policy-${exitCode === 0 ? 'accepted' : 'rejected'} >&2`,
    `exit ${exitCode}`,
    '',
  ].join('\n')
}

describe('headless remote Git operations', { timeout: 120000 }, () => {
  it('uses the original proxy policy and endpoint mapping with no DOM', async () => {
    const calls: string[] = []
    const resolver = async (url: string) => {
      calls.push(url)
      return 'http://127.0.0.1:4321'
    }
    assert.deepEqual(
      await envForProxy('https://example.invalid/repository', {}, resolver),
      { https_proxy: 'http://127.0.0.1:4321' }
    )
    assert.deepEqual(
      await envForProxy('http://example.invalid/repository', {}, resolver),
      { http_proxy: 'http://127.0.0.1:4321' }
    )
    assert.equal(
      await envForProxy(
        'https://example.invalid',
        { ALL_PROXY: 'existing' },
        resolver
      ),
      undefined
    )
    assert.equal(
      await envForProxy(
        'https://example.invalid',
        { HTTPS_PROXY: 'existing' },
        resolver
      ),
      undefined
    )
    assert.equal(
      await envForProxy('file:///fixture.git', {}, resolver),
      undefined
    )
    assert.equal(calls.length, 2)
    const env = await envForRemoteOperation(
      'https://example.invalid/repository',
      resolver
    )
    assert.equal(env.https_proxy, 'http://127.0.0.1:4321')
    assert.equal(env.GIT_TERMINAL_PROMPT, '0')
    assert.equal(getDotComAPIEndpoint(), 'https://api.github.com')
    assert.equal(getHTMLURL('https://api.github.com'), 'https://github.com')
    assert.equal(
      getHTMLURL('https://enterprise.example.invalid:8443/api/v3'),
      'https://enterprise.example.invalid:8443'
    )
    assert.equal(
      getHTMLURL('https://api.fixture.ghe.com:8443/api/v3'),
      'https://fixture.ghe.com:8443/'
    )
    const parser = new GitLFSProgressParser()
    assert.equal(
      parser.parse('download 1/1 1024/2048 payload.dat').kind,
      'progress'
    )
    localStorage.setItem('numberFormat', '|,')
    try {
      const progress = parser.parse('download 1/1 1536/2048 payload.dat')
      assert.ok(progress.kind === 'progress')
      assert.match(progress.details.text, /1,5 KiB \/ 2 KiB/)
    } finally {
      localStorage.removeItem('numberFormat')
    }
  })

  it('fetch/checkRemote updates tracking only, preserving HEAD, index and dirty/untracked files', () =>
    withFixture(async ({ source, repository }) => {
      let proxyCalls = 0
      const operations = new RemoteOperations(async () => {
        proxyCalls++
        return undefined
      })
      const oldTip = await tip(repository.path)
      await writeFile(join(repository.path, 'keep.txt'), 'staged local\n')
      await rawGit(repository.path, ['add', 'keep.txt'])
      await writeFile(join(repository.path, 'keep.txt'), 'unstaged local\n')
      await writeFile(
        join(repository.path, 'untracked.txt'),
        'untracked local\n'
      )
      const index = await readFile(join(repository.resolvedGitDir, 'index'))
      await commitFiles(source, { 'file.txt': 'upstream\n' }, 'Upstream change')
      await rawGit(source, ['push', 'origin', 'main'])
      const progress: string[] = []
      const result = await operations.checkRemote(repository, {
        progressCallback: p => {
          assert.equal(hasRepositoryGitOperationAccess(repository.path), false)
          progress.push(p.kind)
        },
      })
      assert.equal(result.remoteName, 'origin')
      assert.deepEqual(result.state.aheadBehind, { ahead: 0, behind: 1 })
      assert.equal(await tip(repository.path), oldTip)
      assert.equal(
        await tip(repository.path, 'refs/remotes/origin/main'),
        await tip(source)
      )
      assert.deepEqual(
        await readFile(join(repository.resolvedGitDir, 'index')),
        index
      )
      assert.equal(
        await readFile(join(repository.path, 'file.txt'), 'utf8'),
        'base\n'
      )
      assert.equal(
        await readFile(join(repository.path, 'keep.txt'), 'utf8'),
        'unstaged local\n'
      )
      assert.equal(
        await readFile(join(repository.path, 'untracked.txt'), 'utf8'),
        'untracked local\n'
      )
      assert.ok(progress.includes('fetch'))
      assert.equal(
        proxyCalls,
        0,
        'filesystem transfers do not resolve an HTTP proxy'
      )
    }))

  it('pull actually updates the worktree and retains non-overlapping staged/unstaged changes', () =>
    withFixture(async ({ source, repository }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      await writeFile(join(repository.path, 'keep.txt'), 'staged keep\n')
      await rawGit(repository.path, ['add', 'keep.txt'])
      await writeFile(join(repository.path, 'keep.txt'), 'unstaged keep\n')
      await writeFile(
        join(repository.path, 'untracked.txt'),
        'untracked keep\n'
      )
      const staged = await rawGit(repository.path, ['diff', '--cached'])
      await commitFiles(source, { 'file.txt': 'pulled\n' }, 'Actual pull input')
      await rawGit(source, ['push', 'origin', 'main'])
      const result = await operations.pull(repository)
      assert.equal(await tip(repository.path), await tip(source))
      assert.equal(
        await readFile(join(repository.path, 'file.txt'), 'utf8'),
        'pulled\n'
      )
      assert.equal(
        await readFile(join(repository.path, 'keep.txt'), 'utf8'),
        'unstaged keep\n'
      )
      assert.equal(
        await readFile(join(repository.path, 'untracked.txt'), 'utf8'),
        'untracked keep\n'
      )
      assert.equal(await rawGit(repository.path, ['diff', '--cached']), staged)
      assert.deepEqual(result.state.aheadBehind, { ahead: 0, behind: 0 })
      assert.equal(
        await rawGit(repository.path, ['for-each-ref', 'refs/stash']),
        ''
      )
    }))

  for (const rebase of [false, true]) {
    it(`keeps overlapping local changes and refuses configured auto-stash (rebase ${rebase})`, () =>
      withFixture(async ({ source, repository }) => {
        const operations = new RemoteOperations(noNetworkProxy)
        await rawGit(repository.path, ['config', 'pull.rebase', `${rebase}`])
        await rawGit(repository.path, ['config', 'rebase.autoStash', 'true'])
        await rawGit(repository.path, ['config', 'merge.autoStash', 'true'])
        const head = await tip(repository.path)
        await writeFile(join(repository.path, 'file.txt'), 'local unfinished\n')
        await writeFile(
          join(repository.path, 'untracked.txt'),
          'keep new file\n'
        )
        await commitFiles(
          source,
          { 'file.txt': 'remote version\n' },
          'Conflicting worktree input'
        )
        await rawGit(source, ['push', 'origin', 'main'])
        await rejected(
          repository,
          () => operations.pull(repository),
          'local-changes'
        )
        assert.equal(await tip(repository.path), head)
        assert.equal(
          await readFile(join(repository.path, 'file.txt'), 'utf8'),
          'local unfinished\n'
        )
        assert.equal(
          await readFile(join(repository.path, 'untracked.txt'), 'utf8'),
          'keep new file\n'
        )
        assert.equal(
          await rawGit(repository.path, ['for-each-ref', 'refs/stash']),
          ''
        )
        assert.equal(
          (await getStatus(repository, true, true)).doConflictedFilesExist,
          false
        )
        await operations.fetch(repository)
      }))
  }

  it('ordinary push updates only its tracked branch, never force/mirror/tags, and leaves local edits intact', () =>
    withFixture(async ({ remote, source, repository }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      const initial = await tip(remote, 'refs/heads/main')
      await rawGit(remote, ['branch', 'remote-only', initial])
      await commitFiles(
        repository.path,
        { 'pushed.txt': 'ordinary\n' },
        'Local commit'
      )
      await rawGit(repository.path, ['branch', 'not-pushed'])
      await rawGit(repository.path, [
        'tag',
        '-a',
        'not-pushed-tag',
        '-m',
        'Do not push tags',
      ])
      await rawGit(repository.path, ['config', 'push.followTags', 'true'])
      await rawGit(repository.path, ['config', 'remote.origin.mirror', 'true'])
      await rawGit(repository.path, [
        'config',
        'remote.origin.push',
        '+refs/heads/*:refs/heads/*',
      ])
      await writeFile(
        join(repository.path, 'keep.txt'),
        'unfinished local edit\n'
      )
      const index = await readFile(join(repository.resolvedGitDir, 'index'))
      const progress: string[] = []
      await operations.push(repository, {
        progressCallback: p => progress.push(p.kind),
      })
      assert.equal(
        await tip(remote, 'refs/heads/main'),
        await tip(repository.path)
      )
      assert.equal(await tip(remote, 'refs/heads/remote-only'), initial)
      assert.equal(
        (await exec(['rev-parse', '--verify', 'refs/heads/not-pushed'], remote))
          .exitCode,
        128
      )
      assert.equal(
        (
          await exec(
            ['rev-parse', '--verify', 'refs/tags/not-pushed-tag'],
            remote
          )
        ).exitCode,
        128
      )
      assert.equal(
        await readFile(join(repository.path, 'keep.txt'), 'utf8'),
        'unfinished local edit\n'
      )
      assert.deepEqual(
        await readFile(join(repository.resolvedGitDir, 'index')),
        index
      )
      assert.ok(progress.includes('push'))
      await rawGit(source, ['pull', '--ff-only'])
      await commitFiles(
        source,
        { 'remote-new.txt': 'new remote\n' },
        'Remote advance'
      )
      await rawGit(source, ['push', 'origin', 'main'])
      await commitFiles(
        repository.path,
        { 'local-new.txt': 'new local\n' },
        'Local divergence'
      )
      const remoteTip = await tip(remote, 'refs/heads/main')
      await rejected(
        repository,
        () => operations.push(repository),
        'non-fast-forward'
      )
      assert.equal(await tip(remote, 'refs/heads/main'), remoteTip)
      assert.equal(
        await readFile(join(repository.path, 'keep.txt'), 'utf8'),
        'unfinished local edit\n'
      )
    }))

  it('resolves remote names/config again and supports slash remotes and an unambiguous full local ref', () =>
    withFixture(async ({ source, repository, remote }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      assert.equal(
        (await operations.getRemoteInfo(repository)).currentRemoteName,
        'origin'
      )
      await rawGit(repository.path, [
        'remote',
        'rename',
        'origin',
        'team/upstream',
      ])
      await rawGit(repository.path, ['checkout', '-b', 'heads/local'])
      await rawGit(repository.path, [
        'branch',
        '--set-upstream-to=team/upstream/main',
      ])
      await commitFiles(
        repository.path,
        { 'named.txt': 'full ref\n' },
        'Push explicitly resolved ref'
      )
      await operations.push(repository)
      assert.equal(
        await tip(remote, 'refs/heads/main'),
        await tip(repository.path)
      )
      assert.equal(
        (await exec(['rev-parse', '--verify', 'refs/heads/local'], remote))
          .exitCode,
        128
      )
      const info = await operations.getRemoteInfo(repository)
      assert.equal(info.currentRemoteName, 'team/upstream')
      assert.equal(info.currentBranch, 'heads/local')
      await rejected(
        repository,
        () => operations.fetch(repository, { remoteName: 'origin' }),
        'remote-not-found'
      )
      await rawGit(source, ['pull', '--ff-only'])
    }))

  it('returns credential-free information and rejects missing/invalid remotes and unsupported options', () =>
    withFixture(async ({ repository }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      await rawGit(repository.path, [
        'remote',
        'add',
        'private',
        'https://fixture-user:fixture-secret@example.invalid/repo.git?token=fixture-query-secret',
      ])
      const info = await operations.getRemoteInfo(repository)
      assert.deepEqual(info.remotes, [{ name: 'origin' }, { name: 'private' }])
      assert.doesNotMatch(
        JSON.stringify(info),
        /fixture-user|fixture-secret|fixture-query-secret|https:/
      )
      await rawGit(repository.path, [
        'config',
        'transfer.credentialsInUrl',
        'die',
      ])
      await assert.rejects(
        operations.fetch(repository, { remoteName: 'private' }),
        (error: unknown) => {
          assert.ok(error instanceof RemoteOperationError)
          assert.doesNotMatch(
            JSON.stringify(error) + error.message,
            /fixture-user|fixture-secret|fixture-query-secret/
          )
          return true
        }
      )
      for (const remoteName of [
        '',
        '--upload-pack=bad',
        '-unsafe',
        'bad\0name',
        'bad name',
        'bad\nname',
      ]) {
        await rejected(
          repository,
          () => operations.fetch(repository, { remoteName }),
          'invalid-remote'
        )
      }
      await rejected(
        repository,
        () => operations.fetch(repository, { remoteName: 'missing' }),
        'remote-not-found'
      )
      for (const option of [
        { command: 'fetch --all' },
        { url: '/arbitrary' },
        { forceWithLease: true },
        { force: true },
        { mirror: true },
        { tags: ['tag'] },
        { noVerify: true },
        { remoteBranch: '+refs/heads/main' },
      ]) {
        await rejected(
          repository,
          () => operations.push(repository, option as any),
          'invalid-options'
        )
      }
      // Keep transfer.credentialsInUrl=die intact, including during fixture cleanup.
      await rawGit(repository.path, [
        'config',
        '--remove-section',
        'remote.private',
      ])
      await rawGit(repository.path, ['remote', 'remove', 'origin'])
      assert.deepEqual((await operations.getRemoteInfo(repository)).remotes, [])
      await rejected(
        repository,
        () => operations.checkRemote(repository),
        'no-remotes'
      )
      await rejected(
        repository,
        () => operations.pull(repository),
        'no-remotes'
      )
      await rejected(
        repository,
        () => operations.push(repository),
        'no-remotes'
      )
    }))

  it('requires tracking for pull/push and an explicitly matching selected remote', () =>
    withFixture(async ({ repository, remote }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      await rawGit(repository.path, ['remote', 'add', 'other', remote])
      await rejected(
        repository,
        () => operations.pull(repository, { remoteName: 'other' }),
        'remote-mismatch'
      )
      await rejected(
        repository,
        () => operations.push(repository, { remoteName: 'other' }),
        'remote-mismatch'
      )
      await rawGit(repository.path, ['branch', '--unset-upstream'])
      await operations.fetch(repository)
      await rejected(
        repository,
        () => operations.pull(repository),
        'no-tracking'
      )
      await rejected(
        repository,
        () => operations.push(repository, { remoteName: 'origin' }),
        'no-tracking'
      )
      await rawGit(repository.path, ['checkout', '--detach'])
      await rejected(
        repository,
        () => operations.pull(repository),
        'detached-head'
      )
      await rejected(
        repository,
        () => operations.push(repository),
        'detached-head'
      )
      await rawGit(repository.path, ['checkout', '--orphan', 'unborn'])
      await rejected(
        repository,
        () => operations.pull(repository),
        'unborn-branch'
      )
      await rejected(
        repository,
        () => operations.push(repository),
        'unborn-branch'
      )
    }))

  it('leaves newly produced conflicts and rejects an existing operation, including resolved conflicts', () =>
    withFixture(async ({ source, repository }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      await commitFiles(
        repository.path,
        { 'file.txt': 'local commit\n' },
        'Local conflicting commit'
      )
      await commitFiles(
        source,
        { 'file.txt': 'remote commit\n' },
        'Remote conflicting commit'
      )
      await rawGit(source, ['push', 'origin', 'main'])
      const localTip = await tip(repository.path)
      await rejected(repository, () => operations.pull(repository), 'conflicts')
      assert.equal(await tip(repository.path), localTip)
      const conflict = await readFile(join(repository.path, 'file.txt'))
      assert.match(conflict.toString(), /<<<<<<< HEAD/)
      for (const run of [
        () => operations.fetch(repository),
        () => operations.pull(repository),
        () => operations.push(repository),
      ]) {
        await rejected(repository, run, 'conflicts')
      }
      assert.deepEqual(
        await readFile(join(repository.path, 'file.txt')),
        conflict
      )
      await writeFile(
        join(repository.path, 'file.txt'),
        'resolved but not continued\n'
      )
      await rawGit(repository.path, ['add', 'file.txt'])
      const index = await readFile(join(repository.resolvedGitDir, 'index'))
      await rejected(
        repository,
        () => operations.pull(repository),
        'operation-in-progress'
      )
      assert.deepEqual(
        await readFile(join(repository.resolvedGitDir, 'index')),
        index
      )
      assert.equal(
        (await getStatus(repository, true, true)).mergeHeadFound,
        true
      )
      assert.equal(
        await rawGit(repository.path, ['for-each-ref', 'refs/stash']),
        ''
      )
    }))

  it('releases network admission after Git failure and respects a shared Git lease', () =>
    withFixture(async ({ root, repository }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      const remote = await rawGit(repository.path, [
        'remote',
        'get-url',
        'origin',
      ])
      await rawGit(repository.path, [
        'remote',
        'set-url',
        'origin',
        join(root, 'does-not-exist.git'),
      ])
      await rejected(
        repository,
        () => operations.fetch(repository),
        'git-failed'
      )
      await rawGit(repository.path, [
        'remote',
        'set-url',
        'origin',
        remote.trimEnd(),
      ])
      const lease = await acquireAssistedCommitGitLease(repository.path)
      try {
        await assert.rejects(operations.fetch(repository), { code: 'busy' })
      } finally {
        lease.release()
      }
      const first = operations.fetch(repository)
      await assert.rejects(operations.pull(repository), { code: 'busy' })
      await first
      await operations.fetch(repository)
      assert.equal(isRepositoryGitMutationInProgress(repository.path), false)
    }))

  it('uses real pre-push interception, reports output/failure once, and does not ignore or retry it', () =>
    withFixture(async ({ remote, repository }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      await commitFiles(
        repository.path,
        { 'pushed.txt': 'hook input\n' },
        'Hook checked push'
      )
      const remoteTip = await tip(remote, 'refs/heads/main')
      const hook = join(repository.resolvedGitDir, 'hooks', 'pre-push')
      await writeFile(hook, hookScript('pre-push', 1, true), { mode: 0o755 })
      const progress: HookProgress[] = []
      const failures: string[] = []
      let terminal = ''
      await rejected(
        repository,
        () =>
          operations.push(repository, {
            onHookProgress: p => {
              assert.equal(
                hasRepositoryGitOperationAccess(repository.path),
                false
              )
              progress.push(p)
            },
            onHookFailure: async (name, output) => {
              assert.equal(name, 'pre-push')
              assert.equal(
                hasRepositoryGitOperationAccess(repository.path),
                false
              )
              failures.push(outputText(output))
              return 'abort'
            },
            onTerminalOutputAvailable: subscribe => {
              subscribe(chunk => (terminal += outputText(chunk)))
            },
          }),
        'git-failed'
      )
      assert.equal(await tip(remote, 'refs/heads/main'), remoteTip)
      assert.deepEqual(
        progress.map(p => p.status),
        ['started', 'failed']
      )
      assert.equal(failures.length, 1)
      assert.match(failures[0], /pre-push-policy-rejected/)
      assert.match(terminal, /pre-push-policy-rejected/)
      assert.doesNotMatch(terminal, /failure ignored/)
      assert.equal(
        await readFile(
          join(repository.resolvedGitDir, 'remote-hook-runs'),
          'utf8'
        ),
        'pre-push\n'
      )
      assert.match(
        await readFile(
          join(repository.resolvedGitDir, 'remote-hook-input'),
          'utf8'
        ),
        /refs\/heads\/main/
      )
      await writeFile(hook, hookScript('pre-push', 0, true))
      await operations.push(repository)
      assert.equal(
        await tip(remote, 'refs/heads/main'),
        await tip(repository.path)
      )
      assert.equal(
        await readFile(
          join(repository.resolvedGitDir, 'remote-hook-runs'),
          'utf8'
        ),
        'pre-push\npre-push\n'
      )
    }))

  it('forwards pull hook callbacks and leaves rejected merge state for manual recovery', () =>
    withFixture(async ({ source, repository }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      await commitFiles(
        repository.path,
        { 'local.txt': 'local\n' },
        'Local merge input'
      )
      await commitFiles(
        source,
        { 'upstream.txt': 'upstream\n' },
        'Remote merge input'
      )
      await rawGit(source, ['push', 'origin', 'main'])
      const head = await tip(repository.path)
      const hook = join(repository.resolvedGitDir, 'hooks', 'pre-merge-commit')
      await writeFile(hook, hookScript('pre-merge-commit', 1), { mode: 0o755 })
      const progress: string[] = []
      const failures: string[] = []
      await rejected(
        repository,
        () =>
          operations.pull(repository, {
            onHookProgress: p => progress.push(`${p.hookName}:${p.status}`),
            onHookFailure: async (name, output) => {
              failures.push(`${name}:${outputText(output)}`)
              return 'abort'
            },
          }),
        'git-failed'
      )
      assert.equal(await tip(repository.path), head)
      assert.deepEqual(progress, [
        'pre-merge-commit:started',
        'pre-merge-commit:failed',
      ])
      assert.equal(failures.length, 1)
      assert.match(failures[0], /pre-merge-commit-policy-rejected/)
      assert.equal(
        (await getStatus(repository, true, true)).mergeHeadFound,
        true
      )
      await rejected(
        repository,
        () => operations.pull(repository),
        'operation-in-progress'
      )
      // Recovery belongs to the fixture/user, not to RemoteOperations.
      await rawGit(repository.path, ['merge', '--abort'])
      await writeFile(hook, hookScript('pre-merge-commit', 0))
      await operations.pull(repository)
      assert.equal(
        await readFile(join(repository.path, 'upstream.txt'), 'utf8'),
        'upstream\n'
      )
    }))

  it('injects the real proxy resolver through fetch/pull/push, using Git-rewritten push URLs', () =>
    withFixture(async ({ root, remote, source, repository }) => {
      await rawGit(remote, ['config', 'http.receivepack', 'true'])
      const server = await gitHTTP(root)
      const proxy = await forwardingProxy(server.url)
      try {
        const calls: string[] = []
        const operations = new RemoteOperations(async url => {
          calls.push(url)
          return proxy.url
        })
        await rawGit(repository.path, [
          'remote',
          'set-url',
          'origin',
          server.url,
        ])
        await commitFiles(
          source,
          { 'file.txt': 'HTTP remote\n' },
          'HTTP pull input'
        )
        await rawGit(source, ['push', 'origin', 'main'])
        const head = await tip(repository.path)
        await operations.fetch(repository)
        assert.equal(await tip(repository.path), head)
        await operations.pull(repository)
        assert.equal(
          await readFile(join(repository.path, 'file.txt'), 'utf8'),
          'HTTP remote\n'
        )
        await commitFiles(
          repository.path,
          { 'http-push.txt': 'HTTP push\n' },
          'HTTP push input'
        )
        // A distinct fetch origin must not be used to resolve the push proxy.
        // Git expands pushInsteadOf itself; no credentials or URLs come from UI.
        await rawGit(repository.path, [
          'config',
          'remote.origin.pushurl',
          'fixture-push:remote.git',
        ])
        await rawGit(repository.path, [
          'config',
          `url.${server.url}.insteadOf`,
          'fixture-push:remote.git',
        ])
        await operations.push(repository)
        assert.equal(
          await tip(remote, 'refs/heads/main'),
          await tip(repository.path)
        )
        assert.deepEqual(calls, [server.url, server.url, server.url])
        assert.ok(proxy.requests() >= 3)
        assert.ok(server.requests() >= 3)
      } finally {
        await proxy.close()
        await server.close()
      }
    }))

  it('does not bypass credential.helper protection when a loopback remote requires authentication', () =>
    withFixture(async ({ root, repository }) => {
      const server = await gitHTTP(root, true)
      try {
        const operations = new RemoteOperations(noNetworkProxy)
        await rawGit(repository.path, [
          'remote',
          'set-url',
          'origin',
          server.url,
        ])
        await rawGit(repository.path, [
          'config',
          'credential.helper',
          '!printf called > helper-was-called; printf "username=fixture\\npassword=fixture\\n"',
        ])
        await assert.rejects(operations.fetch(repository), RemoteOperationError)
        assert.ok(server.requests() > 0)
        await assert.rejects(
          readFile(join(repository.path, 'helper-was-called')),
          { code: 'ENOENT' }
        )
        assert.equal(
          await rawGit(repository.path, ['remote', 'get-url', 'origin']),
          `${server.url}\n`
        )
        assert.equal(isRepositoryGitMutationInProgress(repository.path), false)
      } finally {
        await server.close()
      }
    }))

  it('cancels a real stalled fetch via Git.options.signal and releases its operation scope', () =>
    withFixture(async ({ repository }) => {
      let entered: () => void = () => {}
      const started = new Promise<void>(resolve => {
        entered = resolve
      })
      const server = createServer(() => entered())
      const url = await listen(server)
      const operations = new RemoteOperations(noNetworkProxy)
      const head = await tip(repository.path)
      try {
        await rawGit(repository.path, [
          'remote',
          'set-url',
          'origin',
          `${url}/remote.git`,
        ])
        const controller = new AbortController()
        const pending = operations.fetch(repository, {
          signal: controller.signal,
        })
        await started
        controller.abort()
        await rejected(
          repository,
          async () => {
            try {
              await pending
            } catch (error) {
              assert.ok(error instanceof RemoteOperationError)
              assert.equal(error.outcome, 'unknown')
              assert.match(error.message, /may already have changed/)
              throw error
            }
          },
          'cancelled'
        )
        assert.equal(await tip(repository.path), head)
        assert.equal(
          await readFile(join(repository.path, 'file.txt'), 'utf8'),
          'base\n'
        )
      } finally {
        await close(server)
      }
    }))

  it('reports cancelled pull as unknown even when its worktree/HEAD have already updated', () =>
    withFixture(async ({ source, repository }) => {
      await commitFiles(
        source,
        { 'file.txt': 'written before cancel\n' },
        'Cancellation pull input'
      )
      await rawGit(source, ['push', 'origin', 'main'])
      const hook = join(repository.resolvedGitDir, 'hooks', 'post-merge')
      await writeFile(hook, hookScript('post-merge', 0), { mode: 0o755 })
      const controller = new AbortController()
      const operations = new RemoteOperations(noNetworkProxy)
      await rejected(
        repository,
        async () => {
          try {
            await operations.pull(repository, {
              signal: controller.signal,
              onHookProgress: progress => {
                if (
                  progress.hookName === 'post-merge' &&
                  progress.status === 'started'
                ) {
                  controller.abort()
                }
              },
            })
          } catch (error) {
            assert.ok(error instanceof RemoteOperationError)
            assert.equal(error.outcome, 'unknown')
            throw error
          }
        },
        'cancelled'
      )
      assert.equal(controller.signal.aborted, true)
      assert.equal(await tip(repository.path), await tip(source))
      assert.equal(
        await readFile(join(repository.path, 'file.txt'), 'utf8'),
        'written before cancel\n'
      )
    }))

  it('reports cancelled push as unknown after the bare remote has accepted its branch update', () =>
    withFixture(async ({ remote, repository }) => {
      await commitFiles(
        repository.path,
        { 'accepted.txt': 'remote has this\n' },
        'Cancellation push input'
      )
      const hook = join(remote, 'hooks', 'post-receive')
      await writeFile(
        hook,
        [
          '#!/bin/sh',
          'echo cancel-after-remote-update >&2',
          'while test ! -f remote-cancel-release; do sleep 0.05; done',
          'printf complete > remote-cancel-complete',
          '',
        ].join('\n'),
        { mode: 0o755 }
      )
      const controller = new AbortController()
      const operations = new RemoteOperations(noNetworkProxy)
      try {
        await rejected(
          repository,
          async () => {
            try {
              await operations.push(repository, {
                signal: controller.signal,
                progressCallback: progress => {
                  if (
                    progress.description?.includes('cancel-after-remote-update')
                  ) {
                    controller.abort()
                  }
                },
              })
            } catch (error) {
              assert.ok(error instanceof RemoteOperationError)
              assert.equal(error.outcome, 'unknown')
              throw error
            }
          },
          'cancelled'
        )
        assert.equal(controller.signal.aborted, true)
        assert.equal(
          await tip(remote, 'refs/heads/main'),
          await tip(repository.path)
        )
      } finally {
        // The remote owns its post-receive hook, not the cancelled local Git.
        await writeFile(join(remote, 'remote-cancel-release'), '')
        await waitForFile(join(remote, 'remote-cancel-complete'))
      }
    }))

  it('rejects multiple push URLs instead of routing a second destination through the first proxy', () =>
    withFixture(async ({ repository, remote }) => {
      const operations = new RemoteOperations(noNetworkProxy)
      await rawGit(repository.path, [
        'remote',
        'set-url',
        '--add',
        '--push',
        'origin',
        remote,
      ])
      await rawGit(repository.path, [
        'remote',
        'set-url',
        '--add',
        '--push',
        'origin',
        `${remote}-other`,
      ])
      await rejected(
        repository,
        () => operations.push(repository),
        'unsupported-push-urls'
      )
    }))

  it('uses resolved linked-worktree metadata for pull and admission checks', () =>
    withFixture(async ({ root, source, repository }) => {
      const linked = join(root, 'linked')
      await rawGit(repository.path, [
        'worktree',
        'add',
        '-b',
        'linked',
        linked,
        'origin/main',
      ])
      await configure(linked)
      // configure's normal hooks path is worktree-local; this fixture has no hooks.
      const worktree = await openRepository(linked)
      const operations = new RemoteOperations(noNetworkProxy)
      const original = await tip(repository.path)
      await commitFiles(
        source,
        { 'file.txt': 'linked pull\n' },
        'Linked pull input'
      )
      await rawGit(source, ['push', 'origin', 'main'])
      await operations.pull(worktree)
      assert.equal(await tip(worktree.path), await tip(source))
      assert.equal(await tip(repository.path), original)
      assert.equal(
        await readFile(join(worktree.path, 'file.txt'), 'utf8'),
        'linked pull\n'
      )
      await mkdir(join(worktree.resolvedGitDir, 'rebase-apply'))
      await rejected(
        worktree,
        () => operations.pull(worktree),
        'operation-in-progress'
      )
    }))

  for (const recurseSubmodules of [false, true]) {
    it(`retains original populated-submodule fetch/pull behavior (recursion ${recurseSubmodules})`, () =>
      withFixture(async ({ root, source, repository }) => {
        const moduleSource = join(root, 'module-source')
        const moduleRemote = join(root, 'module.git')
        await rawGit(root, ['init', '--initial-branch=main', moduleSource])
        await configure(moduleSource)
        await commitFiles(
          moduleSource,
          { 'child.txt': 'child base\n' },
          'Child initial'
        )
        await rawGit(root, [
          'clone',
          '--bare',
          '--',
          moduleSource,
          moduleRemote,
        ])
        await rawGit(moduleSource, ['remote', 'add', 'origin', moduleRemote])
        await rawGit(moduleSource, ['push', '--set-upstream', 'origin', 'main'])
        await rawGit(source, ['submodule', 'add', '--', moduleRemote, 'module'])
        await rawGit(source, ['commit', '-am', 'Add module'])
        await rawGit(source, ['push', 'origin', 'main'])
        await rawGit(repository.path, ['pull', '--ff-only'])
        await rawGit(repository.path, ['submodule', 'update', '--init'])
        const module = join(repository.path, 'module')
        const originalParent = await tip(repository.path)
        const originalChild = await tip(module)
        await commitFiles(
          moduleSource,
          { 'child.txt': 'child remote\n' },
          'Child advance'
        )
        await rawGit(moduleSource, ['push', 'origin', 'main'])
        await rawGit(join(source, 'module'), ['pull', '--ff-only'])
        await rawGit(source, ['commit', '-am', 'Advance module link'])
        await rawGit(source, ['push', 'origin', 'main'])
        const operations = new RemoteOperations(noNetworkProxy)
        await operations.fetch(repository, { recurseSubmodules })
        assert.equal(await tip(repository.path), originalParent)
        assert.equal(await tip(module), originalChild)
        assert.equal(
          await readFile(join(module, 'child.txt'), 'utf8'),
          'child base\n'
        )
        assert.equal(
          await tip(module, 'refs/remotes/origin/main'),
          recurseSubmodules ? await tip(moduleSource) : originalChild
        )
        await operations.pull(repository, { recurseSubmodules })
        assert.equal(await tip(repository.path), await tip(source))
        assert.equal(
          await tip(module),
          recurseSubmodules ? await tip(moduleSource) : originalChild
        )
        assert.equal(
          await readFile(join(module, 'child.txt'), 'utf8'),
          recurseSubmodules ? 'child remote\n' : 'child base\n'
        )
      }))
  }

  it('uses real Git LFS filters/transfers and progress through pull and ordinary push', () =>
    withFixture(async ({ source, repository }) => {
      await rawGit(source, ['lfs', 'install', '--local'])
      await rawGit(repository.path, ['lfs', 'install', '--local'])
      const initial = Buffer.alloc(1024 * 1024, 0x61)
      await commitFiles(
        source,
        {
          '.gitattributes': '*.dat filter=lfs diff=lfs merge=lfs -text\n',
          'payload.dat': initial,
        },
        'LFS initial'
      )
      await rawGit(source, ['push', 'origin', 'main'])
      const operations = new RemoteOperations(noNetworkProxy)
      const progress: string[] = []
      await operations.fetch(repository, {
        progressCallback: p => progress.push(p.kind),
      })
      await assert.rejects(readFile(join(repository.path, 'payload.dat')), {
        code: 'ENOENT',
      })
      await operations.pull(repository, {
        progressCallback: p => progress.push(p.kind),
      })
      assert.deepEqual(
        await readFile(join(repository.path, 'payload.dat')),
        initial
      )
      assert.match(
        await rawGit(repository.path, ['show', 'HEAD:payload.dat']),
        /^version https:\/\/git-lfs.github.com\/spec\/v1\n/
      )
      const pushed = Buffer.alloc(1024 * 1024, 0x62)
      await commitFiles(
        repository.path,
        { 'payload.dat': pushed },
        'LFS ordinary push'
      )
      await operations.push(repository, {
        progressCallback: p => progress.push(p.kind),
      })
      await operations.pull(await openRepository(source))
      assert.deepEqual(await readFile(join(source, 'payload.dat')), pushed)
      assert.ok(
        progress.includes('fetch') &&
          progress.includes('pull') &&
          progress.includes('push')
      )
    }))
})

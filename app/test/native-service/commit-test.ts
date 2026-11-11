import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { spawn } from 'child_process'
import { once } from 'events'
import { createInterface } from 'readline'
import { join, resolve } from 'path'
import {
  cp,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  symlink,
  writeFile,
} from 'fs/promises'
import { exec, resolveGitDir } from 'dugite'

type Response<T = unknown> = {
  id: number | null
  result?: T
  error?: { code: number; message: string }
}
type Request = <T = unknown>(
  method: string,
  params?: object
) => Promise<Response<T>>
type Status = {
  conflicted: boolean
  operation: string | null
  files: ReadonlyArray<{ path: string; status: string }>
}

async function git(path: string, args: string[]) {
  const result = await exec(args, path)
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout
}

async function withRepository(
  run: (path: string, request: Request) => Promise<void>
) {
  const scratch = resolve('_temp/native-service-tests')
  await mkdir(scratch, { recursive: true })
  const path = await mkdtemp(join(scratch, 'commit-'))
  try {
    await git(scratch, ['init', '--initial-branch=main', path])
    await git(path, ['config', 'user.name', 'Native Service Tests'])
    await git(path, ['config', 'user.email', 'native-service@example.invalid'])
    await git(path, ['config', 'commit.gpgsign', 'false'])
    await git(path, ['config', 'core.autocrlf', 'false'])
    // Isolate user hooks while exercising the fixture's real hooks. No hook is
    // disabled or bypassed; the production interceptor runs them below.
    await git(path, ['config', 'core.hooksPath', join(path, '.git', 'hooks')])

    const runtime = join(path, '.git', 'native-service-runtime')
    await cp(resolve('out/native-service'), runtime, { recursive: true })
    const ext = process.platform === 'win32' ? '.exe' : ''
    for (const asset of [
      `process-proxy${ext}`,
      `printenvz${ext}`,
      'desktop-trampoline',
    ]) {
      await cp(resolve('out', asset), join(runtime, asset), { recursive: true })
    }
    // Hook execution resolves packaged Git relative to the bundle. This links
    // the real locked Git payload, rather than replacing resolution with a mock.
    await symlink(
      resolveGitDir(),
      join(runtime, 'git'),
      process.platform === 'win32' ? 'junction' : 'dir'
    )
    const globalConfig = join(path, '.git', 'native-service-global-config')
    await writeFile(globalConfig, '')
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      LOCAL_GIT_DIRECTORY: resolveGitDir(),
      GIT_CONFIG_GLOBAL: globalConfig,
    }
    // The service must resolve effective repository identities itself instead
    // of inheriting the fixture-creation identities from script/test.mjs.
    for (const key of [
      'GIT_AUTHOR_NAME',
      'GIT_AUTHOR_EMAIL',
      'GIT_COMMITTER_NAME',
      'GIT_COMMITTER_EMAIL',
    ]) {
      delete env[key]
    }
    // This is ordinary Node with native persistent preferences, not a test
    // realm. Enable real hook interception even for a production-channel build.
    const child = spawn(
      process.execPath,
      [
        '--experimental-webstorage',
        `--localstorage-file=${join(
          path,
          '.git',
          'native-service-preferences'
        )}`,
        '-e',
        "localStorage.setItem('git-hooks-env-enabled', '1'); require(process.argv[1])",
        join(runtime, 'service.js'),
      ],
      { cwd: path, env, stdio: 'pipe' }
    )
    const exited = once(child, 'close')
    const lines = createInterface({ input: child.stdout })
    const iterator = lines[Symbol.asyncIterator]()
    let errors = ''
    child.stderr.on('data', chunk => {
      errors += chunk.toString()
    })
    let id = 0
    const request: Request = async <T>(method: string, params: object = {}) => {
      const requestID = ++id
      child.stdin.write(
        JSON.stringify({ jsonrpc: '2.0', id: requestID, method, params }) + '\n'
      )
      let timer: NodeJS.Timeout | undefined
      const response = await Promise.race([
        iterator.next(),
        new Promise<never>((_, reject) => {
          timer = setTimeout(
            () =>
              reject(
                new Error(`Service request timed out: ${method}\n${errors}`)
              ),
            15000
          )
        }),
      ]).finally(() => clearTimeout(timer))
      assert.equal(response.done, false, errors)
      const parsed = JSON.parse(response.value!) as Response<T>
      assert.equal(parsed.id, requestID)
      return parsed
    }
    try {
      await run(path, request)
    } finally {
      // Windows holds the working directory and persistent preferences open.
      // Close the service and all stdio before removing the fixture or junction.
      child.stdin.end()
      const killTimer = setTimeout(() => child.kill(), 5000)
      try {
        await exited
      } finally {
        clearTimeout(killTimer)
        lines.close()
      }
    }
    assert.equal(child.exitCode, 0, errors)
  } finally {
    await rm(path, { recursive: true, force: true })
  }
}

async function open(path: string, request: Request) {
  const opened = await request<{ id: number }>('repository.open', { path })
  assert.equal(opened.error, undefined)
  assert.ok(opened.result)
  return opened.result.id
}

async function seed(path: string, files: Record<string, string>) {
  for (const [name, contents] of Object.entries(files)) {
    await writeFile(join(path, name), contents)
  }
  await git(path, ['add', '.'])
  await git(path, ['commit', '-m', 'initial'])
}

async function diverge(path: string) {
  await seed(path, { 'file.txt': 'base\n' })
  await git(path, ['branch', 'topic'])
  await writeFile(join(path, 'file.txt'), 'main\n')
  await git(path, ['commit', '-am', 'main change'])
  await git(path, ['checkout', 'topic'])
  await writeFile(join(path, 'file.txt'), 'topic\n')
  await git(path, ['commit', '-am', 'topic change'])
}

async function rejectWithoutMutation(
  path: string,
  request: Request,
  params: object,
  code: number
) {
  const head = await git(path, ['rev-parse', 'HEAD'])
  const index = await readFile(join(path, '.git', 'index'))
  const response = await request('repository.commit', params)
  assert.equal(response.error?.code, code, JSON.stringify(response))
  assert.equal(response.result, undefined)
  assert.equal(await git(path, ['rev-parse', 'HEAD']), head)
  assert.deepEqual(await readFile(join(path, '.git', 'index')), index)
  return response
}

describe('native selected commits in plain Node', { timeout: 60000 }, () => {
  it('commits whole selected files, clears unrelated staging and preserves unselected work', () =>
    withRepository(async (path, request) => {
      const selected = '中文 name.txt'
      await seed(path, { [selected]: 'before\n', 'unselected.txt': 'before\n' })
      await writeFile(join(path, selected), 'staged selection\n')
      await writeFile(join(path, 'unselected.txt'), 'unselected edit\n')
      await git(path, ['add', '--', selected, 'unselected.txt'])
      await writeFile(
        join(path, selected),
        'current selection\nall selected lines\n'
      )
      await writeFile(join(path, 'untracked.txt'), 'unselected new file\n')
      const id = await open(path, request)
      const info = await request<{ capabilities: string[] }>('system.info')
      assert.ok(info.result?.capabilities.includes('repository.commit'))
      const committed = await request<{ sha: string }>('repository.commit', {
        id,
        paths: [selected],
        summary: 'Selected files',
        description: 'Description\n# preserved commentary',
      })
      assert.equal(committed.error, undefined)
      assert.ok(committed.result)
      assert.deepEqual(Object.keys(committed.result), ['sha'])
      assert.match(committed.result.sha, /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/)
      assert.equal(
        committed.result.sha,
        (await git(path, ['rev-parse', 'HEAD'])).trim()
      )
      assert.equal(
        await git(path, ['show', `HEAD:${selected}`]),
        'current selection\nall selected lines\n'
      )
      assert.equal(await git(path, ['show', 'HEAD:unselected.txt']), 'before\n')
      assert.equal(
        await readFile(join(path, 'unselected.txt'), 'utf8'),
        'unselected edit\n'
      )
      assert.equal(
        await readFile(join(path, 'untracked.txt'), 'utf8'),
        'unselected new file\n'
      )
      assert.equal(
        await git(path, [
          'diff-tree',
          '--no-commit-id',
          '--name-only',
          '-r',
          '-z',
          'HEAD',
        ]),
        `${selected}\0`
      )
      assert.equal(
        (await git(path, ['log', '-1', '--format=%B'])).trimEnd(),
        'Selected files\n\nDescription\n# preserved commentary'
      )
      assert.equal(
        (await git(path, ['log', '-1', '--format=%an <%ae>'])).trim(),
        'Native Service Tests <native-service@example.invalid>'
      )
      assert.equal(await git(path, ['diff', '--cached', '--name-only']), '')
      const status = await request<Status>('repository.status', { id })
      assert.deepEqual(status.result?.files.map(file => file.path).sort(), [
        'unselected.txt',
        'untracked.txt',
      ])
    }))

  it('returns a complete SHA for an initial selected commit', () =>
    withRepository(async (path, request) => {
      await writeFile(join(path, 'selected.txt'), 'selected\n')
      await writeFile(join(path, 'unselected.txt'), 'unselected\n')
      const id = await open(path, request)
      const committed = await request<{ sha: string }>('repository.commit', {
        id,
        paths: ['selected.txt'],
        summary: 'Initial selection',
      })
      assert.equal(committed.error, undefined)
      assert.equal(
        committed.result?.sha,
        (await git(path, ['rev-parse', 'HEAD'])).trim()
      )
      assert.equal(
        await git(path, ['ls-tree', '--name-only', '-z', 'HEAD']),
        'selected.txt\0'
      )
      assert.equal(
        await readFile(join(path, 'unselected.txt'), 'utf8'),
        'unselected\n'
      )
    }))

  it('uses upstream whole-file rename and deletion staging without adding a recreated old path', () =>
    withRepository(async (path, request) => {
      await seed(path, {
        'old.txt': 'original\n',
        'deleted.txt': 'delete me\n',
      })
      await git(path, ['mv', 'old.txt', 'renamed.txt'])
      await writeFile(join(path, 'old.txt'), 'unselected replacement\n')
      await rm(join(path, 'deleted.txt'))
      const id = await open(path, request)
      const status = await request<Status>('repository.status', { id })
      assert.equal(
        status.result?.files.find(file => file.path === 'renamed.txt')?.status,
        'Renamed'
      )
      const committed = await request('repository.commit', {
        id,
        paths: ['renamed.txt', 'deleted.txt'],
        summary: 'Rename and delete',
      })
      assert.equal(committed.error, undefined)
      assert.equal(
        await git(path, ['ls-tree', '--name-only', '-z', 'HEAD']),
        'renamed.txt\0'
      )
      assert.equal(
        await readFile(join(path, 'old.txt'), 'utf8'),
        'unselected replacement\n'
      )
      assert.equal(await git(path, ['show', 'HEAD:renamed.txt']), 'original\n')
    }))

  it('rejects empty, duplicate, invalid and stale selections before changing the index', () =>
    withRepository(async (path, request) => {
      await seed(path, {
        'changed.txt': 'before\n',
        'unchanged.txt': 'unchanged\n',
      })
      await writeFile(join(path, 'changed.txt'), 'after\n')
      await git(path, ['add', 'changed.txt'])
      const id = await open(path, request)
      const base = { id, paths: ['changed.txt'], summary: 'Selection' }
      for (const invalid of [
        { paths: [] },
        { paths: 'changed.txt' },
        { paths: [''] },
        { paths: [1] },
        { paths: ['changed.txt', 'changed.txt'] },
        { paths: ['changed.txt\0'] },
        { summary: '' },
        { summary: ' \n\t' },
        { summary: null },
        { summary: 'nul\0' },
        { description: 1 },
        { description: null },
        { description: 'nul\0' },
        { amend: true },
        { signOff: true },
        { lines: [1] },
        { noVerify: true },
        { allowEmpty: true },
      ]) {
        await rejectWithoutMutation(
          path,
          request,
          { ...base, ...invalid },
          -32602
        )
      }
      await rejectWithoutMutation(
        path,
        request,
        { id, paths: ['changed.txt'] },
        -32602
      )
      await rejectWithoutMutation(
        path,
        request,
        { id, summary: 'Missing paths' },
        -32602
      )
      for (const paths of [
        ['missing.txt'],
        ['unchanged.txt'],
        ['changed.txt', 'missing.txt'],
        ['../outside.txt'],
        [join(path, 'changed.txt')],
      ]) {
        await rejectWithoutMutation(path, request, { ...base, paths }, -32003)
      }
      assert.equal(await readFile(join(path, 'changed.txt'), 'utf8'), 'after\n')
      await git(path, ['reset', '--', '.'])
      await writeFile(join(path, 'changed.txt'), 'before\n')
      await rejectWithoutMutation(path, request, base, -32003)
    }))

  it('rejects unresolved conflicts and a merge even after all conflicts are staged as resolved', () =>
    withRepository(async (path, request) => {
      await diverge(path)
      assert.equal((await exec(['merge', 'main'], path)).exitCode, 1)
      await writeFile(join(path, 'selected.txt'), 'unconflicted selection\n')
      const id = await open(path, request)
      const params = {
        id,
        paths: ['selected.txt'],
        summary: 'Not a merge completion',
      }
      const conflicted = await request<Status>('repository.status', { id })
      assert.equal(conflicted.result?.conflicted, true)
      const conflictBytes = await readFile(join(path, 'file.txt'))
      await rejectWithoutMutation(path, request, params, -32004)
      assert.deepEqual(await readFile(join(path, 'file.txt')), conflictBytes)
      await writeFile(join(path, 'file.txt'), 'resolved\n')
      await git(path, ['add', 'file.txt'])
      const resolved = await request<Status>('repository.status', { id })
      assert.equal(resolved.result?.conflicted, false)
      assert.equal(resolved.result?.operation, 'merge')
      await rejectWithoutMutation(path, request, params, -32004)
      // Stash-style conflicts have no operation head, but remain unmerged in the
      // real index. A plain selected commit must not erase those entries either.
      await git(path, ['reset', '--', '.'])
      await git(path, ['merge', '--abort'])
      await writeFile(join(path, 'file.txt'), 'stashed\n')
      await git(path, ['stash', 'push', '-m', 'conflict input'])
      await writeFile(join(path, 'file.txt'), 'current\n')
      await git(path, ['commit', '-am', 'current before stash'])
      assert.equal((await exec(['stash', 'pop'], path)).exitCode, 1)
      const stashed = await request<Status>('repository.status', { id })
      assert.equal(stashed.result?.conflicted, true)
      assert.equal(stashed.result?.operation, null)
      await rejectWithoutMutation(path, request, params, -32004)
    }))

  it('rejects a cherry-pick after its conflict is resolved but before continuation', () =>
    withRepository(async (path, request) => {
      await diverge(path)
      assert.equal((await exec(['cherry-pick', 'main'], path)).exitCode, 1)
      await writeFile(join(path, 'file.txt'), 'resolved\n')
      await git(path, ['add', 'file.txt'])
      const id = await open(path, request)
      const status = await request<Status>('repository.status', { id })
      assert.equal(status.result?.conflicted, false)
      assert.equal(status.result?.operation, 'cherry-pick')
      await rejectWithoutMutation(
        path,
        request,
        { id, paths: ['file.txt'], summary: 'Not a cherry-pick completion' },
        -32004
      )
    }))

  for (const backend of ['--merge', '--apply']) {
    it(`rejects ${backend} rebase state, including an incomplete upstream snapshot`, () =>
      withRepository(async (path, request) => {
        await diverge(path)
        assert.equal(
          (await exec(['rebase', backend, 'main'], path)).exitCode,
          1
        )
        await writeFile(join(path, 'file.txt'), 'resolved\n')
        await git(path, ['add', 'file.txt'])
        const id = await open(path, request)
        const params = {
          id,
          paths: ['file.txt'],
          summary: 'Not a rebase completion',
        }
        const status = await request<Status>('repository.status', { id })
        assert.equal(status.result?.conflicted, false)
        assert.equal(
          status.result?.operation,
          backend === '--merge' ? 'rebase' : null
        )
        await rejectWithoutMutation(path, request, params, -32004)
        if (backend === '--merge') {
          // Simulate interrupted/incomplete metadata after a real stopped rebase.
          // getStatus cannot construct its snapshot, but raw operation state lives.
          await rm(join(path, '.git', 'rebase-merge', 'orig-head'))
          assert.equal(
            (await request<Status>('repository.status', { id })).result
              ?.operation,
            null
          )
          await rejectWithoutMutation(path, request, params, -32004)
          await rm(join(path, '.git', 'REBASE_HEAD'))
          await rejectWithoutMutation(path, request, params, -32004)
        }
      }))
  }

  it('rejects absent effective Git identities without unstaging the selection', () =>
    withRepository(async (path, request) => {
      await seed(path, { 'selected.txt': 'before\n' })
      await writeFile(join(path, 'selected.txt'), 'after\n')
      await git(path, ['add', 'selected.txt'])
      await git(path, ['config', 'user.useconfigonly', 'true'])
      await git(path, ['config', '--unset', 'user.name'])
      await git(path, ['config', '--unset', 'user.email'])
      const id = await open(path, request)
      await rejectWithoutMutation(
        path,
        request,
        { id, paths: ['selected.txt'], summary: 'Missing identity' },
        -32005
      )
    }))

  for (const hookName of ['pre-commit', 'commit-msg']) {
    it(`propagates ${hookName} failure and output without ignoring or automatically retrying it`, () =>
      withRepository(async (path, request) => {
        await seed(path, {
          'selected.txt': 'before\n',
          'unselected.txt': 'before\n',
        })
        await writeFile(join(path, 'selected.txt'), 'selected\n')
        await writeFile(join(path, 'unselected.txt'), 'unselected\n')
        const hookPath = join(path, '.git', 'hooks', hookName)
        const hook = (exitCode: number) =>
          [
            '#!/bin/sh',
            'test "$GITHUB_DESKTOP" = 1 || { echo missing-production-interception >&2; exit 99; }',
            'printf "run\\n" >> .git/native-service-hook-runs',
            ...(hookName === 'commit-msg' ? ['test -s "$1" || exit 98'] : []),
            `echo ${hookName}-policy-${
              exitCode === 0 ? 'accepted' : 'rejected'
            } >&2`,
            `exit ${exitCode}`,
            '',
          ].join('\n')
        await writeFile(hookPath, hook(1), { mode: 0o755 })
        const head = await git(path, ['rev-parse', 'HEAD'])
        const id = await open(path, request)
        const params = {
          id,
          paths: ['selected.txt'],
          summary: 'Checked by hook',
        }
        const failed = await request('repository.commit', params)
        assert.equal(failed.error?.code, -32000, JSON.stringify(failed))
        assert.equal(failed.result, undefined)
        assert.match(
          failed.error!.message,
          new RegExp(`${hookName}-policy-rejected`)
        )
        assert.match(
          failed.error!.message,
          new RegExp(`Running ${hookName} hook`)
        )
        assert.doesNotMatch(failed.error!.message, /failure ignored/)
        assert.equal(await git(path, ['rev-parse', 'HEAD']), head)
        assert.equal(await git(path, ['rev-list', '--count', 'HEAD']), '1\n')
        assert.equal(
          await readFile(
            join(path, '.git', 'native-service-hook-runs'),
            'utf8'
          ),
          'run\n'
        )
        assert.equal(
          await readFile(join(path, 'selected.txt'), 'utf8'),
          'selected\n'
        )
        assert.equal(
          await readFile(join(path, 'unselected.txt'), 'utf8'),
          'unselected\n'
        )
        assert.equal(
          (await request<Status>('repository.status', { id })).result
            ?.conflicted,
          false
        )
        // Upstream leaves the selected files staged after a hook rejection; the
        // service neither invents rollback nor suppresses that write failure.
        assert.equal(
          await git(path, ['diff', '--cached', '--name-only']),
          'selected.txt\n'
        )
        await writeFile(hookPath, hook(0))
        const retried = await request<{ sha: string }>(
          'repository.commit',
          params
        )
        assert.equal(retried.error, undefined)
        assert.equal(
          retried.result?.sha,
          (await git(path, ['rev-parse', 'HEAD'])).trim()
        )
        assert.equal(
          await readFile(
            join(path, '.git', 'native-service-hook-runs'),
            'utf8'
          ),
          'run\nrun\n'
        )
        assert.equal(
          await git(path, ['show', 'HEAD:unselected.txt']),
          'before\n'
        )
        assert.equal(
          await readFile(join(path, 'unselected.txt'), 'utf8'),
          'unselected\n'
        )
      }))
  }
})

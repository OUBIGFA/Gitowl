import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { spawn } from 'child_process'
import { once } from 'events'
import { createInterface } from 'readline'
import { join, resolve } from 'path'
import { writeFile, readFile, mkdir, mkdtemp, rm } from 'fs/promises'
import { exec, resolveGitDir } from 'dugite'

type Response = {
  id: number | null
  result?: any
  error?: { code: number; message: string }
}

async function withRepository(
  run: (
    path: string,
    request: (method: string, params?: object) => Promise<Response>,
    send: (text: string) => Promise<Response>
  ) => Promise<void>
) {
  const scratch = resolve('_temp/native-service-tests')
  await mkdir(scratch, { recursive: true })
  const path = await mkdtemp(join(scratch, 'repo-'))
  assert.equal(
    (await exec(['init', '--initial-branch=main', path], scratch)).exitCode,
    0
  )
  // This subprocess has no Electron, tsx, jsdom or node:test module mocks. It
  // exercises the same bundle that the native app launches after installation.
  const child = spawn(
    process.execPath,
    [
      '--experimental-webstorage',
      `--localstorage-file=${join(path, '.git', 'native-service-preferences')}`,
      resolve('out/native-service/service.js'),
    ],
    {
      cwd: path,
      env: { ...process.env, LOCAL_GIT_DIRECTORY: resolveGitDir() },
      stdio: 'pipe',
    }
  )
  const exited = once(child, 'exit')
  const lines = createInterface({ input: child.stdout })
  let errors = ''
  child.stderr.on('data', chunk => {
    errors += chunk.toString()
  })
  const iterator = lines[Symbol.asyncIterator]()
  let id = 0
  const send = async (text: string): Promise<Response> => {
    child.stdin.write(text + '\n')
    const response = await iterator.next()
    assert.equal(response.done, false, errors)
    return JSON.parse(response.value!)
  }
  const request = (method: string, params: object = {}) =>
    send(JSON.stringify({ jsonrpc: '2.0', id: ++id, method, params }))
  try {
    await run(path, request, send)
  } finally {
    // Close the child before deleting the fixture: Windows holds its working
    // directory and Node's persistent preferences file open until process exit.
    child.stdin.end()
    const killTimer = setTimeout(() => child.kill(), 5000)
    try {
      await exited
    } finally {
      clearTimeout(killTimer)
      lines.close()
    }
    await rm(path, { recursive: true, force: true })
  }
  assert.equal(child.exitCode, 0, errors)
}

describe('native repository service in plain Node', { timeout: 30000 }, () => {
  it('opens an actual repository, reports changes and preserves the index', () =>
    withRepository(async (path, request) => {
      await writeFile(join(path, '中文 name.txt'), 'before\n')
      assert.equal((await exec(['add', '.'], path)).exitCode, 0)
      assert.equal((await exec(['commit', '-m', 'initial'], path)).exitCode, 0)
      await writeFile(join(path, '中文 name.txt'), 'after\n')
      const before = await readFile(join(path, '.git', 'index'))
      const info = await request('system.info')
      assert.equal(info.result.protocol, 1)
      assert.ok(info.result.capabilities.includes('repository.commit'))
      const opened = await request('repository.open', { path })
      assert.equal(opened.error, undefined)
      const id = opened.result.id
      const status = await request('repository.status', { id })
      assert.deepEqual(status.result.files, [
        { path: '中文 name.txt', status: 'Modified', isSubmodule: false },
      ])
      const diff = await request('repository.diff', {
        id,
        path: '中文 name.txt',
      })
      assert.equal(diff.result.kind, 'text')
      assert.match(diff.result.text, /-before\n\+after/)
      const history = await request('repository.history', { id, limit: 1 })
      assert.equal(history.result[0].summary, 'initial')
      const changeset = await request('repository.changeset', {
        id,
        commit: history.result[0].sha,
      })
      assert.equal(changeset.result.files[0].path, '中文 name.txt')
      assert.deepEqual(await readFile(join(path, '.git', 'index')), before)
    }))

  it('isolates repository handles and rejects arbitrary paths and commands', () =>
    withRepository(async (path, request, send) => {
      assert.equal(
        (await request('shell.exec', { command: 'whoami' })).error?.code,
        -32601
      )
      assert.equal(
        (await request('repository.status', { id: 99 })).error?.code,
        -32002
      )
      const opened = await request('repository.open', { path })
      const id = opened.result.id
      assert.equal(
        (await request('repository.diff', { id, path: '../outside.txt' })).error
          ?.code,
        -32003
      )
      assert.equal(
        (await request('repository.changeset', { id, commit: '--all' })).error
          ?.code,
        -32602
      )
      assert.equal(
        (await request('repository.history', { id, limit: -1 })).error?.code,
        -32602
      )
      assert.deepEqual((await request('repository.history', { id })).result, [])
      assert.equal((await send('not-json')).error?.code, -32700)
      assert.equal(
        (await send('{"jsonrpc":"2.0","id":0,"method":"system.info"}')).error
          ?.code,
        -32600
      )
      assert.equal((await request('system.info')).result.protocol, 1)
    }))

  it('opens from a nested directory and resolves linked-worktree metadata', () =>
    withRepository(async (path, request) => {
      await writeFile(join(path, 'file.txt'), 'initial\n')
      await exec(['add', '.'], path)
      await exec(['commit', '-m', 'initial'], path)
      await mkdir(join(path, 'nested'))
      const opened = await request('repository.open', {
        path: join(path, 'nested'),
      })
      assert.equal(opened.result.path.toLowerCase(), path.toLowerCase())
      const worktree = join(path, 'worktree')
      assert.equal(
        (await exec(['worktree', 'add', '-b', 'other', worktree], path))
          .exitCode,
        0
      )
      const linked = await request('repository.open', { path: worktree })
      assert.notEqual(linked.result.id, opened.result.id)
      assert.equal(
        (await request('repository.status', { id: linked.result.id })).result
          .branch,
        'other'
      )
      assert.equal(
        (await request('repository.status', { id: opened.result.id })).result
          .branch,
        'main'
      )
    }))
})

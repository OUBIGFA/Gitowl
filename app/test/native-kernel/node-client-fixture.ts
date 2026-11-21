import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { createInterface } from 'node:readline'
import { mkdirSync, mkdtempSync } from 'node:fs'
import { join, resolve } from 'node:path'

export function startNodeFixture(profile?: string) {
  const root = resolve('_temp/electron-removal-20261121/git')
  mkdirSync(root, { recursive: true })
  profile ??= mkdtempSync(join(root, 'profile-'))
  const runtime =
    process.env.GITOWL_NODE_RUNTIME ?? resolve('output/native-runtime/backend')
  const executable = join(runtime, 'node.exe')
  const settings = join(profile, 'settings.sqlite')
  const flags = [
    '--experimental-webstorage',
    `--localstorage-file=${settings}`,
    '--conditions=import',
  ]
  const seed = spawnSync(
    executable,
    [
      ...flags,
      '-e',
      `
    localStorage.setItem('stats-opt-out','1');
    localStorage.setItem('has-sent-stats-opt-in-ping','1');
    localStorage.setItem('has-shown-welcome-flow','1');
  `,
    ],
    { encoding: 'utf8', windowsHide: true }
  )
  assert.equal(seed.status, 0, seed.stderr)
  const env = {
    ...process.env,
    NODE_PATH: '',
    NODE_OPTIONS: '',
    GITOWL_USER_DATA: profile,
    GIT_CONFIG_GLOBAL: join(profile, 'gitconfig'),
    GIT_CONFIG_NOSYSTEM: '1',
    GIT_CEILING_DIRECTORIES: root,
    LOCAL_GIT_DIRECTORY: join(runtime, 'git'),
  }
  const child = spawn(executable, [...flags, join(runtime, 'main.js')], {
    env,
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
  })
  let diagnostic = ''
  child.stderr.on('data', data => (diagnostic += data))
  const lines = createInterface({ input: child.stdout })
  const pending = new Map<
    number,
    { resolve: (value: any) => void; reject: (error: Error) => void }
  >()
  const observers = new Set<(snapshot: any) => void>()
  let nextID = 0
  let readyResolve: () => void
  const ready = new Promise<void>(resolveReady => (readyResolve = resolveReady))
  const exited = new Promise<number | null>(resolveExit =>
    child.on('exit', resolveExit)
  )
  child.on('exit', code => {
    for (const request of pending.values())
      {request.reject(new Error(`Backend exited ${code}: ${diagnostic}`))}
    pending.clear()
  })
  const write = (message: unknown) =>
    child.stdin.write(JSON.stringify(message) + '\n')
  lines.on('line', line => {
    const message = JSON.parse(line)
    if (message.method === 'kernel.state') {
      readyResolve()
      for (const observer of observers) {observer(message.params)}
    }
    if (message.method === 'platform.request') {
      const { id, operation } = message.params
      let value: unknown = null
      if (operation === 'should-use-dark-colors') {value = false}
      else if (operation === 'get-notifications-permission') {value = 'denied'}
      else if (
        !['set-native-theme-source', 'resolve-proxy-url'].includes(operation)
      ) {
        write({
          jsonrpc: '2.0',
          id: 100000 + id,
          method: 'kernel.platformResult',
          params: {
            id,
            error: `Unexpected fixture platform request: ${operation}`,
            value: null,
          },
        })
        return
      }
      write({
        jsonrpc: '2.0',
        id: 100000 + id,
        method: 'kernel.platformResult',
        params: { id, error: null, value },
      })
    }
    const request = pending.get(message.id)
    if (request) {
      pending.delete(message.id)
      if (message.error)
        {request.reject(new Error(message.error.message + '\n' + diagnostic))}
      else {request.resolve(message.result)}
    }
  })
  function call(method: string, params?: unknown) {
    return new Promise<any>((resolveCall, reject) => {
      const id = ++nextID
      const timer = setTimeout(() => {
        pending.delete(id)
        reject(new Error(`Backend timeout: ${method}\n${diagnostic}`))
      }, 30000)
      pending.set(id, {
        resolve: value => {
          clearTimeout(timer)
          resolveCall(value)
        },
        reject: error => {
          clearTimeout(timer)
          reject(error)
        },
      })
      write({ jsonrpc: '2.0', id, method, params })
    })
  }
  return {
    profile,
    runtime,
    ready,
    diagnostic: () => diagnostic,
    command: (params: unknown) => call('kernel.command', params),
    waitFor: (predicate: (snapshot: any) => boolean) =>
      new Promise<any>((resolveState, reject) => {
        const timer = setTimeout(() => {
          observers.delete(observe)
          reject(new Error('State did not settle: ' + diagnostic))
        }, 30000)
        const observe = (snapshot: any) => {
          if (predicate(snapshot)) {
            clearTimeout(timer)
            observers.delete(observe)
            resolveState(snapshot)
          }
        }
        observers.add(observe)
        void call('kernel.command', { name: 'state' }).then(
          result => observe(result.snapshot),
          reject
        )
      }),
    async close() {
      await call('kernel.shutdown')
      child.stdin.end()
      assert.equal(await exited, 0, diagnostic)
      lines.close()
    },
    git(path: string, ...args: string[]) {
      const result = spawnSync(
        join(runtime, 'git/cmd/git.exe'),
        ['-C', path, ...args],
        { env, encoding: 'utf8', windowsHide: true }
      )
      assert.equal(result.status, 0, result.stderr)
      return result.stdout.trim()
    },
  }
}

import { createInterface } from 'node:readline'
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { spawn, spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync } from 'node:fs'
import { join, resolve } from 'node:path'

describe('complete Node desktop kernel', () => {
  it('loads the original presentation state without Electron and settles an admitted request before EOF', async () => {
    const root = resolve('_temp/electron-removal-20261121/runtime')
    mkdirSync(root, { recursive: true })
    const profile = mkdtempSync(join(root, 'profile-'))
    const runtime =
      process.env.GITOWL_NODE_RUNTIME ?? resolve('out/native-kernel-node')
    const executable = process.env.GITOWL_NODE_RUNTIME
      ? join(runtime, 'node.exe')
      : process.execPath
    const settings = join(profile, 'settings.sqlite')
    const flags = [
      '--experimental-webstorage',
      `--localstorage-file=${settings}`,
    ]
    const seeded = spawnSync(
      executable,
      [
        ...flags,
        '-e',
        `
      localStorage.setItem('stats-opt-out', '1');
      localStorage.setItem('has-sent-stats-opt-in-ping', '1');
      localStorage.setItem('has-shown-welcome-flow', '1');
    `,
      ],
      { encoding: 'utf8', timeout: 10000 }
    )
    assert.equal(seeded.status, 0, seeded.stderr)
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      GITOWL_USER_DATA: profile,
      NODE_PATH: process.env.GITOWL_NODE_RUNTIME
        ? ''
        : resolve('app/node_modules'),
      GIT_CONFIG_GLOBAL: join(profile, 'gitconfig'),
      GIT_CONFIG_NOSYSTEM: '1',
      LOCAL_GIT_DIRECTORY: process.env.GITOWL_NODE_RUNTIME
        ? join(runtime, 'git')
        : resolve('app/node_modules/dugite/git'),
    }
    delete env.ELECTRON_RUN_AS_NODE
    const child = spawn(
      executable,
      [...flags, '--conditions=import', join(runtime, 'main.js')],
      { env, stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true }
    )
    let diagnostics = ''
    child.stderr.on('data', chunk => {
      diagnostics += chunk
    })
    const lines = createInterface({ input: child.stdout })
    const messages: any[] = []
    let requested = false
    const result = await new Promise<number | null>((resolveExit, reject) => {
      const timeout = setTimeout(() => {
        child.kill()
        reject(new Error(diagnostics))
      }, 30000)
      child.on('error', reject)
      child.on('exit', code => {
        clearTimeout(timeout)
        resolveExit(code)
      })
      lines.on('line', line => {
        const message = JSON.parse(line)
        messages.push(message)
        if (message.method === 'platform.request') {
          const operation = message.params.operation
          assert.ok(
            ['set-native-theme-source', 'should-use-dark-colors'].includes(
              operation
            ),
            operation
          )
          child.stdin.write(
            JSON.stringify({
              jsonrpc: '2.0',
              id: 1000 + message.params.id,
              method: 'kernel.platformResult',
              params: {
                id: message.params.id,
                error: null,
                value: operation === 'should-use-dark-colors' ? false : null,
              },
            }) + '\n'
          )
        }
        if (message.method === 'kernel.state' && !requested) {
          requested = true
          child.stdin.end(
            JSON.stringify({
              jsonrpc: '2.0',
              id: 1,
              method: 'kernel.command',
              params: { name: 'state' },
            }) + '\n'
          )
        }
      })
    })
    lines.close()
    assert.equal(result, 0, diagnostics)
    const reply = messages.find(message => message.id === 1)
    assert.ok(reply?.result?.snapshot, JSON.stringify(messages) + diagnostics)
    assert.deepEqual(reply.result.snapshot.repositories, [])
    assert.ok(reply.result.snapshot.menu.items.length > 0)
    assert.equal(reply.result.snapshot.protocol, 1)
    assert.doesNotMatch(diagnostics, /uncaught|ReferenceError|not defined/i)
  })
})

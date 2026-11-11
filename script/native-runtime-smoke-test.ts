import assert from 'node:assert/strict'
import { it } from 'node:test'
import { spawn, execFile } from 'child_process'
import { promisify } from 'util'
import { createInterface } from 'readline'
import { mkdir, mkdtemp, readFile, rm } from 'fs/promises'
import { join, resolve } from 'path'
import { chromium } from 'playwright'
import { inventory } from './prepare-native-runtime'

/** Read-only diagnostics from the actual Electron main, not a mock renderer. */
async function inspectMain(endpoint: string, expression: string) {
  const socket = new WebSocket(endpoint)
  await new Promise<void>((resolve, reject) => {
    socket.addEventListener('open', () => resolve(), { once: true })
    socket.addEventListener(
      'error',
      () => reject(new Error('Packaged Electron inspector failed')),
      { once: true }
    )
  })
  try {
    return await new Promise<any>((resolve, reject) => {
      socket.addEventListener('message', event => {
        const response = JSON.parse(String(event.data))
        if (response.id !== 1) {
          return
        }
        if (response.error || response.result.exceptionDetails) {
          reject(
            new Error(
              JSON.stringify(response.error ?? response.result.exceptionDetails)
            )
          )
        } else {
          resolve(response.result.result.value)
        }
      })
      socket.send(
        JSON.stringify({
          id: 1,
          method: 'Runtime.evaluate',
          params: {
            expression,
            returnByValue: true,
            includeCommandLineAPI: true,
          },
        })
      )
    })
  } finally {
    socket.close()
  }
}

/** Opt-in: run against the prepared production package, never the seeded out/. */
it(
  'smokes the packaged original Electron kernel, Git and Copilot/Koffi FFI',
  {
    skip: !process.env.GITOWL_RUNTIME_SMOKE || process.platform !== 'win32',
    timeout: 60000,
  },
  async () => {
    const runtime = resolve(process.env.GITOWL_RUNTIME_SMOKE!)
    const app = join(runtime, 'app')
    const receipt = JSON.parse(
      await readFile(join(runtime, '../runtime-receipt.json'), 'utf8')
    )
    assert.equal(receipt.originalCore, 'desktop/desktop')
    assert.equal(receipt.presentationComplete, false)
    assert.deepEqual(await inventory(runtime), receipt.files)
    const names = receipt.files.map((file: { path: string }) => file.path)
    for (const file of [
      'electron.exe',
      'LICENSE',
      'LICENSES.chromium.html',
      'app/main.js',
      'app/native-kernel.js',
      'app/native-kernel.html',
      'app/koffi.node',
      'app/git/cmd/git.exe',
      'app/git/ucrt64/libexec/git-core/git-credential-desktop.exe',
      'app/desktop-trampoline/desktop-askpass-trampoline.exe',
      'app/process-proxy.exe',
      'app/printenvz.exe',
      'app/copilot/prebuilds/win32-x64/copilot-runtime.exe',
      'app/copilot/prebuilds/win32-x64/runtime.node',
      'app/static/licenses.json',
      'app/emoji.json',
    ]) {
      assert.ok(
        names.includes(file),
        `Missing upstream runtime resource: ${file}`
      )
    }
    assert.ok(
      !names.some((name: string) =>
        /(?:native-service|seed-profile|\/node\.exe$|^app\/renderer\.js$)/.test(
          name
        )
      )
    )
    const pkg = JSON.parse(await readFile(join(app, 'package.json'), 'utf8'))
    assert.equal(pkg.productName, 'Gitowl')
    assert.equal(pkg.version, '0.0.1')
    assert.equal(pkg.bundleID, 'io.github.oubigfa.gitowl')
    const git = await promisify(execFile)(
      join(app, 'git/cmd/git.exe'),
      ['--version'],
      { cwd: app }
    )
    assert.match(git.stdout, /^git version 2\.56\.0/)

    const scratch = resolve('_temp/kernel-reuse/runtime-packaging')
    await mkdir(scratch, { recursive: true })
    const data = await mkdtemp(join(scratch, 'smoke-profile-'))
    const env = {
      ...process.env,
      GITOWL_USER_DATA: data,
      ELECTRON_NO_ATTACH_CONSOLE: '1',
    }
    for (const key of Object.keys(env)) {
      if (
        /^(?:ELECTRON_RUN_AS_NODE|NODE_OPTIONS|GH_TOKEN|GITHUB_TOKEN|COPILOT_.*TOKEN|GIT_(?:DIR|WORK_TREE|COMMON_DIR|INDEX_FILE|OBJECT_DIRECTORY|ALTERNATE_OBJECT_DIRECTORIES|EXEC_PATH)|LOCAL_GIT_DIRECTORY|DESKTOP_GITHUB_DOTCOM_.*)$/i.test(
          key
        )
      ) {
        delete env[key as keyof typeof env]
      }
    }
    const child = spawn(
      join(runtime, 'electron.exe'),
      ['--inspect=0', '--remote-debugging-port=0', app, '--gitowl-native-core'],
      { cwd: app, env, stdio: 'pipe', windowsHide: true }
    )
    const exited = new Promise<number | null>((resolve, reject) => {
      child.once('exit', resolve)
      child.once('error', reject)
    })
    let diagnostics = ''
    let browserEndpoint: string | undefined
    let mainEndpoint: string | undefined
    child.stderr.on('data', chunk => {
      diagnostics = (diagnostics + chunk.toString()).slice(-32768)
      mainEndpoint = /Debugger listening on (ws:\/\/\S+)/.exec(diagnostics)?.[1]
      browserEndpoint = /DevTools listening on (ws:\/\/\S+)/.exec(
        diagnostics
      )?.[1]
    })
    const lines = createInterface({ input: child.stdout })
    let id = 0
    const pending = new Map<
      number,
      { resolve: (value: any) => void; reject: (error: Error) => void }
    >()
    lines.on('line', line => {
      if (line.trim().length === 0) {
        return
      }
      try {
        const message = JSON.parse(line)
        if (message.id !== undefined) {
          const waiter = pending.get(message.id)
          pending.delete(message.id)
          if (message.error) {
            waiter?.reject(new Error(message.error.message))
          } else {
            waiter?.resolve(message.result)
          }
        }
      } catch (error) {
        for (const waiter of pending.values()) {
          waiter.reject(error as Error)
        }
        pending.clear()
      }
    })
    child.once('exit', code => {
      for (const waiter of pending.values()) {
        waiter.reject(
          new Error(`Packaged kernel exited (${code}): ${diagnostics}`)
        )
      }
      pending.clear()
    })
    const call = (method: string, params?: unknown) =>
      new Promise<any>((resolve, reject) => {
        const request = ++id
        const timer = setTimeout(() => {
          pending.delete(request)
          reject(
            new Error(
              `Packaged kernel did not answer ${method}: ${diagnostics}`
            )
          )
        }, 30000)
        pending.set(request, {
          resolve: value => {
            clearTimeout(timer)
            resolve(value)
          },
          reject: error => {
            clearTimeout(timer)
            reject(error)
          },
        })
        child.stdin.write(
          JSON.stringify({ jsonrpc: '2.0', id: request, method, params }) + '\n'
        )
      })
    try {
      const result = await call('kernel.command', { name: 'state' })
      assert.equal(result.snapshot.core, 'desktop/desktop')
      assert.equal(result.snapshot.presentationComplete, false)
      assert.deepEqual(result.snapshot.accounts, [])
      assert.deepEqual(result.snapshot.repositories, [])
      // A fresh production profile must not silently bypass welcome by importing
      // app/test/native-kernel/seed-profile.js or another synthetic profile.
      assert.equal(result.snapshot.welcome, true)
      assert.ok(browserEndpoint, diagnostics)
      assert.ok(mainEndpoint, diagnostics)
      const main = await inspectMain(
        mainEndpoint,
        `(() => {
        const { app, BrowserWindow } = require('electron');
        return { name: app.getName(), version: app.getVersion(), appPath: app.getAppPath(),
          userData: app.getPath('userData'), visible: BrowserWindow.getAllWindows().map(window => window.isVisible()) };
      })()`
      )
      assert.equal(main.name, 'Gitowl')
      assert.deepEqual(receipt.buildFlags, {
        GITOWL_NATIVE_BUILD: '1',
        NODE_ENV: 'production',
        RELEASE_CHANNEL: 'research-preview',
      })
      assert.equal(main.version, '0.0.1')
      assert.equal(main.appPath, app)
      assert.equal(main.userData, join(data, 'desktop-kernel'))
      assert.deepEqual(main.visible, [false])
      const browser = await chromium.connectOverCDP(browserEndpoint)
      try {
        const pages = browser.contexts()[0].pages()
        assert.equal(pages.length, 1)
        const page = pages[0]
        assert.match(new URL(page.url()).pathname, /native-kernel\.html$/)
        assert.equal(
          await page.evaluate(() => document.body.childElementCount),
          0
        )
        const ffi = await page.evaluate(
          ({ app }) => {
            const require = (window as any).require
            const path = require('path')
            const koffi = require(path.join(app, 'koffi.node'))
            const library = koffi.load(
              path.join(app, 'copilot/prebuilds/win32-x64/runtime.node')
            )
            library.unload()
            return {
              koffi: koffi.version,
              git: process.env.LOCAL_GIT_DIRECTORY,
              expectedGit: path.join(app, 'git'),
            }
          },
          { app }
        )
        assert.equal(ffi.git, ffi.expectedGit)
        assert.ok(ffi.koffi)
      } finally {
        await browser.close()
      }
      const shutdown = await call('kernel.shutdown')
      assert.equal(shutdown.quitting, true)
    } finally {
      // Only read/state diagnostics were admitted. Normal EOF settles those;
      // no writes, accounts, paid Copilot requests, install or force kill occurs.
      child.stdin.end()
      assert.equal(await exited, 0, diagnostics)
      lines.close()
      await rm(data, {
        recursive: true,
        force: true,
        maxRetries: 20,
        retryDelay: 100,
      })
    }
  }
)

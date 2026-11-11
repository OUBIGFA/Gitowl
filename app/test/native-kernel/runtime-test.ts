import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { spawn, ChildProcessWithoutNullStreams } from 'child_process'
import { chromium, Browser } from 'playwright'
import { createInterface } from 'readline'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { exec } from 'dugite'
import type {
  NativeKernelCommand,
  NativeKernelSnapshot,
} from '../../src/native-kernel/protocol'

async function git(path: string, args: string[]) {
  const result = await exec(args, path)
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout
}

function kernelClient(
  child: ChildProcessWithoutNullStreams,
  diagnostics: () => string
) {
  const pending = new Map<
    number,
    { resolve: (value: any) => void; reject: (error: Error) => void }
  >()
  const lines = createInterface({ input: child.stdout })
  let id = 0
  let latest: NativeKernelSnapshot | undefined
  const observers = new Set<(state: NativeKernelSnapshot) => void>()
  const observe = (state: NativeKernelSnapshot) => {
    if (latest !== undefined && state.revision < latest.revision) {
      return
    }
    latest = state
    for (const listener of observers) {
      listener(state)
    }
  }
  lines.on('line', line => {
    if (line.trim().length === 0) {
      return
    }
    try {
      const message = JSON.parse(line)
      if (message.method === 'kernel.state') {
        observe(message.params)
      }
      if (message.result?.snapshot) {
        observe(message.result.snapshot)
      }
      if (typeof message.id !== 'number') {
        return
      }
      const waiter = pending.get(message.id)
      if (!waiter) {
        return
      }
      pending.delete(message.id)
      if (message.error) {
        waiter.reject(new Error(message.error.message))
      } else {
        waiter.resolve(message.result)
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
        new Error(`Kernel exited (${code}) before replying: ${diagnostics()}`)
      )
    }
    pending.clear()
  })
  const call = (method: string, params?: unknown) =>
    new Promise<any>((resolve, reject) => {
      const requestID = ++id
      const timeout = setTimeout(() => {
        pending.delete(requestID)
        reject(
          new Error(
            `Kernel request did not settle: ${method}: ${diagnostics()}`
          )
        )
      }, 20000)
      pending.set(requestID, {
        resolve: value => {
          clearTimeout(timeout)
          resolve(value)
        },
        reject: error => {
          clearTimeout(timeout)
          reject(error)
        },
      })
      child.stdin.write(
        JSON.stringify({ jsonrpc: '2.0', id: requestID, method, params }) + '\n'
      )
    })
  return {
    request: async (
      command: NativeKernelCommand
    ): Promise<NativeKernelSnapshot & { committed?: boolean }> => {
      const result = await call('kernel.command', command)
      return { ...result.snapshot, committed: result.committed }
    },
    waitForState: (predicate: (state: NativeKernelSnapshot) => boolean) =>
      new Promise<NativeKernelSnapshot>((resolve, reject) => {
        if (latest && predicate(latest)) {
          resolve(latest)
          return
        }
        const listener = (state: NativeKernelSnapshot) => {
          if (!predicate(state)) {
            return
          }
          clearTimeout(timeout)
          observers.delete(listener)
          resolve(state)
        }
        const timeout = setTimeout(() => {
          observers.delete(listener)
          reject(
            new Error(`Original kernel state did not settle: ${diagnostics()}`)
          )
        }, 20000)
        observers.add(listener)
      }),
    shutdown: () => call('kernel.shutdown'),
    close: () => lines.close(),
  }
}

/** Read the actual Electron main realm through its test-only Node inspector. */
async function inspectorClient(endpoint: string) {
  const socket = new WebSocket(endpoint)
  await new Promise<void>((resolve, reject) => {
    socket.addEventListener('open', () => resolve(), { once: true })
    socket.addEventListener(
      'error',
      () => reject(new Error('Electron inspector connection failed')),
      { once: true }
    )
  })
  let id = 0
  return {
    evaluate: (expression: string) =>
      new Promise<any>((resolve, reject) => {
        const requestID = ++id
        const onMessage = (event: MessageEvent) => {
          const message = JSON.parse(String(event.data))
          if (message.id !== requestID) {
            return
          }
          socket.removeEventListener('message', onMessage)
          if (message.error || message.result.exceptionDetails) {
            reject(
              new Error(
                JSON.stringify(message.error ?? message.result.exceptionDetails)
              )
            )
          } else {
            resolve(message.result.result.value)
          }
        }
        socket.addEventListener('message', onMessage)
        socket.send(
          JSON.stringify({
            id: requestID,
            method: 'Runtime.evaluate',
            params: {
              expression,
              returnByValue: true,
              includeCommandLineAPI: true,
            },
          })
        )
      }),
    close: () => socket.close(),
  }
}

function kernelEnvironment(data: string): Record<string, string> {
  const env: Record<string, string> = Object.fromEntries(
    Object.entries(process.env).filter(
      (entry): entry is [string, string] => entry[1] !== undefined
    )
  )
  env.GITOWL_USER_DATA = data
  env.TEST_ENV = '1'
  // Electron's Windows bootstrap otherwise routes stdio to the parent's
  // console and replaces the native-owned input pipe before JS can read it.
  env.ELECTRON_NO_ATTACH_CONSOLE = '1'
  // Use a disposable original Chromium profile, never import user accounts or
  // replace production AppStore/Dispatcher with unit-test browser shims.
  for (const key of Object.keys(env)) {
    if (
      /^(?:ELECTRON_RUN_AS_NODE|NODE_OPTIONS|GH_TOKEN|GITHUB_TOKEN|COPILOT_.*TOKEN|GIT_(?:DIR|WORK_TREE|COMMON_DIR|INDEX_FILE|OBJECT_DIRECTORY|ALTERNATE_OBJECT_DIRECTORIES|EXEC_PATH)|DESKTOP_GITHUB_DOTCOM_.*)$/i.test(
        key
      )
    ) {
      delete env[key]
    }
  }
  return env
}

async function startKernel(data: string) {
  const env = kernelEnvironment(data)
  // Playwright's electron.launch deliberately gives fd 0 an ignored handle.
  // Spawn the actual native-owned pipes, then attach read-only diagnostics.
  const child = spawn(
    resolve('node_modules/electron/dist/electron.exe'),
    [
      '--inspect=0',
      '--remote-debugging-port=0',
      resolve('out/main.js'),
      '--gitowl-native-core',
    ],
    { cwd: resolve('.'), env, stdio: 'pipe', windowsHide: true }
  )
  let stderr = ''
  const client = kernelClient(child, () => stderr)
  const exited = new Promise<void>(resolve =>
    child.once('exit', () => resolve())
  )
  const endpoints = await new Promise<{ node: string; browser: string }>(
    (resolve, reject) => {
      const timeout = setTimeout(
        () => reject(new Error(`Electron launch timed out: ${stderr}`)),
        30000
      )
      const check = (chunk: Buffer) => {
        stderr = (stderr + chunk.toString()).slice(-32768)
        const node = /Debugger listening on (ws:\/\/\S+)/.exec(stderr)?.[1]
        const browser = /DevTools listening on (ws:\/\/\S+)/.exec(stderr)?.[1]
        if (node && browser) {
          clearTimeout(timeout)
          resolve({ node, browser })
        }
      }
      child.stderr.on('data', check)
      child.once('error', error => {
        clearTimeout(timeout)
        reject(error)
      })
      child.once('exit', code => {
        clearTimeout(timeout)
        reject(new Error(`Electron launch exited (${code}): ${stderr}`))
      })
    }
  ).catch(async error => {
    child.stdin.end()
    await exited
    client.close()
    throw error
  })
  let browser: Browser | undefined
  let inspector: Awaited<ReturnType<typeof inspectorClient>> | undefined
  try {
    // A snapshot proves the original renderer loaded before attaching Chromium
    // diagnostics; failures retain the kernel's stderr rather than cleanup noise.
    await client.request({ name: 'state' })
    browser = await chromium.connectOverCDP(endpoints.browser)
    inspector = await inspectorClient(endpoints.node)
  } catch (error) {
    inspector?.close()
    await browser?.close()
    child.stdin.end()
    await exited
    client.close()
    throw new Error(`Native kernel diagnostics: ${stderr}`, { cause: error })
  }
  return {
    client,
    browser,
    inspector,
    close: async () => {
      try {
        await client.shutdown()
      } finally {
        inspector.close()
        await browser.close()
        child.stdin.end()
        await exited
        client.close()
      }
    },
  }
}

describe(
  'original Desktop kernel with no web frontend mounted',
  { timeout: 90000 },
  () => {
    it('delivers a queued original-kernel result after the native parent closes stdin', async () => {
      if (process.platform !== 'win32') {
        return
      }
      const scratch = resolve('_temp/kernel-integration')
      await mkdir(scratch, { recursive: true })
      const data = await mkdtemp(join(scratch, 'eof-'))
      const child = spawn(
        resolve('node_modules/electron/dist/electron.exe'),
        [resolve('out/main.js'), '--gitowl-native-core'],
        {
          env: kernelEnvironment(data),
          stdio: 'pipe',
          windowsHide: true,
        }
      )
      let stderr = ''
      child.stderr.on('data', chunk => {
        stderr = (stderr + chunk.toString()).slice(-32768)
      })
      const client = kernelClient(child, () => stderr)
      const exited = new Promise<number | null>(resolve =>
        child.once('exit', resolve)
      )
      try {
        const result = client.request({ name: 'state' })
        child.stdin.end()
        const state = await result
        assert.equal(state.core, 'desktop/desktop')
        assert.equal(state.presentationComplete, false)
        assert.equal(await exited, 0, stderr)
      } finally {
        child.stdin.end()
        await exited
        client.close()
        await rm(data, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 100,
        })
      }
    })

    it('runs shared AppStore/Dispatcher, commits the original selection and restores its real database', async () => {
      if (process.platform !== 'win32') {
        return
      }
      const scratch = resolve('_temp/kernel-integration')
      await mkdir(scratch, { recursive: true })
      const fixture = await mkdtemp(join(scratch, 'case-'))
      let kernel: Awaited<ReturnType<typeof startKernel>> | undefined
      try {
        const repository = join(fixture, 'project')
        await git(fixture, ['init', '--initial-branch=main', repository])
        await git(repository, ['config', 'user.name', 'Gitowl Kernel Tests'])
        await git(repository, [
          'config',
          'user.email',
          'kernel@example.invalid',
        ])
        await git(repository, ['config', 'commit.gpgsign', 'false'])
        await writeFile(join(repository, 'selected.txt'), 'before\n')
        await writeFile(join(repository, 'unselected.txt'), 'before\n')
        await git(repository, ['add', '.'])
        await git(repository, ['commit', '-m', 'initial'])
        await writeFile(join(repository, 'selected.txt'), 'after\n')
        await writeFile(join(repository, 'unselected.txt'), 'keep local\n')

        const data = join(fixture, 'data')
        kernel = await startKernel(data)
        const { client } = kernel
        const initial = await client.request({ name: 'state' })
        assert.equal(initial.core, 'desktop/desktop')
        assert.equal(initial.presentationComplete, false)
        assert.deepEqual(initial.accounts, [])
        assert.deepEqual(initial.repositories, [])
        const page = kernel.browser.contexts()[0].pages()[0]
        assert.equal(
          await page.evaluate(() => document.body.childElementCount),
          0
        )
        assert.deepEqual(
          await kernel.inspector.evaluate(
            "require('electron').BrowserWindow.getAllWindows().map(window => window.isVisible())"
          ),
          [false]
        )

        const added = await client.request({
          name: 'repositories.add',
          paths: [repository],
        })
        assert.equal(added.repositories.length, 1)
        const reference = {
          id: added.repositories[0].id,
          path: added.repositories[0].path,
        }
        const selected = await client.request({
          name: 'repository.select',
          repository: reference,
        })
        assert.equal(selected.selected?.branch, 'main')
        assert.equal(selected.selected?.files.length, 2)
        await client.request({
          name: 'repository.files.includeAll',
          repository: reference,
          included: false,
        })
        const file = selected.selected!.files.find(
          file => file.path === 'selected.txt'
        )!
        const changed = await client.request({
          name: 'repository.files.include',
          repository: reference,
          fileIDs: [file.id],
          included: true,
        })
        assert.equal(
          changed.selected!.files.find(item => item.id === file.id)?.inclusion,
          'all'
        )
        assert.equal(
          changed.selected!.files.find(file => file.path === 'unselected.txt')
            ?.inclusion,
          'none'
        )
        const draft = {
          summary: 'Original kernel commit',
          description: 'From the native presentation bridge',
          timestamp: Date.now(),
        }
        const withDraft = await client.request({
          name: 'repository.message',
          repository: reference,
          message: draft,
        })
        assert.deepEqual(withDraft.selected!.message, draft)
        const committed = await client.request({
          name: 'repository.commit',
          repository: reference,
          context: { summary: draft.summary, description: draft.description },
        })
        assert.equal(committed.committed, true)
        assert.equal(
          (
            await git(repository, ['show', '--format=', '--name-only', 'HEAD'])
          ).trim(),
          'selected.txt'
        )
        assert.equal(
          await readFile(join(repository, 'unselected.txt'), 'utf8'),
          'keep local\n'
        )
        assert.match(
          await git(repository, ['diff', '--name-only']),
          /unselected\.txt/
        )
        const refreshed = await client.request({
          name: 'repository.refresh',
          repository: reference,
        })
        assert.deepEqual(
          refreshed.selected!.files.map(file => file.path),
          ['unselected.txt']
        )
        assert.equal(
          await page.evaluate(() => document.body.childElementCount),
          0
        )
        assert.deepEqual(
          await kernel.inspector.evaluate(
            "require('electron').BrowserWindow.getAllWindows().map(window => window.isVisible())"
          ),
          [false]
        )

        await kernel.close()
        kernel = undefined
        kernel = await startKernel(data)
        const restored = await kernel.client.request({ name: 'state' })
        assert.deepEqual(
          restored.repositories.map(repo => ({ id: repo.id, path: repo.path })),
          [reference]
        )
        assert.equal(restored.selected?.repository.path, reference.path)
        // Original loadInitialState restores selection before its repository
        // refresh settles. Observe that real state update, not a fake ready flag.
        const settled = await kernel.client.waitForState(
          state => state.selected?.branch === 'main'
        )
        assert.equal(settled.selected?.branch, 'main')
      } finally {
        await kernel?.close()
        await rm(fixture, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 100,
        })
      }
    })
  }
)

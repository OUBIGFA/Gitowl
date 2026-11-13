import assert from 'node:assert/strict'
import { EventEmitter } from 'events'
import { PassThrough, Writable } from 'stream'
import { before, describe, it, mock } from 'node:test'
import type { WebContents } from 'electron'
import type {
  NativeKernelResponse,
  INativeKernelSnapshot,
} from '../../src/native-kernel/protocol'

// Only the Electron IPC hardware boundary is replaced. The production parser,
// readline framing, admission, EOF and Writable flushing run unmodified.
const listeners = new Map<
  string,
  (event: { sender: WebContents }, value: any) => void
>()
const dispatched: Array<{ channel: string; value: unknown }> = []
mock.module('../../src/main-process/ipc-main', {
  namedExports: {
    on: (
      channel: string,
      listener: (event: { sender: WebContents }, value: any) => void
    ) => listeners.set(channel, listener),
  },
})
mock.module('../../src/main-process/ipc-webcontents', {
  namedExports: {
    send: (_: WebContents, channel: string, value: unknown) =>
      dispatched.push({ channel, value }),
  },
})
let NativeKernelTransport: typeof import('../../src/native-kernel/main-transport').NativeKernelTransport
before(async () => {
  ;({ NativeKernelTransport } = await import(
    '../../src/native-kernel/main-transport'
  ))
})

type WireMessage = {
  id?: number | null
  method?: string
  result?: {
    snapshot?: INativeKernelSnapshot
    committed?: boolean
    quitting?: boolean
  }
  error?: { code: number; message: string }
}
const snapshot: INativeKernelSnapshot = {
  protocol: 1,
  core: 'desktop/desktop',
  presentationComplete: false,
  revision: 1,
  imageDiffType: 0,
  repositories: [],
  accounts: [],
  selected: null,
  popups: [],
  bannerType: null,
  foldoutType: null,
  currentPopup: null,
  welcome: false,
  onboarding: null,
  signIn: null,
  errorCount: 0,
}
const line = (
  id: number,
  method = 'kernel.command',
  params: unknown = { name: 'state' }
) => JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n'
const tick = () => new Promise<void>(resolve => setImmediate(resolve))

function harness(holdOutcome = false) {
  listeners.clear()
  dispatched.length = 0
  const input = new PassThrough()
  const messages: WireMessage[] = []
  const writes = new EventEmitter()
  let release: (() => void) | undefined
  const output = new Writable({
    write: (chunk, _, callback) => {
      const message: WireMessage = JSON.parse(chunk.toString())
      messages.push(message)
      writes.emit('message')
      if (holdOutcome && message.id === 1 && message.result?.snapshot) {
        release = () => callback()
      } else {
        setImmediate(callback)
      }
    },
  })
  const exits: number[] = []
  const transport = new NativeKernelTransport(input, output, code =>
    exits.push(code)
  )
  const renderer = new EventEmitter() as unknown as WebContents
  transport.attach(renderer)
  const ipc = (channel: string, value: unknown, sender = renderer) =>
    listeners.get(channel)!({ sender }, value)
  const waitForMessages = (predicate: (messages: WireMessage[]) => boolean) =>
    new Promise<void>(resolve => {
      if (predicate(messages)) {
        resolve()
        return
      }
      const listener = () => {
        if (!predicate(messages)) {
          return
        }
        writes.removeListener('message', listener)
        resolve()
      }
      writes.on('message', listener)
    })
  return {
    input,
    output,
    transport,
    renderer,
    messages,
    exits,
    ipc,
    waitForMessages,
    release: () => release!(),
  }
}

describe('native kernel transport lifecycle', () => {
  it('opens an external URL through the foreground native host and awaits its result', async () => {
    const h = harness()
    const url = 'https://github.com/login/oauth/authorize?state=fixture'
    const opened = h.transport.openExternal(url)
    await h.waitForMessages(messages =>
      messages.some(message => message.method === 'external.open')
    )
    const request = h.messages.find(
      message => message.method === 'external.open'
    ) as any
    assert.equal(request.params.url, url)
    h.input.write(
      line(1, 'kernel.externalResult', { id: request.params.id, opened: true })
    )
    assert.equal(await opened, true)
    h.input.end()
  })

  it('does not report a browser launch as successful after the native host disconnects', async () => {
    const h = harness()
    const opened = h.transport.openExternal('https://github.com/login')
    h.input.end()
    assert.equal(await opened, false)
    await tick()
    await tick()
    assert.deepEqual(h.exits, [0])
  })

  it('routes a folder picker with an empty initial path to the visible native host', async () => {
    const h = harness()
    const selected = h.transport.showFileDialog('open', {
      defaultPath: '',
      properties: ['openDirectory', 'createDirectory'],
    })
    await h.waitForMessages(messages =>
      messages.some(message => message.method === 'dialog.request')
    )
    const request = h.messages.find(
      message => message.method === 'dialog.request'
    ) as any
    assert.equal(request.params.options.defaultPath, '')
    assert.deepEqual(request.params.options.properties, [
      'openDirectory',
      'createDirectory',
    ])
    h.input.write(
      line(1, 'kernel.dialogResult', {
        id: request.params.id,
        paths: ['C:/projects/example'],
        error: null,
      })
    )
    assert.deepEqual(await selected, ['C:/projects/example'])
    h.input.end()
  })

  it('settles an abandoned native picker when the presenter disconnects', async () => {
    const h = harness()
    const selected = h.transport.showFileDialog('save', {
      defaultPath: 'patch.diff',
    })
    h.input.end()
    assert.deepEqual(await selected, [])
    await tick()
    await tick()
    assert.deepEqual(h.exits, [0])
  })

  it('acknowledges shutdown and drains parent EOF before quitting with an owned input read', async () => {
    const h = harness()
    h.input.write(line(1, 'kernel.shutdown'))
    await h.waitForMessages(messages =>
      messages.some(message => message.result?.quitting)
    )
    await tick()
    assert.deepEqual(
      h.exits,
      [],
      'the parent must release its pipe before Electron joins its I/O workers'
    )
    // Requests queued after shutdown are never dispatched, but the input must
    // keep draining so a Windows fd read can observe the parent closing it.
    h.input.end(line(2))
    await tick()
    await tick()
    assert.equal(dispatched.length, 0)
    assert.deepEqual(h.exits, [0])
  })

  it('flushes an admitted outcome after stdin EOF before quitting', async () => {
    const h = harness(true)
    h.input.end(line(1))
    await tick()
    assert.equal(
      dispatched.length,
      0,
      'request waits for the original renderer'
    )
    assert.deepEqual(h.exits, [], 'EOF must not cancel an admitted action')
    h.ipc('native-kernel-state', snapshot)
    await tick()
    assert.deepEqual(dispatched[0].value, { id: 1, command: { name: 'state' } })
    const response: NativeKernelResponse = {
      id: 1,
      result: { snapshot, committed: true },
    }
    h.ipc('native-kernel-response', response)
    await tick()
    assert.equal(
      h.messages.at(-1)?.result?.committed,
      true,
      'final outcome remains on stdout'
    )
    assert.deepEqual(
      h.exits,
      [],
      'Writable callback must settle before quitting'
    )
    h.release()
    await tick()
    assert.deepEqual(h.exits, [0])
  })

  it('keeps the original result channel when active IDs collide across methods', async () => {
    const h = harness()
    h.ipc('native-kernel-state', snapshot)
    h.input.write(line(1))
    await tick()
    h.input.write(
      line(1, 'kernel.windowState', {
        visible: true,
        focused: true,
        minimized: false,
        state: 'normal',
        zoom: 1,
      })
    )
    h.input.write(line(1, 'kernel.shutdown'))
    h.input.write(line(1))
    await h.waitForMessages(
      messages => messages.filter(message => message.id === null).length === 3
    )
    assert.equal(
      dispatched.filter(event => event.channel === 'native-kernel-request')
        .length,
      1
    )
    assert.equal(
      h.messages.filter(message => message.id === 1).length,
      0,
      'collision diagnostics cannot finish the original call'
    )
    assert.equal(
      h.messages.filter(
        message => message.id === null && message.error?.code === -32600
      ).length,
      3
    )
    h.ipc('native-kernel-response', {
      id: 1,
      result: { snapshot, committed: true },
    })
    h.input.end()
    await tick()
    await tick()
    assert.equal(
      h.messages.find(message => message.id === 1)?.result?.committed,
      true
    )
    assert.deepEqual(h.exits, [0])
  })

  it('stops admitting commands after output failure while an original action settles', async () => {
    const h = harness()
    h.ipc('native-kernel-state', snapshot)
    h.input.write(line(1))
    await tick()
    h.output.destroy(new Error('EPIPE'))
    await tick()
    h.input.write(line(2))
    await tick()
    assert.equal(
      dispatched.filter(event => event.channel === 'native-kernel-request')
        .length,
      1
    )
    assert.deepEqual(
      h.exits,
      [],
      'an admitted write is not killed when the parent cannot read'
    )
    h.ipc('native-kernel-response', { id: 1, result: { snapshot } })
    await tick()
    assert.deepEqual(h.exits, [1])
    h.input.end()
  })

  it('reports initialization failure for queued work and never dispatches it', async () => {
    const h = harness()
    h.input.write(line(1))
    await tick()
    h.ipc('native-kernel-failure', 'initialization')
    await tick()
    await tick()
    assert.equal(dispatched.length, 0)
    assert.match(
      h.messages.find(message => message.id === 1)!.error!.message,
      /before this action was started/
    )
    assert.ok(h.messages.some(message => message.method === 'kernel.failure'))
    assert.deepEqual(h.exits, [1])
    h.input.end()
  })

  it('reports unknown outcomes on renderer loss without claiming cancellation', async () => {
    const h = harness()
    h.ipc('native-kernel-state', snapshot)
    h.input.write(line(1))
    await tick()
    ;(h.renderer as unknown as EventEmitter).emit(
      'render-process-gone',
      {},
      { reason: 'crashed' }
    )
    await tick()
    await tick()
    const response = h.messages.find(message => message.id === 1)!
    assert.match(response.error!.message, /may already have changed/)
    assert.equal(response.result, undefined)
    assert.deepEqual(h.exits, [1])
    h.input.end()
  })

  it('does not accept state or responses from a different Electron sender', async () => {
    const h = harness()
    const other = new EventEmitter() as unknown as WebContents
    h.input.write(line(1))
    await tick()
    h.ipc('native-kernel-state', snapshot, other)
    h.ipc('native-kernel-response', { id: 1, result: { snapshot } }, other)
    assert.equal(dispatched.length, 0)
    assert.equal(h.messages.length, 0)
    h.ipc('native-kernel-state', snapshot)
    h.ipc('native-kernel-response', { id: 1, result: { snapshot } })
    h.input.end()
    await tick()
    await tick()
    assert.deepEqual(h.exits, [0])
  })

  it('sends focus and window changes only when the native facts changed', async () => {
    const h = harness()
    const state = {
      visible: true,
      focused: true,
      minimized: false,
      state: 'normal',
      zoom: 1,
    }
    h.input.write(line(1, 'kernel.windowState', state))
    h.input.write(line(2, 'kernel.windowState', state))
    await tick()
    assert.equal(
      dispatched.filter(event => event.channel === 'focus').length,
      1
    )
    assert.equal(
      dispatched.filter(event => event.channel === 'window-state-changed')
        .length,
      0
    )
    assert.deepEqual(h.transport.getState(), state)
    h.input.end()
    await tick()
    await tick()
    assert.deepEqual(h.exits, [0])
  })
})

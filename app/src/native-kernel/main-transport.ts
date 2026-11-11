import { createInterface, Interface } from 'readline'
import { Readable, Writable } from 'stream'
import type { WebContents } from 'electron'
import * as ipcMain from '../main-process/ipc-main'
import * as ipcWebContents from '../main-process/ipc-webcontents'
import { isRecord } from '../lib/is-record'
import { parseKernelCommand } from './command-validation'
import type { NativeWindowAction } from './protocol'
import type { WindowState } from '../lib/window-state'

/** Window facts come from the actual native presenter, not a hidden browser. */
export interface NativeWindowState {
  readonly visible: boolean
  readonly focused: boolean
  readonly minimized: boolean
  readonly state: WindowState
  readonly zoom: number
}

export type NativeKernelFailurePhase =
  | 'initialization'
  | 'load'
  | 'renderer'
  | 'main'

export interface NativeWindowPresentation {
  action(action: NativeWindowAction): void
  getState(): NativeWindowState
  setZoom(factor: number): void
  fail(phase: NativeKernelFailurePhase): void
}

function validID(id: unknown): id is number {
  return typeof id === 'number' && Number.isSafeInteger(id) && id > 0
}

function windowState(value: unknown): NativeWindowState | undefined {
  if (
    !isRecord(value) ||
    typeof value.visible !== 'boolean' ||
    typeof value.focused !== 'boolean' ||
    typeof value.minimized !== 'boolean' ||
    !['normal', 'maximized', 'minimized', 'full-screen', 'hidden'].includes(
      value.state as string
    ) ||
    typeof value.zoom !== 'number' ||
    !Number.isFinite(value.zoom) ||
    value.zoom < 0.25 ||
    value.zoom > 5
  ) {
    return undefined
  }
  return {
    visible: value.visible,
    focused: value.focused,
    minimized: value.minimized,
    state: value.state as WindowState,
    zoom: value.zoom,
  }
}

/**
 * One native-owned pipe surrounds the original Desktop main and renderer IPC,
 * which retain sender validation. This class owns transport IDs and window
 * facts only, never Git operations, accounts or recovery decisions.
 */
export class NativeKernelTransport implements NativeWindowPresentation {
  private renderer: WebContents | undefined
  private attached = false
  private ready = false
  private readonly queued = new Map<
    number,
    ReturnType<typeof parseKernelCommand>
  >()
  private readonly pending = new Set<number>()
  private state: NativeWindowState = {
    visible: false,
    focused: false,
    minimized: false,
    state: 'normal',
    zoom: 1,
  }
  private lines: Interface | undefined
  private outputError: Error | undefined
  private inputClosed = false
  private failed = false
  private writes = 0
  private quitStarted = false

  public constructor(
    private readonly input: Readable,
    private readonly output: Writable,
    private readonly quit: (exitCode: number) => void
  ) {
    ipcMain.on('native-kernel-state', (event, snapshot) => {
      if (event.sender !== this.renderer || this.failed || this.outputError) {
        return
      }
      this.ready = true
      this.send({ jsonrpc: '2.0', method: 'kernel.state', params: snapshot })
      // EOF stops new admissions, not requests already admitted before ready.
      for (const [id, command] of this.queued) {
        ipcWebContents.send(event.sender, 'native-kernel-request', {
          id,
          command,
        })
      }
      this.queued.clear()
    })
    ipcMain.on('native-kernel-response', (event, response) => {
      if (event.sender !== this.renderer || !this.pending.delete(response.id)) {
        return
      }
      this.send({ jsonrpc: '2.0', ...response })
      this.quitWhenSettled()
    })
    ipcMain.on('native-kernel-presentation-action', (event, action) => {
      if (event.sender !== this.renderer || this.failed || this.outputError)
        return
      // Toolkit-only actions travel in FIFO order, not a replayable snapshot.
      this.send({
        jsonrpc: '2.0',
        method: 'kernel.presentation',
        params: { action },
      })
    })
    ipcMain.on('native-kernel-failure', (event, phase) => {
      if (event.sender === this.renderer) {
        this.fail(phase)
      }
    })
    this.output.on('error', error => this.outputFailed(error))
    this.input.once('end', () => this.quitWhenSettled())
    void this.read().catch(error => {
      log.error('Native kernel input failed', error)
      this.stopAdmission()
    })
  }

  public attach(renderer: WebContents): void {
    if (this.attached) {
      throw new Error('A native transport already owns its kernel renderer')
    }
    this.attached = true
    this.renderer = renderer
    renderer.once('render-process-gone', (_, details) => {
      log.error(`Original Desktop kernel renderer stopped: ${details.reason}`)
      this.fail('renderer')
    })
    renderer.once('destroyed', () => {
      if (!this.quitStarted) {
        this.fail('renderer')
      }
    })
  }

  /** Failure never turns an already-dispatched write into a claimed cancellation. */
  public fail(phase: NativeKernelFailurePhase): void {
    if (this.failed || this.quitStarted) {
      return
    }
    this.failed = true
    this.ready = false
    this.send({
      jsonrpc: '2.0',
      method: 'kernel.failure',
      params: {
        phase,
        message:
          'Desktop kernel failed; inspect the application logs before retrying an operation',
      },
    })
    for (const id of this.pending) {
      this.send({
        jsonrpc: '2.0',
        id,
        error: {
          code: -32000,
          message: this.queued.has(id)
            ? 'Desktop kernel failed before this action was started'
            : 'Desktop kernel stopped; this operation may already have changed the repository',
        },
      })
    }
    this.pending.clear()
    this.queued.clear()
    this.renderer = undefined
    this.stopAdmission()
  }

  public fatal(): void {
    this.fail('main')
  }

  public action(action: NativeWindowAction): void {
    this.send({ jsonrpc: '2.0', method: 'window.action', params: { action } })
  }

  public getState(): NativeWindowState {
    return this.state
  }

  public setZoom(factor: number): void {
    this.send({ jsonrpc: '2.0', method: 'window.zoom', params: { factor } })
  }

  private stopAdmission(): void {
    this.inputClosed = true
    this.lines?.close()
    // Readline pauses the stream when closed. Drain without admitting more
    // commands so the parent's acknowledged shutdown can release its pipe.
    // Electron must observe EOF before joining a Windows blocking fd reader.
    this.input.resume()
    this.quitWhenSettled()
  }

  private outputFailed(error: Error): void {
    if (this.outputError !== undefined) {
      return
    }
    this.outputError = error
    log.error(
      'Native kernel output failed; no further actions will be admitted',
      error
    )
    // These commands have not reached Dispatcher. Do not execute them once
    // there is no longer a result channel; dispatched actions still settle.
    for (const id of this.queued.keys()) {
      this.pending.delete(id)
    }
    this.queued.clear()
    this.stopAdmission()
  }

  private quitWhenSettled(): void {
    if (
      !this.inputClosed ||
      this.pending.size !== 0 ||
      this.writes !== 0 ||
      (!this.failed && !this.outputError && !this.input.readableEnded) ||
      this.quitStarted
    ) {
      return
    }
    this.quitStarted = true
    this.quit(this.failed || this.outputError ? 1 : 0)
  }

  private send(value: unknown): void {
    if (this.outputError !== undefined || this.quitStarted) {
      return
    }
    // stdin EOF is independent of stdout: preserve every admitted outcome and
    // wait for its write callback before quitting, including a final commit.
    this.writes++
    try {
      this.output.write(JSON.stringify(value) + '\n', error => {
        this.writes--
        if (error) {
          this.outputFailed(error)
        }
        this.quitWhenSettled()
      })
    } catch (error) {
      this.writes--
      this.outputFailed(error as Error)
    }
  }

  private updateWindowState(id: number, value: unknown): void {
    const next = windowState(value)
    if (next === undefined) {
      this.send({
        jsonrpc: '2.0',
        id,
        error: { code: -32602, message: 'Invalid native window state' },
      })
      return
    }
    if (this.renderer) {
      if (next.focused !== this.state.focused) {
        ipcWebContents.send(this.renderer, next.focused ? 'focus' : 'blur')
      }
      if (next.state !== this.state.state) {
        ipcWebContents.send(this.renderer, 'window-state-changed', next.state)
      }
      if (next.zoom !== this.state.zoom) {
        ipcWebContents.send(this.renderer, 'zoom-factor-changed', next.zoom)
      }
    }
    this.state = next
    this.send({ jsonrpc: '2.0', id, result: { updated: true } })
  }

  private async read(): Promise<void> {
    const lines = createInterface({ input: this.input, crlfDelay: Infinity })
    this.lines = lines
    try {
      for await (const line of lines) {
        if (this.inputClosed) {
          break
        }
        let message: unknown
        try {
          if (Buffer.byteLength(line) > 8 * 1024 * 1024) {
            throw new Error('size')
          }
          message = JSON.parse(line)
        } catch {
          this.send({
            jsonrpc: '2.0',
            id: null,
            error: { code: -32700, message: 'Invalid native JSON request' },
          })
          continue
        }
        if (
          !isRecord(message) ||
          message.jsonrpc !== '2.0' ||
          !validID(message.id) ||
          typeof message.method !== 'string'
        ) {
          this.send({
            jsonrpc: '2.0',
            id: null,
            error: { code: -32600, message: 'Invalid native request' },
          })
          continue
        }
        const id = message.id
        // A conflicting control request must not masquerade as the outcome of
        // an in-flight commit with the same ID. Diagnose with null, keep it alive.
        if (this.pending.has(id)) {
          this.send({
            jsonrpc: '2.0',
            id: null,
            error: {
              code: -32600,
              message:
                'Duplicate active native request ID; the original outcome remains pending',
            },
          })
          continue
        }
        if (message.method === 'kernel.shutdown') {
          if (this.pending.size !== 0) {
            this.send({
              jsonrpc: '2.0',
              id,
              error: {
                code: -32001,
                message:
                  'Desktop actions are still pending; await their outcomes before quitting',
              },
            })
          } else {
            this.send({ jsonrpc: '2.0', id, result: { quitting: true } })
            this.stopAdmission()
            break
          }
          continue
        }
        if (message.method === 'kernel.windowState') {
          this.updateWindowState(id, message.params)
          continue
        }
        if (message.method !== 'kernel.command') {
          this.send({
            jsonrpc: '2.0',
            id,
            error: { code: -32601, message: 'Unknown native method' },
          })
          continue
        }
        try {
          const command = parseKernelCommand(message.params)
          this.pending.add(id)
          if (this.ready && this.renderer) {
            ipcWebContents.send(this.renderer, 'native-kernel-request', {
              id,
              command,
            })
          } else {
            this.queued.set(id, command)
          }
        } catch {
          this.send({
            jsonrpc: '2.0',
            id,
            error: {
              code: -32602,
              message: 'Invalid Desktop presentation command',
            },
          })
        }
      }
    } finally {
      lines.close()
      log.info(
        `Native kernel parent input closed; ${this.pending.size} admitted actions remain`
      )
      // No operation is replayed, killed or declared cancelled here. Its
      // original owner remains alive until its pending outcome is flushed.
      this.stopAdmission()
    }
  }
}

import { promisify } from 'util'

// The real CommonJS exports object, not a bundler namespace copy: callers such
// as dugite read `child_process.execFile` from this same object at call time.
const childProcess: typeof import('child_process') = require('child_process')

/**
 * Electron is a GUI process without a console on Windows. Every console
 * program it starts (Git, hooks, credential helpers) therefore gets a new,
 * visible console window unless `windowsHide` is set, which shows up as
 * flashing black windows. Upstream call sites rarely pass the option, so the
 * default is changed once at the module boundary instead of at each caller.
 *
 * Launches that are meant to be seen keep their behavior: an explicit
 * `windowsHide`, a `detached` editor/integration, or a `shell` terminal launch.
 */
type Options = Record<string, unknown>

const methods = [
  'spawn',
  'spawnSync',
  'execFile',
  'execFileSync',
  'exec',
  'execSync',
  'fork',
] as const

type Method = typeof methods[number]

const installed = Symbol.for('gitowl.hideChildProcessWindows')

function isOptions(value: unknown): value is Options {
  return (
    typeof value === 'object' &&
    value !== null &&
    !Array.isArray(value) &&
    !ArrayBuffer.isView(value)
  )
}

function hidden(options: Options): Options {
  if (
    'windowsHide' in options ||
    options.detached === true ||
    (options.shell !== undefined && options.shell !== false)
  ) {
    return options
  }
  return { ...options, windowsHide: true }
}

/** Returns the arguments with a hidden-window default for one method call. */
export function withHiddenWindow(
  method: Method,
  args: ReadonlyArray<unknown>
): Array<unknown> {
  const next = [...args]
  for (let i = 1; i < next.length; i++) {
    if (isOptions(next[i])) {
      next[i] = hidden(next[i] as Options)
      return next
    }
  }
  // No options object: place one where Node expects it, before a callback.
  const position =
    method === 'exec' || method === 'execSync'
      ? 1
      : Array.isArray(next[1])
      ? 2
      : 1
  const options = { windowsHide: true }
  if (next.length > position && next[position] == null) {
    next[position] = options
  } else if (next.length < position) {
    while (next.length < position) {
      next.push(undefined)
    }
    next.push(options)
  } else {
    next.splice(position, 0, options)
  }
  return next
}

/** Installs the default on a `child_process`-shaped module once. */
export function hideChildProcessWindows(
  target: Record<string, unknown> = childProcess as unknown as Record<
    string,
    unknown
  >,
  platform: NodeJS.Platform = process.platform
): void {
  if (platform !== 'win32' || Reflect.get(target, installed) === true) {
    return
  }
  for (const method of methods) {
    const original = target[method]
    if (typeof original !== 'function') {
      continue
    }
    const wrapped = function (this: unknown, ...args: Array<unknown>) {
      return original.apply(this, withHiddenWindow(method, args))
    }
    // promisify(execFile/exec) resolves { stdout, stderr }; keep that
    // contract by routing the adjusted arguments through Node's own version.
    const custom = (original as unknown as Record<symbol, unknown>)[
      promisify.custom
    ]
    if (typeof custom === 'function') {
      Object.defineProperty(wrapped, promisify.custom, {
        value: (...args: Array<unknown>) =>
          custom(...withHiddenWindow(method, args)),
      })
    }
    target[method] = wrapped
  }
  Object.defineProperty(target, installed, { value: true })
}

hideChildProcessWindows()

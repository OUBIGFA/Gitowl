import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { promisify } from 'util'
import {
  hideChildProcessWindows,
  withHiddenWindow,
} from '../../src/lib/hide-child-process-windows'

const callback = () => {}

describe('hidden child process windows', () => {
  it('adds the default where Node expects options', () => {
    assert.deepEqual(withHiddenWindow('spawn', ['git']), [
      'git',
      { windowsHide: true },
    ])
    assert.deepEqual(withHiddenWindow('spawn', ['git', ['status']]), [
      'git',
      ['status'],
      { windowsHide: true },
    ])
    assert.deepEqual(
      withHiddenWindow('execFile', ['git', ['status'], callback]),
      ['git', ['status'], { windowsHide: true }, callback]
    )
    assert.deepEqual(withHiddenWindow('execFile', ['git', callback]), [
      'git',
      { windowsHide: true },
      callback,
    ])
    assert.deepEqual(
      withHiddenWindow('execFile', ['git', ['status'], undefined, callback]),
      ['git', ['status'], { windowsHide: true }, callback]
    )
    assert.deepEqual(withHiddenWindow('exec', ['git status', callback]), [
      'git status',
      { windowsHide: true },
      callback,
    ])
  })

  it('keeps the caller options and adds only the missing default', () => {
    const env = { PATH: 'x' }
    assert.deepEqual(withHiddenWindow('spawn', ['git', ['log'], { env }]), [
      'git',
      ['log'],
      { env, windowsHide: true },
    ])
  })

  it('leaves explicit and intentionally visible launches unchanged', () => {
    for (const options of [
      { windowsHide: false },
      { detached: true },
      { shell: true },
      { shell: 'cmd.exe' },
    ]) {
      const [, , actual] = withHiddenWindow('spawn', ['tool', [], options])
      assert.equal(actual, options)
    }
  })

  it('installs once, only on Windows, and keeps promisify results', async () => {
    const seen: Array<unknown> = []
    const execFile = Object.assign(
      (...args: Array<unknown>) => {
        seen.push(args)
      },
      {
        [promisify.custom]: (...args: Array<unknown>) => {
          seen.push(args)
          return Promise.resolve({ stdout: 'out', stderr: '' })
        },
      }
    )
    const other = { execFile }
    hideChildProcessWindows(other, 'linux')
    assert.equal(other.execFile, execFile)

    const target: Record<string, unknown> = { execFile }
    hideChildProcessWindows(target, 'win32')
    const wrapped = target.execFile
    hideChildProcessWindows(target, 'win32')
    assert.equal(target.execFile, wrapped)
    ;(wrapped as (...args: Array<unknown>) => void)('git', ['status'])
    const promised = promisify(wrapped as any) as (
      ...args: Array<unknown>
    ) => Promise<unknown>
    const result = await promised('git', ['status'])
    assert.deepEqual(result, { stdout: 'out', stderr: '' })
    assert.deepEqual(seen, [
      ['git', ['status'], { windowsHide: true }],
      ['git', ['status'], { windowsHide: true }],
    ])
  })

  it('keeps the real execFile promise contract after installation', async () => {
    const real: typeof import('child_process') = require('child_process')
    hideChildProcessWindows(real, 'win32')
    const result = await promisify(real.execFile)(process.execPath, [
      '-e',
      'process.stdout.write("ok")',
    ])
    assert.deepEqual(result, { stdout: 'ok', stderr: '' })
  })
})

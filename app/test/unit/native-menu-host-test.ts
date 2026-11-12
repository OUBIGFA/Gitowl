import assert from 'node:assert/strict'
import { it, mock } from 'node:test'

class WindowBoundary {
  public static getAllWindows() {
    return []
  }
  public shown = 0
  public events: unknown[][] = []
  public webContents = {
    isDestroyed: () => false,
    send: (...args: unknown[]) => this.events.push(args),
  }
  public isVisible() {
    return false
  }
  public show() {
    this.shown++
  }
}

it('an original menu click emits once without exposing the hidden native kernel window', async () => {
  // Replace only the already-mocked Electron boundary for this isolated test
  // process; production stores and the real menu implementation stay intact.
  mock.restoreAll()
  const boundary = mock.module('electron', {
    namedExports: {
      BrowserWindow: WindowBoundary,
      Menu: {},
      app: {},
      shell: { openPath: async () => '' },
      ipcRenderer: { invoke: async () => {}, on: () => {} },
    },
  })
  const { emit } = await import(
    '../../src/main-process/menu/build-default-menu'
  )
  const previous = process.env.GITOWL_NATIVE_CORE
  try {
    const window = new WindowBoundary()
    process.env.GITOWL_NATIVE_CORE = '1'
    emit('show-preferences')(
      {} as Electron.MenuItem,
      window as unknown as Electron.BrowserWindow,
      {} as Electron.KeyboardEvent
    )
    assert.equal(window.shown, 0)
    assert.deepEqual(window.events, [['menu-event', 'show-preferences']])
    delete process.env.GITOWL_NATIVE_CORE
    emit('show-about')(
      {} as Electron.MenuItem,
      window as unknown as Electron.BrowserWindow,
      {} as Electron.KeyboardEvent
    )
    assert.equal(
      window.shown,
      1,
      'ordinary upstream menu behavior stays unchanged'
    )
  } finally {
    boundary.restore()
    if (previous === undefined) {
      delete process.env.GITOWL_NATIVE_CORE
    } else {
      process.env.GITOWL_NATIVE_CORE = previous
    }
  }
})

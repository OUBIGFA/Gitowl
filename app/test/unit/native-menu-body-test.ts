import assert from 'node:assert/strict'
import { after, it } from 'node:test'
import { EventEmitter } from 'events'
import { ipcRenderer } from 'electron'

const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
after(() => window.close())

it('imports and executes the shared native/test menu controller with body0 and no Canvas initialization', async t => {
  const create = document.createElement.bind(document)
  t.mock.method(
    document,
    'createElement',
    (name: string, options?: ElementCreationOptions) => {
      assert.notEqual(
        name.toLowerCase(),
        'canvas',
        'native menu import must not initialize a Canvas-backed React dependency'
      )
      return create(name, options)
    }
  )
  assert.equal(document.body.childElementCount, 0)
  const { NativeMenuPresenter } = await import(
    '../../src/native-kernel/menu-presenter'
  )
  const { getAppRuntime } = await import('../../src/ui/app-runtime')
  const { PopupType } = await import('../../src/models/popup')
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  const runtime = getAppRuntime()
  const presenter = new NativeMenuPresenter(runtime)
  const dev = __DEV__
  try {
    Object.assign(globalThis, { __DEV__: true })
    await presenter.handleEvent('test-about-dialog')
    assert.equal(
      runtime.appStore.getState().currentPopup?.type,
      PopupType.TestAbout
    )
    await runtime.dispatcher.closePopup(PopupType.TestAbout)
    await presenter.handleEvent('test-icons')
    assert.equal(
      runtime.appStore.getState().currentPopup?.type,
      PopupType.TestIcons
    )
    assert.equal(document.body.childElementCount, 0)
  } finally {
    Object.assign(globalThis, { __DEV__: dev })
    presenter.dispose()
  }
})

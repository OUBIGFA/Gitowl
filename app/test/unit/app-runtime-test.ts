import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { EventEmitter } from 'events'
import { ipcRenderer } from 'electron'

// The repository's global test setup already supplies the Electron platform
// object. Complete that platform boundary, without mocking any kernel store.
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})

describe('shared upstream app runtime', () => {
  it('returns the same original kernel and routes frontend changes through its Dispatcher', async t => {
    // Dispose the test's browser realm after exercising the renderer kernel;
    // its legitimate background timers must not outlive this unit-test owner.
    t.after(() => window.close())
    localStorage.clear()
    // The test never opts a real user into telemetry or performs a request.
    localStorage.setItem('stats-opt-out', '1')
    localStorage.setItem('has-sent-stats-opt-in-ping', '1')
    const { getAppRuntime } = await import('../../src/ui/app-runtime')
    const { DesktopPresenter } = await import(
      '../../src/native-kernel/desktop-presenter'
    )
    const { AppStore } = await import('../../src/lib/stores/app-store')
    const { Dispatcher } = await import('../../src/ui/dispatcher/dispatcher')
    const runtime = getAppRuntime()
    assert.equal(getAppRuntime(), runtime)
    assert.ok(runtime.appStore instanceof AppStore)
    assert.ok(runtime.dispatcher instanceof Dispatcher)
    assert.equal(await runtime.accountsStore.getAll().then(a => a.length), 0)
    await runtime.dispatcher.setRepositoryFilterText('shared-project')
    assert.equal(
      runtime.appStore.getState().repositoryFilterText,
      'shared-project'
    )
    await getAppRuntime().dispatcher.setAccessKeyHighlightState(true)
    assert.equal(runtime.appStore.getState().highlightAccessKeys, true)
    await runtime.dispatcher.setAccessKeyHighlightState(false)
    assert.equal(getAppRuntime().appStore.getState().highlightAccessKeys, false)
    const presenter = new DesktopPresenter(runtime)
    const { PopupType } = await import('../../src/models/popup')
    await runtime.dispatcher.showPopup({ type: PopupType.About })
    const popup = presenter.getState().currentPopup
    assert.equal(popup?.type, PopupType.About)
    assert.ok(popup?.id !== undefined)
    await presenter.closePopup(popup.id)
    assert.equal(runtime.appStore.getState().currentPopup, null)
    assert.throws(
      () => presenter.getRepository({ id: 900, path: 'not-a-known-project' }),
      /reference is no longer available/
    )
  })
})

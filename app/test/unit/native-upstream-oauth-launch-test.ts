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

it('forwards both original callback schemes through original parseAppURL, Dispatcher and SignInStore exactly once', async t => {
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  const { getAppRuntime } = await import('../../src/ui/app-runtime')
  const { DesktopPresenter } = await import(
    '../../src/native-kernel/desktop-presenter'
  )
  const runtime = getAppRuntime()
  const presenter = new DesktopPresenter(runtime)
  // A passthrough spy: the real SignInStore must reject unsolicited callbacks,
  // without reading another product's sessions or issuing a token exchange.
  const callback = t.mock.method(runtime.signInStore, 'resolveOAuthRequest')
  const fetch = t.mock.method(globalThis, 'fetch', async () =>
    assert.fail('unsolicited callback must not exchange credentials')
  )
  for (const scheme of ['x-github-desktop-dev-auth', 'x-github-desktop-auth']) {
    const previous = callback.mock.callCount()
    await presenter.dispatchRawURL(`${scheme}://oauth?code=a%2Bb&state=x%2Fy`)
    assert.equal(callback.mock.callCount(), previous + 1)
    assert.deepEqual(callback.mock.calls[previous].arguments, [
      { name: 'oauth', code: 'a+b', state: 'x/y' },
    ])
  }
  assert.equal(runtime.signInStore.getState(), null)
  assert.equal(fetch.mock.callCount(), 0)
})

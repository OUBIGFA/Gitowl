import assert from 'node:assert/strict'
import { after, it } from 'node:test'
import { EventEmitter } from 'events'
import { ipcRenderer } from 'electron'
import { NativeStartup } from '../../src/native-kernel/startup'

const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
after(() => window.close())

it('runs the shared deferred startup only once and disposes recurring work and platform listeners', async t => {
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  const { getAppRuntime } = await import('../../src/ui/app-runtime')
  const runtime = getAppRuntime()
  // Keep original runtime/dispatch state. Isolate only asynchronous external
  // work; fake timers prove the host cannot leak a recurring startup job.
  const emoji = t.mock.method(runtime.appStore, 'loadEmoji', async () => {})
  const stats = t.mock.method(runtime.dispatcher, 'reportStats', async () => {})
  const lfs = t.mock.method(
    runtime.dispatcher,
    'installGlobalLFSFilters',
    async () => {}
  )
  const launch = t.mock.method(
    runtime.dispatcher,
    'recordLaunchStats',
    () => {}
  )
  const postError = t.mock.method(
    runtime.dispatcher,
    'postError',
    async () => {}
  )
  t.mock.timers.enable({ apis: ['setInterval'] })
  const before = ipc.listenerCount('launch-timing-stats')
  const startup = new NativeStartup(runtime)
  assert.equal(ipc.listenerCount('launch-timing-stats'), before + 1)
  await startup.start()
  await startup.start()
  assert.equal(emoji.mock.callCount(), 1)
  assert.equal(lfs.mock.callCount(), 1)
  assert.equal(stats.mock.callCount(), 1)
  ipc.emit(
    'launch-timing-stats',
    {},
    { mainReadyTime: 1, loadTime: 2, rendererReadyTime: 3 }
  )
  assert.equal(launch.mock.callCount(), 1)
  t.mock.timers.tick(4 * 60 * 60 * 1000)
  assert.equal(stats.mock.callCount(), 2)
  startup.checkForUpdates(false)
  assert.match(
    String(postError.mock.calls[0].arguments[0]),
    /not configured for Gitowl/
  )
  startup.dispose()
  startup.dispose()
  assert.equal(ipc.listenerCount('launch-timing-stats'), before)
  t.mock.timers.tick(4 * 60 * 60 * 1000)
  await startup.start()
  assert.equal(stats.mock.callCount(), 2)
})

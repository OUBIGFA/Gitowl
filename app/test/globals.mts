import 'fake-indexeddb/auto'
import 'global-jsdom/register'
import { mock } from 'node:test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { EventEmitter } from 'node:events'
import { createRequire } from 'node:module'
const { installDesktopHost } = createRequire(import.meta.url)(
  '../src/lib/desktop-host'
) as typeof import('../src/lib/desktop-host')

// These constants are defined by Webpack at build time, but since tests aren't
// built with Webpack we need to make sure these exist at runtime.
const packageInfo = await import('../package.json')

Object.assign(globalThis, {
  __DEV__: false,
  __TEST__: true,
  __DEV_SECRETS__: false,
  __APP_NAME__: packageInfo.productName,
  __APP_VERSION__: packageInfo.version,
  __RELEASE_CHANNEL__: 'development',
  __UPDATES_URL__: '',
  __SHA__: 'test',
  __DARWIN__: process.platform === 'darwin',
  __WIN32__: process.platform === 'win32',
  __LINUX__: process.platform === 'linux',
  log: {
    error: () => {},
    warn: () => {},
    info: () => {},
    debug: () => {},
  },

  // The following types are part of the WebWorker support in Node.js and are a
  // common source of hangs in tests due to libraries creating them but not
  // properly cleaning them up. See for example
  // https://github.com/facebook/react/issues/20756, and
  // https://github.com/dexie/Dexie.js/pull/1577.
  //
  // We've upgraded Dexie already but react-dom is a bigger beast and we don't
  // need any of them to run our tests so we just delete them here. In fact,
  // this is exactly what the react-16-node-hanging-test-fix patch does, see
  // https://www.npmjs.com/package/react-16-node-hanging-test-fix?activeTab=code
  MessageChannel: undefined,
  MessagePort: undefined,
  BroadcastChannel: undefined,
})

const fixtureEvents = new EventEmitter()
mock.module('electron', {
  namedExports: {
    shell: {
      // Consumed through `app-shell`'s `openPath`, which binds this eagerly at
      // import time, so it has to exist here for tests to be able to mock it.
      openPath: async () => '',
    },
    ipcRenderer: {
      send: mock.fn(() => {}),
      // `get-path` answers like Electron's app.getPath, with a disposable root.
      invoke: mock.fn(async (channel?: string, name?: string) =>
        channel === 'get-path'
          ? join(tmpdir(), 'desktop-unit', `${name}`)
          : undefined
      ),
      on: fixtureEvents.on.bind(fixtureEvents),
      once: fixtureEvents.once.bind(fixtureEvents),
      removeListener: fixtureEvents.removeListener.bind(fixtureEvents),
      emit: fixtureEvents.emit.bind(fixtureEvents),
    },
  },
})

// Existing store tests replace this historical boundary. Route that fixture to
// the new host so the same behavior assertions keep exercising the real stores.
const boundary = await import('electron')
const events = new EventEmitter()
events.on = ((channel, callback) => {
  boundary.ipcRenderer.on(String(channel), callback)
  return events
}) as typeof events.on
events.once = ((channel, callback) => {
  boundary.ipcRenderer.once(String(channel), callback)
  return events
}) as typeof events.once
events.removeListener = ((channel, callback) => {
  boundary.ipcRenderer.removeListener(String(channel), callback)
  return events
}) as typeof events.removeListener
events.emit = ((channel, ...args) =>
  boundary.ipcRenderer.emit(channel, ...args)) as typeof events.emit
installDesktopHost({
  userData: join(tmpdir(), 'desktop-unit'),
  events,
  send: (channel, args) => boundary.ipcRenderer.send(channel, ...args),
  invoke: (channel, args) => boundary.ipcRenderer.invoke(channel, ...args),
})

import assert from 'node:assert/strict'
import { after, it } from 'node:test'
import { ipcRenderer } from 'electron'
import { updateStore, UpdateStatus } from '../../src/ui/lib/update-store'

after(() => window.close())

it('manual About updates fail honestly before contacting an Electron feed or requesting Squirrel install', async t => {
  const previous = process.env.GITOWL_NATIVE_CORE
  const calls: string[] = []
  t.mock.method(globalThis, 'fetch', async () => {
    calls.push('fetch')
    throw new Error('must not contact a feed')
  })
  t.mock.method(ipcRenderer, 'invoke', async () => {
    calls.push('ipc')
  })
  Object.assign(ipcRenderer, {
    sendSync: () => {
      calls.push('quit')
    },
    send: () => {
      calls.push('send')
    },
  })
  try {
    process.env.GITOWL_NATIVE_CORE = '1'
    assert.equal(updateStore.state.status, UpdateStatus.UpdateNotChecked)
    await assert.rejects(
      updateStore.checkForUpdates(false, true),
      /Gitowl updates are not configured/
    )
    assert.throws(
      () => updateStore.quitAndInstallUpdate(),
      /Gitowl updates are not configured/
    )
    assert.equal(
      updateStore.state.status,
      UpdateStatus.UpdateNotChecked,
      'unconfigured must never become latest/update-not-available'
    )
    assert.deepEqual(calls, [])
  } finally {
    if (previous === undefined) {
      delete process.env.GITOWL_NATIVE_CORE
    } else {
      process.env.GITOWL_NATIVE_CORE = previous
    }
  }
})

import assert from 'node:assert/strict'
import { it } from 'node:test'
import { getDesktopHost } from '../../src/lib/desktop-host'
import { emit } from '../../src/main-process/menu/build-default-menu'

it('the shared menu definition delivers its original named action through the native host', () => {
  const host = getDesktopHost()
  const received: string[] = []
  const listener = (_: unknown, name: string) => received.push(name)
  host.events.on('menu-event', listener)
  try {
    emit('show-preferences')()
    emit('show-about')()
    assert.deepEqual(received, ['show-preferences', 'show-about'])
  } finally {
    host.events.removeListener('menu-event', listener)
  }
})

import assert from 'node:assert/strict'
import { it } from 'node:test'
import { PassThrough } from 'node:stream'
import { NodeDesktopHost } from '../../src/native-kernel/node-host'
import { NodeKernelTransport } from '../../src/native-kernel/node-transport'

it('retains notification event data across host restart and removes failed delivery', async t => {
  const transports: NodeKernelTransport[] = []
  const streams: PassThrough[] = []
  const createHost = () => {
    const input = new PassThrough()
    const output = new PassThrough()
    streams.push(input, output)
    const transport = new NodeKernelTransport(input, output, () => {})
    transports.push(transport)
    return new NodeDesktopHost('fixture-profile', transport)
  }
  t.after(() => streams.forEach(stream => stream.destroy()))
  const event = { type: 'pr-comment', owner: 'owner', repo: 'repo', pull_request_number: 3 }
  const first = createHost()
  let id = ''
  t.mock.method(transports[0], 'requestPlatform', async (_channel: string, args: readonly unknown[]) => {
    id = args[3] as string
    assert.deepEqual(JSON.parse(localStorage.getItem(`native-notification:${id}`)!), event)
    return id
  })
  assert.equal(await first.invoke('show-notification', ['title', 'body', event]), id)
  const next = createHost()
  const delivered: unknown[] = []
  next.events.on('notification-event', (_sender, kind, notification, info) => delivered.push([kind, notification, info]))
  transports[1].events.emit('notification-event', 'click', id)
  assert.deepEqual(delivered, [['click', id, event]])
  assert.equal(localStorage.getItem(`native-notification:${id}`), null)
  transports[1].events.emit('notification-event', 'click', 'unknown')
  assert.equal(delivered.length, 1)
  t.mock.method(transports[0], 'requestPlatform', async (_channel: string, args: readonly unknown[]) => {
    id = args[3] as string
    throw new Error('OS denied notification')
  })
  await assert.rejects(first.invoke('show-notification', ['title', 'body', event]), /OS denied/)
  assert.equal(localStorage.getItem(`native-notification:${id}`), null)
})

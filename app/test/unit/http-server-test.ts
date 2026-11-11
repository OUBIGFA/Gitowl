import assert from 'node:assert/strict'
import { it } from 'node:test'
import { createServer, Server } from 'http'
import { AddressInfo } from 'net'
import { listenOnLoopback } from '../helpers/http-server'

async function close(server: Server) {
  server.closeAllConnections()
  if (server.listening) {
    await new Promise<void>((resolve, reject) =>
      server.close(error => (error ? reject(error) : resolve()))
    )
  }
}

it('serves real HTTP when the OS first assigns a Fetch-blocked port', async t => {
  const server = createServer((_, response) => response.end('loopback fixture'))
  t.after(() => close(server))
  const address = server.address.bind(server)
  let firstAllocation = true
  // Only the OS-selected port is substituted. Listening, closing, rebinding
  // and the final Fetch request use real sockets; no service port is occupied.
  t.mock.method(server, 'address', () => {
    const actual = address()
    if (firstAllocation && actual !== null && typeof actual !== 'string') {
      firstAllocation = false
      return { ...actual, port: 6000 }
    }
    return actual
  })
  await listenOnLoopback(server)
  const endpoint = server.address() as AddressInfo
  const response = await fetch(`http://127.0.0.1:${endpoint.port}/`)
  assert.equal(await response.text(), 'loopback fixture')
  assert.equal(endpoint.address, '127.0.0.1')
})

it('keeps independent real loopback servers available at the same time', async t => {
  const servers = [
    createServer((_, r) => r.end('first')),
    createServer((_, r) => r.end('second')),
  ]
  t.after(async () => {
    await Promise.all(servers.map(close))
  })
  await Promise.all(servers.map(listenOnLoopback))
  const ports = servers.map(server => (server.address() as AddressInfo).port)
  assert.notEqual(ports[0], ports[1])
  assert.deepEqual(
    await Promise.all(
      ports.map(async port => (await fetch(`http://127.0.0.1:${port}/`)).text())
    ),
    ['first', 'second']
  )
})

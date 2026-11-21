import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { createServer, request as httpRequest } from 'node:http'
import { connect, Socket } from 'node:net'
import { once } from 'node:events'
import { Agent, fetch } from 'undici'
import { createHash } from 'node:crypto'
import { NativeProxyDispatcher, NativeWebSocket } from '../../src/native-kernel/node-network'

describe('native proxy delegation', () => {
  it('preserves the original Alive origin on real Undici WebSocket handshakes', { timeout: 15000 }, async () => {
    const server = createServer()
    const sockets = new Set<Socket>()
    const origins: Array<string | undefined> = []
    server.on('connection', socket => {
      sockets.add(socket)
      socket.on('close', () => sockets.delete(socket))
    })
    server.on('upgrade', (request, socket) => {
      origins.push(request.headers.origin)
      const accept = createHash('sha1')
        .update(request.headers['sec-websocket-key']! + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11')
        .digest('base64')
      socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ' + accept + '\r\n\r\n')
    })
    server.listen(0, '127.0.0.1')
    await once(server, 'listening')
    // The local fixture substitutes the network endpoint, not the client or
    // handshake. It never connects to a real account or sends credentials.
    const dispatcher = new Agent({ connect: (_options, callback) => {
      const socket = connect((server.address() as { port: number }).port, '127.0.0.1')
      socket.once('connect', () => callback(null, socket))
      socket.once('error', error => callback(error, null))
    } })
    try {
      for (const url of ['wss://alive.github.com/socket', 'wss://alive.acme.ghe.com/socket', 'wss://example.invalid/socket', 'ws://alive.github.com/socket']) {
        const socket = new NativeWebSocket(url, { dispatcher })
        await new Promise<void>((resolve, reject) => {
          socket.addEventListener('open', () => resolve(), { once: true })
          socket.addEventListener('error', () => reject(new Error('WebSocket handshake failed')), { once: true })
        })
      }
      assert.deepEqual(origins, ['https://desktop.github.com', 'https://desktop.acme.ghe.com', undefined, undefined])
    } finally {
      for (const socket of sockets) { socket.destroy() }
      await dispatcher.destroy()
      await new Promise<void>(resolve => server.close(() => resolve()))
    }
  })
  it(
    'sends real HTTP traffic through the selected proxy and propagates resolver failure',
    { timeout: 15000 },
    async () => {
      const target = createServer((_, response) =>
        response.end('through native proxy')
      )
      target.listen(0, '127.0.0.1')
      await once(target, 'listening')
      let connections = 0
      const proxy = createServer((request, response) => {
        connections++
        const forwarded = httpRequest(
          request.url!,
          { headers: request.headers },
          upstream => {
            response.writeHead(upstream.statusCode!, upstream.headers)
            upstream.pipe(response)
          }
        )
        forwarded.on('error', () => response.destroy())
        request.pipe(forwarded)
      })
      const sockets = new Set<Socket>()
      proxy.on('connection', socket => {
        sockets.add(socket)
        socket.on('close', () => sockets.delete(socket))
      })
      proxy.on('connect', (request, client, head) => {
        connections++
        const [host, port] = request.url!.split(':')
        const upstream = connect(Number(port), host, () => {
          client.write('HTTP/1.1 200 Connection Established\r\n\r\n')
          if (head.length) {upstream.write(head)}
          client.pipe(upstream).pipe(client)
        })
        upstream.on('error', () => client.destroy())
        client.on('error', () => upstream.destroy())
        client.on('close', () => upstream.destroy())
      })
      proxy.listen(0, '127.0.0.1')
      await once(proxy, 'listening')
      const port = (server: typeof target) =>
        (server.address() as { port: number }).port
      const dispatcher = new NativeProxyDispatcher(
        async () => `http://127.0.0.1:${port(proxy)}`
      )
      try {
        const response = await fetch(`http://127.0.0.1:${port(target)}/data`, {
          dispatcher,
          signal: AbortSignal.timeout(5000),
        })
        assert.equal(await response.text(), 'through native proxy')
        assert.equal(connections, 1)
        const failure = new NativeProxyDispatcher(async () => {
          throw new Error('resolver unavailable')
        })
        await assert.rejects(
          fetch(`http://127.0.0.1:${port(target)}/data`, {
            dispatcher: failure,
            signal: AbortSignal.timeout(5000),
          }),
          error =>
            (error as Error & { cause?: Error }).cause?.message ===
            'resolver unavailable'
        )
        await failure.destroy()
      } finally {
        await dispatcher.destroy()
        for (const socket of sockets) {socket.destroy()}
        target.closeAllConnections()
        await Promise.all([
          new Promise<void>(r => target.close(() => r())),
          new Promise<void>(r => proxy.close(() => r())),
        ])
      }
    }
  )
})

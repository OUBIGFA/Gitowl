import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { PassThrough } from 'stream'
import { createInterface } from 'readline'
import { serve, INativeRPCService } from '../../src/native-service/protocol'

const request = (id: number, method: string) =>
  JSON.stringify({ jsonrpc: '2.0', id, method }) + '\n'

describe('native RPC cancellation', () => {
  it('serves a second request while a slow operation is active and cancels only the matching one', async () => {
    const input = new PassThrough()
    const output = new PassThrough()
    const lines = createInterface({ input: output })
    const replies = lines[Symbol.asyncIterator]()
    let closed = 0
    const service: INativeRPCService = {
      invoke: async (method, _params, signal) => {
        if (method === 'slow') {
          await new Promise<void>((_resolve, reject) =>
            signal!.addEventListener(
              'abort',
              () => reject(new Error('cancelled')),
              { once: true }
            )
          )
        }
        return method
      },
      close: async () => {
        closed++
      },
    }
    const running = serve(input, output, service)
    try {
      input.write(request(1, 'slow') + request(2, 'fast'))
      assert.deepEqual(JSON.parse((await replies.next()).value!), {
        jsonrpc: '2.0',
        id: 2,
        result: 'fast',
      })
      input.write(
        JSON.stringify({
          jsonrpc: '2.0',
          method: '$/cancelRequest',
          params: { id: 1 },
        }) + '\n'
      )
      const cancelled = JSON.parse((await replies.next()).value!)
      assert.equal(cancelled.id, 1)
      assert.equal(cancelled.error.code, -32800)
      input.write(request(3, 'still-connected'))
      assert.equal(
        JSON.parse((await replies.next()).value!).result,
        'still-connected'
      )
      input.end()
      await running
      assert.equal(closed, 1)
    } finally {
      input.destroy()
      output.destroy()
      lines.close()
    }
  })

  it('cancels owned work and releases services when the parent closes stdin', async () => {
    const input = new PassThrough()
    const output = new PassThrough()
    let start!: () => void
    const started = new Promise<void>(resolve => {
      start = resolve
    })
    let aborted = false
    let closed = false
    const running = serve(input, output, {
      invoke: async (_method, _params, signal) =>
        new Promise((_resolve, reject) => {
          signal!.addEventListener(
            'abort',
            () => {
              aborted = true
              reject(new Error('owner left'))
            },
            { once: true }
          )
          start()
        }),
      close: async () => {
        closed = true
      },
    })
    input.write(request(1, 'slow'))
    await started
    input.end()
    await running
    assert.equal(aborted, true)
    assert.equal(closed, true)
    output.destroy()
  })
})

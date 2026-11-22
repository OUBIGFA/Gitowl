import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { describe, it, mock } from 'node:test'

// Only the OS thread is replaced: the client's own scheduling, request
// matching and shutdown run unchanged.
const workers: FakeWorker[] = []
class FakeWorker extends EventEmitter {
  public terminated = false
  public constructor() {
    super()
    workers.push(this)
  }
  public unref() {}
  public postMessage(message: { id: number }) {
    const frame = { data: new Uint8Array(4), width: 1, height: 1 }
    queueMicrotask(() => this.emit('message', { id: message.id, frame }))
  }
  public terminate() {
    this.terminated = true
    return Promise.resolve(0)
  }
}
mock.module('worker_threads', { namedExports: { Worker: FakeWorker } })

describe('native image worker client', () => {
  it('stops an idle decoder and starts a new one for the next image', async () => {
    mock.timers.enable({ apis: ['setTimeout'] })
    try {
      const { decodeNativeImage, stopImageWorker } = await import(
        '../../src/native-kernel/image-worker-client'
      )
      await decodeNativeImage(new Uint8Array(1), 0, 'image/png')
      await decodeNativeImage(new Uint8Array(1), 0, 'image/png')
      assert.equal(workers.length, 1)
      mock.timers.tick(59_999)
      assert.equal(workers[0].terminated, false)
      mock.timers.tick(1)
      assert.equal(workers[0].terminated, true)

      await decodeNativeImage(new Uint8Array(1), 0, 'image/png')
      assert.equal(workers.length, 2)
      stopImageWorker()
      assert.equal(workers[1].terminated, true)
    } finally {
      mock.timers.reset()
    }
  })
})

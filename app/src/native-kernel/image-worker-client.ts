import { Worker } from 'worker_threads'
import { join } from 'path'
import type { IDecodedImageFrame } from './node-image-decoder'

let worker: Worker | undefined
let nextID = 0
const pending = new Map<
  number,
  {
    resolve: (frame: IDecodedImageFrame) => void
    reject: (error: Error) => void
    timer: ReturnType<typeof setTimeout>
  }
>()

/** Codec work cannot block the Git/auth event loop. One lazy worker reuses the
 * official decoder instance; its timeout can actually interrupt CPU-bound work.
 */
export function decodeNativeImage(
  data: Uint8Array,
  index: number,
  mediaType: string
): Promise<IDecodedImageFrame> {
  if (worker === undefined) {
    const active = new Worker(join(__dirname, 'image-worker.js'), {
      resourceLimits: { maxOldGenerationSizeMb: 384 },
    })
    worker = active
    active.unref()
    active.on('message', result => {
      const request = pending.get(result.id)
      if (!request) {return}
      pending.delete(result.id)
      clearTimeout(request.timer)
      if (result.error) {request.reject(new Error(result.error))}
      else {request.resolve(result.frame)}
    })
    active.on('error', () =>
      stopImageWorker(new Error('The image decoder stopped'))
    )
    active.on('exit', code => {
      if (active === worker)
        {stopImageWorker(new Error(`The image decoder exited (${code})`))}
    })
  }
  return new Promise((resolve, reject) => {
    const id = ++nextID
    const timer = setTimeout(
      () => stopImageWorker(new Error('Image decoding timed out')),
      15000
    )
    pending.set(id, { resolve, reject, timer })
    const bytes = Uint8Array.from(data)
    worker!.postMessage({ id, data: bytes, index, mediaType }, [bytes.buffer])
  })
}

export function stopImageWorker(
  error = new Error('Image decoding stopped during shutdown')
): void {
  const active = worker
  worker = undefined
  if (active) {void active.terminate()}
  for (const request of pending.values()) {
    clearTimeout(request.timer)
    request.reject(error)
  }
  pending.clear()
}

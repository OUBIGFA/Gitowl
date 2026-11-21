import { parentPort } from 'worker_threads'
import { decodeImageFrame } from './node-image-decoder'

parentPort!.on('message', async ({ id, data, index, mediaType }) => {
  try {
    const frame = await decodeImageFrame(data, index, mediaType)
    parentPort!.postMessage({ id, frame }, [frame.data.buffer as ArrayBuffer])
  } catch {
    // Codec diagnostics can contain file content. Keep the process boundary's
    // error vocabulary independent of arbitrary decoder messages.
    parentPort!.postMessage({
      id,
      error: 'The image decoder could not display this image',
    })
  }
})

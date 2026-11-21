import { readFile } from 'fs/promises'
import { createRequire } from 'module'
import {
  ImageMagick,
  initializeImageMagick,
  MagickFormat,
  ResourceLimits,
} from '@imagemagick/magick-wasm'
import * as UPNG from 'upng-js'
declare const __non_webpack_require__: NodeRequire

// Webpack rewrites even createRequire(...).resolve(literal) to its module ID.
// Its documented native require escape keeps the package's actual WASM path.
const nativeRequire =
  typeof __non_webpack_require__ === 'function'
    ? __non_webpack_require__
    : createRequire(__filename)

export interface IDecodedImageFrame {
  readonly data: Uint8Array
  readonly width: number
  readonly height: number
  readonly duration: number
  readonly frames: number
  readonly repetitions: number
}

let initialized: Promise<void> | undefined

/** ImageMagick owns codec parsing, disposal/compositing, orientation and color
 * conversion. The host only translates its documented frame metadata to the
 * existing native presentation contract (microseconds and repeat count).
 */
export async function decodeImageFrame(
  data: Uint8Array,
  index: number,
  mediaType?: string
): Promise<IDecodedImageFrame> {
  // ImageMagick's APNG coder delegates to an external ffmpeg executable, which
  // is unavailable in its WASM distribution. Reuse Photopea's APNG decoder and
  // complete-frame compositor instead of interpreting animation chunks here.
  if (mediaType === 'image/png') {
    const png = UPNG.decode(Uint8Array.from(data).buffer)
    if (png.tabs.acTL !== undefined) {
      const count = png.frames.length
      if (!Number.isSafeInteger(index) || index < 0 || index >= count) {
        throw new Error('Image frame is no longer available')
      }
      if (png.width * png.height * 4 * count > 256 * 1024 * 1024) {
        throw new Error('Image exceeds the decoded preview memory budget')
      }
      const frames = UPNG.toRGBA8(png)
      const loops = png.tabs.acTL.num_plays
      return {
        data: new Uint8Array(frames[index]),
        width: png.width,
        height: png.height,
        frames: count,
        duration: png.frames[index].delay * 1000,
        repetitions: loops === 0 ? -1 : loops - 1,
      }
    }
  }
  initialized ??= readFile(
    nativeRequire.resolve('@imagemagick/magick-wasm/magick.wasm')
  )
    .then(bytes => initializeImageMagick(bytes))
    .then(() => {
      ResourceLimits.memory = BigInt(256 * 1024 * 1024)
      ResourceLimits.disk = BigInt(0)
      ResourceLimits.maxMemoryRequest = BigInt(256 * 1024 * 1024)
      ResourceLimits.time = BigInt(15)
    })
  await initialized
  const decode = (
    images: import('@imagemagick/magick-wasm').IMagickImageCollection
  ) => {
    if (!Number.isSafeInteger(index) || index < 0 || index >= images.length) {
      throw new Error('Image frame is no longer available')
    }
    const frames = images.length
    const iterations = images[0].animationIterations
    if (frames > 1) {
      images.coalesce()
    }
    const image = images[index]
    image.autoOrient()
    const { width, height } = image
    if (width <= 0 || height <= 0 || width * height * 4 > 256 * 1024 * 1024) {
      throw new Error('Image exceeds the decoded preview memory budget')
    }
    const pixels = image.getPixels(p =>
      p.toByteArray(0, 0, width, height, 'RGBA')
    )
    if (pixels === null || pixels.length !== width * height * 4) {
      throw new Error('Image decoding returned an invalid pixel plane')
    }
    return {
      data: Uint8Array.from(pixels),
      width,
      height,
      frames,
      duration:
        frames > 1
          ? Math.round(
              (image.animationDelay * 1000000) / image.animationTicksPerSecond
            )
          : 100000,
      repetitions: frames > 1 ? (iterations === 0 ? -1 : iterations - 1) : 0,
    }
  }
  return mediaType === 'image/x-icon' ||
    mediaType === 'image/vnd.microsoft.icon'
    ? ImageMagick.readCollection(data, MagickFormat.Ico, decode)
    : ImageMagick.readCollection(data, decode)
}

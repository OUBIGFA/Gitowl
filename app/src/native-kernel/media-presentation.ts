import parseDDS from 'parse-dds'
import { createHash, randomUUID } from 'crypto'
import type { Image as RepositoryImage } from '../models/diff'
import { convertDDSImage } from '../ui/diff/image-diffs/dds-converter'

/** Pixel presentation only. No filesystem, Git, account or network authority. */
export interface INativeMediaCommand {
  readonly name: 'media.frame.read'
  readonly mediaID: string
  readonly frame: number
  readonly offset: number
}
export interface IKernelMediaChunk {
  readonly contents: string
  readonly offset: number
  readonly total: number
  readonly width: number
  readonly height: number
  readonly duration: number
  readonly frames: number
  /** -1 means repeat forever; otherwise repetitions after the first playback. */
  readonly repetitions: number
}
interface IPixels {
  readonly data: Uint8Array
  readonly width: number
  readonly height: number
  readonly duration: number
}
interface IMedia {
  readonly fingerprint: string
  readonly owners: Set<object>
  readonly image: RepositoryImage
  element?: HTMLImageElement
  decoder?: ImageDecoder
  initialization?: Promise<void>
  frame?: { index: number; pixels: Promise<IPixels> }
  frames: number
  repetitions: number
  closed: boolean
}
const ids = new WeakMap<RepositoryImage, string>()
const fingerprints = new WeakMap<RepositoryImage, string>()
const contentIDs = new Map<string, string>()
const media = new Map<string, IMedia>()
let projected: Set<string> | null = null
let projectedOwners: Map<object, Set<string>> | null = null

// Keep only media the current original snapshot presents. A removed image's
// decoder is closed, and its in-flight reply is rejected rather than recycled.
export function withMediaProjection<T>(
  project: () => T,
  owners: ReadonlyArray<object> = []
): T {
  const seen = new Set<string>()
  projected = seen
  const ownerImages = new Map<object, Set<string>>()
  projectedOwners = ownerImages
  try {
    return project()
  } finally {
    projected = null
    projectedOwners = null
    for (const [id, entry] of media) {
      for (const owner of entry.owners) {
        if (
          !owners.includes(owner) ||
          (ownerImages.has(owner) && !ownerImages.get(owner)!.has(id))
        ) {
          entry.owners.delete(owner)
        }
      }
      if (!seen.has(id) && entry.owners.size === 0) {
        entry.closed = true
        entry.decoder?.close()
        if (entry.element) {
          entry.element.src = ''
        }
        media.delete(id)
        if (contentIDs.get(entry.fingerprint) === id) {
          contentIDs.delete(entry.fingerprint)
        }
      }
    }
  }
}
export function projectKernelMediaOwner(owner: object): void {
  projectedOwners?.set(owner, new Set())
}

export function registerKernelMedia(
  image: RepositoryImage,
  owner?: object
): string {
  let id = ids.get(image)
  if (id === undefined || !media.has(id)) {
    // AppStore can refresh an unchanged Image object. Original <img> retains
    // its identical data URL in that case; keep decoder identity/timing too.
    let fingerprint = fingerprints.get(image)
    if (fingerprint === undefined) {
      fingerprint = createHash('sha256')
        .update(image.mediaType)
        .update('\0')
        .update(String(image.bytes))
        .update('\0')
        .update(image.contents)
        .digest('hex')
      fingerprints.set(image, fingerprint)
    }
    id = contentIDs.get(fingerprint) ?? randomUUID()
    ids.set(image, id)
    if (!media.has(id)) {
      media.set(id, {
        image,
        fingerprint,
        owners: new Set(),
        frames: 1,
        repetitions: 0,
        closed: false,
      })
      contentIDs.set(fingerprint, id)
    }
  }
  if (owner !== undefined) {
    media.get(id)!.owners.add(owner)
    if (projectedOwners !== null) {
      if (!projectedOwners.has(owner)) {
        projectedOwners.set(owner, new Set())
      }
      projectedOwners.get(owner)!.add(id)
    }
  }
  projected?.add(id)
  return id
}

export function parseNativeMediaCommand(
  value: Record<string, unknown>
): INativeMediaCommand {
  if (
    Object.keys(value).some(
      k => !['name', 'mediaID', 'frame', 'offset'].includes(k)
    ) ||
    typeof value.mediaID !== 'string' ||
    !/^[0-9a-f-]{36}$/.test(value.mediaID) ||
    !Number.isSafeInteger(value.frame) ||
    (value.frame as number) < 0 ||
    !Number.isSafeInteger(value.offset) ||
    (value.offset as number) < 0
  ) {
    throw new Error('Invalid image presentation request')
  }
  return value as unknown as INativeMediaCommand
}

// Bound copies before allocating RGBA buffers; decoder failures remain a local
// preview error and cannot consume the original kernel's entire heap.
function frameSize(width: number, height: number): number {
  const size = width * height * 4
  if (
    !Number.isSafeInteger(width) ||
    !Number.isSafeInteger(height) ||
    width <= 0 ||
    height <= 0 ||
    !Number.isSafeInteger(size) ||
    size > 256 * 1024 * 1024
  ) {
    throw new Error('Image exceeds the decoded preview memory budget')
  }
  return size
}

async function initialize(entry: IMedia) {
  const type = entry.image.mediaType
  // Chromium owns PNG/WebP/GIF animation compositing, disposal, timing and
  // repetition. No fcTL/ANMF parser or approximated first-frame animation.
  if (
    ['image/png', 'image/webp', 'image/gif'].includes(type) &&
    typeof ImageDecoder !== 'undefined' &&
    (await ImageDecoder.isTypeSupported(type))
  ) {
    // Original <img> consumes contents, not the potentially pooled backing
    // ArrayBuffer in rawContents. Decode exactly those displayed source bytes.
    const data = Uint8Array.from(Buffer.from(entry.image.contents, 'base64'))
    entry.decoder = new ImageDecoder({ data, type, preferAnimation: true })
    await entry.decoder.tracks.ready
    const track = entry.decoder.tracks.selectedTrack
    if (track === null) {
      throw new Error('Image has no display track')
    }
    entry.frames = track.frameCount
    entry.repetitions = Number.isFinite(track.repetitionCount)
      ? track.repetitionCount
      : -1
  }
  const animationRequired =
    type === 'image/gif' ||
    (type === 'image/png' &&
      Buffer.from(entry.image.contents, 'base64').includes(
        Buffer.from('acTL')
      )) ||
    (type === 'image/webp' &&
      Buffer.from(entry.image.contents, 'base64').includes(Buffer.from('ANIM')))
  if (animationRequired && entry.decoder === undefined) {
    throw new Error('Chromium animation decoding is unavailable')
  }
  if (entry.closed) {
    entry.decoder?.close()
    throw new Error('Image is no longer displayed')
  }
}

function ddsImageURL(image: RepositoryImage): string {
  const raw = Uint8Array.from(Buffer.from(image.contents, 'base64')).buffer
  const metadata = parseDDS(raw)
  const [width, height] = metadata.images[0].shape
  frameSize(width, height)
  return convertDDSImage(raw)
}

async function pixels(entry: IMedia, index: number): Promise<IPixels> {
  await (entry.initialization ??= initialize(entry))
  if (entry.closed || index >= entry.frames) {
    throw new Error('Image frame is no longer available')
  }
  if (entry.decoder !== undefined) {
    const { image } = await entry.decoder.decode({
      frameIndex: index,
      completeFramesOnly: true,
    })
    try {
      const rect =
        image.visibleRect ??
        new DOMRect(0, 0, image.codedWidth, image.codedHeight)
      const width = rect.width,
        height = rect.height
      frameSize(image.displayWidth, image.displayHeight)
      if (width !== image.displayWidth || height !== image.displayHeight) {
        // Let Chromium apply its display geometry, never assume coded pixels
        // equal display pixels or reproduce crop/orientation math ourselves.
        const canvas = document.createElement('canvas')
        canvas.width = image.displayWidth
        canvas.height = image.displayHeight
        const context = canvas.getContext('2d', { willReadFrequently: true })
        if (context === null) {
          throw new Error('Image conversion is unavailable')
        }
        context.drawImage(image, 0, 0, canvas.width, canvas.height)
        const rgba = context.getImageData(0, 0, canvas.width, canvas.height)
        return {
          data: new Uint8Array(rgba.data.buffer),
          width: canvas.width,
          height: canvas.height,
          duration: image.duration ?? 100000,
        }
      }
      const options: VideoFrameCopyToOptions = {
        format: 'RGBA',
        colorSpace: 'srgb',
        rect,
        layout: [{ offset: 0, stride: width * 4 }],
      }
      const size = frameSize(width, height)
      if (image.allocationSize(options) !== size) {
        throw new Error('Unexpected Chromium image plane allocation')
      }
      const data = new Uint8Array(size)
      const layout = await image.copyTo(data, options)
      if (
        layout.length !== 1 ||
        layout[0].offset !== 0 ||
        layout[0].stride !== width * 4
      ) {
        throw new Error('Unexpected Chromium image plane layout')
      }
      return { data, width, height, duration: image.duration ?? 100000 }
    } finally {
      image.close()
    }
  }
  // Same image-resource security mode as original <img>: detached data URL,
  // never SVG markup in the Node-enabled document, object/iframe or external URL.
  // Canvas is imaging conversion only and is never mounted or shown as UI.
  const image = new window.Image()
  entry.element = image
  image.src =
    entry.image.mediaType === 'image/vnd-ms.dds'
      ? ddsImageURL(entry.image)
      : `data:${entry.image.mediaType};base64,${entry.image.contents}`
  await image.decode()
  frameSize(image.naturalWidth, image.naturalHeight)
  const canvas = document.createElement('canvas')
  canvas.width = image.naturalWidth
  canvas.height = image.naturalHeight
  const context = canvas.getContext('2d', { willReadFrequently: true })
  if (context === null) {
    throw new Error('Image conversion is unavailable')
  }
  context.drawImage(image, 0, 0)
  const rgba = context.getImageData(0, 0, canvas.width, canvas.height)
  entry.element = undefined
  image.src = ''
  return {
    data: new Uint8Array(rgba.data.buffer),
    width: canvas.width,
    height: canvas.height,
    duration: 100000,
  }
}

// Bounded diagnostic vocabulary: decoder failures never echo data URLs, SVG
// contents, filenames, arbitrary exception properties or credentials to the UI.
export function kernelMediaErrorMessage(error: unknown): string {
  const messages = new Set([
    'Image is no longer displayed',
    'Image frame is no longer available',
    'Image decoding timed out',
    'Image exceeds the decoded preview memory budget',
    'Chromium animation decoding is unavailable',
    'Unexpected Chromium image plane allocation',
    'Unexpected Chromium image plane layout',
  ])
  if (error instanceof Error && messages.has(error.message)) {
    return error.message
  }
  if (typeof DOMException !== 'undefined' && error instanceof DOMException) {
    const names = new Set([
      'EncodingError',
      'InvalidStateError',
      'IndexSizeError',
      'AbortError',
      'SecurityError',
      'NotSupportedError',
    ])
    if (names.has(error.name)) {
      return `Chromium image decoder: ${error.name}`
    }
  }
  return 'The original image decoder could not display this image'
}

/** Bounded chunks never compete with a Git/recovery result for a 64 MiB frame.
 * The one decoded frame is cached until its next frame is requested. */
export async function readKernelMedia(
  command: INativeMediaCommand
): Promise<IKernelMediaChunk> {
  const entry = media.get(command.mediaID)
  if (entry === undefined || entry.closed) {
    throw new Error('Image is no longer displayed')
  }
  if (entry.frame?.index !== command.frame) {
    entry.frame = { index: command.frame, pixels: pixels(entry, command.frame) }
  }
  const requestedFrame = entry.frame
  let frame: IPixels
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    frame = await Promise.race([
      requestedFrame.pixels,
      new Promise<never>((_, reject) => {
        timer = setTimeout(
          () => reject(new Error('Image decoding timed out')),
          15000
        )
      }),
    ])
  } catch (error) {
    entry.closed = true
    entry.decoder?.close()
    if (entry.element) {
      entry.element.src = ''
    }
    entry.element = undefined
    entry.frame = undefined
    throw error
  } finally {
    if (timer !== undefined) {
      clearTimeout(timer)
    }
  }
  if (
    entry.closed ||
    media.get(command.mediaID) !== entry ||
    command.offset > frame.data.length
  ) {
    throw new Error('Image is no longer displayed')
  }
  const chunk = frame.data.subarray(
    command.offset,
    command.offset + 1024 * 1024
  )
  // Keep decoded pixels only while their chunks are being consumed, not for
  // every image a long-lived popup has ever visited.
  if (
    command.offset + chunk.length === frame.data.length &&
    entry.frame === requestedFrame
  ) {
    entry.frame = undefined
  }
  return {
    contents: Buffer.from(
      chunk.buffer,
      chunk.byteOffset,
      chunk.byteLength
    ).toString('base64'),
    offset: command.offset,
    total: frame.data.length,
    width: frame.width,
    height: frame.height,
    duration: frame.duration,
    frames: entry.frames,
    repetitions: entry.repetitions,
  }
}

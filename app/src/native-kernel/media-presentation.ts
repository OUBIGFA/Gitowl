import { createHash, randomUUID } from 'crypto'
import type { Image as RepositoryImage } from '../models/diff'
import { decodeNativeImage } from './image-worker-client'

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

async function pixels(entry: IMedia, index: number): Promise<IPixels> {
  if (entry.closed) {throw new Error('Image is no longer displayed')}
  // The Node backend decodes only in its worker; loading the codec here too
  // would keep a second copy of ImageMagick's and UPNG's modules resident.
  const decoded = await (process.env.GITOWL_NODE_CORE === '1'
    ? decodeNativeImage
    : (await import('./node-image-decoder')).decodeImageFrame)(
    Buffer.from(entry.image.contents, 'base64'),
    index,
    entry.image.mediaType
  )
  if (entry.closed) {throw new Error('Image is no longer displayed')}
  entry.frames = decoded.frames
  entry.repetitions = decoded.repetitions
  return decoded
}

// Bounded diagnostic vocabulary: decoder failures never echo data URLs, SVG
// contents, filenames, arbitrary exception properties or credentials to the UI.
export function kernelMediaErrorMessage(error: unknown): string {
  const messages = new Set([
    'Image is no longer displayed',
    'Image frame is no longer available',
    'Image decoding timed out',
    'Image exceeds the decoded preview memory budget',
    'Image decoding returned an invalid pixel plane',
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
      return `Image decoder: ${error.name}`
    }
  }
  return 'The image decoder could not display this image'
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

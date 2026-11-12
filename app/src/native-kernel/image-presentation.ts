import type { IAppState } from '../lib/app-state'
import { ImageDiffType, Image } from '../models/diff'
import { registerKernelMedia } from './media-presentation'
import type { Dispatcher } from '../ui/dispatcher/dispatcher'

export interface IKernelImage {
  readonly contents: string
  readonly mediaType: string
  readonly bytes: number
  readonly mediaID?: string
  readonly previewError?: string
}

// Original Image objects are immutable. Cache only their bounded display DTO,
// so progress snapshots do not rerun the original off-DOM DDS GPU conversion.
const imageViews = new WeakMap<Image, IKernelImage>()

export function projectKernelImage(image: Image, owner?: object): IKernelImage {
  const cached = imageViews.get(image)
  if (cached !== undefined) {
    if (cached.mediaID !== undefined) {
      const mediaID = registerKernelMedia(image, owner)
      if (mediaID !== cached.mediaID) {
        const view = { ...cached, mediaID }
        imageViews.set(image, view)
        return view
      }
    }
    return cached
  }
  let view: IKernelImage = {
    contents: image.contents,
    mediaType: image.mediaType,
    bytes: image.bytes,
  }
  // Four simultaneously displayed inline images (both diff and popup sides)
  // remain below the transport's 64 MiB line cap. Large/codec-specific images
  // are demand-decoded and chunked by the retained Chromium image engine.
  if (
    image.contents.length > 8 * 1024 * 1024 ||
    !['image/png', 'image/jpeg', 'image/gif', 'image/bmp'].includes(
      image.mediaType
    ) ||
    (image.mediaType === 'image/png' &&
      Buffer.from(image.contents, 'base64').includes(Buffer.from('acTL')))
  ) {
    view = {
      contents: '',
      mediaType: image.mediaType,
      bytes: image.bytes,
      mediaID: registerKernelMedia(image, owner),
    }
  }
  imageViews.set(image, view)
  return view
}

/** Global AppStore preference: never derived from the selected repository. */
export interface INativeImagePresentation {
  readonly imageDiffType: ImageDiffType
}

export interface INativeImagePresentationCommand {
  readonly name: 'imageDiff.type'
  readonly imageDiffType: ImageDiffType
}

export function projectImagePresentation(
  state: IAppState
): INativeImagePresentation {
  return { imageDiffType: state.imageDiffType }
}

/** Parent command-validation hook. Unknown command names fall through; an
 * invalid image command must fail instead of being coerced to another mode. */
export function parseImagePresentationCommand(
  value: Record<string, unknown>
): INativeImagePresentationCommand | null {
  if (value.name !== 'imageDiff.type') {
    return null
  }
  if (
    Object.keys(value).some(k => k !== 'name' && k !== 'imageDiffType') ||
    ![
      ImageDiffType.TwoUp,
      ImageDiffType.Swipe,
      ImageDiffType.OnionSkin,
      ImageDiffType.Difference,
    ].includes(value.imageDiffType as ImageDiffType)
  ) {
    throw new Error('Invalid image diff presentation command')
  }
  return value as unknown as INativeImagePresentationCommand
}

/** Parent execute-command hook, after its ordinary readiness/revision checks.
 * _changeImageDiffType owns localStorage['image-diff-type'] and emits state. */
export function executeImagePresentationCommand(
  dispatcher: Dispatcher,
  command: INativeImagePresentationCommand
): Promise<void> {
  return dispatcher.changeImageDiffType(command.imageDiffType)
}

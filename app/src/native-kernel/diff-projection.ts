import { DiffType, IDiff } from '../models/diff'
import type { Popup } from '../models/popup'
import type { IKernelDiff } from './protocol'
import { projectKernelImage } from './image-presentation'
import { parseRepositoryIdentifier } from '../lib/remote-parsing'

/** Keep the original diff data shared by the workspace and PR preview. Image
 * leases belong to their actual popup, so covered dialogs retain their media. */
export function projectKernelDiff(
  diff: IDiff | null,
  owner?: Popup
): IKernelDiff | null {
  if (diff === null) {
    return null
  }
  switch (diff.kind) {
    case DiffType.Text:
    case DiffType.LargeText:
      return {
        kind: diff.kind === DiffType.Text ? 'text' : 'large-text',
        text: diff.text,
      }
    case DiffType.Image:
      return {
        kind: 'image',
        previous: diff.previous && projectKernelImage(diff.previous, owner),
        current: diff.current && projectKernelImage(diff.current, owner),
      }
    case DiffType.Binary:
      return { kind: 'binary' }
    case DiffType.Submodule: {
      const repository =
        diff.url === null ? null : parseRepositoryIdentifier(diff.url)
      return {
        kind: 'submodule',
        submodule: {
          path: diff.path,
          url: diff.url,
          oldSHA: diff.oldSHA,
          newSHA: diff.newSHA,
          repositoryURL:
            repository === null
              ? null
              : `https://${repository.hostname}/${repository.owner}/${repository.name}`,
          status: { ...diff.status },
        },
      }
    }
    case DiffType.Unrenderable:
      return { kind: 'unrenderable' }
  }
}

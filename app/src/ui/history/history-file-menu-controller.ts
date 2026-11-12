import * as Path from 'path'
import { IWorkspaceMenuItem } from '../lib/workspace-menu'
import { Repository } from '../../models/repository'
import { CommittedFileChange } from '../../models/status'
import { Commit } from '../../models/commit'
import { writeClipboardText } from '../main-process-proxy'
import { revealInFileManager } from '../../lib/app-shell'
import { getDotComAPIEndpoint } from '../../lib/api'
import {
  isSafeFileExtension,
  DefaultEditorLabel,
  RevealInFileManagerLabel,
  OpenWithDefaultProgramLabel,
  CopyFilePathLabel,
  CopyRelativeFilePathLabel,
} from '../lib/context-menu'

/** Disk existence is resolved by the caller before presenting this menu, as
 * upstream does; deleted historical blobs aren't opened as working files. */
export function generateHistoryFileContextMenu(
  repository: Repository,
  file: CommittedFileChange,
  fileExistsOnDisk: boolean,
  selectedCommits: ReadonlyArray<Commit>,
  localCommitSHAs: ReadonlyArray<string>,
  externalEditorLabel: string | undefined,
  onOpenEditor: (path: string) => void,
  onOpenItem: (path: string) => void,
  onViewCommit: (sha: string, filePath: string) => void
): IWorkspaceMenuItem[] {
  if (!fileExistsOnDisk) {
    return [
      {
        labelKey: 'workspace.fileMissing',
        label: __DARWIN__
          ? 'File Does Not Exist on Disk'
          : 'File does not exist on disk',
        enabled: false,
      },
    ]
  }
  const enterprise =
    repository.gitHubRepository !== null &&
    repository.gitHubRepository.endpoint !== getDotComAPIEndpoint()
  return [
    {
      labelKey: 'workspace.reveal',
      label: RevealInFileManagerLabel,
      action: () => revealInFileManager(repository, file.path),
    },
    {
      labelKey: externalEditorLabel
        ? 'workspace.openEditorNamed'
        : 'workspace.openEditor',
      labelData: externalEditorLabel ? [externalEditorLabel] : [],
      label: externalEditorLabel
        ? `Open in ${externalEditorLabel}`
        : DefaultEditorLabel,
      action: () => onOpenEditor(file.path),
    },
    {
      labelKey: 'workspace.openDefault',
      label: OpenWithDefaultProgramLabel,
      action: () => onOpenItem(file.path),
      enabled: isSafeFileExtension(Path.extname(file.path)),
    },
    { type: 'separator' },
    {
      labelKey: 'workspace.copyPath',
      label: CopyFilePathLabel,
      action: () => writeClipboardText(Path.join(repository.path, file.path)),
    },
    {
      labelKey: 'workspace.copyRelativePath',
      label: CopyRelativeFilePathLabel,
      action: () => writeClipboardText(Path.normalize(file.path)),
    },
    { type: 'separator' },
    {
      labelKey: enterprise
        ? 'workspace.viewEnterprise'
        : 'workspace.viewGitHub',
      label: enterprise ? 'View on GitHub Enterprise' : 'View on GitHub',
      action: () => onViewCommit(selectedCommits[0].sha, file.path),
      enabled:
        selectedCommits.length === 1 &&
        !localCommitSHAs.includes(selectedCommits[0].sha) &&
        !!repository.gitHubRepository,
    },
  ]
}

import { DiffSelectionType } from '../../models/diff'
import { ICommitContext } from '../../models/commit'
import { PopupType } from '../../models/popup'
import { Repository } from '../../models/repository'
import { WorkingDirectoryStatus } from '../../models/status'
import { filesNotTrackedByLFS } from '../../lib/git/lfs'
import { getLargeFilePaths } from '../../lib/large-files'
import { hasUnresolvedConflicts, isConflictedFile } from '../../lib/status'
import type { Dispatcher } from '../dispatcher'

/**
 * Original ChangesSidebar submission preflight, shared by both frontends.
 * Confirmation dialogs intentionally call commitIncludedChanges directly:
 * running this again after consent would reopen the very same warning.
 */
export async function createWorkbenchCommit(
  dispatcher: Dispatcher,
  repository: Repository,
  workingDirectory: WorkingDirectoryStatus,
  context: ICommitContext
): Promise<boolean> {
  const oversizedFiles = await getLargeFilePaths(repository, workingDirectory)
  const filesIgnoredByLFS = await filesNotTrackedByLFS(
    repository,
    oversizedFiles
  )
  if (filesIgnoredByLFS.length !== 0) {
    dispatcher.showPopup({
      type: PopupType.OversizedFiles,
      oversizedFiles: filesIgnoredByLFS,
      context,
      repository,
    })
    return false
  }
  const conflictedFilesLeft = workingDirectory.files.filter(
    file =>
      isConflictedFile(file.status) &&
      file.selection.getSelectionType() === DiffSelectionType.None
  )
  if (conflictedFilesLeft.length === 0) {
    dispatcher.clearBanner()
    dispatcher.incrementMetric('unguidedConflictedMergeCompletionCount')
  }
  const conflictedFilesSelected = workingDirectory.files.filter(
    file =>
      isConflictedFile(file.status) &&
      hasUnresolvedConflicts(file.status) &&
      file.selection.getSelectionType() !== DiffSelectionType.None
  )
  if (conflictedFilesSelected.length > 0) {
    dispatcher.showPopup({
      type: PopupType.CommitConflictsWarning,
      files: conflictedFilesSelected,
      repository,
      context,
    })
    return false
  }
  return dispatcher.commitIncludedChanges(repository, context)
}

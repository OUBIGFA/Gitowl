import { FoldoutType } from '../../lib/app-state'
import { Dispatcher } from '../dispatcher'
import { Repository } from '../../models/repository'
import { WorktreeEntry } from '../../models/worktree'
import { PopupType } from '../../models/popup'

/** Shared toolbar/list navigation. Missing worktrees must enter the original
 * confirmation/retry flow, never be silently pruned by the presentation host. */
export async function selectWorkspaceWorktree(
  dispatcher: Dispatcher,
  repository: Repository,
  worktree: WorktreeEntry
) {
  dispatcher.closeFoldout(FoldoutType.Worktree)
  if (worktree.isPrunable) {
    dispatcher.requestDeleteWorktree(repository, worktree.path)
    return
  }
  await dispatcher.switchWorktree(repository, worktree)
}

export function renameWorkspaceWorktree(
  dispatcher: Dispatcher,
  repository: Repository,
  path: string
) {
  dispatcher.closeFoldout(FoldoutType.Worktree)
  dispatcher.showPopup({
    type: PopupType.RenameWorktree,
    repository,
    worktreePath: path,
  })
}

export function removeWorkspaceWorktree(
  dispatcher: Dispatcher,
  repository: Repository,
  path: string
) {
  dispatcher.closeFoldout(FoldoutType.Worktree)
  dispatcher.requestDeleteWorktree(repository, path)
}

export function createWorkspaceWorktree(
  dispatcher: Dispatcher,
  repository: Repository
) {
  dispatcher.closeFoldout(FoldoutType.Worktree)
  dispatcher.showPopup({ type: PopupType.AddWorktree, repository })
}

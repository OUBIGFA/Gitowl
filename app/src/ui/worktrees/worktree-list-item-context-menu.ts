import * as Path from 'path'

import { IWorkspaceMenuItem as IMenuItem } from '../lib/workspace-menu'
import { writeClipboardText } from '../main-process-proxy'
import { isRepositoryAffectedByAssistedCommit } from '../../lib/git/repository-operation'

interface IWorktreeContextMenuConfig {
  readonly path: string
  readonly isMainWorktree: boolean
  readonly isLocked: boolean
  readonly onRenameWorktree?: (path: string) => void
  readonly onRemoveWorktree?: (path: string) => void
}

export function generateWorktreeContextMenuItems(
  config: IWorktreeContextMenuConfig
): ReadonlyArray<IMenuItem> {
  const { path, isMainWorktree, isLocked, onRenameWorktree, onRemoveWorktree } =
    config
  const name = Path.basename(path)
  const canMutate =
    !isMainWorktree && !isLocked && !isRepositoryAffectedByAssistedCommit(path)
  const items = new Array<IMenuItem>()

  if (onRenameWorktree !== undefined) {
    items.push({
      labelKey: 'workspace.rename',
      label: 'Rename…',
      action: () => onRenameWorktree(path),
      enabled: canMutate,
    })
  }

  items.push({
    labelKey: 'workspace.copyWorktree',
    label: __DARWIN__ ? 'Copy Worktree Name' : 'Copy worktree name',
    action: () => writeClipboardText(name),
  })

  items.push({
    labelKey: 'workspace.copyWorktreePath',
    label: __DARWIN__ ? 'Copy Worktree Path' : 'Copy worktree path',
    action: () => writeClipboardText(path),
  })

  items.push({ type: 'separator' })

  if (onRemoveWorktree !== undefined) {
    items.push({
      labelKey: 'workspace.delete',
      label: 'Delete…',
      action: () => onRemoveWorktree(path),
      enabled: canMutate,
    })
  }

  return items
}

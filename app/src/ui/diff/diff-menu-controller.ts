import { Dispatcher } from '../dispatcher'
import { Repository } from '../../models/repository'
import { WorkingDirectoryFileChange } from '../../models/status'
import {
  DiffSelection,
  ITextDiff,
  DiffHunkExpansionType,
} from '../../models/diff'
import { PopupType } from '../../models/popup'
import { IWorkspaceMenuItem } from '../lib/workspace-menu'

/** Both toolkits keep the original patch/selection together through the
 * confirmation; expanded display coordinates must never be used as a patch. */
export function discardWorkspaceDiffSelection(
  dispatcher: Dispatcher,
  repository: Repository,
  file: WorkingDirectoryFileChange,
  diff: ITextDiff,
  selection: DiffSelection,
  confirm: boolean
) {
  return confirm
    ? dispatcher.showPopup({
        type: PopupType.ConfirmDiscardSelection,
        repository,
        file,
        diff,
        selection,
      })
    : dispatcher.discardChangesFromSelection(
        repository,
        file.path,
        diff,
        selection
      )
}

export function workspaceDiffExpansionEnabled(
  diff: ITextDiff,
  expanded: boolean
) {
  return (
    expanded ||
    diff.hunks.length !== 1 ||
    diff.hunks[0].expansionType !== DiffHunkExpansionType.None
  )
}

export function generateDiffExpansionMenuItem(
  diff: ITextDiff,
  canExpand: boolean,
  expanded: boolean,
  onExpand: () => void,
  onCollapse: () => void
): IWorkspaceMenuItem | null {
  if (!canExpand) {
    return null
  }
  return expanded
    ? {
        labelKey: 'workspace.collapseExpanded',
        label: __DARWIN__
          ? 'Collapse Expanded Lines'
          : 'Collapse expanded lines',
        action: onCollapse,
      }
    : {
        labelKey: 'workspace.expandWhole',
        label: __DARWIN__ ? 'Expand Whole File' : 'Expand whole file',
        action: onExpand,
        enabled: workspaceDiffExpansionEnabled(diff, expanded),
      }
}

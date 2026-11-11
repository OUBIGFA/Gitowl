import type { Banner } from '../models/banner'
import type { IRepositoryState, Foldout } from '../lib/app-state'
import {
  HistoryTabMode,
  ChangesSelectionKind,
  RepositorySectionTab,
} from '../lib/app-state'
import { createHash } from 'crypto'
import { DiffType, ITextDiff } from '../models/diff'
import { CommittedFileChange } from '../models/status'
import type { IFileContents } from '../ui/diff/syntax-highlighting'
import { TipState } from '../models/tip'
import { MultiCommitOperationKind } from '../models/multi-commit-operation'
import type { RebasePreview } from '../models/rebase'

// These are presentation leases, not an alternate store. Objects used by menu
// continuations remain inside Desktop, and a banner replaced with another of
// the same type must never execute the replaced banner's continuation.
const bannerIDs = new WeakMap<Banner, number>()
let nextBannerID = 0
export function workspaceBannerID(banner: Banner): number {
  let id = bannerIDs.get(banner)
  if (id === undefined) {
    id = ++nextBannerID
    bannerIDs.set(banner, id)
  }
  return id
}

const navigationFilters = new Map<
  string,
  {
    readonly foldout: Foldout | null
    readonly filters: WorkspaceNavigationFilters
  }
>()
export interface WorkspaceNavigationFilters {
  readonly branches: string
  readonly pullRequests: string
  readonly worktrees: string
  readonly compare: string
}
export function workspaceNavigationFilters(
  path: string,
  foldout: Foldout | null = null
): WorkspaceNavigationFilters {
  const saved = navigationFilters.get(path)
  // BranchesContainer/WorktreeList are unmounted when the original foldout
  // closes. A new foldout object is a new filter session, even on the same repo.
  if (foldout !== null && saved?.foldout === foldout) return saved.filters
  return {
    branches: '',
    pullRequests: '',
    worktrees: '',
    compare: saved?.filters.compare ?? '',
  }
}
export function setWorkspaceNavigationFilter(
  path: string,
  scope: keyof WorkspaceNavigationFilters,
  text: string,
  foldout: Foldout | null
) {
  navigationFilters.set(path, {
    foldout,
    filters: { ...workspaceNavigationFilters(path, foldout), [scope]: text },
  })
}

export function workspaceConflictIdentity(
  state: IRepositoryState
): string | null {
  const conflict = state.changesState.conflictState
  if (conflict === null) return null
  const tip = state.branchesState.tip
  const head =
    tip.kind === TipState.Valid
      ? tip.branch.tip.sha
      : tip.kind === TipState.Detached
      ? tip.currentSha
      : ''
  return createHash('sha256')
    .update(
      JSON.stringify([
        conflict.kind,
        head,
        state.multiCommitOperationState?.originalBranchTip,
        state.multiCommitOperationState?.operationDetail.kind,
      ])
    )
    .digest('hex')
}

export function selectedWorkspaceDiff(state: IRepositoryState) {
  const selection = state.changesState.selection
  const diff =
    state.selectedSection === RepositorySectionTab.History
      ? state.commitSelection.diff
      : selection.kind === ChangesSelectionKind.Stash
      ? selection.selectedStashedFileDiff
      : selection.diff
  const file =
    state.selectedSection === RepositorySectionTab.History
      ? state.commitSelection.file
      : selection.kind === ChangesSelectionKind.Stash
      ? selection.selectedStashedFile
      : selection.selectedFileIDs.length === 1
      ? state.changesState.workingDirectory.files.find(
          f => f.id === selection.selectedFileIDs[0]
        )
      : undefined
  return diff == null || file == null ? null : { file, diff }
}

export function selectedWorkspaceTextDiff(state: IRepositoryState) {
  const selected = selectedWorkspaceDiff(state)
  if (selected?.diff.kind !== DiffType.Text) return null
  const { file, diff } = selected
  const tip = state.branchesState.tip
  const revision =
    file instanceof CommittedFileChange
      ? [file.commitish, file.parentCommitish]
      : tip.kind === TipState.Valid
      ? [tip.branch.tip.sha]
      : tip.kind === TipState.Detached
      ? [tip.currentSha]
      : []
  return {
    file,
    diff,
    identity: createHash('sha256')
      .update(JSON.stringify([file.id, revision, diff.text]))
      .digest('hex'),
  }
}
export interface WorkspaceDiffPresentation {
  readonly identity: string
  readonly fileID: string
  readonly contents: IFileContents
  readonly expanded: ITextDiff | null
}
const diffViews = new Map<string, WorkspaceDiffPresentation>()
export function workspaceDiffPresentation(
  path: string,
  state: IRepositoryState
) {
  const selected = selectedWorkspaceTextDiff(state)
  const view = diffViews.get(path)
  return selected !== null &&
    view?.identity === selected.identity &&
    view.fileID === selected.file.id
    ? view
    : null
}
export function setWorkspaceDiffPresentation(
  path: string,
  view: WorkspaceDiffPresentation
) {
  diffViews.set(path, view)
}

export interface WorkspaceComparePresentation {
  readonly identity: string
  readonly operation:
    | MultiCommitOperationKind.Merge
    | MultiCommitOperationKind.Squash
    | MultiCommitOperationKind.Rebase
  readonly preview: RebasePreview | null
}
const comparisonViews = new Map<string, WorkspaceComparePresentation>()
export function workspaceComparisonIdentity(state: IRepositoryState) {
  const tip = state.branchesState.tip
  const form = state.compareState.formState
  return tip.kind === TipState.Valid && form.kind === HistoryTabMode.Compare
    ? [
        tip.branch.ref,
        tip.branch.tip.sha,
        form.comparisonBranch.ref,
        form.comparisonBranch.tip.sha,
      ].join(':')
    : ''
}
export function workspaceComparison(
  path: string,
  state: IRepositoryState
): WorkspaceComparePresentation {
  const identity = workspaceComparisonIdentity(state)
  const view = comparisonViews.get(path)
  return view?.identity === identity
    ? view
    : { identity, operation: MultiCommitOperationKind.Merge, preview: null }
}
export function setWorkspaceComparison(
  path: string,
  view: WorkspaceComparePresentation
) {
  comparisonViews.set(path, view)
}

/** Keyboard reorder is a frontend mode in the original Compare component too. */
const reorderSelections = new Map<string, ReadonlyArray<string>>()
export function workspaceReorderSelection(path: string) {
  return reorderSelections.get(path) ?? []
}
export function setWorkspaceReorderSelection(
  path: string,
  shas: ReadonlyArray<string>
) {
  if (shas.length === 0) reorderSelections.delete(path)
  else reorderSelections.set(path, [...shas])
}

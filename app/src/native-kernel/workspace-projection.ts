import { createHash } from 'crypto'
import type { IAppState, IRepositoryState } from '../lib/app-state'
import {
  FoldoutType,
  SelectionType,
  ChangesSelectionKind,
  HistoryTabMode,
  ComparisonMode,
  RepositorySectionTab,
} from '../lib/app-state'
import { Repository } from '../models/repository'
import { BranchesTab } from '../models/branches-tab'
import { Branch, BranchType } from '../models/branch'
import { TipState } from '../models/tip'
import { Banner, BannerType } from '../models/banner'
import { StashedChangesLoadStates } from '../models/stash-entry'
import { isAssistedCommitRepositoryLocked } from '../models/assisted-commit-run'
import { isRepositoryAffectedByAssistedCommit } from '../lib/git/repository-operation'
import { compareOperationDisabled } from '../ui/history/compare-operation-controller'
import { ComputedAction } from '../models/computed-action'
import { pullRequestSearchText } from '../ui/branches/branch-controller'
import { groupBranches } from '../ui/branches/group-branches'
import {
  getWorktreeDisplayName,
  getWorktreeDescription,
} from '../models/worktree'
import type { IWorkspaceMenuItem } from '../ui/lib/workspace-menu'
import type {
  WorkspaceMenuTarget,
  IWorkspaceFileFilter,
} from './workspace-protocol'
import type { INativeRepositoryReference } from './desktop-presenter'
import { buildWorkspaceMenu } from './workspace-presenter'
import {
  workspaceBannerID,
  workspaceReorderSelection,
  workspaceNavigationFilters,
  IWorkspaceNavigationFilters,
  workspaceComparison,
  selectedWorkspaceTextDiff,
  workspaceDiffPresentation,
  workspaceConflictIdentity,
} from './newworkspace'
import { getConflictedFiles } from '../lib/status'
import { MultiCommitOperationStepKind } from '../models/multi-commit-operation'
import { workspaceDiffExpansionEnabled } from '../ui/diff/diff-menu-controller'
import { getTextDiffWithBottomDummyHunk } from '../ui/diff/text-diff-expansion'
import { projectDiffSelection, IKernelDiffSelection } from './line-selection'
import { applyFilters } from '../ui/changes/filter-changes-logic'
import { filterListItems } from '../ui/lib/filter-list-items'

/** Uses the exact shared augmented-list fuzzy ranking and AND option filters.
 * showChangesFilter is global IAppState state, not a repository preference. */
export function visibleWorkingDirectoryFiles(
  state: IRepositoryState,
  showChangesFilter = true
) {
  const filters = state.changesState.fileListFilter
  const items = state.changesState.workingDirectory.files.map(change => ({
    id: change.id,
    text: [change.path],
    change,
  }))
  return filterListItems(
    items,
    showChangesFilter ? filters.filterText : '',
    item => applyFilters(item, showChangesFilter, filters)
  ).map(({ item }) => item.change)
}

export interface IKernelWorkspaceMenuItem {
  readonly labelKey: string
  readonly data: ReadonlyArray<string>
  readonly enabled: boolean
  readonly separator: boolean
  readonly checked?: boolean
  readonly children: ReadonlyArray<IKernelWorkspaceMenuItem>
}
export interface IKernelWorkspaceMenu {
  readonly target: WorkspaceMenuTarget
  readonly lease: string
  readonly items: ReadonlyArray<IKernelWorkspaceMenuItem>
}
export interface IKernelWorkspaceBranch {
  readonly ref: string
  readonly name: string
  readonly remote: boolean
  readonly current: boolean
  readonly tip: string
}
export interface IKernelWorkspaceBanner {
  readonly id: number
  readonly type: string
  readonly labelKey: string
  readonly data: ReadonlyArray<string>
  readonly actions: ReadonlyArray<string>
  readonly timeout: number
  readonly dismissable: boolean
}
export interface IKernelWorkspaceDTO {
  readonly repository: INativeRepositoryReference | null
  readonly navigation:
    | ''
    | 'branches'
    | 'pullRequests'
    | 'worktrees'
    | 'repositories'
  readonly currentBranchRef: string | null
  readonly filters: IWorkspaceNavigationFilters
  readonly locked: boolean
  readonly checkoutInProgress: boolean
  readonly branches: ReadonlyArray<{
    readonly group: string
    readonly items: ReadonlyArray<IKernelWorkspaceBranch>
  }>
  readonly pullRequests: ReadonlyArray<{
    readonly number: number
    readonly title: string
    readonly author: string
    readonly head: string
    readonly base: string
    readonly draft: boolean
    readonly checkoutAvailable: boolean
  }>
  readonly pullRequestsAvailable: boolean
  readonly loadingPullRequests: boolean
  readonly worktrees: ReadonlyArray<{
    readonly path: string
    readonly name: string
    readonly description: string
    readonly main: boolean
    readonly current: boolean
    readonly locked: boolean
    readonly missing: boolean
  }>
  readonly compare: {
    readonly showBranchList: boolean
    readonly mode: 'history' | 'ahead' | 'behind'
    readonly ref: string | null
    readonly ahead: number
    readonly behind: number
    readonly mergeStatus: string | null
    readonly operation: string
    readonly operationDisabled: boolean
    readonly previewStatus: string | null
    readonly rebaseCommits: number
    readonly branches: ReadonlyArray<IKernelWorkspaceBranch>
  }
  readonly historySelectionContiguous: boolean
  /** Original GitStore commits not on the upstream (or on no remote). */
  readonly localCommitSHAs: ReadonlyArray<string>
  /** Original tags created locally that the next push will publish. */
  readonly tagsToPush: ReadonlyArray<string>
  readonly reorderSelection: ReadonlyArray<string>
  readonly stash: {
    readonly sha: string
    readonly branch: string
    readonly loadState: string
    readonly selected: boolean
    readonly fileID: string | null
    readonly files: ReadonlyArray<{
      readonly id: string
      readonly path: string
      readonly status: string
    }>
  } | null
  readonly filesFilter: IWorkspaceFileFilter
  readonly showFilesFilter: boolean
  readonly visibleFileIDs: ReadonlyArray<string>
  readonly menus: ReadonlyArray<IKernelWorkspaceMenu>
  readonly banner: IKernelWorkspaceBanner | null
  readonly conflict: {
    readonly kind: string
    readonly identity: string
    readonly remaining: number
    readonly canContinue: boolean
    readonly canAbort: boolean
    readonly untrackedExcluded: boolean
  } | null
  readonly textDiff: {
    readonly fileID: string
    readonly identity: string
    readonly prepared: boolean
    readonly canExpand: boolean
    readonly expandedText: string | null
    readonly selection: IKernelDiffSelection | null
  } | null
  readonly hideWhitespace: boolean
  readonly sideBySide: boolean
  readonly checkMarks: boolean
}

export function workspaceLocked(
  repository: Repository,
  state: IRepositoryState
) {
  return (
    isAssistedCommitRepositoryLocked(state.changesState.assistedCommit) ||
    isRepositoryAffectedByAssistedCommit(repository.path)
  )
}

/** Only presentation metadata crosses the boundary, never callbacks or roles. */
export function projectContextMenuItems(
  items: ReadonlyArray<IWorkspaceMenuItem>
): IKernelWorkspaceMenuItem[] {
  return items.map(item => {
    if (item.type !== 'separator' && item.labelKey === undefined) {
      throw new Error('Workspace menu label lacks a translation key')
    }
    return {
      labelKey: item.labelKey ?? '',
      data: item.labelData ?? [],
      enabled: item.enabled !== false,
      separator: item.type === 'separator',
      checked: item.checked,
      children: projectContextMenuItems(item.submenu ?? []),
    }
  })
}

export function workspaceMenuDTO(
  repository: Repository,
  state: IRepositoryState,
  target: WorkspaceMenuTarget,
  items: ReadonlyArray<IWorkspaceMenuItem>
): IKernelWorkspaceMenu {
  const projected = projectContextMenuItems(items)
  const tip = state.branchesState.tip
  const head =
    tip.kind === TipState.Valid
      ? tip.branch.tip.sha
      : tip.kind === TipState.Detached
      ? tip.currentSha
      : tip.kind === TipState.Unborn
      ? tip.ref
      : null
  const lease = createHash('sha256')
    .update(
      JSON.stringify({
        repository: [repository.id, repository.path],
        head,
        target,
        items: projected,
        selection: state.commitSelection.shas,
        files:
          state.changesState.selection.kind ===
          ChangesSelectionKind.WorkingDirectory
            ? state.changesState.selection.selectedFileIDs
            : [],
        locked: workspaceLocked(repository, state),
        committing: state.isCommitting,
      })
    )
    .digest('hex')
  return { target, lease, items: projected }
}

function projectBanner(banner: Banner | null): IKernelWorkspaceBanner | null {
  if (banner === null) {
    return null
  }
  let data: string[] = []
  let actions: string[] = []
  let timeout = 0
  let dismissable = true
  switch (banner.type) {
    case BannerType.SuccessfulMerge:
    case BannerType.BranchAlreadyUpToDate:
      data = [banner.ourBranch, banner.theirBranch ?? '']
      timeout = 5000
      break
    case BannerType.SuccessfulRebase:
      data = [banner.targetBranch, banner.baseBranch ?? '']
      timeout = 5000
      break
    case BannerType.MergeConflictsFound:
      data = [banner.ourBranch]
      actions = ['open']
      dismissable = false
      break
    case BannerType.RebaseConflictsFound:
      data = [banner.targetBranch]
      actions = ['open']
      dismissable = false
      break
    case BannerType.CherryPickConflictsFound:
      data = [banner.targetBranchName]
      actions = ['open']
      dismissable = false
      break
    case BannerType.ConflictsFound:
      actions = ['open']
      dismissable = false
      break // JSX operationDescription remains inside Desktop.
    case BannerType.SuccessfulCherryPick:
      data = [String(banner.count), banner.targetBranchName]
      actions = ['undo']
      timeout = 15000
      break
    case BannerType.CherryPickUndone:
      data = [String(banner.countCherryPicked), banner.targetBranchName]
      timeout = 5000
      break
    case BannerType.SuccessfulSquash:
    case BannerType.SuccessfulReorder:
      data = [String(banner.count)]
      actions = ['undo']
      timeout = 15000
      break
    case BannerType.SquashUndone:
    case BannerType.ReorderUndone:
      data = [String(banner.commitsCount)]
      timeout = 5000
      break
    case BannerType.OpenThankYouCard:
      actions = ['open', 'throwAway']
      break
    case BannerType.OSVersionNoLongerSupported:
      actions = ['support']
      break
  }
  if (dismissable) {
    actions.push('dismiss')
  }
  const detail =
    banner.type === BannerType.SuccessfulMerge ||
    banner.type === BannerType.BranchAlreadyUpToDate
      ? banner.theirBranch
      : banner.type === BannerType.SuccessfulRebase
      ? banner.baseBranch
      : undefined
  return {
    id: workspaceBannerID(banner),
    type: banner.type,
    labelKey: `workspace.banner.${banner.type}${
      detail === undefined ? '' : '.withSource'
    }`,
    data,
    actions,
    timeout,
    dismissable,
  }
}

/** Whitelisted view-only state. Raw Account, callback, JSX, Map, and retry
 * continuation objects deliberately have no route into this wire contract. */
export function projectNativeWorkspace(app: IAppState): IKernelWorkspaceDTO {
  const selected =
    app.selectedState?.type === SelectionType.Repository
      ? app.selectedState
      : null
  const state = selected?.state
  const repository = selected?.repository
  const tip = state?.branchesState.tip
  const current = tip?.kind === TipState.Valid ? tip.branch : null
  const branchView = (b: Branch): IKernelWorkspaceBranch => ({
    ref: b.ref,
    name: b.name,
    remote: b.type === BranchType.Remote,
    current: b.ref === current?.ref,
    tip: b.tip.sha,
  })
  const compareView =
    state === undefined || repository === undefined
      ? null
      : workspaceComparison(repository.path, state)
  const form = state?.compareState.formState
  const stash = state?.changesState.stashEntry
  const stashSelection =
    state?.changesState.selection.kind === ChangesSelectionKind.Stash
      ? state.changesState.selection
      : null
  const filters = {
    ...workspaceNavigationFilters(repository?.path ?? '', app.currentFoldout),
    compare: state?.compareState.filterText ?? '',
  }
  const filtered = <T>(
    items: ReadonlyArray<T>,
    text: string,
    getText: (item: T) => string[]
  ) =>
    filterListItems(
      items.map(item => ({ item, text: getText(item) })),
      text
    ).map(({ item }) => item.item)
  const selectedDiff =
    state === undefined ? null : selectedWorkspaceTextDiff(state)
  const diffView =
    state === undefined || repository === undefined
      ? null
      : workspaceDiffPresentation(repository.path, state)
  const expandableDiff =
    selectedDiff === null || diffView === null
      ? null
      : getTextDiffWithBottomDummyHunk(
          selectedDiff.diff,
          selectedDiff.diff.hunks,
          diffView.contents.oldContents.length,
          diffView.contents.newContents.length
        ) ?? selectedDiff.diff
  const diffSelection =
    state === undefined
      ? null
      : projectDiffSelection(state, app.showDiffCheckMarks)
  const expandedSelection =
    state !== undefined &&
    diffView?.expanded != null &&
    state.changesState.selection.kind === ChangesSelectionKind.WorkingDirectory
      ? projectDiffSelection(
          {
            ...state,
            changesState: {
              ...state.changesState,
              selection: {
                ...state.changesState.selection,
                diff: diffView.expanded,
              },
            },
          },
          app.showDiffCheckMarks
        )
      : null
  const conflict = state?.changesState.conflictState ?? null
  const conflictCount =
    conflict === null || state === undefined
      ? 0
      : getConflictedFiles(
          state.changesState.workingDirectory,
          conflict.manualResolutions
        ).length
  const conflictStep = state?.multiCommitOperationState?.step.kind
  const canOperateConflict =
    conflictStep === MultiCommitOperationStepKind.ShowConflicts ||
    conflictStep === MultiCommitOperationStepKind.ShowCopilotConflicts ||
    conflictStep === MultiCommitOperationStepKind.HideConflicts
  const menus: IKernelWorkspaceMenu[] = []
  if (repository !== undefined && state !== undefined) {
    const targets: WorkspaceMenuTarget[] = [
      ...state.branchesState.allBranches
        .filter(b => !b.isDesktopForkRemoteBranch)
        .map(b => ({ kind: 'branch' as const, id: b.ref })),
      ...state.branchesState.openPullRequests.map(pr => ({
        kind: 'pullRequest' as const,
        id: String(pr.pullRequestNumber),
      })),
      ...state.worktrees.map(w => ({ kind: 'worktree' as const, id: w.path })),
      ...visibleWorkingDirectoryFiles(state, app.showChangesFilter).map(f => ({
        kind: 'file' as const,
        id: f.id,
      })),
      ...state.compareState.commitSHAs
        .filter(sha => state.commitLookup.has(sha))
        .map(sha => ({ kind: 'commit' as const, id: sha })),
      ...state.commitSelection.changesetData.files.map(f => ({
        kind: 'historyFile' as const,
        id: f.id,
      })),
      { kind: 'changes', id: '' },
    ]
    for (const target of targets) {
      menus.push(
        workspaceMenuDTO(
          repository,
          state,
          target,
          buildWorkspaceMenu(app, repository, state, target)
        )
      )
    }
  }
  return {
    repository:
      repository === undefined
        ? null
        : { id: repository.id, path: repository.path },
    navigation:
      app.currentFoldout?.type === FoldoutType.Repository
        ? 'repositories'
        : app.currentFoldout?.type === FoldoutType.Worktree
        ? 'worktrees'
        : app.currentFoldout?.type === FoldoutType.Branch
        ? app.selectedBranchesTab === BranchesTab.PullRequests &&
          repository?.gitHubRepository != null
          ? 'pullRequests'
          : 'branches'
        : '',
    currentBranchRef: current?.ref ?? null,
    filters,
    locked:
      repository !== undefined &&
      state !== undefined &&
      workspaceLocked(repository, state),
    checkoutInProgress: state?.checkoutProgress != null,
    branches:
      state === undefined
        ? []
        : groupBranches(
            state.branchesState.defaultBranch,
            current,
            state.branchesState.allBranches,
            state.branchesState.recentBranches
          ).map(g => ({
            group: g.identifier,
            items: filtered(g.items, filters.branches, i => [
              i.branch.name,
            ]).map(i => branchView(i.branch)),
          })),
    pullRequests:
      filtered(
        state?.branchesState.openPullRequests ?? [],
        filters.pullRequests,
        pullRequestSearchText
      ).map(pr => ({
        number: pr.pullRequestNumber,
        title: pr.title,
        author: pr.author,
        head: pr.head.ref,
        base: pr.base.ref,
        draft: pr.draft,
        checkoutAvailable: pr.head.gitHubRepository.cloneURL !== null,
      })) ?? [],
    pullRequestsAvailable: repository?.gitHubRepository != null,
    loadingPullRequests: state?.branchesState.isLoadingPullRequests ?? false,
    worktrees:
      filtered(state?.worktrees ?? [], filters.worktrees, w => [
        getWorktreeDisplayName(w),
      ]).map(w => ({
        path: w.path,
        name: getWorktreeDisplayName(w),
        description: getWorktreeDescription(w),
        main: w.type === 'main',
        current: w.path === repository?.path,
        locked: w.isLocked,
        missing: w.isPrunable,
      })) ?? [],
    compare: {
      showBranchList: state?.compareState.showBranchList ?? false,
      mode:
        form?.kind !== HistoryTabMode.Compare
          ? 'history'
          : form.comparisonMode === ComparisonMode.Ahead
          ? 'ahead'
          : 'behind',
      ref:
        form?.kind === HistoryTabMode.Compare
          ? form.comparisonBranch.ref
          : null,
      ahead: form?.kind === HistoryTabMode.Compare ? form.aheadBehind.ahead : 0,
      behind:
        form?.kind === HistoryTabMode.Compare ? form.aheadBehind.behind : 0,
      mergeStatus: state?.compareState.mergeStatus?.kind ?? null,
      operation: compareView?.operation.toLowerCase() ?? 'merge',
      operationDisabled:
        state === undefined ||
        compareView === null ||
        form?.kind !== HistoryTabMode.Compare ||
        compareOperationDisabled(
          compareView.operation,
          compareView.preview,
          state.compareState.mergeStatus,
          form.aheadBehind.behind
        ),
      previewStatus: compareView?.preview?.kind ?? null,
      rebaseCommits:
        compareView?.preview?.kind === ComputedAction.Clean
          ? compareView.preview.commitsAhead.length
          : 0,
      branches: filtered(
        state?.compareState.branches ?? [],
        filters.compare,
        b => [b.name]
      ).map(branchView),
    },
    historySelectionContiguous: state?.commitSelection.isContiguous ?? true,
    localCommitSHAs: state?.localCommitSHAs ?? [],
    tagsToPush: state?.tagsToPush ?? [],
    reorderSelection:
      repository === undefined
        ? []
        : workspaceReorderSelection(repository.path).filter(sha =>
            state?.compareState.commitSHAs.includes(sha)
          ),
    stash:
      stash == null
        ? null
        : {
            sha: stash.stashSha,
            branch: stash.branchName,
            loadState: stash.files.kind,
            selected: stashSelection !== null,
            fileID: stashSelection?.selectedStashedFile?.id ?? null,
            files:
              stash.files.kind === StashedChangesLoadStates.Loaded
                ? stash.files.files.map(f => ({
                    id: f.id,
                    path: f.path,
                    status: f.status.kind,
                  }))
                : [],
          },
    filesFilter:
      state === undefined
        ? {
            filterText: '',
            isIncludedInCommit: false,
            isExcludedFromCommit: false,
            isNewFile: false,
            isModifiedFile: false,
            isDeletedFile: false,
          }
        : {
            filterText: state.changesState.fileListFilter.filterText,
            isIncludedInCommit:
              state.changesState.fileListFilter.isIncludedInCommit,
            isExcludedFromCommit:
              state.changesState.fileListFilter.isExcludedFromCommit,
            isNewFile: state.changesState.fileListFilter.isNewFile,
            isModifiedFile: state.changesState.fileListFilter.isModifiedFile,
            isDeletedFile: state.changesState.fileListFilter.isDeletedFile,
          },
    showFilesFilter: app.showChangesFilter,
    visibleFileIDs:
      state === undefined
        ? []
        : visibleWorkingDirectoryFiles(state, app.showChangesFilter).map(
            f => f.id
          ),
    menus,
    banner: projectBanner(app.currentBanner),
    conflict:
      state === undefined || repository === undefined || conflict === null
        ? null
        : {
            kind: conflict.kind,
            identity: workspaceConflictIdentity(state)!,
            remaining: conflictCount,
            canContinue:
              canOperateConflict &&
              conflictCount === 0 &&
              !state.isCommitting &&
              !workspaceLocked(repository, state),
            canAbort:
              (canOperateConflict ||
                conflictStep ===
                  MultiCommitOperationStepKind.ShowCopilotConflictsLoading) &&
              !state.isCommitting &&
              !workspaceLocked(repository, state),
            untrackedExcluded:
              conflict.kind === 'rebase' &&
              state.changesState.workingDirectory.files.some(f =>
                f.isUntracked()
              ),
          },
    textDiff:
      selectedDiff === null
        ? null
        : {
            fileID: selectedDiff.file.id,
            identity: selectedDiff.identity,
            prepared: diffView !== null,
            canExpand:
              diffView !== null &&
              diffView.contents.canBeExpanded &&
              diffView.contents.newContents.length > 0 &&
              expandableDiff !== null &&
              workspaceDiffExpansionEnabled(
                expandableDiff,
                diffView.expanded !== null
              ),
            expandedText: diffView?.expanded?.text ?? null,
            selection:
              expandedSelection === null || diffSelection === null
                ? null
                : { ...expandedSelection, identity: diffSelection.identity },
          },
    hideWhitespace:
      state?.selectedSection === RepositorySectionTab.History
        ? app.hideWhitespaceInHistoryDiff
        : app.hideWhitespaceInChangesDiff,
    sideBySide: app.showSideBySideDiff,
    checkMarks: app.showDiffCheckMarks,
  }
}

import * as Path from 'path'
import { existsSync } from 'fs'
import memoizeOne from 'memoize-one'
import type { AppRuntime } from '../ui/app-runtime'
import {
  FoldoutType,
  IAppState,
  IRepositoryState,
  ChangesSelectionKind,
  HistoryTabMode,
  ComparisonMode,
  RepositorySectionTab,
  isRebaseConflictState,
} from '../lib/app-state'
import {
  Repository,
  isRepositoryWithGitHubRepository,
} from '../models/repository'
import { BranchesTab } from '../models/branches-tab'
import { TipState } from '../models/tip'
import { BannerType } from '../models/banner'
import { PopupType } from '../models/popup'
import { StashedChangesLoadStates } from '../models/stash-entry'
import { DiffType } from '../models/diff'
import {
  MultiCommitOperationKind,
  MultiCommitOperationStepKind,
} from '../models/multi-commit-operation'
import {
  compareOperationDisabled,
  dispatchCompareOperation,
} from '../ui/history/compare-operation-controller'
import { updateRebasePreview } from '../ui/lib/update-branch'
import { getMultiCommitOperationChooseBranchStep } from '../lib/multi-commit-operation'
import { hasConflictedFiles, getConflictedFiles } from '../lib/status'
import { createCommitURL } from '../lib/commit-url'
import { setNumber } from '../lib/local-storage'
import { UnsupportedOSBannerDismissedAtKey } from '../ui/banners/unsupported-os-state'
import { openFile } from '../ui/lib/open-file'
import { IWorkspaceMenuItem } from '../ui/lib/workspace-menu'
import { generateBranchContextMenuItems } from '../ui/branches/branch-list-item-context-menu'
import { generatePullRequestContextMenuItems } from '../ui/branches/pull-request-list-item-context-menu'
import { generateWorktreeContextMenuItems } from '../ui/worktrees/worktree-list-item-context-menu'
import { generateChangesContextMenu } from '../ui/changes/changes-menu-controller'
import {
  ICommitMenuConfig,
  generateCommitContextMenu,
} from '../ui/history/commit-menu-controller'
import { generateHistoryFileContextMenu } from '../ui/history/history-file-menu-controller'
import {
  checkoutWorkspaceBranch,
  checkoutWorkspacePullRequest,
  deleteWorkspaceBranch,
  checkoutWorkspaceBranchInWorktree,
  checkoutWorkspacePRInWorktree,
} from '../ui/branches/branch-controller'
import {
  selectWorkspaceWorktree,
  renameWorkspaceWorktree,
  removeWorkspaceWorktree,
  createWorkspaceWorktree,
} from '../ui/worktrees/worktree-controller'
import {
  restoreWorkspaceStash,
  discardWorkspaceStash,
} from '../ui/stashing/stash-controller'
import {
  planWorkspaceCommitInsertion,
  compareWorkspaceBranch,
  reorderWorkspaceCommits,
  squashWorkspaceCommits,
} from '../ui/history/history-workspace-controller'
import { continueOperation, confirmAbortOperation } from './operation-dialogs'
import {
  NativeWorkspaceAction,
  INativeWorkspaceCommand,
  WorkspaceMenuTarget,
} from './workspace-protocol'
import { discardWorkspaceDiffSelection } from '../ui/diff/diff-menu-controller'
import { getFileContents } from '../ui/diff/syntax-highlighting'
import {
  expandWholeTextDiff,
  getTextDiffWithBottomDummyHunk,
} from '../ui/diff/text-diff-expansion'
import { projectDiffSelection } from './line-selection'
import { workspaceMenuDTO, workspaceLocked } from './workspace-projection'
import {
  workspaceBannerID,
  setWorkspaceReorderSelection,
  setWorkspaceNavigationFilter,
  workspaceComparison,
  setWorkspaceComparison,
  selectedWorkspaceTextDiff,
  selectedWorkspaceDiff,
  workspaceDiffPresentation,
  setWorkspaceDiffPresentation,
  workspaceConflictIdentity,
} from './newworkspace'

function required<T>(value: T | undefined | null, name: string): T {
  if (value === undefined || value === null) {
    throw new Error(`The ${name} changed; refresh the workspace`)
  }
  return value
}

// Match the original commit list's index: repeated membership/row lookups must
// not scan the entire loaded history for every projected context menu.
const historyIndex = memoizeOne(
  (shas: ReadonlyArray<string>) =>
    new Map(shas.map((sha, index) => [sha, index]))
)

function commitsFor(state: IRepositoryState, shas: ReadonlyArray<string>) {
  const index = historyIndex(state.compareState.commitSHAs)
  return shas.map(sha => {
    if (!index.has(sha)) {
      throw new Error('The history selection is no longer available')
    }
    return required(state.commitLookup.get(sha), 'commit')
  })
}

function retainedRef(state: IRepositoryState, shas: ReadonlyArray<string>) {
  const index = Math.max(
    ...shas.map(sha => state.compareState.commitSHAs.indexOf(sha))
  )
  return index === state.compareState.commitSHAs.length - 1
    ? null
    : `${state.compareState.commitSHAs[index]}^`
}

/** Construct the exact shared original menus. Projection calls this without a
 * runtime and only reads metadata; invoking any action requires a real runtime. */
export function buildWorkspaceMenu(
  app: IAppState,
  repository: Repository,
  state: IRepositoryState,
  target: WorkspaceMenuTarget,
  runtime?: AppRuntime
): ReadonlyArray<IWorkspaceMenuItem> {
  const dispatcher = () => required(runtime, 'kernel runtime').dispatcher
  const editor = app.useCustomEditor
    ? undefined
    : app.selectedExternalEditor ?? undefined
  const openEditor = (path: string) =>
    dispatcher().openInExternalEditor(Path.join(repository.path, path))
  const open = (path: string) =>
    openFile(Path.join(repository.path, path), dispatcher())
  const viewCommit = (sha: string, path?: string) => {
    if (repository.gitHubRepository === null) {
      return
    }
    const url = createCommitURL(repository.gitHubRepository, sha, path)
    if (url !== null) {
      return dispatcher().openInBrowser(url)
    }
    return
  }
  switch (target.kind) {
    case 'branch': {
      const branch = required(
        state.branchesState.allBranches.find(b => b.ref === target.id),
        'branch'
      )
      const current =
        state.branchesState.tip.kind === TipState.Valid &&
        state.branchesState.tip.branch.ref === branch.ref
      const pr = current ? state.branchesState.currentPullRequest : null
      return generateBranchContextMenuItems({
        branch,
        onRenameBranch: () =>
          dispatcher().showPopup({
            type: PopupType.RenameBranch,
            repository,
            branch,
          }),
        onDeleteBranch: () =>
          deleteWorkspaceBranch(dispatcher(), repository, branch),
        onCheckoutInNewWorktree: () =>
          checkoutWorkspaceBranchInWorktree(dispatcher(), repository, branch),
        onViewBranchOnGitHub:
          current &&
          repository.gitHubRepository?.htmlURL &&
          branch.upstreamWithoutRemote
            ? () =>
                dispatcher().openInBrowser(
                  `${
                    repository.gitHubRepository!.htmlURL
                  }/tree/${encodeURIComponent(branch.upstreamWithoutRemote!)}`
                )
            : undefined,
        onViewPullRequestOnGitHub:
          pr === null ? undefined : () => dispatcher().showPullRequestByPR(pr),
      })
    }
    case 'worktree': {
      const tree = required(
        state.worktrees.find(w => w.path === target.id),
        'worktree'
      )
      return generateWorktreeContextMenuItems({
        path: tree.path,
        isMainWorktree: tree.type === 'main',
        isLocked: tree.isLocked,
        onRenameWorktree: path =>
          renameWorkspaceWorktree(dispatcher(), repository, path),
        onRemoveWorktree: path =>
          removeWorkspaceWorktree(dispatcher(), repository, path),
      })
    }
    case 'pullRequest': {
      const pr = required(
        state.branchesState.openPullRequests.find(
          p => String(p.pullRequestNumber) === target.id
        ),
        'pull request'
      )
      return generatePullRequestContextMenuItems({
        onViewPullRequestOnGitHub: () => dispatcher().showPullRequestByPR(pr),
        onCheckoutInNewWorktree: () =>
          checkoutWorkspacePRInWorktree(dispatcher(), repository, pr),
      })
    }
    case 'changes': {
      if (
        state.isCommitting ||
        workspaceLocked(repository, state) ||
        (state.changesState.conflictState !== null &&
          isRebaseConflictState(state.changesState.conflictState))
      ) {
        return []
      }
      const hasChanges = state.changesState.workingDirectory.files.length > 0
      return [
        {
          labelKey: 'workspace.discardAll',
          label: 'Discard all changes…',
          enabled: hasChanges,
          action: () =>
            dispatcher().showPopup({
              type: PopupType.ConfirmDiscardChanges,
              repository,
              files: state.changesState.workingDirectory.files,
              showDiscardChangesSetting: false,
              discardingAllChanges: true,
            }),
        },
        {
          labelKey:
            state.changesState.stashEntry === null
              ? 'workspace.stashAll'
              : 'workspace.stashAllConfirm',
          label: 'Stash all changes',
          enabled:
            hasChanges &&
            state.branchesState.tip.kind === TipState.Valid &&
            state.changesState.conflictState === null &&
            !hasConflictedFiles(state.changesState.workingDirectory),
          action: () => dispatcher().createStashForCurrentBranch(repository),
        },
      ]
    }
    case 'file': {
      if (state.isCommitting || workspaceLocked(repository, state)) {
        return []
      }
      const file = required(
        state.changesState.workingDirectory.files.find(f => f.id === target.id),
        'file'
      )
      return generateChangesContextMenu(
        {
          repository,
          workingDirectory: state.changesState.workingDirectory,
          selectedFileIDs:
            state.changesState.selection.kind ===
            ChangesSelectionKind.WorkingDirectory
              ? state.changesState.selection.selectedFileIDs
              : [],
          askForConfirmationOnDiscardChanges:
            app.askForConfirmationOnDiscardChanges,
          externalEditorLabel: editor,
          onDiscard: paths => {
            const files = paths.map(path =>
              required(
                state.changesState.workingDirectory.files.find(
                  f => f.path === path
                ),
                'file'
              )
            )
            if (files.length === 1) {
              return app.askForConfirmationOnDiscardChanges
                ? dispatcher().showPopup({
                    type: PopupType.ConfirmDiscardChanges,
                    repository,
                    files,
                  })
                : dispatcher().discardChanges(repository, files)
            }
            return dispatcher().showPopup({
              type: PopupType.ConfirmDiscardChanges,
              repository,
              files,
              showDiscardChangesSetting: false,
              discardingAllChanges:
                files.length ===
                state.changesState.workingDirectory.files.length,
            })
          },
          onIgnoreFile: async paths => {
            try {
              await dispatcher().appendIgnoreFile(repository, paths)
            } catch (error) {
              log.error('Failed to ignore file(s)', error)
              await dispatcher().postError(error)
            }
          },
          onIgnorePattern: async patterns => {
            try {
              await dispatcher().appendIgnoreRule(repository, patterns)
            } catch (error) {
              log.error('Failed to ignore pattern(s)', error)
              await dispatcher().postError(error)
            }
          },
          onIncludeChanged: (file, included) =>
            dispatcher().changeFileIncluded(repository, file, included),
          onOpenItem: open,
          onOpenItemInExternalEditor: openEditor,
        },
        file,
        state.changesState.conflictState !== null &&
          isRebaseConflictState(state.changesState.conflictState)
      )
    }
    case 'historyFile': {
      const file = required(
        state.commitSelection.changesetData.files.find(f => f.id === target.id),
        'history file'
      )
      return generateHistoryFileContextMenu(
        repository,
        file,
        existsSync(Path.join(repository.path, file.path)),
        commitsFor(state, state.commitSelection.shas),
        state.localCommitSHAs,
        editor,
        openEditor,
        open,
        viewCommit
      )
    }
    case 'commit': {
      const commit = commitsFor(state, [target.id])[0]
      const selected = commitsFor(state, state.commitSelection.shas)
      const form = state.compareState.formState
      const history = form.kind === HistoryTabMode.History
      const config: ICommitMenuConfig = {
        gitHubRepository: repository.gitHubRepository,
        localCommitSHAs: state.localCommitSHAs,
        tagsToPush: state.tagsToPush ?? [],
        canUndoCommits: history,
        canAmendCommits: history,
        canResetToCommits: history,
        isMultiCommitOperationInProgress:
          state.multiCommitOperationState !== null,
        isAssistedCommitRepositoryLocked: workspaceLocked(repository, state),
        disableReordering: !history,
        disableSquashing: !history,
        onUndoCommit: c => dispatcher().undoCommit(repository, c),
        onAmendCommit: (c, local) =>
          dispatcher().startAmendingRepository(repository, c, local),
        onResetToCommit: c => dispatcher().resetToCommit(repository, c),
        onCheckoutCommit: c =>
          app.askForConfirmationOnCheckoutCommit
            ? dispatcher().showPopup({
                type: PopupType.ConfirmCheckoutCommit,
                repository,
                commit: c,
              })
            : dispatcher().checkoutCommit(repository, c),
        onKeyboardReorder: commits => {
          required(runtime, 'kernel runtime')
          setWorkspaceReorderSelection(
            repository.path,
            commits.map(c => c.sha)
          )
        },
        onRevertCommit:
          history || form.comparisonMode === ComparisonMode.Ahead
            ? c => dispatcher().revertCommit(repository, c)
            : undefined,
        onCreateBranch: c =>
          dispatcher().showPopup({
            type: PopupType.CreateBranch,
            repository,
            targetCommit: c,
          }),
        onCreateTag: sha =>
          dispatcher().showCreateTagDialog(repository, sha, state.localTags),
        onDeleteTag: tag => dispatcher().showDeleteTagDialog(repository, tag),
        onViewCommitOnGitHub: viewCommit,
        onCherryPick: commits => {
          const tip = state.branchesState.tip
          if (tip.kind !== TipState.Valid) {
            throw new Error('A branch is required to start cherry-picking')
          }
          dispatcher().initializeMultiCommitOperation(
            repository,
            {
              kind: MultiCommitOperationKind.CherryPick,
              sourceBranch: tip.branch,
              branchCreated: false,
              commits,
            },
            null,
            commits,
            tip.branch.tip.sha
          )
          dispatcher().setMultiCommitOperationStep(
            repository,
            getMultiCommitOperationChooseBranchStep(state)
          )
          dispatcher().incrementMetric('cherryPickViaContextMenuCount')
          dispatcher().showPopup({
            type: PopupType.MultiCommitOperation,
            repository,
          })
        },
        onSquash: (commits, onto, contextMenu) =>
          squashWorkspaceCommits(
            dispatcher(),
            repository,
            commits,
            onto,
            retainedRef(state, [...commits.map(c => c.sha), onto.sha]),
            contextMenu
          ),
      }
      const row = required(
        historyIndex(state.compareState.commitSHAs).get(commit.sha),
        'commit row'
      )
      const items = generateCommitContextMenu(config, row, commit, selected)
      // A timeline row keeps its own copy entry during multi-selection. Reuse
      // the original single-commit action without changing AppStore selection
      // or replacing the existing batch-operation menu policy.
      if (selected.length > 1) {
        const copy = generateCommitContextMenu(config, row, commit, [
          commit,
        ]).find(item => item.labelKey === 'workspace.copySHA')
        if (copy !== undefined) {
          items.push({ type: 'separator' }, copy)
        }
      }
      return items
    }
  }
}

/** All execution goes through verified original contracts and current objects.
 * No function, Account, token, or JSX from these closures is serialized. */
export async function executeNativeWorkspaceCommand(
  runtime: AppRuntime,
  resolvedRepository: Repository | null,
  command: INativeWorkspaceCommand | NativeWorkspaceAction
) {
  const action = 'name' in command ? command.action : command
  if (action.type === 'banner') {
    return executeWorkspaceBanner(runtime, action)
  }
  const repository = required(resolvedRepository, 'repository')
  const dispatcher = runtime.dispatcher
  const app = runtime.appStore.getState()
  const state = runtime.repositoryStateManager.get(repository)
  const branch = (ref: string) =>
    required(
      state.branchesState.allBranches.find(b => b.ref === ref),
      'branch'
    )
  const stash = (sha?: string) => {
    const entry = required(state.changesState.stashEntry, 'stash')
    if (sha !== undefined && sha !== entry.stashSha) {
      throw new Error('The stash changed; review it again before continuing')
    }
    return entry
  }
  switch (action.type) {
    case 'submodule.open': {
      const selected = selectedWorkspaceDiff(state)
      if (
        selected?.file.id !== action.fileID ||
        selected.diff.kind !== DiffType.Submodule ||
        selected.diff.url === null
      ) {
        throw new Error('The submodule changed; select it again')
      }
      // Resolve the original fullPath here; the native frontend cannot submit
      // another path or turn a removed historical submodule into an opener.
      dispatcher.incrementMetric('openSubmoduleFromDiffCount')
      // Like the original Repository view, acknowledge the opener immediately.
      // Its Open in Desktop promise is a clone continuation, not a modal reply.
      void dispatcher.openOrAddRepository(selected.diff.fullPath)
      return
    }
    case 'conflicts.open':
    case 'conflicts.continue':
    case 'conflicts.abort': {
      if (workspaceConflictIdentity(state) !== action.identity) {
        throw new Error('The conflict operation changed; review it again')
      }
      // Original status loading reconstructs restart-time merge/rebase/cherry-
      // pick flow. Never synthesize its branch/commit state in this presenter.
      await dispatcher.refreshRepository(repository)
      let latest = runtime.repositoryStateManager.get(repository)
      let operation = required(
        latest.multiCommitOperationState,
        'conflict operation'
      )
      if (operation.step.kind === MultiCommitOperationStepKind.HideConflicts) {
        await dispatcher.setMultiCommitOperationStep(repository, {
          kind: MultiCommitOperationStepKind.ShowConflicts,
          conflictState: {
            ...operation.step.conflictState,
            manualResolutions: required(
              latest.changesState.conflictState,
              'conflict state'
            ).manualResolutions,
          },
        })
        latest = runtime.repositoryStateManager.get(repository)
        operation = required(
          latest.multiCommitOperationState,
          'conflict operation'
        )
      }
      if (
        !runtime.appStore
          .getState()
          .allPopups.some(
            p =>
              p.type === PopupType.MultiCommitOperation &&
              p.repository.path === repository.path
          )
      ) {
        await dispatcher.showPopup({
          type: PopupType.MultiCommitOperation,
          repository,
        })
      }
      if (action.type === 'conflicts.open') {
        return
      }
      const props = {
        dispatcher,
        repository,
        state: operation,
        workingDirectory: latest.changesState.workingDirectory,
        conflictState: latest.changesState.conflictState,
      }
      if (action.type === 'conflicts.abort') {
        return confirmAbortOperation(props)
      }
      const conflict = required(props.conflictState, 'conflict state')
      if (
        latest.isCommitting ||
        workspaceLocked(repository, latest) ||
        getConflictedFiles(props.workingDirectory, conflict.manualResolutions)
          .length > 0
      ) {
        throw new Error('Resolve all conflicts before continuing')
      }
      return continueOperation(props)
    }
    case 'navigation.open':
      if (workspaceLocked(repository, state)) {
        throw new Error('Repository operations are locked')
      }
      if (action.target === 'worktrees') {
        return dispatcher.showFoldout({ type: FoldoutType.Worktree })
      }
      if (
        action.target === 'pullRequests' &&
        !isRepositoryWithGitHubRepository(repository)
      ) {
        throw new Error('This repository has no GitHub pull requests')
      }
      await dispatcher.changeBranchesTab(
        action.target === 'pullRequests'
          ? BranchesTab.PullRequests
          : BranchesTab.Branches
      )
      return dispatcher.showFoldout({ type: FoldoutType.Branch })
    case 'navigation.close': {
      const type =
        action.target === 'worktrees'
          ? FoldoutType.Worktree
          : FoldoutType.Branch
      if (app.currentFoldout?.type !== type) {
        return
      }
      if (
        type === FoldoutType.Branch &&
        (app.selectedBranchesTab === BranchesTab.PullRequests &&
          isRepositoryWithGitHubRepository(repository)) !==
          (action.target === 'pullRequests')
      ) {
        return
      }
      return dispatcher.closeFoldout(type)
    }
    case 'navigation.filter':
      if (action.scope === 'compare') {
        return dispatcher.updateCompareForm(repository, {
          filterText: action.text,
        })
      }
      setWorkspaceNavigationFilter(
        repository.path,
        action.scope,
        action.text,
        app.currentFoldout
      )
      return
    case 'pullRequest.refresh':
      if (!isRepositoryWithGitHubRepository(repository)) {
        throw new Error('This repository has no GitHub pull requests')
      }
      return dispatcher.refreshPullRequests(repository)
    case 'branch.checkout':
      if (workspaceLocked(repository, state)) {
        throw new Error('Repository operations are locked')
      }
      return checkoutWorkspaceBranch(dispatcher, repository, branch(action.ref))
    case 'branch.create':
      return dispatcher.showPopup({
        type: PopupType.CreateBranch,
        repository,
        initialName: action.initialName,
      })
    case 'pullRequest.checkout': {
      if (!isRepositoryWithGitHubRepository(repository)) {
        throw new Error('This repository has no GitHub pull requests')
      }
      const pr = required(
        state.branchesState.openPullRequests.find(
          p => p.pullRequestNumber === action.number
        ),
        'pull request'
      )
      return checkoutWorkspacePullRequest(dispatcher, repository, pr)
    }
    case 'worktree.select':
      return selectWorkspaceWorktree(
        dispatcher,
        repository,
        required(
          state.worktrees.find(w => w.path === action.path),
          'worktree'
        )
      )
    case 'worktree.create':
      return createWorkspaceWorktree(dispatcher, repository)
    case 'compare.branch':
      return compareWorkspaceBranch(
        dispatcher,
        repository,
        branch(action.ref),
        action.mode === 'ahead' ? ComparisonMode.Ahead : ComparisonMode.Behind
      )
    case 'compare.list':
      return dispatcher.updateCompareForm(repository, {
        showBranchList: action.visible,
      })
    case 'compare.history':
      await dispatcher.executeCompare(repository, {
        kind: HistoryTabMode.History,
      })
      return dispatcher.updateCompareForm(repository, {
        filterText: '',
        showBranchList: false,
      })
    case 'compare.operation': {
      const form = state.compareState.formState
      const tip = state.branchesState.tip
      if (form.kind !== HistoryTabMode.Compare || tip.kind !== TipState.Valid) {
        throw new Error('No branch comparison is selected')
      }
      const operation =
        action.operation === 'rebase'
          ? MultiCommitOperationKind.Rebase
          : action.operation === 'squash'
          ? MultiCommitOperationKind.Squash
          : MultiCommitOperationKind.Merge
      const previous = workspaceComparison(repository.path, state)
      setWorkspaceComparison(repository.path, {
        ...previous,
        operation,
        preview: null,
      })
      if (operation === MultiCommitOperationKind.Rebase) {
        await updateRebasePreview(
          form.comparisonBranch,
          tip.branch,
          repository,
          preview =>
            setWorkspaceComparison(repository.path, {
              identity: previous.identity,
              operation,
              preview,
            })
        )
      }
      return
    }
    case 'compare.merge': {
      const form = state.compareState.formState
      const tip = state.branchesState.tip
      const presentation = workspaceComparison(repository.path, state)
      if (
        form.kind !== HistoryTabMode.Compare ||
        form.comparisonMode !== ComparisonMode.Behind ||
        tip.kind !== TipState.Valid ||
        compareOperationDisabled(
          presentation.operation,
          presentation.preview,
          state.compareState.mergeStatus,
          form.aheadBehind.behind
        )
      ) {
        throw new Error('This comparison cannot be updated')
      }
      await dispatchCompareOperation(
        dispatcher,
        repository,
        presentation.operation,
        tip.branch,
        form.comparisonBranch,
        state.compareState.mergeStatus,
        presentation.preview
      )
      await dispatcher.executeCompare(repository, {
        kind: HistoryTabMode.History,
      })
      return dispatcher.updateCompareForm(repository, {
        showBranchList: false,
        filterText: '',
      })
    }
    case 'history.select': {
      commitsFor(state, action.shas)
      const selected = state.compareState.commitSHAs.filter(sha =>
        action.shas.includes(sha)
      )
      const indexes = selected.map(sha =>
        state.compareState.commitSHAs.indexOf(sha)
      )
      const contiguous = indexes.every(
        (index, i) => i === 0 || index === indexes[i - 1] + 1
      )
      await dispatcher.changeCommitSelection(repository, selected, contiguous)
      return dispatcher.loadChangedFilesForCurrentSelection(repository)
    }
    case 'history.reorder.cancel':
      setWorkspaceReorderSelection(repository.path, [])
      return
    case 'history.reorder': {
      if (
        state.compareState.formState.kind !== HistoryTabMode.History ||
        workspaceLocked(repository, state) ||
        state.multiCommitOperationState !== null
      ) {
        throw new Error('Commits cannot be reordered now')
      }
      const commits = commitsFor(state, action.shas)
      const base =
        action.beforeSHA === null
          ? null
          : commitsFor(state, [action.beforeSHA])[0]
      const plan = planWorkspaceCommitInsertion(
        state.compareState.commitSHAs,
        base === null ? 0 : state.compareState.commitSHAs.indexOf(base.sha) + 1,
        action.shas
      )
      if (plan === null) {
        return
      }
      setWorkspaceReorderSelection(repository.path, [])
      return reorderWorkspaceCommits(
        dispatcher,
        repository,
        base,
        commits,
        plan.lastRetainedCommitRef
      )
    }
    case 'stash.view':
      stash()
      await dispatcher.changeRepositorySection(
        repository,
        RepositorySectionTab.Changes
      )
      return dispatcher.selectStashedFile(repository)
    case 'stash.hide':
      return dispatcher.hideStashedChanges(repository)
    case 'stash.create':
      return dispatcher.createStashForCurrentBranch(repository)
    case 'stash.restore':
      return restoreWorkspaceStash(dispatcher, repository, stash(action.sha))
    case 'stash.discard':
      return discardWorkspaceStash(
        dispatcher,
        repository,
        stash(action.sha),
        app.askForConfirmationOnDiscardStash
      )
    case 'stash.file': {
      const entry = stash(action.sha)
      if (entry.files.kind !== StashedChangesLoadStates.Loaded) {
        throw new Error('Stash files are still loading')
      }
      return dispatcher.selectStashedFile(
        repository,
        required(
          entry.files.files.find(f => f.id === action.fileID),
          'stash file'
        )
      )
    }
    case 'files.filter': {
      const f = action.filter
      dispatcher.setChangesListFilterText(repository, f.filterText)
      dispatcher.setIncludedChangesInCommitFilter(
        repository,
        f.isIncludedInCommit
      )
      dispatcher.setFilterExcludedFiles(repository, f.isExcludedFromCommit)
      dispatcher.setFilterNewFiles(repository, f.isNewFile)
      dispatcher.setFilterModifiedFiles(repository, f.isModifiedFile)
      dispatcher.setFilterDeletedFiles(repository, f.isDeletedFile)
      return
    }
    case 'menu': {
      const items = buildWorkspaceMenu(
        app,
        repository,
        state,
        action.target,
        runtime
      )
      const projected = workspaceMenuDTO(
        repository,
        state,
        action.target,
        items
      )
      if (projected.lease !== action.lease) {
        throw new Error('This context menu changed; open it again')
      }
      let menu: ReadonlyArray<IWorkspaceMenuItem> = items
      let item: IWorkspaceMenuItem | undefined
      for (let i = 0; i < action.path.length; i++) {
        item = menu[action.path[i]]
        if (item === undefined || item.enabled === false) {
          throw new Error('This menu action is not available')
        }
        if (i < action.path.length - 1) {
          menu = required(item.submenu, 'submenu')
        }
      }
      if (item?.action === undefined) {
        throw new Error('This menu entry is not an action')
      }
      return item.action()
    }
    case 'diff.prepare':
    case 'diff.expand':
    case 'diff.collapse': {
      const selected = selectedWorkspaceTextDiff(state)
      if (
        selected === null ||
        selected.file.id !== action.fileID ||
        selected.identity !== action.identity
      ) {
        // Preparation is background presentation read-ahead. Selecting another
        // file can supersede it before its queued request runs; unlike an
        // explicit expand/discard action it must not raise a Git error popup.
        if (action.type === 'diff.prepare') {
          return
        }
        throw new Error('The diff changed; open the menu again')
      }
      let view = workspaceDiffPresentation(repository.path, state)
      if (view === null) {
        const contents = await getFileContents(repository, selected.file)
        const latest = selectedWorkspaceTextDiff(
          runtime.repositoryStateManager.get(repository)
        )
        if (
          latest?.identity !== selected.identity ||
          latest.file.id !== selected.file.id
        ) {
          if (action.type === 'diff.prepare') {
            return
          }
          throw new Error('The diff changed while loading its contents')
        }
        view = {
          identity: selected.identity,
          fileID: selected.file.id,
          contents,
          expanded: null,
        }
      }
      if (action.type === 'diff.collapse') {
        view = { ...view, expanded: null }
      }
      if (action.type === 'diff.expand') {
        if (
          !view.contents.canBeExpanded ||
          view.contents.newContents.length === 0
        ) {
          throw new Error('This file cannot be expanded')
        }
        const diff =
          getTextDiffWithBottomDummyHunk(
            selected.diff,
            selected.diff.hunks,
            view.contents.oldContents.length,
            view.contents.newContents.length
          ) ?? selected.diff
        const expanded = expandWholeTextDiff(diff, view.contents.newContents)
        if (expanded === undefined) {
          throw new Error('The current file contents do not match this patch')
        }
        view = { ...view, expanded }
      }
      setWorkspaceDiffPresentation(repository.path, view)
      return
    }
    case 'diff.discard': {
      const projection = projectDiffSelection(state, app.showDiffCheckMarks)
      if (
        state.isCommitting ||
        workspaceLocked(repository, state) ||
        app.hideWhitespaceInChangesDiff ||
        projection === null ||
        projection.identity !== action.identity ||
        projection.fileID !== action.fileID ||
        action.lineIDs.some(
          id => !projection.lines.some(line => line.id === id)
        )
      ) {
        throw new Error(
          'The diff changed; review the current patch before discarding lines'
        )
      }
      const file = required(
        state.changesState.workingDirectory.files.find(
          f => f.id === action.fileID
        ),
        'diff file'
      )
      const selectionState = state.changesState.selection
      if (
        selectionState.kind !== ChangesSelectionKind.WorkingDirectory ||
        selectionState.diff?.kind !== DiffType.Text
      ) {
        throw new Error('The diff is unavailable')
      }
      let selection = file.selection.withSelectNone()
      for (const id of action.lineIDs) {
        selection = selection.withLineSelection(id, true)
      }
      return discardWorkspaceDiffSelection(
        dispatcher,
        repository,
        file,
        selectionState.diff,
        selection,
        app.askForConfirmationOnDiscardChanges
      )
    }
    case 'diff.option':
      switch (action.option) {
        case 'whitespace':
          return state.selectedSection === RepositorySectionTab.History
            ? dispatcher.onHideWhitespaceInHistoryDiffChanged(
                action.value,
                repository
              )
            : dispatcher.onHideWhitespaceInChangesDiffChanged(
                action.value,
                repository
              )
        case 'sideBySide':
          return dispatcher.onShowSideBySideDiffChanged(action.value)
        case 'checkMarks':
          return dispatcher.setDiffCheckMarksSetting(action.value)
      }
  }
}

function executeWorkspaceBanner(
  runtime: AppRuntime,
  action: Extract<NativeWorkspaceAction, { type: 'banner' }>
) {
  const { dispatcher } = runtime
  const banner = runtime.appStore.getState().currentBanner
  if (banner === null || workspaceBannerID(banner) !== action.id) {
    throw new Error('This banner has been replaced')
  }
  if (
    action.action === 'support' &&
    banner.type === BannerType.OSVersionNoLongerSupported
  ) {
    return dispatcher.openInBrowser(
      'https://docs.github.com/en/desktop/installing-and-configuring-github-desktop/overview/supported-operating-systems'
    )
  }
  if (action.action === 'dismiss') {
    if (
      [
        BannerType.MergeConflictsFound,
        BannerType.RebaseConflictsFound,
        BannerType.CherryPickConflictsFound,
        BannerType.ConflictsFound,
      ].includes(banner.type)
    ) {
      throw new Error('Reopen the conflict dialog to continue')
    }
    if (banner.type === BannerType.OSVersionNoLongerSupported) {
      setNumber(UnsupportedOSBannerDismissedAtKey, Date.now())
    }
    return dispatcher.clearBanner()
  }
  switch (banner.type) {
    case BannerType.MergeConflictsFound:
      if (action.action !== 'open') {
        break
      }
      dispatcher.clearBanner()
      dispatcher.showPopup(banner.popup)
      dispatcher.incrementMetric('mergeConflictsDialogReopenedCount')
      return
    case BannerType.RebaseConflictsFound:
      if (action.action !== 'open') {
        break
      }
      dispatcher.clearBanner()
      banner.onOpenDialog()
      dispatcher.incrementMetric('rebaseConflictsDialogReopenedCount')
      return
    case BannerType.CherryPickConflictsFound:
    case BannerType.ConflictsFound:
      if (action.action !== 'open') {
        break
      }
      dispatcher.clearBanner()
      banner.onOpenConflictsDialog()
      return
    case BannerType.SuccessfulCherryPick:
    case BannerType.SuccessfulSquash:
    case BannerType.SuccessfulReorder:
      if (action.action !== 'undo') {
        break
      }
      dispatcher.clearBanner()
      banner.onUndo()
      return
    case BannerType.OpenThankYouCard:
      if (action.action === 'open') {
        return banner.onOpenCard()
      }
      if (action.action === 'throwAway') {
        dispatcher.clearBanner()
        return banner.onThrowCardAway()
      }
  }
  throw new Error('This banner does not offer that action')
}

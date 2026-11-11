import { join } from 'path'
import { Disposable } from 'event-kit'
import type { AppRuntime } from '../ui/app-runtime'
import type { Dispatcher } from '../ui/dispatcher'
import {
  IAppState,
  IRepositoryState,
  IMultiCommitOperationState,
  ConflictState,
  RepositorySectionTab,
  SelectionType,
  isMergeConflictState,
  isRebaseConflictState,
} from '../lib/app-state'
import { Popup, PopupType } from '../models/popup'
import { Repository } from '../models/repository'
import type { Branch } from '../models/branch'
import { Author, isKnownAuthor } from '../models/author'
import { AuthorInputController } from '../ui/lib/author-input/author-from-hit'
import {
  evaluateCommitRules,
  hasCommitRuleFailure,
  CommitRuleStatus,
} from '../ui/changes/commit-policy'
import { getAccountForRepository } from '../lib/get-account-for-repository'
import { useRepoRulesLogic } from '../lib/helpers/repo-rules'
import type { ICommitContext } from '../models/commit'
import { DefaultCommitMessage, ICommitMessage } from '../models/commit-message'
import { UncommittedChangesStrategy } from '../models/uncommitted-changes-strategy'
import { RetryActionType } from '../models/retry-actions'
import { TipState } from '../models/tip'
import {
  MultiCommitOperationKind as Operation,
  MultiCommitOperationStepKind as Step,
  instanceOfIBaseRebaseDetails,
} from '../models/multi-commit-operation'
import { ManualConflictResolution } from '../models/manual-conflict-resolution'
import { isConflictWithMarkers } from '../models/status'
import {
  getResolvedFiles,
  getConflictedFiles,
  isConflictedFile,
} from '../lib/status'
import { BannerType } from '../models/banner'
import { ComputedAction } from '../models/computed-action'
import { getAheadBehind, revSymmetricDifference } from '../lib/git'
import { determineMergeability } from '../lib/git/merge-tree'
import { updateRebasePreview } from '../ui/lib/update-branch'
import { startTimer } from '../ui/lib/timing'
import { displayDialogDiagnostic } from '../ui/lib/dialog-url'
import { sanitizedRefName } from '../lib/sanitize-ref-name'
import { getAccountForCopilotConflictResolution } from '../lib/get-account-for-repository'
import { enableCopilotConflictResolution } from '../lib/feature-flag'
import {
  NativeDialogModule,
  NativeDialogPresentation,
  NativeDialogField,
  NativeDialogAction,
  dialogValues,
  dialogString,
  dialogBoolean,
} from './dialog-module'

type Of<T extends PopupType> = Extract<Popup, { type: T }>
type Dismiss = () => void

/** Shared upstream UI continuations. They deliberately keep the per-dialog
 * order (including close-before-operation and finally-close differences).
 * React and native hosts supply only presentation state and dismissal. */
export async function discardOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.ConfirmDiscardChanges>,
  confirm: boolean,
  onDismissed: Dismiss,
  onConfirmChanged: (value: boolean) => void = (value: boolean) =>
    dispatcher.setConfirmDiscardChangesSetting(value)
) {
  await dispatcher.discardChanges(popup.repository, popup.files)
  onConfirmChanged(confirm)
  onDismissed()
}

export async function discardSelectionOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.ConfirmDiscardSelection>,
  confirm: boolean,
  onDismissed: Dismiss
) {
  await dispatcher.discardChangesFromSelection(
    popup.repository,
    popup.file.path,
    popup.diff,
    popup.selection
  )
  dispatcher.setConfirmDiscardChangesSetting(confirm)
  onDismissed()
}

export async function switchBranchOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.StashAndSwitchBranch>,
  hasAssociatedStash: boolean,
  move: boolean,
  onDismissed: Dismiss
) {
  if (!move && hasAssociatedStash) {
    dispatcher.showPopup({
      type: PopupType.ConfirmOverwriteStash,
      repository: popup.repository,
      branchToCheckout: popup.branchToCheckout,
    })
    return
  }
  const timer = startTimer('stash and checkout', popup.repository)
  try {
    await dispatcher.checkoutBranch(
      popup.repository,
      popup.branchToCheckout,
      move
        ? UncommittedChangesStrategy.MoveToNewBranch
        : UncommittedChangesStrategy.StashOnCurrentBranch
    )
  } finally {
    timer.done()
    onDismissed()
  }
}

export async function overwriteStashOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.ConfirmOverwriteStash>
) {
  if (popup.branchToCheckout !== null) {
    await dispatcher.checkoutBranch(
      popup.repository,
      popup.branchToCheckout,
      UncommittedChangesStrategy.StashOnCurrentBranch
    )
  } else {
    await dispatcher.createStashForCurrentBranch(popup.repository, false)
  }
}

export async function undoOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.WarnLocalChangesBeforeUndo>,
  confirm: boolean
) {
  dispatcher.setConfirmUndoCommitSetting(confirm)
  await dispatcher.undoCommit(popup.repository, popup.commit, false)
}

export async function discardRetryOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.DiscardChangesRetry>,
  confirm: boolean,
  onDismissed: Dismiss,
  onConfirmChanged: (value: boolean) => void = (value: boolean) =>
    dispatcher.setConfirmDiscardChangesPermanentlySetting(value)
) {
  await dispatcher.performRetry(popup.retryAction)
  onConfirmChanged(confirm)
  onDismissed()
}

export async function stashAndRetryOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.LocalChangesOverwritten>,
  hasExistingStash: boolean,
  onDismissed: Dismiss
) {
  if (hasExistingStash) {
    onDismissed()
    return
  }
  const created = await dispatcher.createStashForCurrentBranch(
    popup.repository,
    false
  )
  onDismissed()
  if (created) {
    await dispatcher.performRetry(popup.retryAction)
  }
}

export function dismissLocalChangesOperation(
  dispatcher: Dispatcher,
  popup: Of<PopupType.LocalChangesOverwritten>,
  onDismissed: Dismiss
) {
  onDismissed()
  if (popup.retryAction.type === RetryActionType.Rebase) {
    dispatcher.closePopup(PopupType.MultiCommitOperation)
  }
}

/** CommitMessageDialog explicitly passes commitToAmend=null even when the
 * workbench is amending. Never derive a different amend flag from that draft. */
export function commitDialogContext(
  message: ICommitMessage,
  coAuthors: ReadonlyArray<Author>,
  coAuthorInputEnabled: boolean,
  amend: boolean
): ICommitContext {
  return {
    summary: message.summary,
    description: message.description,
    trailers: coAuthorInputEnabled
      ? coAuthors.filter(isKnownAuthor).map(a => ({
          token: 'Co-Authored-By',
          value: `${a.name} <${a.email}>`,
        }))
      : [],
    amend,
    messageGeneratedByCopilot: message.generatedByCopilot ?? false,
  }
}

/** Multi-commit UI state transitions, extracted from BaseMultiCommitOperation
 * and its operation-specific controllers, not replacement Git algorithms. */
export type OperationControllerProps = {
  readonly dispatcher: Dispatcher
  readonly repository: Repository
  readonly state: IMultiCommitOperationState
  readonly workingDirectory: IRepositoryState['changesState']['workingDirectory']
  readonly conflictState: ConflictState | null
}

export function endOperationFlow(
  p: Pick<OperationControllerProps, 'dispatcher' | 'repository'>
) {
  p.dispatcher.closePopup(PopupType.MultiCommitOperation)
  p.dispatcher.endMultiCommitOperation(p.repository)
}

export async function abortOperation(p: OperationControllerProps) {
  const {
    dispatcher,
    repository,
    state: { operationDetail },
  } = p
  endOperationFlow(p)
  switch (operationDetail.kind) {
    case Operation.Merge:
      return operationDetail.isSquash
        ? dispatcher.abortSquashMerge(repository)
        : dispatcher.abortMerge(repository)
    case Operation.CherryPick:
      return dispatcher.abortCherryPick(
        repository,
        operationDetail.sourceBranch
      )
    case Operation.Rebase:
    case Operation.Reorder:
    case Operation.Squash:
      return dispatcher.abortRebase(repository)
  }
}

export async function confirmAbortOperation(
  p: OperationControllerProps,
  onAbort: () => Promise<void> = () => abortOperation(p)
) {
  const { step, userHasResolvedConflicts } = p.state
  if (
    step.kind !== Step.ShowConflicts &&
    step.kind !== Step.ShowCopilotConflicts &&
    step.kind !== Step.ShowCopilotConflictsLoading
  ) {
    throw new Error('Invalid operation step')
  }
  const { conflictState } = step
  if (
    userHasResolvedConflicts ||
    getResolvedFiles(p.workingDirectory, conflictState.manualResolutions)
      .length > 0
  ) {
    await p.dispatcher.setMultiCommitOperationStep(p.repository, {
      kind: Step.ConfirmAbort,
      conflictState,
      returnToStepKind: step.kind,
    })
    return
  }
  await onAbort()
}

export function returnToConflictsOperation(p: OperationControllerProps) {
  const { step } = p.state
  if (step.kind !== Step.ConfirmAbort) {
    throw new Error('Invalid operation step')
  }
  return p.dispatcher.setMultiCommitOperationStep(p.repository, {
    kind:
      step.returnToStepKind ??
      (p.state.useCopilotConflictResolution
        ? Step.ShowCopilotConflicts
        : Step.ShowConflicts),
    conflictState: step.conflictState,
  })
}

export async function continueOperation(p: OperationControllerProps) {
  const { repository, dispatcher, workingDirectory, state, conflictState } = p
  const { operationDetail, originalBranchTip } = state
  if (conflictState === null) {
    endOperationFlow(p)
    return
  }
  switch (operationDetail.kind) {
    case Operation.Merge: {
      if (
        (state.step.kind !== Step.ShowConflicts &&
          state.step.kind !== Step.ShowCopilotConflicts) ||
        !isMergeConflictState(conflictState)
      ) {
        throw new Error('Invalid merge conflict state')
      }
      await dispatcher.finishConflictedMerge(
        repository,
        workingDirectory,
        {
          type: BannerType.SuccessfulMerge,
          ourBranch: conflictState.currentBranch,
          theirBranch: state.step.conflictState.theirBranch,
        },
        operationDetail.isSquash
      )
      await dispatcher.setCommitMessage(repository, DefaultCommitMessage)
      await dispatcher.changeRepositorySection(
        repository,
        RepositorySectionTab.Changes
      )
      endOperationFlow(p)
      dispatcher.incrementMetric('guidedConflictedMergeCompletionCount')
      return
    }
    case Operation.CherryPick:
      if (state.targetBranch === null) {
        throw new Error('Invalid cherry-pick target')
      }
      await dispatcher.switchMultiCommitOperationToShowProgress(repository)
      await dispatcher.continueCherryPick(
        repository,
        workingDirectory.files,
        {
          kind: 'cherryPick',
          targetBranchName: state.targetBranch.name,
          manualResolutions: conflictState.manualResolutions,
        },
        operationDetail.commits,
        operationDetail.sourceBranch
      )
      return
    case Operation.Rebase:
    case Operation.Squash:
    case Operation.Reorder: {
      const kind = operationDetail.kind
      if (
        originalBranchTip === null ||
        !isRebaseConflictState(conflictState) ||
        !instanceOfIBaseRebaseDetails(operationDetail)
      ) {
        throw new Error('Invalid rebase conflict state')
      }
      const { targetBranch, baseBranch } = conflictState
      const { commits, currentTip } = operationDetail
      await dispatcher.switchMultiCommitOperationToShowProgress(repository)
      const result = await dispatcher.continueRebase(
        kind,
        repository,
        workingDirectory,
        {
          kind: 'rebase',
          currentTip,
          targetBranch,
          baseBranch,
          originalBranchTip,
          baseBranchTip: currentTip,
          manualResolutions: conflictState.manualResolutions,
        }
      )
      await dispatcher.processMultiCommitOperationRebaseResult(
        kind,
        repository,
        result,
        commits.length + 1,
        targetBranch,
        kind === Operation.Rebase
          ? baseBranch || ''
          : `${kind.toLowerCase()} commit`
      )
      return
    }
  }
}

export function beginOperation(p: OperationControllerProps) {
  const {
    dispatcher,
    repository,
    state: { operationDetail: detail, targetBranch },
  } = p
  switch (detail.kind) {
    case Operation.Rebase:
      if (detail.sourceBranch === null || targetBranch === null) {
        throw new Error('Invalid rebase target')
      }
      return dispatcher.startRebase(
        repository,
        detail.sourceBranch,
        targetBranch,
        detail.commits,
        { continueWithForcePush: true }
      )
    case Operation.Squash:
      return dispatcher.squash(
        repository,
        detail.commits,
        detail.targetCommit,
        detail.lastRetainedCommitRef,
        detail.commitContext,
        true
      )
    case Operation.Reorder:
      return dispatcher.reorderCommits(
        repository,
        detail.commits,
        detail.beforeCommit,
        detail.lastRetainedCommitRef,
        true
      )
    case Operation.Merge:
    case Operation.CherryPick:
      throw new Error('Invalid operation warning')
  }
}

// No parameters/plural substitution: all native static prose comes from keys
// in references/native-ui-operation-labels.json. Dynamic content is technical
// original branch/path/author/message data, never raw Popup/Repository objects.
const k = (name: string) => `operation.${name}`
const readonly = (
  id: string,
  label: string,
  text: string
): NativeDialogField => ({
  id,
  labelKey: k(label),
  kind: 'readonly',
  text,
  readOnly: true,
})
const check = (
  id: string,
  label: string,
  value: boolean
): NativeDialogField => ({ id, labelKey: k(label), kind: 'checkbox', value })
const action = (
  id: string,
  label: string,
  destructive = false
): NativeDialogAction => ({
  id,
  labelKey: k(label),
  primary: id === 'submit' || id === 'continue',
  destructive,
})
const confirmField = (value: boolean) => check('confirm', 'confirmAgain', value)

class OperationOwnerUnavailable extends Error {}
const ownerRepositoryStates = new WeakMap<Popup, () => IRepositoryState>()

function repositoryState(
  popup: Extract<Popup, { repository: Repository }>,
  state: IAppState
): IRepositoryState {
  const owned = ownerRepositoryStates.get(popup)
  if (owned !== undefined) return owned()
  const selected = state.selectedState
  if (
    selected?.type !== SelectionType.Repository ||
    selected.repository.id !== popup.repository.id ||
    selected.repository.path !== popup.repository.path
  ) {
    throw new OperationOwnerUnavailable('Operation repository is unavailable')
  }
  return selected.state
}

function operationProps(
  runtime: AppRuntime,
  popup: Of<PopupType.MultiCommitOperation>
): OperationControllerProps {
  const rs = runtime.repositoryStateManager.get(popup.repository)
  if (rs.multiCommitOperationState === null) {
    throw new Error('No current operation')
  }
  return {
    dispatcher: runtime.dispatcher,
    repository: popup.repository,
    state: rs.multiCommitOperationState,
    workingDirectory: rs.changesState.workingDirectory,
    conflictState: rs.changesState.conflictState,
  }
}

/** Ephemeral modal draft only; no independent repository/operation state. Weak
 * ownership retains it while an original error/unknown-author popup covers it. */
const drafts = new WeakMap<Popup, ICommitMessage>()
const coAuthorVisibility = new WeakMap<Popup, boolean>()
const localAuthors = new WeakMap<Popup, ReadonlyArray<Author>>()
const authorInputs = new WeakMap<Popup, AuthorInputController>()
const ruleChecks = new WeakMap<
  Popup,
  {
    readonly input: ReadonlyArray<unknown>
    status: CommitRuleStatus | null
    pending: boolean
  }
>()
const busy = new WeakSet<Popup>()
const errors = new WeakMap<Popup, string>()
const observers = new WeakMap<Popup, () => void>()

/** Match DialogStack lifetime: covering a popup keeps its draft and observer. */
export function mountOperationDialog(
  runtime: AppRuntime,
  popup: Popup,
  onChange: () => void
): Disposable {
  if (!types.includes(popup.type) || observers.has(popup)) {
    throw new Error('Invalid operation dialog lifecycle')
  }
  observers.set(popup, onChange)
  if ('repository' in popup && popup.repository instanceof Repository) {
    const repository = popup.repository
    // Popup ownership is independent of the selected repository. Deep links
    // can select another project while this original continuation is alive.
    ownerRepositoryStates.set(popup, () =>
      runtime.repositoryStateManager.get(repository)
    )
  }
  if (popup.type === PopupType.CommitMessage) {
    const input = authorsFor(runtime, popup)
    if (popup.repository.gitHubRepository !== null) {
      void input
        .resume(popup.repository)
        .catch(error => runtime.dispatcher.postError(error))
    }
  }
  return new Disposable(() => {
    authorInputs.get(popup)?.dispose()
    authorInputs.delete(popup)
    localAuthors.delete(popup)
    ruleChecks.delete(popup)
    observers.delete(popup)
    ownerRepositoryStates.delete(popup)
    drafts.delete(popup)
    coAuthorVisibility.delete(popup)
    errors.delete(popup)
  })
}

function changed(popup: Popup) {
  observers.get(popup)?.()
}

function messageFor(popup: Of<PopupType.CommitMessage>) {
  return drafts.get(popup) ?? popup.commitMessage ?? DefaultCommitMessage
}

function coAuthorsFor(popup: Of<PopupType.CommitMessage>) {
  return localAuthors.get(popup) ?? popup.coAuthors
}

function authorsFor(runtime: AppRuntime, popup: Of<PopupType.CommitMessage>) {
  let input = authorInputs.get(popup)
  if (input !== undefined) {
    return input
  }
  input = new AuthorInputController(
    runtime.gitHubUserStore,
    () => {
      const app = runtime.appStore.getState(),
        selected = app.selectedState
      return app.allPopups.includes(popup) &&
        selected?.type === SelectionType.Repository &&
        selected.repository.hash === popup.repository.hash
        ? {
            repository: selected.repository,
            account:
              getAccountForRepository(app.accounts, selected.repository) ??
              undefined,
          }
        : null
    },
    () => coAuthorsFor(popup),
    authors => localAuthors.set(popup, authors),
    () => changed(popup),
    () => busy.has(popup)
  )
  authorInputs.set(popup, input)
  return input
}

function dialogRules(popup: Of<PopupType.CommitMessage>, app: IAppState) {
  const state = repositoryState(popup, app)
  const account = getAccountForRepository(app.accounts, popup.repository)
  if (!useRepoRulesLogic(account, popup.repository)) {
    return null
  }
  const message = messageFor(popup),
    authors = coAuthorsFor(popup)
  const input = [
    account,
    state.changesState.currentRepoRulesInfo,
    state.commitAuthor,
    state.branchesState.tip,
    state.aheadBehind,
    message,
    authors,
  ]
  const previous = ruleChecks.get(popup)
  if (previous?.input.every((value, index) => value === input[index])) {
    return previous
  }
  const check = {
    input,
    status: null as CommitRuleStatus | null,
    pending: true,
  }
  ruleChecks.set(popup, check)
  void evaluateCommitRules(
    popup.repository,
    state,
    account,
    commitDialogContext(message, authors, true, false)
  )
    .then(status => {
      if (ruleChecks.get(popup) !== check) {
        return
      }
      check.status = status
      check.pending = false
      changed(popup)
    })
    .catch(error => {
      if (ruleChecks.get(popup) !== check) {
        return
      }
      errors.set(popup, error instanceof Error ? error.message : String(error))
      // Formatting failed, so no rule result can authorize a submission.
      changed(popup)
    })
  return check
}

type OperationConflictPresentation = Pick<
  OperationControllerProps,
  'state' | 'workingDirectory' | 'conflictState'
>

function conflictFields(p: OperationConflictPresentation): NativeDialogField[] {
  const { step } = p.state
  if (!('conflictState' in step)) {
    return []
  }
  const fields: NativeDialogField[] = []
  const copilot = step.kind === Step.ShowCopilotConflicts
  for (const file of p.workingDirectory.files.filter(f =>
    isConflictedFile(f.status)
  )) {
    const skipped = p.state.copilotSkippedFiles?.find(f => f.path === file.path)
    const suggestion = p.state.copilotResolutions?.find(
      f => f.path === file.path
    )
    fields.push(readonly(`file:${file.id}`, 'file', file.path))
    if (suggestion) {
      fields.push(
        readonly(`reason:${file.id}`, 'reason', suggestion.reasoning ?? '')
      )
    }
    if (skipped) {
      fields.push(readonly(`skipped:${file.id}`, 'skipped', skipped.reason))
    }
    const manual = step.conflictState.manualResolutions.get(file.path)
    // Separate translated checkboxes avoid hard-coded static select labels.
    fields.push(
      check(`ours:${file.id}`, 'ours', manual === ManualConflictResolution.ours)
    )
    fields.push(
      check(
        `theirs:${file.id}`,
        'theirs',
        manual === ManualConflictResolution.theirs
      )
    )
    if (copilot && !skipped && suggestion) {
      fields.push(
        check(`copilot:${file.id}`, 'copilotSuggestion', manual === undefined)
      )
    }
  }
  return fields
}

function hasUnresolvedSkipped(p: OperationConflictPresentation) {
  return (p.state.copilotSkippedFiles ?? []).some(skipped => {
    const file = p.workingDirectory.files.find(f => f.path === skipped.path)
    if (!file || !isConflictedFile(file.status)) {
      return false
    }
    if (
      isConflictWithMarkers(file.status) &&
      file.status.conflictMarkerCount === 0
    ) {
      return false
    }
    const manual = p.conflictState?.manualResolutions.get(file.path)
    return (
      manual !== ManualConflictResolution.ours &&
      manual !== ManualConflictResolution.theirs
    )
  })
}

function multiPresentation(
  popup: Of<PopupType.MultiCommitOperation>,
  state: IAppState
): Pick<NativeDialogPresentation, 'fields' | 'actions' | 'descriptionKey'> {
  const rs = repositoryState(popup, state)
  const op = rs.multiCommitOperationState
  if (op === null) {
    throw new OperationOwnerUnavailable('No current operation')
  }
  const p: OperationConflictPresentation = {
    state: op,
    workingDirectory: rs.changesState.workingDirectory,
    conflictState: rs.changesState.conflictState,
  }
  // The phase-specific description explains the next action; internal step
  // enum names are implementation state, not another user-facing parameter.
  const fields: NativeDialogField[] = [
    readonly('operation', 'operation', op.operationDetail.kind),
  ]
  // Branch operations have no commit range until the original preview runs.
  // An empty read-only editor suggests a missing required input to the user.
  if ('commits' in op.operationDetail && op.operationDetail.commits.length > 0) {
    fields.push({
      id: 'commits',
      labelKey: k('commits'),
      kind: 'list',
      text: op.operationDetail.commits
        .map(c => `${c.sha} ${c.summary}`)
        .join('\n'),
      readOnly: true,
    })
  }
  if (op.targetBranch) {
    fields.push(readonly('target', 'targetBranch', op.targetBranch.name))
  }
  switch (op.step.kind) {
    case Step.ChooseBranch: {
      const currentBranch = op.step.currentBranch
      fields.push({
        id: 'branch',
        labelKey: k('branch'),
        kind: 'select',
        value:
          op.step.initialBranch?.name ??
          (op.step.defaultBranch !== op.step.currentBranch
            ? op.step.defaultBranch?.name ?? ''
            : ''),
        choices: op.step.allBranches.map(b => ({
          id: b.name,
          label: b.name,
          disabled: b.name === currentBranch.name,
        })),
        required: true,
      })
      if (op.operationDetail.kind === Operation.CherryPick) {
        fields.push({
          id: 'newBranchName',
          labelKey: k('newBranchName'),
          kind: 'text',
          value: '',
        })
        return {
          fields,
          actions: [
            action('submit', 'begin'),
            action('createBranch', 'newBranch'),
            action('cancel', 'cancel'),
          ],
          descriptionKey: k('chooseBranchDescription'),
        }
      }
      return {
        fields,
        actions: [
          action('submit', 'begin'),
          action('merge', 'merge'),
          action('squashMerge', 'squashMerge'),
          action('rebase', 'rebase'),
          action('cancel', 'cancel'),
        ],
        descriptionKey: k('chooseBranchDescription'),
      }
    }
    case Step.CreateBranch: {
      fields.push({
        id: 'name',
        labelKey: k('newBranchName'),
        kind: 'text',
        value: op.step.targetBranchName,
        required: true,
      })
      const branches = [
        op.step.upstreamDefaultBranch,
        op.step.defaultBranch,
        op.step.tip.kind === TipState.Valid ? op.step.tip.branch : null,
      ].filter((b): b is Branch => b !== null)
      fields.push({
        id: 'startPoint',
        labelKey: k('startPoint'),
        kind: 'select',
        value: branches[0]?.name ?? '',
        choices: [
          ...new Map(
            branches.map(b => [b.name, { id: b.name, label: b.name }])
          ).values(),
        ],
      })
      return {
        fields,
        actions: [
          action('submit', 'createAndCherryPick'),
          action('cancel', 'cancel'),
        ],
      }
    }
    case Step.WarnForcePush:
      fields.push(confirmField(state.askForConfirmationOnForcePush))
      return {
        fields,
        actions: [action('submit', 'begin'), action('cancel', 'cancel')],
        descriptionKey: k('forcePushDescription'),
      }
    case Step.ShowProgress:
      fields.push({
        id: 'progress',
        labelKey: k('progress'),
        kind: 'progress',
        value: op.progress?.value ?? 0,
        text: op.progress?.title ?? '',
      })
      return { fields, actions: [] }
    case Step.ConfirmAbort:
      return {
        fields,
        actions: [
          action('abort', 'abort', true),
          action('return', 'returnToConflicts'),
        ],
        descriptionKey: k('abortDescription'),
      }
    case Step.HideConflicts:
      return {
        fields,
        actions: [action('showConflicts', 'showConflicts')],
        descriptionKey: k('hiddenConflictsDescription'),
      }
    case Step.ShowCopilotConflictsLoading:
      fields.push(
        readonly(
          'reasoning',
          'progress',
          op.copilotResolutionProgress?.reasoningSnippet ?? ''
        )
      )
      return {
        fields,
        actions: [
          action('stop', 'stop'),
          action('abort', 'abort', true),
          action('cancel', 'hideConflicts'),
        ],
        descriptionKey: k('copilotLoadingDescription'),
      }
    case Step.ShowConflicts:
    case Step.ShowCopilotConflicts: {
      fields.push(...conflictFields(p))
      const canContinue =
        op.step.kind === Step.ShowCopilotConflicts
          ? !hasUnresolvedSkipped(p)
          : getConflictedFiles(
              p.workingDirectory,
              op.step.conflictState.manualResolutions
            ).length === 0
      const actions: NativeDialogAction[] = [
        { ...action('continue', 'continue'), disabled: !canContinue },
        action('resolve', 'applyChoices'),
        action('openFile', 'openFile'),
        action('openShell', 'openShell'),
        action('abort', 'abort', true),
        action('cancel', 'hideConflicts'),
      ]
      fields.push({
        id: 'file',
        labelKey: k('file'),
        kind: 'select',
        choices: p.workingDirectory.files
          .filter(f => isConflictedFile(f.status))
          .map(f => ({ id: f.id, label: f.path })),
      })
      if (op.step.kind === Step.ShowCopilotConflicts) {
        actions.push(action('manual', 'manual'))
      } else if (
        enableCopilotConflictResolution() &&
        getConflictedFiles(
          p.workingDirectory,
          op.step.conflictState.manualResolutions
        ).length > 0 &&
        getAccountForCopilotConflictResolution(state.accounts, popup.repository)
      ) {
        actions.push(action('copilot', 'resolveCopilot'))
      }
      return { fields, actions, descriptionKey: k('conflictsDescription') }
    }
  }
}

function operationActionFields(
  popup: Popup,
  actionID: string,
  fields: ReadonlyArray<NativeDialogField>
): ReadonlyArray<string> {
  if (['cancel', 'close', 'showFiles'].includes(actionID)) {
    return []
  }
  if (popup.type === PopupType.CommitMessage) {
    if (actionID === 'searchAuthors') {
      return ['authorQuery']
    }
    if (actionID === 'addAuthor') {
      return ['authorHit']
    }
    if (actionID === 'removeAuthor') {
      return ['author']
    }
    return [
      'summary',
      'description',
      'showCoAuthors',
      'skipCommitHooks',
      'signOffCommits',
    ]
  }
  if (popup.type === PopupType.MultiCommitOperation) {
    switch (actionID) {
      case 'createBranch':
        return ['newBranchName']
      case 'merge':
      case 'squashMerge':
      case 'rebase':
        return ['branch']
      case 'openFile':
        return ['file']
      case 'resolve':
        return fields.filter(f => f.kind === 'checkbox').map(f => f.id)
      case 'submit':
        return fields
          .filter(
            f =>
              !f.readOnly &&
              !['readonly', 'list', 'progress'].includes(f.kind) &&
              f.id !== 'newBranchName'
          )
          .map(f => f.id)
      default:
        return []
    }
  }
  return fields
    .filter(
      f => !f.readOnly && !['readonly', 'list', 'progress'].includes(f.kind)
    )
    .map(f => f.id)
}

function project(popup: Popup, state: IAppState): NativeDialogPresentation {
  try {
    return projectOperation(popup, state)
  } catch (error) {
    if (!(error instanceof OperationOwnerUnavailable)) throw error
    // Do not let a missing owner's form suppress every other kernel snapshot.
    // No operation is fabricated; the existing original cancel stays available.
    return {
      popupID: popup.id ?? null,
      type: popup.type,
      supported: true,
      layout: 'form',
      titleKey: k(popup.type),
      descriptionKey: k('repositoryUnavailable'),
      fields: [],
      actions: [{ ...action('cancel', 'cancel'), fields: [] }],
    }
  }
}

function projectOperation(
  popup: Popup,
  state: IAppState
): NativeDialogPresentation {
  if (!types.includes(popup.type)) {
    throw new Error('Wrong operation dialog type')
  }
  const base: NativeDialogPresentation = {
    popupID: popup.id ?? null,
    type: popup.type,
    supported: true,
    layout: 'form',
    titleKey: k(popup.type),
    fields: [],
    actions: [action('submit', 'continue'), action('cancel', 'cancel')],
    error: errors.has(popup)
      ? errors.get(popup) === 'A commit summary is required'
        ? k('summaryRequired')
        : displayDialogDiagnostic(errors.get(popup)!, state.accounts)
      : null,
  }
  let fields: NativeDialogField[] = []
  let actions = base.actions
  let descriptionKey: string | undefined
  switch (popup.type) {
    case PopupType.ConfirmDiscardChanges:
      fields = [
        readonly('files', 'files', popup.files.map(f => f.path).join('\n')),
      ]
      if (popup.showDiscardChangesSetting !== false) {
        fields.push(confirmField(state.askForConfirmationOnDiscardChanges))
      }
      actions = [
        action(
          'submit',
          popup.discardingAllChanges ? 'discardAll' : 'discard',
          true
        ),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('discardDescription')
      break
    case PopupType.ConfirmDiscardSelection:
      fields = [
        readonly('file', 'selectedChanges', popup.file.path),
        confirmField(true),
      ]
      actions = [action('submit', 'discard', true), action('cancel', 'cancel')]
      descriptionKey = k('discardSelectionDescription')
      break
    case PopupType.OversizedFiles:
      fields = [readonly('files', 'files', popup.oversizedFiles.join('\n'))]
      actions = [
        action('submit', 'commitAnyway', true),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('oversizedDescription')
      break
    case PopupType.CommitConflictsWarning:
      fields = [
        readonly('files', 'files', popup.files.map(f => f.path).join('\n')),
      ]
      actions = [
        action('submit', 'commitAnyway', true),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('commitConflictsDescription')
      break
    case PopupType.StashAndSwitchBranch: {
      const rs = repositoryState(popup, state)
      if (rs.branchesState.tip.kind !== TipState.Valid) {
        throw new OperationOwnerUnavailable('Invalid current branch')
      }
      fields = [
        readonly('current', 'currentBranch', rs.branchesState.tip.branch.name),
        readonly('target', 'targetBranch', popup.branchToCheckout.name),
        check('move', 'moveChanges', false),
      ]
      if (rs.changesState.stashEntry !== null) {
        fields.push(
          readonly(
            'stash',
            'existingStash',
            rs.changesState.stashEntry.stashSha
          )
        )
      }
      actions = [action('submit', 'switchBranch'), action('cancel', 'cancel')]
      descriptionKey = k('switchBranchDescription')
      break
    }
    case PopupType.ConfirmOverwriteStash:
      fields = popup.branchToCheckout
        ? [readonly('target', 'targetBranch', popup.branchToCheckout.name)]
        : []
      actions = [
        action('submit', 'overwrite', true),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('overwriteDescription')
      break
    case PopupType.ConfirmDiscardStash:
      fields = [
        readonly('stash', 'stash', popup.stash.stashSha),
        confirmField(state.askForConfirmationOnDiscardStash),
      ]
      actions = [action('submit', 'discard', true), action('cancel', 'cancel')]
      descriptionKey = k('discardStashDescription')
      break
    case PopupType.ConfirmCheckoutCommit:
      // Keep App.tsx's original setting basis, including the upstream discard-stash preference.
      fields = [
        readonly(
          'commit',
          'commit',
          `${popup.commit.sha} ${popup.commit.summary}`
        ),
        confirmField(state.askForConfirmationOnDiscardStash),
      ]
      descriptionKey = k('checkoutDescription')
      break
    case PopupType.LocalChangesOverwritten: {
      fields = [readonly('files', 'files', popup.files.join('\n'))]
      const selected = state.selectedState
      const stash =
        selected?.type === SelectionType.Repository
          ? selected.state.changesState.stashEntry
          : null
      actions =
        stash !== null || popup.retryAction.type === RetryActionType.PopStash
          ? [action('close', 'close')]
          : [action('submit', 'stashContinue'), action('cancel', 'close')]
      descriptionKey = k('localChangesDescription')
      break
    }
    case PopupType.WarnLocalChangesBeforeUndo:
      fields = [
        readonly(
          'commit',
          'commit',
          `${popup.commit.sha} ${popup.commit.summary}`
        ),
      ]
      if (!popup.commit.isMergeCommit) {
        fields.push(confirmField(state.askForConfirmationOnUndoCommit))
      }
      actions = [action('submit', 'continue', true), action('cancel', 'cancel')]
      descriptionKey = k(
        popup.commit.isMergeCommit
          ? popup.isWorkingDirectoryClean
            ? 'mergeUndoDescription'
            : 'dirtyMergeUndoDescription'
          : 'undoDescription'
      )
      break
    case PopupType.WarningBeforeReset:
      fields = [
        readonly(
          'commit',
          'commit',
          `${popup.commit.sha} ${popup.commit.summary}`
        ),
      ]
      actions = [action('submit', 'continue', true), action('cancel', 'cancel')]
      descriptionKey = k('resetDescription')
      break
    case PopupType.WarnForcePush:
      fields = [
        readonly('operation', 'operation', popup.operation),
        confirmField(state.askForConfirmationOnForcePush),
      ]
      actions = [action('submit', 'begin'), action('cancel', 'cancel')]
      descriptionKey = k('forcePushDescription')
      break
    case PopupType.DiscardChangesRetry:
      fields = [confirmField(true)]
      actions = [
        action('submit', 'discardPermanently', true),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('discardRetryDescription')
      break
    case PopupType.UnknownAuthors:
      fields = [
        readonly(
          'authors',
          'unknownAuthors',
          popup.authors.map(a => a.username).join('\n')
        ),
      ]
      actions = [
        action('submit', 'commitAnyway', true),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('unknownAuthorsDescription')
      break
    case PopupType.ConfirmCommitFilteredChanges:
      fields = [confirmField(true)]
      actions = [
        action('submit', 'commitAnyway', true),
        action('showFiles', 'showFiles'),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('filteredDescription')
      break
    case PopupType.CommitMessage: {
      const message = messageFor(popup)
      const rs = repositoryState(popup, state)
      fields = [
        readonly('context', 'context', popup.dialogTitle),
        {
          id: 'summary',
          labelKey: k('summary'),
          kind: 'text',
          value: message.summary,
          required: !popup.prepopulateCommitSummary,
          changeAction: 'change',
        },
        {
          id: 'description',
          labelKey: k('description'),
          kind: 'multiline',
          value: message.description ?? '',
          changeAction: 'change',
        },
        {
          ...check(
            'showCoAuthors',
            'coAuthors',
            coAuthorVisibility.get(popup) ?? popup.showCoAuthoredBy
          ),
          changeAction: 'change',
          disabled: popup.repository.gitHubRepository === null,
        },
        readonly(
          'authors',
          'coAuthors',
          coAuthorsFor(popup)
            .map(a =>
              isKnownAuthor(a) ? `${a.name} <${a.email}>` : a.username
            )
            .join('\n')
        ),
        {
          ...check('skipCommitHooks', 'skipCommitHooks', rs.skipCommitHooks),
          changeAction: 'change',
        },
        {
          ...check('signOffCommits', 'signOffCommits', rs.signOffCommits),
          changeAction: 'change',
        },
      ]
      const rules = dialogRules(popup, state)
      const blocked =
        rules?.status !== null &&
        rules?.status !== undefined &&
        hasCommitRuleFailure(rules.status)
      if (rules !== null) {
        fields.push(
          readonly(
            'rules',
            rules.pending
              ? 'rulesPending'
              : blocked
              ? 'rulesBlocked'
              : 'rulesPassed',
            ''
          )
        )
      }
      if (
        state.showCommitLengthWarning &&
        (rules?.status?.message ?? 'pass') === 'pass' &&
        message.summary.length > 50
      ) {
        fields.push(readonly('summaryLength', 'summaryLength', ''))
      }
      const authorState = authorInputs.get(popup)?.getState()
      if (
        popup.repository.gitHubRepository !== null &&
        (coAuthorVisibility.get(popup) ?? popup.showCoAuthoredBy)
      ) {
        fields.push(
          {
            id: 'authorQuery',
            labelKey: k('searchAuthors'),
            kind: 'text',
            value: authorState?.authorQuery ?? '',
            changeAction: 'searchAuthors',
          },
          {
            id: 'authorHit',
            labelKey: k('authorHit'),
            kind: 'select',
            choices:
              authorState?.authorHits.map(hit => ({
                id: hit.id,
                label: `${hit.username} ${hit.name} ${hit.email}`.trim(),
              })) ?? [],
          },
          {
            id: 'author',
            labelKey: k('coAuthors'),
            kind: 'select',
            choices:
              authorState?.coAuthors.map(author => ({
                id: author.id,
                label:
                  author.kind === 'known'
                    ? `${author.name} <${author.email}>`
                    : author.username ?? '',
              })) ?? [],
          }
        )
        for (const author of authorState?.coAuthors ?? []) {
          if (author.kind === 'unknown') {
            fields.push(
              readonly(
                `authorStatus:${author.id}`,
                author.state === 'searching'
                  ? 'authorSearching'
                  : 'authorNotFound',
                author.username ?? ''
              )
            )
          }
        }
      }
      actions = [
        {
          ...action('submit', 'saveCommitMessage'),
          disabled: rules?.pending || blocked,
        },
        action('change', 'editMessage'),
        ...(fields.some(field => field.id === 'authorQuery')
          ? [
              action('searchAuthors', 'searchAuthors'),
              {
                ...action('addAuthor', 'addAuthor'),
                disabled: !authorState?.authorHits.length,
              },
              {
                ...action('removeAuthor', 'removeAuthor'),
                disabled: !authorState?.coAuthors.length,
              },
            ]
          : []),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('commitMessageDescription')
      break
    }
    case PopupType.MultiCommitOperation: {
      const multi = multiPresentation(popup, state)
      return {
        ...base,
        ...multi,
        fields: multi.fields.map(f => ({
          ...f,
          disabled: busy.has(popup) || f.disabled,
        })),
        actions: multi.actions.map(a => ({
          ...a,
          fields: operationActionFields(popup, a.id, multi.fields),
          disabled: busy.has(popup) || a.disabled,
        })),
      }
    }
    case PopupType.AssistedCommitDisclaimer:
      actions = [
        action('submit', 'understand', true),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('assistedDisclaimerDescription')
      break
    case PopupType.CopilotConflictResolutionDisclaimer:
      actions = [
        action('submit', 'understand', true),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('conflictDisclaimerDescription')
      break
    case PopupType.CopilotConflictResolutionAlwaysNudge:
      actions = [
        action('yes', 'yes'),
        action('no', 'no'),
        action('cancel', 'cancel'),
      ]
      descriptionKey = k('alwaysDescription')
      break
  }
  const isBusy = busy.has(popup)
  return {
    ...base,
    descriptionKey,
    fields: fields.map(f => ({ ...f, disabled: isBusy || f.disabled })),
    actions: actions.map(a => ({
      ...a,
      fields: operationActionFields(popup, a.id, fields),
      disabled: isBusy || a.disabled,
    })),
  }
}

function emptyValues(values: unknown) {
  return dialogValues(values ?? {}, [])
}
function booleanValues(values: unknown, id: string) {
  return dialogBoolean(dialogValues(values, [id])[id])
}
function close(runtime: AppRuntime, popup: Popup) {
  runtime.dispatcher.closePopupById(popup.id!)
}

async function dismissMulti(
  runtime: AppRuntime,
  popup: Of<PopupType.MultiCommitOperation>
) {
  const p = operationProps(runtime, popup)
  const { step } = p.state
  switch (step.kind) {
    case Step.ShowProgress:
      throw new Error('Operation cannot be dismissed during progress')
    case Step.ConfirmAbort:
      await returnToConflictsOperation(p)
      return
    case Step.HideConflicts:
      close(runtime, popup)
      return
    case Step.ShowConflicts:
    case Step.ShowCopilotConflicts:
    case Step.ShowCopilotConflictsLoading: {
      if (p.state.operationDetail.kind === Operation.Merge) {
        if (
          p.conflictState === null ||
          !isMergeConflictState(p.conflictState)
        ) {
          endOperationFlow(p)
          return
        }
        p.dispatcher.incrementMetric('mergeConflictsDialogDismissalCount')
        if (
          getConflictedFiles(
            p.workingDirectory,
            p.conflictState.manualResolutions
          ).length > 0
        ) {
          p.dispatcher.incrementMetric(
            'anyConflictsLeftOnMergeConflictsDialogDismissalCount'
          )
        }
      }
      if (
        getResolvedFiles(
          p.workingDirectory,
          step.conflictState.manualResolutions
        ).length > 0 &&
        !p.state.userHasResolvedConflicts
      ) {
        p.dispatcher.setConflictsResolved(p.repository)
      }
      // The native banner gets original technical branch data, not a serialized React node.
      const prefix = {
        [Operation.Merge]: 'merge into',
        [Operation.Rebase]: 'rebasing',
        [Operation.CherryPick]: 'cherry-picking onto',
        [Operation.Squash]: 'squashing commits on',
        [Operation.Reorder]: 'reordering commits on',
      }[p.state.operationDetail.kind]
      const description = `${prefix} ${p.state.targetBranch?.name ?? ''}`
      if (step.kind === Step.ShowConflicts) {
        p.dispatcher.setMultiCommitOperationStep(p.repository, {
          kind: Step.HideConflicts,
          conflictState: step.conflictState,
        })
        p.dispatcher.closePopup(PopupType.MultiCommitOperation)
        await p.dispatcher.onConflictsFoundBanner(
          p.repository,
          description,
          step.conflictState
        )
      } else {
        p.dispatcher.closePopup(PopupType.MultiCommitOperation)
        p.dispatcher.setBanner({
          type: BannerType.ConflictsFound,
          operationDescription: description,
          onOpenConflictsDialog: () => {
            p.dispatcher.showPopup({
              type: PopupType.MultiCommitOperation,
              repository: p.repository,
            })
          },
        })
      }
      return
    }
    case Step.ChooseBranch:
    case Step.CreateBranch:
    case Step.WarnForcePush:
      endOperationFlow(p)
      return
  }
}

async function cancel(runtime: AppRuntime, popup: Popup) {
  if (!types.includes(popup.type)) {
    throw new Error('Wrong operation dialog type')
  }
  if (busy.has(popup)) {
    throw new Error('Operation dialog is busy')
  }
  if (popup.type === PopupType.MultiCommitOperation) {
    await dismissMulti(runtime, popup)
    return
  }
  if (popup.type === PopupType.LocalChangesOverwritten) {
    dismissLocalChangesOperation(runtime.dispatcher, popup, () =>
      close(runtime, popup)
    )
    return
  }
  authorInputs.get(popup)?.dispose()
  authorInputs.delete(popup)
  localAuthors.delete(popup)
  ruleChecks.delete(popup)
  drafts.delete(popup)
  coAuthorVisibility.delete(popup)
  close(runtime, popup)
}

async function replyMulti(
  runtime: AppRuntime,
  popup: Of<PopupType.MultiCommitOperation>,
  actionID: string,
  values: unknown
) {
  const p = operationProps(runtime, popup)
  const { step, operationDetail } = p.state
  switch (step.kind) {
    case Step.ChooseBranch: {
      if (
        actionID === 'createBranch' &&
        operationDetail.kind === Operation.CherryPick
      ) {
        const name = dialogString(
          dialogValues(values, ['newBranchName']).newBranchName
        )
        await p.dispatcher.setCherryPickCreateBranchFlowStep(
          p.repository,
          name,
          operationDetail.commits,
          operationDetail.sourceBranch
        )
        return
      }
      const name = dialogString(dialogValues(values, ['branch']).branch)
      const branch = step.allBranches.find(
        b => b.name === name && b.name !== step.currentBranch.name
      )
      if (!branch) {
        throw new Error('Invalid current branch choice')
      }
      if (actionID !== 'submit') {
        if (actionID === 'merge') {
          await p.dispatcher.startMergeBranchOperation(
            p.repository,
            false,
            branch
          )
          return
        }
        if (actionID === 'squashMerge') {
          await p.dispatcher.startMergeBranchOperation(
            p.repository,
            true,
            branch
          )
          return
        }
        if (actionID === 'rebase') {
          await p.dispatcher.showRebaseDialog(p.repository, branch)
          return
        }
        throw new Error('Invalid operation choice')
      }
      if (operationDetail.kind === Operation.CherryPick) {
        p.dispatcher.setMultiCommitOperationTargetBranch(p.repository, branch)
        p.dispatcher.setCherryPickBranchCreated(p.repository, false)
        await p.dispatcher.cherryPick(
          p.repository,
          branch,
          operationDetail.commits,
          operationDetail.sourceBranch
        )
        return
      }
      if (operationDetail.kind === Operation.Rebase) {
        let preview: import('../models/rebase').RebasePreview | null = null
        await updateRebasePreview(
          branch,
          step.currentBranch,
          p.repository,
          value => {
            preview = value
          }
        )
        const result = preview as
          | import('../models/rebase').RebasePreview
          | null
        if (
          !result ||
          result.kind !== ComputedAction.Clean ||
          result.commitsBehind.length === 0
        ) {
          throw new Error('Cannot start rebase for current selection')
        }
        await p.dispatcher.startRebase(
          p.repository,
          branch,
          step.currentBranch,
          result.commitsAhead
        )
        return
      }
      if (operationDetail.kind !== Operation.Merge) {
        throw new Error('Invalid choose-branch operation')
      }
      const status = await determineMergeability(
        p.repository,
        step.currentBranch,
        branch
      ).catch(e => {
        log.error('Failed determining mergeability', e)
        return { kind: ComputedAction.Clean } as const
      })
      const aheadBehind = await getAheadBehind(
        p.repository,
        revSymmetricDifference('', branch.name)
      )
      if (
        status.kind === ComputedAction.Invalid ||
        (status.kind !== ComputedAction.Conflicts &&
          (aheadBehind?.behind ?? 0) === 0)
      ) {
        throw new Error('Cannot start merge for current selection')
      }
      const merging = p.dispatcher.mergeBranch(
        p.repository,
        branch,
        status,
        operationDetail.isSquash
      )
      p.dispatcher.closePopup(PopupType.MultiCommitOperation)
      await merging
      return
    }
    case Step.CreateBranch: {
      if (operationDetail.kind !== Operation.CherryPick) {
        throw new Error('Invalid create-branch operation')
      }
      const v = dialogValues(values, ['name', 'startPoint'])
      const name = sanitizedRefName(dialogString(v.name))
      if (!name) {
        throw new Error('Invalid new branch name')
      }
      const start = dialogString(v.startPoint)
      const current = step.tip.kind === TipState.Valid ? step.tip.branch : null
      const branches = [
        step.upstreamDefaultBranch,
        step.defaultBranch,
        current,
      ].filter((b): b is Branch => b !== null)
      const selected = branches.find(b => b.name === start)
      if (start !== '' && !selected) {
        throw new Error('Invalid branch start point')
      }
      if (start === '' && branches.length > 0) {
        throw new Error('Invalid branch start point')
      }
      await p.dispatcher.startCherryPickWithBranchName(
        p.repository,
        name,
        selected === current ? null : selected?.name ?? null,
        selected !== null && selected === step.upstreamDefaultBranch,
        operationDetail.commits,
        operationDetail.sourceBranch
      )
      return
    }
    case Step.WarnForcePush:
      p.dispatcher.setConfirmForcePushSetting(booleanValues(values, 'confirm'))
      await beginOperation(p)
      return
    case Step.ConfirmAbort:
      emptyValues(values)
      if (actionID === 'return') {
        await returnToConflictsOperation(p)
      } else {
        await abortOperation(p)
      }
      return
    case Step.HideConflicts:
      emptyValues(values)
      await p.dispatcher.setMultiCommitOperationStep(p.repository, {
        kind: Step.ShowConflicts,
        conflictState: step.conflictState,
      })
      return
    case Step.ShowProgress:
      throw new Error('Operation has no progress actions')
    case Step.ShowCopilotConflictsLoading:
      emptyValues(values)
      if (actionID === 'stop') {
        p.dispatcher.abortCopilotConflictResolution(p.repository)
        p.dispatcher.setMultiCommitOperationStepWithCopilotResolution(
          p.repository,
          { kind: Step.ShowConflicts, conflictState: step.conflictState },
          false
        )
      } else {
        await confirmAbortOperation(p)
      }
      return
    case Step.ShowConflicts:
    case Step.ShowCopilotConflicts:
      if (actionID === 'resolve') {
        const fields = conflictFields(p).filter(f => f.kind === 'checkbox')
        const v = dialogValues(
          values,
          fields.map(f => f.id)
        )
        // Validate every choice before invoking the first dispatcher update.
        const updates = p.workingDirectory.files
          .filter(f => isConflictedFile(f.status))
          .map(file => {
            const choices = fields.filter(f => f.id.endsWith(`:${file.id}`))
            const enabled = choices.filter(f => dialogBoolean(v[f.id]))
            if (enabled.length > 1) {
              throw new Error('Conflicting resolution choices')
            }
            return { file, choice: enabled[0]?.id.split(':')[0] }
          })
        for (const { file, choice } of updates) {
          p.dispatcher.updateManualConflictResolution(
            p.repository,
            file.path,
            choice === 'ours'
              ? ManualConflictResolution.ours
              : choice === 'theirs'
              ? ManualConflictResolution.theirs
              : null
          )
        }
        return
      }
      if (actionID === 'openFile') {
        const fileID = dialogString(dialogValues(values, ['file']).file)
        const file = p.workingDirectory.files.find(
          f => f.id === fileID && isConflictedFile(f.status)
        )
        if (!file) {
          throw new Error('Invalid conflict file')
        }
        await p.dispatcher.openInExternalEditor(
          join(p.repository.path, file.path)
        )
        return
      }
      emptyValues(values)
      if (actionID === 'openShell') {
        await p.dispatcher.openShell(p.repository.path)
        return
      }
      if (actionID === 'manual') {
        p.dispatcher.setMultiCommitOperationStepWithCopilotResolution(
          p.repository,
          { kind: Step.ShowConflicts, conflictState: step.conflictState },
          false
        )
        return
      }
      if (actionID === 'copilot') {
        await p.dispatcher.attemptCopilotConflictResolution(p.repository)
        return
      }
      if (actionID === 'abort') {
        await confirmAbortOperation(p)
        return
      }
      if (step.kind === Step.ShowCopilotConflicts) {
        if (hasUnresolvedSkipped(p)) {
          throw new Error('Skipped conflicts are unresolved')
        }
        await p.dispatcher.applyCopilotConflictResolutions(p.repository)
      } else if (
        getConflictedFiles(
          p.workingDirectory,
          step.conflictState.manualResolutions
        ).length > 0
      ) {
        throw new Error('Conflicts are unresolved')
      }
      await continueOperation(p)
      return
  }
}

async function reply(
  runtime: AppRuntime,
  popup: Popup,
  actionID: string,
  values: unknown
) {
  const presentation = project(popup, runtime.appStore.getState())
  const available = presentation.actions.find(
    a => a.id === actionID && !a.disabled
  )
  if (!available) {
    throw new Error('Invalid operation dialog action')
  }
  if (actionID === 'cancel') {
    emptyValues(values)
    await cancel(runtime, popup)
    return
  }
  if (popup.type === PopupType.MultiCommitOperation) {
    // State-driven dispatcher transitions, not a blanket cancel/retry policy.
    busy.add(popup)
    changed(popup)
    try {
      await replyMulti(runtime, popup, actionID, values)
    } finally {
      busy.delete(popup)
      changed(popup)
    }
    return
  }
  if (
    popup.type === PopupType.CommitMessage &&
    ['change', 'searchAuthors', 'addAuthor', 'removeAuthor'].includes(actionID)
  ) {
    const v = dialogValues(values, available.fields ?? [])
    if (actionID === 'change') {
      // The native changeAction sends a partial typed draft, never a commit.
      for (const [key, value] of Object.entries(v)) {
        if (['summary', 'description'].includes(key)) {
          dialogString(value)
        } else {
          dialogBoolean(value)
        }
      }
      const old = messageFor(popup)
      const message = {
        ...old,
        summary:
          v.summary === undefined ? old.summary : dialogString(v.summary),
        description:
          v.description === undefined
            ? old.description
            : dialogString(v.description),
      }
      if (
        message.summary !== old.summary ||
        message.description !== old.description
      ) {
        message.generatedByCopilot = false
        message.timestamp = Date.now()
      }
      drafts.set(popup, message)
      if (v.showCoAuthors !== undefined) {
        coAuthorVisibility.set(popup, dialogBoolean(v.showCoAuthors))
      }
      runtime.dispatcher.updateCommitOptions(popup.repository, {
        ...(v.skipCommitHooks === undefined
          ? {}
          : { skipCommitHooks: dialogBoolean(v.skipCommitHooks) }),
        ...(v.signOffCommits === undefined
          ? {}
          : { signOffCommits: dialogBoolean(v.signOffCommits) }),
      })
    } else {
      const input = authorsFor(runtime, popup)
      if (actionID === 'searchAuthors') {
        await input.search(popup.repository, dialogString(v.authorQuery))
      }
      if (actionID === 'addAuthor') {
        await input.add(popup.repository, dialogString(v.authorHit))
      }
      if (actionID === 'removeAuthor') {
        input.remove(popup.repository, dialogString(v.author))
      }
    }
    errors.delete(popup)
    changed(popup)
    return
  }
  const dismiss = () => close(runtime, popup)
  const d = runtime.dispatcher
  // Validation completes before busy, draft or any original continuation changes.
  const editable = presentation.fields.filter(
    f => !f.readOnly && f.kind !== 'readonly' && f.kind !== 'list'
  )
  const v = dialogValues(values ?? {}, available.fields ?? [])
  for (const field of editable) {
    if (actionID === 'showFiles' || actionID === 'close') {
      break
    }
    if (!available.fields?.includes(field.id)) {
      continue
    }
    if (field.kind === 'checkbox') {
      dialogBoolean(v[field.id])
    } else {
      dialogString(v[field.id])
    }
  }
  busy.add(popup)
  errors.delete(popup)
  changed(popup)
  try {
    switch (popup.type) {
      case PopupType.ConfirmDiscardChanges:
        await discardOperation(
          d,
          popup,
          popup.showDiscardChangesSetting === false
            ? runtime.appStore.getState().askForConfirmationOnDiscardChanges
            : dialogBoolean(v.confirm),
          dismiss
        )
        break
      case PopupType.ConfirmDiscardSelection:
        await discardSelectionOperation(
          d,
          popup,
          dialogBoolean(v.confirm),
          dismiss
        )
        break
      case PopupType.OversizedFiles:
      case PopupType.CommitConflictsWarning:
        dismiss()
        await d.commitIncludedChanges(popup.repository, popup.context)
        if (popup.type === PopupType.CommitConflictsWarning) {
          d.clearBanner()
        }
        d.setCommitMessage(popup.repository, DefaultCommitMessage)
        break
      case PopupType.StashAndSwitchBranch:
        await switchBranchOperation(
          d,
          popup,
          runtime.repositoryStateManager.get(popup.repository).changesState
            .stashEntry !== null,
          dialogBoolean(v.move),
          dismiss
        )
        break
      case PopupType.ConfirmOverwriteStash:
        await overwriteStashOperation(d, popup)
        dismiss()
        break
      case PopupType.ConfirmDiscardStash:
        d.setConfirmDiscardStashSetting(dialogBoolean(v.confirm))
        await d.dropStash(popup.repository, popup.stash)
        dismiss()
        break
      case PopupType.ConfirmCheckoutCommit:
        d.setConfirmCheckoutCommitSetting(dialogBoolean(v.confirm))
        await d.checkoutCommit(popup.repository, popup.commit)
        dismiss()
        break
      case PopupType.LocalChangesOverwritten: {
        if (actionID === 'close') {
          dismiss()
          break
        }
        const selected = runtime.appStore.getState().selectedState
        await stashAndRetryOperation(
          d,
          popup,
          selected?.type === SelectionType.Repository &&
            selected.state.changesState.stashEntry !== null,
          dismiss
        )
        break
      }
      case PopupType.WarnLocalChangesBeforeUndo:
        await undoOperation(
          d,
          popup,
          popup.commit.isMergeCommit
            ? runtime.appStore.getState().askForConfirmationOnUndoCommit
            : dialogBoolean(v.confirm)
        )
        dismiss()
        break
      case PopupType.WarningBeforeReset:
        await d.resetToCommit(popup.repository, popup.commit, false)
        dismiss()
        break
      case PopupType.WarnForcePush:
        d.setConfirmForcePushSetting(dialogBoolean(v.confirm))
        popup.onBegin()
        dismiss()
        break
      case PopupType.DiscardChangesRetry:
        await discardRetryOperation(d, popup, dialogBoolean(v.confirm), dismiss)
        break
      case PopupType.UnknownAuthors:
        popup.onCommit()
        dismiss()
        break
      case PopupType.ConfirmCommitFilteredChanges:
        if (actionID === 'showFiles') {
          popup.showFilesToBeCommitted()
          dismiss()
          break
        }
        d.setConfirmCommitFilteredChanges(dialogBoolean(v.confirm))
        popup.onCommitAnyway()
        dismiss()
        break
      case PopupType.CommitMessage: {
        const old = messageFor(popup)
        const message = {
          ...old,
          summary: dialogString(v.summary),
          description: dialogString(v.description),
        }
        if (!popup.prepopulateCommitSummary && message.summary.length === 0) {
          throw new Error('A commit summary is required')
        }
        // Changes to text remove AI attribution just like the original input.
        if (
          message.summary !== old.summary ||
          message.description !== old.description
        ) {
          message.generatedByCopilot = false
          message.timestamp = Date.now()
        }
        drafts.set(popup, message)
        coAuthorVisibility.set(popup, dialogBoolean(v.showCoAuthors))
        d.updateCommitOptions(popup.repository, {
          skipCommitHooks: dialogBoolean(v.skipCommitHooks),
          signOffCommits: dialogBoolean(v.signOffCommits),
        })
        const originalAccount = getAccountForRepository(
          runtime.appStore.getState().accounts,
          popup.repository
        )
        const submit = async () => {
          const app = runtime.appStore.getState()
          const selected = app.selectedState
          if (
            !app.allPopups.includes(popup) ||
            selected?.type !== SelectionType.Repository ||
            selected.repository.hash !== popup.repository.hash ||
            getAccountForRepository(app.accounts, popup.repository) !==
              originalAccount
          ) {
            throw new Error(
              'This commit message belongs to a previous repository or account'
            )
          }
          const context = commitDialogContext(
            message,
            coAuthorsFor(popup),
            popup.repository.gitHubRepository !== null,
            false
          )
          busy.add(popup)
          changed(popup)
          const timer = startTimer('create commit', popup.repository)
          try {
            const rules = await evaluateCommitRules(
              popup.repository,
              selected.state,
              originalAccount,
              context
            )
            if (hasCommitRuleFailure(rules)) {
              throw new Error(
                "The commit does not meet this repository's rules"
              )
            }
            const latestApp = runtime.appStore.getState(),
              latest = latestApp.selectedState
            if (
              !latestApp.allPopups.includes(popup) ||
              latest?.type !== SelectionType.Repository ||
              latest.repository.hash !== popup.repository.hash ||
              getAccountForRepository(latestApp.accounts, popup.repository) !==
                originalAccount
            ) {
              throw new Error(
                'The commit message context changed; review it again'
              )
            }
            const created = await popup.onSubmitCommitMessage(context)
            if (created) {
              drafts.set(popup, DefaultCommitMessage)
            }
          } catch (error) {
            errors.set(
              popup,
              error instanceof Error ? error.message : String(error)
            )
            throw error
          } finally {
            timer.done()
            busy.delete(popup)
            changed(popup)
          }
        }
        const unknown = coAuthorsFor(popup).filter(a => !isKnownAuthor(a))
        if (unknown.length > 0) {
          d.showUnknownAuthorsCommitWarning(unknown, () => {
            void submit().catch(error => d.postError(error))
          })
        } else {
          await submit()
        }
        // The callback owns closing this popup (squash does so before starting).
        // false leaves the dialog draft intact; never clear the workbench draft.
        break
      }
      case PopupType.AssistedCommitDisclaimer:
        popup.onAccepted()
        dismiss()
        break
      case PopupType.CopilotConflictResolutionDisclaimer:
        d.updateCopilotConflictResolutionDisclaimerLastSeen()
        d.attemptCopilotConflictResolution(popup.repository)
        dismiss()
        break
      case PopupType.CopilotConflictResolutionAlwaysNudge:
        if (actionID === 'yes') {
          d.setAlwaysUseCopilotForConflictResolution(true)
        }
        d.closePopup(PopupType.CopilotConflictResolutionAlwaysNudge)
        d.attemptCopilotConflictResolution(popup.repository)
        break
    }
  } catch (error) {
    errors.set(popup, error instanceof Error ? error.message : String(error))
    throw error
  } finally {
    busy.delete(popup)
    changed(popup)
  }
}

const types: ReadonlyArray<PopupType> = [
  PopupType.ConfirmDiscardChanges,
  PopupType.ConfirmDiscardSelection,
  PopupType.OversizedFiles,
  PopupType.CommitConflictsWarning,
  PopupType.StashAndSwitchBranch,
  PopupType.ConfirmOverwriteStash,
  PopupType.ConfirmDiscardStash,
  PopupType.ConfirmCheckoutCommit,
  PopupType.LocalChangesOverwritten,
  PopupType.WarnLocalChangesBeforeUndo,
  PopupType.WarningBeforeReset,
  PopupType.WarnForcePush,
  PopupType.DiscardChangesRetry,
  PopupType.UnknownAuthors,
  PopupType.ConfirmCommitFilteredChanges,
  PopupType.CommitMessage,
  PopupType.MultiCommitOperation,
  PopupType.AssistedCommitDisclaimer,
  PopupType.CopilotConflictResolutionDisclaimer,
  PopupType.CopilotConflictResolutionAlwaysNudge,
]

export const operationDialogs: NativeDialogModule = {
  types,
  project,
  reply,
  cancel,
}

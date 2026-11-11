import { Dispatcher } from '../dispatcher'
import { Repository } from '../../models/repository'
import { Branch } from '../../models/branch'
import { MergeTreeResult } from '../../models/merge'
import { RebasePreview } from '../../models/rebase'
import { ComputedAction } from '../../models/computed-action'
import { MultiCommitOperationKind } from '../../models/multi-commit-operation'

/** The compare CTA's original eligibility and dispatch are shared by both
 * frontends; selecting rebase must use its real preview's commits, not a name-
 * based approximation or a second Git implementation. */
export function compareOperationDisabled(
  operation: MultiCommitOperationKind,
  preview: RebasePreview | null,
  mergeStatus: MergeTreeResult | null,
  commitsBehind: number
) {
  const count =
    operation === MultiCommitOperationKind.Rebase
      ? preview?.kind === ComputedAction.Clean
        ? preview.commitsAhead.length
        : 0
      : commitsBehind
  return (
    count <= 0 ||
    (operation === MultiCommitOperationKind.Rebase
      ? preview === null || preview.kind !== ComputedAction.Clean
      : mergeStatus !== null && mergeStatus.kind === ComputedAction.Invalid)
  )
}

export function dispatchCompareOperation(
  dispatcher: Dispatcher,
  repository: Repository,
  operation: MultiCommitOperationKind,
  currentBranch: Branch,
  comparisonBranch: Branch,
  mergeStatus: MergeTreeResult | null,
  preview: RebasePreview | null
) {
  if (operation === MultiCommitOperationKind.Rebase) {
    const commits =
      preview?.kind === ComputedAction.Clean ? preview.commitsAhead : []
    return dispatcher.startRebase(
      repository,
      comparisonBranch,
      currentBranch,
      commits
    )
  }
  const isSquash = operation === MultiCommitOperationKind.Squash
  dispatcher.initializeMultiCommitOperation(
    repository,
    {
      kind: MultiCommitOperationKind.Merge,
      isSquash,
      sourceBranch: comparisonBranch,
    },
    currentBranch,
    [],
    currentBranch.tip.sha
  )
  dispatcher.incrementMetric('mergesInitiatedFromComparison')
  return dispatcher.mergeBranch(
    repository,
    comparisonBranch,
    mergeStatus,
    isSquash
  )
}

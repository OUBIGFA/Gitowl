import { Repository } from '../../models/repository'
import { Commit, ICommitContext } from '../../models/commit'
import { defaultErrorHandler, Dispatcher } from '../dispatcher'
import { doMergeCommitsExistAfterCommit } from '../../lib/git'
import { getUniqueCoauthorsAsAuthors } from '../../lib/unique-coauthors-as-authors'
import { getSquashedCommitDescription } from '../../lib/squash/squashed-commit-description'
import { PopupType } from '../../models/popup'
import { Branch } from '../../models/branch'
import { ComparisonMode, HistoryTabMode } from '../../lib/app-state'

// Shared keyboard/drop insertion policy. Dropping a contiguous selection onto
// itself is not an operation and must not open a force-push/replay prompt.
export function planWorkspaceCommitInsertion(
  commitSHAs: ReadonlyArray<string>,
  row: number,
  selectedSHAs: ReadonlyArray<string>
) {
  const baseIndex = row === 0 ? null : row - 1
  if (
    commitSHAs.length === 0 ||
    (baseIndex !== null && baseIndex > commitSHAs.length)
  )
    return null
  const indexes = selectedSHAs.map(sha => commitSHAs.indexOf(sha)).sort()
  const contiguous = indexes.every(
    (value, i, array) => i === array.length - 1 || value === array[i + 1] - 1
  )
  if (
    contiguous &&
    ((baseIndex === null && indexes[0] === 0) ||
      baseIndex === indexes[0] - 1 ||
      (baseIndex !== null && indexes.includes(baseIndex)))
  )
    return null
  const maxIndex = Math.max(
    ...indexes,
    ...(baseIndex === null ? [] : [baseIndex])
  )
  return {
    baseSHA: baseIndex === null ? null : commitSHAs[baseIndex] ?? null,
    lastRetainedCommitRef:
      maxIndex !== commitSHAs.length - 1 ? `${commitSHAs[maxIndex]}^` : null,
  }
}

export async function compareWorkspaceBranch(
  dispatcher: Dispatcher,
  repository: Repository,
  branch: Branch,
  comparisonMode = ComparisonMode.Behind
) {
  await dispatcher.executeCompare(repository, {
    kind: HistoryTabMode.Compare,
    comparisonMode,
    branch,
  })
  dispatcher.updateCompareForm(repository, {
    filterText: branch.name,
    showBranchList: false,
  })
}

/** Replay guards are shared with React, not reimplemented by the native host. */
export async function reorderWorkspaceCommits(
  dispatcher: Dispatcher,
  repository: Repository,
  baseCommit: Commit | null,
  commits: ReadonlyArray<Commit>,
  lastRetainedCommitRef: string | null
) {
  if (await doMergeCommitsExistAfterCommit(repository, lastRetainedCommitRef)) {
    defaultErrorHandler(
      new Error(
        'Unable to reorder. Reordering replays all commits up to the last one required for the reorder. A merge commit cannot exist among those commits.'
      ),
      dispatcher
    )
    return
  }
  return dispatcher.reorderCommits(
    repository,
    commits,
    baseCommit,
    lastRetainedCommitRef
  )
}

export async function squashWorkspaceCommits(
  dispatcher: Dispatcher,
  repository: Repository,
  toSquash: ReadonlyArray<Commit>,
  squashOnto: Commit,
  lastRetainedCommitRef: string | null,
  isInvokedByContextMenu: boolean
) {
  const toSquashSansSquashOnto = toSquash.filter(c => c.sha !== squashOnto.sha)
  const allCommitsInSquash = [...toSquashSansSquashOnto, squashOnto]
  const coAuthors = getUniqueCoauthorsAsAuthors(allCommitsInSquash)
  const squashedDescription = getSquashedCommitDescription(
    toSquashSansSquashOnto,
    squashOnto
  )
  if (await doMergeCommitsExistAfterCommit(repository, lastRetainedCommitRef)) {
    defaultErrorHandler(
      new Error(
        'Unable to squash. Squashing replays all commits up to the last one required for the squash. A merge commit cannot exist among those commits.'
      ),
      dispatcher
    )
    return
  }
  dispatcher.recordSquashInvoked(isInvokedByContextMenu)
  dispatcher.showPopup({
    type: PopupType.CommitMessage,
    repository,
    coAuthors,
    showCoAuthoredBy: coAuthors.length > 0,
    commitMessage: {
      summary: squashOnto.summary,
      description: squashedDescription,
      timestamp: Date.now(),
    },
    dialogTitle: `Squash ${allCommitsInSquash.length} Commits`,
    dialogButtonText: `Squash ${allCommitsInSquash.length} Commits`,
    prepopulateCommitSummary: true,
    onSubmitCommitMessage: async (context: ICommitContext) => {
      dispatcher.closePopup(PopupType.CommitMessage)
      dispatcher.squash(
        repository,
        toSquashSansSquashOnto,
        squashOnto,
        lastRetainedCommitRef,
        context
      )
      return true
    },
  })
}

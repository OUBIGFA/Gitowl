import type { IRepositoryState } from '../../lib/app-state'
import type { ICommitContext } from '../../models/commit'
import { TipState } from '../../models/tip'
import { formatCommitMessage } from '../../lib/format-commit-message'
import { useRepoRulesLogic } from '../../lib/helpers/repo-rules'
import { basename } from 'path'
import type { Account } from '../../models/account'
import { Repository } from '../../models/repository'
import {
  AppFileStatusKind,
  WorkingDirectoryFileChange,
} from '../../models/status'
import { RepoRulesInfo } from '../../models/repo-rules'
import type { IAheadBehind } from '../../models/branch'
import { getAccountForCommitMessageGeneration } from '../../lib/get-account-for-repository'
import {
  enableCopilotAssistedCommit,
  enableCopilotSdkCommitMessageGeneration,
} from '../../lib/feature-flag'

/** Original single-file summary, distinct from a translated interface label. */
export function defaultCommitSummary(
  files: ReadonlyArray<WorkingDirectoryFileChange>,
  prepopulate: boolean
) {
  if (!prepopulate) {
    return 'Summary (required)'
  }
  const file = files[0]
  const name = basename(file.path)
  switch (file.status.kind) {
    case AppFileStatusKind.New:
    case AppFileStatusKind.Untracked:
      return `Create ${name}`
    case AppFileStatusKind.Deleted:
      return `Delete ${name}`
    default:
      return `Update ${name}`
  }
}

export type CommitRuleStatus = {
  readonly enabled: boolean
  readonly info: RepoRulesInfo
  readonly message: 'pass' | 'bypass' | 'fail'
  readonly author: 'pass' | 'bypass' | 'fail'
  readonly branch: 'pass' | 'bypass' | 'fail'
  readonly aheadBehind: IAheadBehind | null
}

/** Evaluate the original rule objects against Git's actual trailer formatting.
 * Shared by the workbench admission and the operation-local message draft. */
export async function evaluateCommitRules(
  repository: Repository,
  state: IRepositoryState,
  account: Account | null,
  context: ICommitContext
): Promise<CommitRuleStatus> {
  const info = state.changesState.currentRepoRulesInfo
  const enabled = useRepoRulesLogic(account, repository)
  const branch =
    state.branchesState.tip.kind === TipState.Valid
      ? state.branchesState.tip.branch.name
      : null
  return {
    enabled,
    info,
    aheadBehind: state.aheadBehind,
    message: enabled
      ? info.commitMessagePatterns.getFailedRules(
          await formatCommitMessage(repository, context)
        ).status
      : 'pass',
    author:
      enabled && state.commitAuthor?.email
        ? info.commitAuthorEmailPatterns.getFailedRules(
            state.commitAuthor.email
          ).status
        : 'pass',
    branch:
      enabled && branch
        ? info.branchNamePatterns.getFailedRules(branch).status
        : 'pass',
  }
}

/** Original CommitMessage.hasRepoRuleFailure predicate, used by both views. */
export function hasCommitRuleFailure(
  status: CommitRuleStatus,
  checkCommitMessage = true
): boolean {
  return (
    status.enabled &&
    (status.info.basicCommitWarning === true ||
      status.info.signedCommitsRequired === true ||
      status.info.pullRequestRequired === true ||
      (checkCommitMessage && status.message === 'fail') ||
      status.author === 'fail' ||
      (status.aheadBehind === null &&
        (status.info.creationRestricted === true || status.branch === 'fail')))
  )
}

/** Availability is an original account/feature decision, never a Go SDK guess. */
export function copilotAssistedCommitAvailable(input: {
  readonly repository: Repository
  readonly accounts: ReadonlyArray<Account>
  readonly amending: boolean
  readonly hasCreateAction: boolean
  readonly hasModeAction: boolean
  readonly hasMode: boolean
}) {
  const account = getAccountForCommitMessageGeneration(
    input.accounts,
    input.repository
  )
  return (
    enableCopilotAssistedCommit() &&
    input.hasCreateAction &&
    input.hasModeAction &&
    input.hasMode &&
    !input.amending &&
    !input.repository.isTutorialRepository &&
    account !== undefined &&
    account.token.length > 0 &&
    enableCopilotSdkCommitMessageGeneration(account)
  )
}

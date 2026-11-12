import {
  API,
  APICheckConclusion,
  APICheckStatus,
  IAPICreatePushProtectionBypassResponse,
} from '../../lib/api'
import {
  IRefCheck,
  getCheckRunActionsWorkflowRuns,
  getLatestPRWorkflowRunsLogsForCheckRun,
} from '../../lib/ci-checks/ci-checks'
import { getAccountForRepository } from '../../lib/get-account-for-repository'
import { assertNever } from '../../lib/fatal-error'
import { offsetFromNow } from '../../lib/offset-from'
import { Account } from '../../models/account'
import { PopupType } from '../../models/popup'
import {
  Repository,
  RepositoryWithGitHubRepository,
  getNonForkGitHubRepository,
} from '../../models/repository'
import { GitHubRepository } from '../../models/github-repository'
import { PullRequest } from '../../models/pull-request'
import { Branch } from '../../models/branch'
import { ReleaseSummary } from '../../models/release-notes'
import { IPullRequestState } from '../../lib/app-state'
import { SignInResult } from '../../lib/stores/sign-in-store'
import { Dispatcher } from '../dispatcher'
import {
  BypassReason,
  BypassReasonType,
} from '../secret-scanning/bypass-reason'
import type { ISecretScanResult } from '../secret-scanning/push-protection-error-dialog'

export interface IPushBranchDialogProps {
  readonly dispatcher: Dispatcher
  readonly repository: Repository
  readonly branch: Branch
  readonly onConfirm: (repository: Repository, branch: Branch) => void
  readonly onDismissed: () => void
}

/** Original publish/push-then-PR continuation, including its finally-unlock.
 * The Dispatcher owns push failures/recovery; no invented success or retry. */
export async function submitPushBranchDialog(
  props: IPushBranchDialogProps,
  setPushing: (value: boolean) => void
) {
  setPushing(true)
  try {
    await props.dispatcher.push(props.repository)
  } finally {
    setPushing(false)
  }
  props.onConfirm(props.repository, props.branch)
  props.onDismissed()
}

/** Only the published/ahead branch footer offers creating without pushing. */
export function createPullRequestWithoutPush(props: IPushBranchDialogProps) {
  props.onConfirm(props.repository, props.branch)
  props.onDismissed()
}

/** Original Workflow/SAML continuation. Only a successful original sign-in retries. */
export async function reauthorizeDialog(
  dispatcher: Dispatcher,
  endpoint: string,
  onSuccess: () => void,
  onDismissed: () => void
) {
  const result = await new Promise<SignInResult>(resolve => {
    dispatcher.beginBrowserBasedSignIn(endpoint, resolve)
  })
  if (result.kind === 'success') {
    onSuccess()
  }
  onDismissed()
}

/** Original CI suite eligibility, including the completed and thirty-day checks. */
export async function determineCheckRerunnability(
  dispatcher: Dispatcher,
  repository: GitHubRepository,
  localRepository: Repository,
  checkRuns: ReadonlyArray<IRefCheck>,
  failedOnly: boolean
) {
  const considered = failedOnly
    ? checkRuns.filter(cr => cr.conclusion === APICheckConclusion.Failure)
    : checkRuns
  const ids = new Set(considered.map(cr => cr.checkSuiteId))
  const suites = await Promise.all(
    [...ids]
      .filter((id): id is number => id !== null)
      .map(id => dispatcher.fetchCheckSuite(repository, localRepository, id))
  )
  const eligible = suites
    .filter(
      cs =>
        cs !== null &&
        cs.rerequestable &&
        Date.parse(cs.created_at) > offsetFromNow(-30, 'days') &&
        cs.status === APICheckStatus.Completed
    )
    .map(cs => cs!.id)
  return {
    rerunnable: considered.filter(
      cr => cr.checkSuiteId !== null && eligible.includes(cr.checkSuiteId)
    ),
    nonRerunnable: considered.filter(
      cr => cr.checkSuiteId === null || !eligible.includes(cr.checkSuiteId)
    ),
  }
}

/** Retain original failure/retry: a partial/empty response never closes the dialog. */
export async function rerunDialogChecks(
  dispatcher: Dispatcher,
  repository: GitHubRepository,
  localRepository: Repository,
  prRef: string,
  rerunnable: ReadonlyArray<IRefCheck>,
  failedOnly: boolean
): Promise<boolean> {
  const results = await dispatcher.rerequestCheckSuites(
    repository,
    localRepository,
    rerunnable,
    failedOnly
  )
  if (results.length === 0 || results.some(result => !result)) {
    return false
  }
  await dispatcher.manualRefreshSubscription(
    repository,
    localRepository,
    prRef,
    rerunnable
  )
  dispatcher.incrementMetric('rerunsChecks')
  return true
}

/** Original logs loading stages; caller owns account-change generation/unmount cancellation. */
export async function loadDialogCheckLogs(
  accounts: ReadonlyArray<Account>,
  repository: RepositoryWithGitHubRepository,
  pullRequest: PullRequest,
  originalChecks: ReadonlyArray<IRefCheck>,
  cancelled: () => boolean,
  update: (checks: ReadonlyArray<IRefCheck>, loading: boolean) => void
) {
  const checks = originalChecks.map(check => ({
    ...check,
    actionJobSteps: undefined,
    actionsWorkflow: undefined,
  }))
  update(checks, true)
  const account = getAccountForRepository(accounts, repository)
  if (account === null) {
    update(checks, false)
    return
  }
  const api = API.fromAccount(account)
  const github = getNonForkGitHubRepository(repository)
  const actions = await getCheckRunActionsWorkflowRuns(
    account,
    github.owner.login,
    github.name,
    pullRequest.head.ref,
    checks
  )
  if (cancelled()) {
    return
  }
  update(actions, true)
  const loaded = await getLatestPRWorkflowRunsLogsForCheckRun(
    api,
    github.owner.login,
    github.name,
    actions
  )
  if (cancelled()) {
    return
  }
  update(loaded, false)
}

/** Original notification footer switch; approved reviews deliberately only dismiss. */
export async function submitNotificationDialog(
  dispatcher: Dispatcher,
  repository: RepositoryWithGitHubRepository,
  pullRequest: PullRequest,
  shouldSwitch: boolean,
  setSwitching: (switching: boolean) => void,
  record: () => void,
  onDismissed: () => void
) {
  if (shouldSwitch) {
    setSwitching(true)
    await dispatcher.selectRepository(repository)
    await dispatcher.checkoutPullRequest(repository, pullRequest)
    setSwitching(false)
    record()
  }
  onDismissed()
}

/** Original PR preview submit, deliberately not an API PR creation implementation. */
export function submitPullRequestPreview(
  dispatcher: Dispatcher,
  repository: Repository,
  state: IPullRequestState,
  hasPullRequest: boolean,
  onDismissed: () => void
) {
  if (hasPullRequest) {
    dispatcher.showPullRequest(repository)
  } else {
    dispatcher.createPullRequest(repository, state.baseBranch ?? undefined)
    dispatcher.incrementMetric('createPullRequestCount')
    dispatcher.incrementMetric('createPullRequestFromPreviewCount')
  }
  onDismissed()
}

/** Shared original App popup promise and metrics; callers must explicitly choose a reason. */
export function openSecretBypassDialog(
  dispatcher: Dispatcher,
  secret: ISecretScanResult
) {
  return new Promise<IAPICreatePushProtectionBypassResponse | null>(resolve => {
    dispatcher.showPopup({
      type: PopupType.BypassPushProtection,
      secret,
      bypassPushProtection: (originalSecret, reason) => {
        dispatcher
          .createPushProtectionBypass(
            reason,
            originalSecret.id,
            originalSecret.bypassURL
          )
          .then(response => {
            recordSecretBypassStats(dispatcher, reason)
            resolve(response)
          })
          .catch(error => {
            resolve(null)
            dispatcher.postError(error)
          })
          .finally(() => {
            dispatcher.closePopup(PopupType.BypassPushProtection)
          })
      },
      onDismissed: () => {
        resolve(null)
      },
    })
  })
}

function recordSecretBypassStats(
  dispatcher: Dispatcher,
  reason: BypassReasonType
) {
  dispatcher.incrementMetric('secretsDetectedOnPushBypassedCount')
  switch (reason) {
    case BypassReason.FalsePositive:
      dispatcher.incrementMetric(
        'secretsDetectedOnPushBypassedAsFalsePositiveCount'
      )
      break
    case BypassReason.UsedInTests:
      dispatcher.incrementMetric(
        'secretsDetectedOnPushBypassedAsUsedInTestCount'
      )
      break
    case BypassReason.WillFixLater:
      dispatcher.incrementMetric(
        'secretsDetectedOnPushBypassedAsWillFixLaterCount'
      )
      break
    default:
      return assertNever(reason, `Unknown Bypass reason: ${reason}`)
  }
}

/** Original release range precedence: multi-release 'other' and thank-yous are omitted. */
export function displayDialogRelease(
  newReleases: ReadonlyArray<ReleaseSummary>
) {
  const latest = newReleases.at(0)
  const oldest = newReleases.at(-1)
  if (latest === undefined || oldest === undefined || latest === oldest) {
    return latest
  }
  return {
    latestVersion: `${oldest.latestVersion} - ${latest.latestVersion}`,
    datePublished: `${oldest.datePublished} to ${latest.datePublished}`,
    enhancements: newReleases.flatMap(r => r.enhancements),
    bugfixes: newReleases.flatMap(r => r.bugfixes),
    pretext: newReleases.flatMap(r => r.pretext),
    other: [],
    thankYous: [],
  }
}

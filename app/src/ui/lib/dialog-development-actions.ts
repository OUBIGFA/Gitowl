import { getAccountKey } from '../../models/account'
import type { Account } from '../../models/account'
import type { CLIAction } from '../../lib/cli-action'
import type { IAPIComment } from '../../lib/api'
import type { NotificationsDebugStore } from '../../lib/stores/notifications-debug-store'
import type { ValidNotificationPullRequestReview } from '../../lib/valid-notification-pull-request-review'
import type { PullRequest } from '../../models/pull-request'
import type { RepositoryWithGitHubRepository } from '../../models/repository'
import type { Dispatcher } from '../dispatcher'
import { UpdateStatus } from './update-store'
import type { IUpdateState } from './update-store'

/** The original development dialogs' nonvisual choices and continuations.
 * These never authorize production debug actions or expose token sessions. */
export enum TestNotificationType {
  PullRequestReview,
  PullRequestComment,
  ChecksFailed,
}
export enum TestNotificationStepKind {
  SelectPullRequest,
  SelectPullRequestReview,
  SelectPullRequestComment,
}
export type TestNotificationFlow = {
  readonly type: TestNotificationType
  readonly steps: ReadonlyArray<TestNotificationStepKind>
}
export const TestNotificationFlows: ReadonlyArray<TestNotificationFlow> = [
  {
    type: TestNotificationType.PullRequestReview,
    steps: [
      TestNotificationStepKind.SelectPullRequest,
      TestNotificationStepKind.SelectPullRequestReview,
    ],
  },
  {
    type: TestNotificationType.PullRequestComment,
    steps: [
      TestNotificationStepKind.SelectPullRequest,
      TestNotificationStepKind.SelectPullRequestComment,
    ],
  },
  {
    type: TestNotificationType.ChecksFailed,
    steps: [TestNotificationStepKind.SelectPullRequest],
  },
]
export type TestNotificationStepResultMap = Map<
  TestNotificationStepKind.SelectPullRequest,
  {
    readonly kind: TestNotificationStepKind.SelectPullRequest
    readonly pullRequest: PullRequest
  }
> &
  Map<
    TestNotificationStepKind.SelectPullRequestReview,
    {
      readonly kind: TestNotificationStepKind.SelectPullRequestReview
      readonly review: ValidNotificationPullRequestReview
    }
  > &
  Map<
    TestNotificationStepKind.SelectPullRequestComment,
    {
      readonly kind: TestNotificationStepKind.SelectPullRequestComment
      readonly comment: IAPIComment
      readonly isIssueComment: boolean
    }
  >

/** Original flow fetches. Callers own loading state and unmount cancellation. */
export async function loadNotificationTestStep(
  store: NotificationsDebugStore,
  repository: RepositoryWithGitHubRepository,
  flow: TestNotificationFlow,
  results: TestNotificationStepResultMap
) {
  const step = flow.steps[results.size]
  if (step === TestNotificationStepKind.SelectPullRequest) {
    return {
      pullRequests: await store.getPullRequests(repository, {
        filterByComments: flow.type === TestNotificationType.PullRequestComment,
        filterByReviews: flow.type === TestNotificationType.PullRequestReview,
      }),
    }
  }
  const pullRequest = results.get(
    TestNotificationStepKind.SelectPullRequest
  )?.pullRequest
  if (pullRequest === undefined) {
    return null
  }
  if (step === TestNotificationStepKind.SelectPullRequestReview) {
    return {
      reviews: await store.getPullRequestReviews(
        repository,
        pullRequest.pullRequestNumber
      ),
    }
  }
  if (step === TestNotificationStepKind.SelectPullRequestComment) {
    return {
      comments: await store.getPullRequestComments(
        repository,
        pullRequest.pullRequestNumber
      ),
    }
  }
  return null
}

/** Keep the original debug store as the notification simulator, including its
 * account routing, Alive events and checks. No frontend-crafted notification. */
export function simulateTestNotification(
  store: NotificationsDebugStore,
  dispatcher: Dispatcher,
  repository: RepositoryWithGitHubRepository,
  flow: TestNotificationFlow,
  results: TestNotificationStepResultMap
) {
  const pullRequest = results.get(
    TestNotificationStepKind.SelectPullRequest
  )?.pullRequest
  if (pullRequest === undefined) {
    return
  }
  if (flow.type === TestNotificationType.PullRequestReview) {
    const review = results.get(
      TestNotificationStepKind.SelectPullRequestReview
    )?.review
    if (review !== undefined) {
      store.simulatePullRequestReviewNotification(
        repository,
        pullRequest,
        review
      )
    }
  } else if (flow.type === TestNotificationType.PullRequestComment) {
    const result = results.get(
      TestNotificationStepKind.SelectPullRequestComment
    )
    if (result !== undefined) {
      store.simulatePullRequestCommentNotification(
        repository,
        pullRequest,
        result.comment,
        result.isIssueComment
      )
    }
  } else {
    store.simulatePullRequestChecksFailed(repository, pullRequest, dispatcher)
  }
}

export function backNotificationTest(
  flow: TestNotificationFlow | null,
  results: TestNotificationStepResultMap
) {
  if (flow === null) {
    return null
  }
  if (results.size === 0) {
    return {
      selectedFlow: null,
      stepResults: new Map() as TestNotificationStepResultMap,
    }
  }
  const last = flow.steps.at(results.size - 1)
  if (last === undefined) {
    return null
  }
  const next: Map<TestNotificationStepKind, unknown> = new Map(results)
  next.delete(last)
  return {
    selectedFlow: flow,
    stepResults: next as TestNotificationStepResultMap,
  }
}

/** Same trimming and optional branch semantics as the upstream CLI test form. */
export function getTestCLIAction(
  kind: CLIAction['kind'],
  pathInput: string,
  urlInput: string,
  branchInput: string
): CLIAction | null {
  if (kind === 'open-repository') {
    const path = pathInput.trim()
    return path.length === 0 ? null : { kind, path }
  }
  const url = urlInput.trim()
  const branch = branchInput.trim()
  return url.length === 0
    ? null
    : { kind, url, branch: branch.length === 0 ? undefined : branch }
}

export function firstTestAccountKey(accounts: ReadonlyArray<Account>): string {
  const first = accounts.at(0)
  return first === undefined ? '' : getAccountKey(first)
}
export function testTokenLocalDateTime(expiresAt: number | undefined): string {
  if (expiresAt === undefined) {
    return ''
  }
  const date = new Date(expiresAt)
  return new Date(expiresAt - date.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 19)
}
export function chosenTestTokenExpiration(
  dateTime: string
): number | undefined {
  const value = new Date(dateTime).getTime()
  return Number.isSafeInteger(value) && value >= 0 ? value : undefined
}

export function initialAboutTestState(): IUpdateState {
  return {
    status: UpdateStatus.UpdateNotAvailable,
    lastSuccessfulCheck: new Date(Date.now() - 1000 * 60 * 60),
    isX64ToARM64ImmediateAutoUpdate: false,
    newReleases: [],
    prioritizeUpdate: false,
    prioritizeUpdateInfoUrl: undefined,
  }
}

/** The test dialog simulates timing only; its install action just dismisses. */
export async function checkAboutTestUpdates(
  update: (state: Partial<IUpdateState>) => void,
  delay: (ms: number) => Promise<unknown>
) {
  update({ status: UpdateStatus.CheckingForUpdates })
  await delay(5000)
  update({ status: UpdateStatus.UpdateAvailable })
  await delay(10000)
  update({ status: UpdateStatus.UpdateReady, lastSuccessfulCheck: new Date() })
}

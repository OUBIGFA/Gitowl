import { ProviderStore } from '../lib/commit-generation/provider-store'
import { API } from '../lib/api'
import { installAuthenticatedImageTokenHandler } from '../lib/authenticated-image-token-handler'
import {
  Dispatcher,
  externalEditorErrorHandler,
  openShellErrorHandler,
  mergeConflictHandler,
  lfsAttributeMismatchHandler,
  defaultErrorHandler,
  missingRepositoryHandler,
  backgroundTaskHandler,
  pushNeedsPullHandler,
  upstreamAlreadyExistsHandler,
  rebaseConflictsHandler,
  localChangesOverwrittenHandler,
  refusedWorkflowUpdate,
  samlReauthRequired,
  insufficientGitHubRepoPermissions,
  discardChangesHandler,
  secretScanningPushProtectionErrorHandler,
} from './dispatcher'
import {
  AppStore,
  GitHubUserStore,
  CloningRepositoriesStore,
  CopilotStore,
  IssuesStore,
  SignInStore,
  RepositoriesStore,
  TokenStore,
  AccountsStore,
  PullRequestStore,
} from '../lib/stores'
import {
  GitHubUserDatabase,
  IssuesDatabase,
  RepositoriesDatabase,
  PullRequestDatabase,
} from '../lib/databases'
import { StatsDatabase, StatsStore } from '../lib/stats'
import {
  UiActivityMonitor,
  IUiActivityMonitor,
} from './lib/ui-activity-monitor'
import { RepositoryStateCache } from '../lib/stores/repository-state-cache'
import { ApiRepositoriesStore } from '../lib/stores/api-repositories-store'
import { CommitStatusStore } from '../lib/stores/commit-status-store'
import { PullRequestCoordinator } from '../lib/stores/pull-request-coordinator'
import { AheadBehindStore } from '../lib/stores/ahead-behind-store'
import { trampolineUIHelper } from '../lib/trampoline/trampoline-ui-helper'
import { AliveStore } from '../lib/stores/alive-store'
import { NotificationsStore } from '../lib/stores/notifications-store'
import { NotificationsDebugStore } from '../lib/stores/notifications-debug-store'
import { trampolineServer } from '../lib/trampoline/trampoline-server'
import { TrampolineCommandIdentifier } from '../lib/trampoline/trampoline-command'
import { createAskpassTrampolineHandler } from '../lib/trampoline/trampoline-askpass-handler'
import { createCredentialHelperTrampolineHandler } from '../lib/trampoline/trampoline-credential-helper'

/**
 * One Desktop kernel per renderer. Both the upstream React entry and a native
 * presentation host obtain these same original stores and dispatcher here.
 * This module assembles them; it never substitutes Git, auth or recovery logic.
 * Its Electron/IndexedDB environment remains real, not a mocked Node DOM.
 */
function assembleAppRuntime(activityMonitor: IUiActivityMonitor) {
  const gitHubUserStore = new GitHubUserStore(
    new GitHubUserDatabase('GitHubUserDatabase')
  )
  const cloningRepositoriesStore = new CloningRepositoriesStore()
  const issuesStore = new IssuesStore(new IssuesDatabase('IssuesDatabase'))
  const statsStore = new StatsStore(
    new StatsDatabase('StatsDatabase'),
    activityMonitor
  )
  const accountsStore = new AccountsStore(localStorage, TokenStore)
  API.setTokenProvider(accountsStore.resolveToken)
  API.onTokenInvalidated(accountsStore.handleTokenInvalidated)
  installAuthenticatedImageTokenHandler(accountsStore.resolveToken)
  const signInStore = new SignInStore(accountsStore)

  trampolineServer.registerCommandHandler(
    TrampolineCommandIdentifier.AskPass,
    createAskpassTrampolineHandler(accountsStore)
  )
  const repositoriesStore = new RepositoriesStore(
    new RepositoriesDatabase('Database')
  )
  trampolineServer.registerCommandHandler(
    TrampolineCommandIdentifier.CredentialHelper,
    createCredentialHelperTrampolineHandler(accountsStore, repositoriesStore)
  )
  const pullRequestStore = new PullRequestStore(
    new PullRequestDatabase('PullRequestDatabase'),
    repositoriesStore
  )
  const pullRequestCoordinator = new PullRequestCoordinator(
    pullRequestStore,
    repositoriesStore
  )
  const repositoryStateManager = new RepositoryStateCache(statsStore)
  const apiRepositoriesStore = new ApiRepositoriesStore(accountsStore)
  const commitStatusStore = new CommitStatusStore(accountsStore)
  const aheadBehindStore = new AheadBehindStore()
  const aliveStore = new AliveStore(accountsStore)
  const copilotStore = new CopilotStore(accountsStore)
  const notificationsStore = new NotificationsStore(
    accountsStore,
    aliveStore,
    pullRequestCoordinator,
    statsStore,
    repositoriesStore
  )
  const notificationsDebugStore = new NotificationsDebugStore(
    accountsStore,
    notificationsStore,
    pullRequestCoordinator
  )
  const appStore = new AppStore(
    gitHubUserStore,
    cloningRepositoriesStore,
    issuesStore,
    statsStore,
    signInStore,
    accountsStore,
    repositoriesStore,
    pullRequestCoordinator,
    repositoryStateManager,
    apiRepositoriesStore,
    notificationsStore,
    copilotStore,
    new ProviderStore(TokenStore, localStorage)
  )
  const dispatcher = new Dispatcher(
    appStore,
    repositoryStateManager,
    statsStore,
    commitStatusStore
  )
  // The order is part of upstream error propagation and recovery. UI replacement
  // must not collapse these handlers into a generic RPC error or a retry loop.
  for (const handler of [
    defaultErrorHandler,
    upstreamAlreadyExistsHandler,
    externalEditorErrorHandler,
    openShellErrorHandler,
    mergeConflictHandler,
    lfsAttributeMismatchHandler,
    insufficientGitHubRepoPermissions,
    pushNeedsPullHandler,
    samlReauthRequired,
    backgroundTaskHandler,
    missingRepositoryHandler,
    localChangesOverwrittenHandler,
    rebaseConflictsHandler,
    refusedWorkflowUpdate,
    discardChangesHandler,
    secretScanningPushProtectionErrorHandler,
  ]) {
    dispatcher.registerErrorHandler(handler)
  }
  trampolineUIHelper.setDispatcher(dispatcher)

  return {
    appStore,
    dispatcher,
    repositoryStateManager,
    accountsStore,
    repositoriesStore,
    signInStore,
    gitHubUserStore,
    cloningRepositoriesStore,
    issuesStore,
    statsStore,
    pullRequestStore,
    pullRequestCoordinator,
    apiRepositoriesStore,
    commitStatusStore,
    aheadBehindStore,
    aliveStore,
    copilotStore,
    notificationsStore,
    notificationsDebugStore,
  }
}

export type AppRuntime = ReturnType<typeof assembleAppRuntime>

let runtime: AppRuntime | undefined

/** Obtain the existing kernel; a second frontend must not create a second state owner. */
export function getAppRuntime(
  activityMonitor?: IUiActivityMonitor
): AppRuntime {
  if (runtime === undefined) {
    runtime = assembleAppRuntime(activityMonitor ?? new UiActivityMonitor())
  }
  return runtime
}

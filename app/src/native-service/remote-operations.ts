import { join, resolve } from 'path'
import { ExecError, GitError as DugiteError } from 'dugite'
import { Repository } from '../models/repository'
import { IRemote } from '../models/remote'
import { IAheadBehind } from '../models/branch'
import {
  IFetchProgress,
  IPullProgress,
  IPushProgress,
} from '../models/progress'
import { getRemotes } from '../lib/git/remote'
import { getStatus, IStatusResult } from '../lib/git/status'
import { getCurrentUpstreamRef } from '../lib/git/rev-parse'
import { getSymbolicRef } from '../lib/git/refs'
import { getConfigValue } from '../lib/git/config'
import { fetch } from '../lib/git/fetch'
import { pull } from '../lib/git/pull'
import { push } from '../lib/git/push'
import { git, isGitError, HookCallbackOptions } from '../lib/git/core'
import { GitProxyResolver } from '../lib/git/environment'
import { AuthenticationErrors } from '../lib/git/authentication'
import {
  withRepositoryGitOperation,
  withoutRepositoryGitAccess,
} from '../lib/git/repository-operation'
import { isRebaseHeadSet } from '../lib/git/operation-state'
import { AssistedCommitError } from '../lib/git/assisted-commit/error'
import { findDefaultRemote } from '../lib/stores/helpers/find-default-remote'
import { pathExists } from '../lib/path-exists'
import { isRecord } from '../lib/is-record'

/** Safe to transport: URLs, credentials, and account tokens are never included. */
export interface IRemoteInformation {
  readonly remotes: ReadonlyArray<{ readonly name: string }>
  readonly currentRemoteName: string | null
  readonly currentBranch: string | null
  readonly currentUpstreamBranch: string | null
  readonly aheadBehind: IAheadBehind | null
}

export interface IRemoteOperationResult {
  readonly remoteName: string
  readonly state: IRemoteInformation
}

export type RemoteOperationErrorCode =
  | 'invalid-options'
  | 'invalid-remote'
  | 'no-remotes'
  | 'remote-not-found'
  | 'no-tracking'
  | 'remote-mismatch'
  | 'invalid-tracking'
  | 'unborn-branch'
  | 'detached-head'
  | 'conflicts'
  | 'operation-in-progress'
  | 'busy'
  | 'cancelled'
  | 'local-changes'
  | 'non-fast-forward'
  | 'authentication-failed'
  | 'unsupported-push-urls'
  | 'git-failed'

/** Raw Git diagnostics can contain URL credentials; expose only safe classifications. */
export class RemoteOperationError extends Error {
  public constructor(
    public readonly code: RemoteOperationErrorCode,
    message: string,
    public readonly gitError: DugiteError | null = null,
    /** Interrupted Git can already have written locally or remotely. */
    public readonly outcome: 'unknown' | undefined = undefined
  ) {
    super(message)
    this.name = 'RemoteOperationError'
  }
}

interface IRemoteSelection {
  /** A configured name, never a URL, command, refspec, or caller-owned IRemote. */
  readonly remoteName?: string
  /** The parent-owned request scope, never a client-supplied execution option. */
  readonly signal?: AbortSignal
}

export interface IRemoteFetchOptions extends IRemoteSelection {
  readonly progressCallback?: (progress: IFetchProgress) => void
  readonly isBackgroundTask?: boolean
  readonly recurseSubmodules?: boolean
}

export interface IRemotePullOptions
  extends IRemoteSelection,
    HookCallbackOptions {
  readonly progressCallback?: (progress: IPullProgress) => void
  readonly recurseSubmodules?: boolean
}

export interface IRemotePushOptions
  extends IRemoteSelection,
    HookCallbackOptions {
  readonly progressCallback?: (progress: IPushProgress) => void
}

interface IRemoteState {
  readonly remotes: ReadonlyArray<IRemote>
  readonly status: IStatusResult
  readonly localRef: string | null
  readonly upstreamRef: string | null
  readonly upstreamRemoteName: string | null
  readonly mergeRef: string | null
}

function validateRemoteName(name: unknown): asserts name is string {
  if (
    typeof name !== 'string' ||
    name.length === 0 ||
    name.startsWith('-') ||
    /[\x00-\x20\x7f]/.test(name)
  ) {
    throw new RemoteOperationError(
      'invalid-remote',
      'Expected a valid remote name'
    )
  }
}

// Unsupported mutation options must not silently become an ordinary operation.
function validateOptions(
  options: IRemoteSelection | undefined,
  keys: string[]
) {
  if (
    options !== undefined &&
    (!isRecord(options) ||
      Object.keys(options).some(key => !keys.includes(key)))
  ) {
    throw new RemoteOperationError(
      'invalid-options',
      'Unsupported remote option'
    )
  }
  if (options?.remoteName !== undefined) {
    validateRemoteName(options.remoteName)
  }
}

async function readState(repository: Repository): Promise<IRemoteState> {
  const remotes = await getRemotes(repository)
  const status = await getStatus(repository, true, true)
  const localRef = await getSymbolicRef(repository, 'HEAD')
  const branch = localRef?.startsWith('refs/heads/')
    ? localRef.slice('refs/heads/'.length)
    : null
  const [upstreamRef, upstreamRemoteName, mergeRef] =
    branch === null || status.currentTip === undefined
      ? [null, null, null]
      : await Promise.all([
          getCurrentUpstreamRef(repository.path),
          getConfigValue(repository, `branch.${branch}.remote`),
          getConfigValue(repository, `branch.${branch}.merge`),
        ])
  return {
    remotes,
    status,
    localRef,
    upstreamRef,
    upstreamRemoteName,
    mergeRef,
  }
}

function currentRemote(state: IRemoteState): IRemote | null {
  return (
    state.remotes.find(remote => remote.name === state.upstreamRemoteName) ??
    findDefaultRemote(state.remotes)
  )
}

function information(state: IRemoteState): IRemoteInformation {
  return {
    remotes: state.remotes.map(remote => ({ name: remote.name })),
    currentRemoteName: currentRemote(state)?.name ?? null,
    currentBranch: state.status.currentBranch ?? null,
    currentUpstreamBranch: state.status.currentUpstreamBranch ?? null,
    aheadBehind: state.status.branchAheadBehind ?? null,
  }
}

async function assertNoOperation(
  repository: Repository,
  status: IStatusResult
) {
  if (status.doConflictedFilesExist) {
    throw new RemoteOperationError(
      'conflicts',
      'Resolve existing conflicts first'
    )
  }
  // A resolved index or incomplete rebase snapshot does not mean the operation
  // has finished. This service does not continue, abort, reset, clean, or stash it.
  if (
    status.mergeHeadFound ||
    status.squashMsgFound ||
    status.rebaseInternalState !== null ||
    status.isCherryPickingHeadFound ||
    (await isRebaseHeadSet(repository)) ||
    (
      await Promise.all(
        ['rebase-merge', 'rebase-apply', 'sequencer', 'REVERT_HEAD'].map(name =>
          pathExists(join(repository.resolvedGitDir, name))
        )
      )
    ).some(Boolean)
  ) {
    throw new RemoteOperationError(
      'operation-in-progress',
      'Finish the in-progress Git operation first'
    )
  }
}

function selectRemote(state: IRemoteState, name?: string): IRemote {
  if (state.remotes.length === 0) {
    throw new RemoteOperationError(
      'no-remotes',
      'The repository has no remotes'
    )
  }
  const remote =
    name === undefined
      ? currentRemote(state)
      : state.remotes.find(remote => remote.name === name)
  if (remote === null || remote === undefined) {
    throw new RemoteOperationError(
      'remote-not-found',
      'The selected remote no longer exists'
    )
  }
  validateRemoteName(remote.name)
  return remote
}

function assertTracking(state: IRemoteState, remote: IRemote) {
  if (state.localRef === null || !state.localRef.startsWith('refs/heads/')) {
    throw new RemoteOperationError(
      'detached-head',
      'The repository has a detached HEAD'
    )
  }
  if (state.status.currentTip === undefined) {
    throw new RemoteOperationError(
      'unborn-branch',
      'The current branch has no commits'
    )
  }
  if (
    state.upstreamRef === null ||
    state.upstreamRemoteName === null ||
    state.mergeRef === null
  ) {
    throw new RemoteOperationError(
      'no-tracking',
      'The current branch has no remote tracking branch'
    )
  }
  if (state.upstreamRemoteName !== remote.name) {
    throw new RemoteOperationError(
      'remote-mismatch',
      'Select the current tracking remote for this operation'
    )
  }
  if (!state.mergeRef.startsWith('refs/heads/')) {
    throw new RemoteOperationError(
      'invalid-tracking',
      'Tracking must target an ordinary remote branch'
    )
  }
  return { localRef: state.localRef, remoteRef: state.mergeRef }
}

/** Observers do not inherit the operation's admission or credential ownership. */
function hookOptions(options: HookCallbackOptions): HookCallbackOptions {
  return {
    onHookProgress:
      options.onHookProgress === undefined
        ? undefined
        : progress =>
            withoutRepositoryGitAccess(() => options.onHookProgress!(progress)),
    onHookFailure: (name, output) =>
      withoutRepositoryGitAccess(
        () =>
          options.onHookFailure?.(name, output) ??
          Promise.resolve('abort' as const)
      ),
    onTerminalOutputAvailable:
      options.onTerminalOutputAvailable === undefined
        ? undefined
        : subscribe =>
            withoutRepositoryGitAccess(() =>
              options.onTerminalOutputAvailable!(subscribe)
            ),
  }
}

function progressObserver<T>(callback: ((progress: T) => void) | undefined) {
  return callback === undefined
    ? undefined
    : (progress: T) => withoutRepositoryGitAccess(() => callback(progress))
}

function operationError(error: unknown): RemoteOperationError {
  if (error instanceof RemoteOperationError) {
    return error
  }
  if (error instanceof AssistedCommitError && error.code === 'busy') {
    return new RemoteOperationError(
      'busy',
      'Another Git operation owns this repository'
    )
  }
  if (error instanceof ExecError && error.code === 'ABORT_ERR') {
    return new RemoteOperationError(
      'cancelled',
      'Git was cancelled, but tracking refs, the worktree, or the remote may already have changed. Refresh and inspect before retrying.',
      null,
      'unknown'
    )
  }
  const parsed = isGitError(error) ? error.result.gitError : null
  if (parsed !== null && AuthenticationErrors.has(parsed)) {
    return new RemoteOperationError(
      'authentication-failed',
      'Remote authentication failed or access was denied',
      parsed
    )
  }
  if (parsed === DugiteError.PushNotFastForward) {
    return new RemoteOperationError(
      'non-fast-forward',
      'The remote has new commits; pull before pushing',
      parsed
    )
  }
  if (
    parsed === DugiteError.MergeConflicts ||
    parsed === DugiteError.RebaseConflicts ||
    parsed === DugiteError.UnresolvedConflicts
  ) {
    return new RemoteOperationError(
      'conflicts',
      'Git reported conflicts; resolve them before continuing',
      parsed
    )
  }
  if (
    parsed === DugiteError.MergeWithLocalChanges ||
    parsed === DugiteError.RebaseWithLocalChanges ||
    parsed === DugiteError.LocalChangesOverwritten
  ) {
    return new RemoteOperationError(
      'local-changes',
      'Git refused to overwrite local changes',
      parsed
    )
  }
  return new RemoteOperationError(
    'git-failed',
    'Git remote operation failed; local changes and any operation state were left for manual inspection',
    parsed
  )
}

/**
 * Headless remote workflows, not a second Git implementation. The trusted parent
 * supplies its real system/PAC resolver and owns credential trampoline handlers.
 * Clients choose configured remote names only; pull/push use the current tracking
 * branch and never publish implicitly, force, mirror, push tags, or auto-stash.
 */
export class RemoteOperations {
  private readonly activeRepositories = new Set<string>()

  public constructor(private readonly resolveProxy: GitProxyResolver) {}

  public async getRemoteInfo(
    repository: Repository
  ): Promise<IRemoteInformation> {
    try {
      return await withRepositoryGitOperation(
        repository.path,
        'read',
        async () => information(await readState(repository))
      )
    } catch (error) {
      throw operationError(error)
    }
  }

  /** Checking for updates is a fetch, never a pull or a worktree fast-forward. */
  public checkRemote(repository: Repository, options?: IRemoteFetchOptions) {
    return this.fetch(repository, options)
  }

  public async fetch(
    repository: Repository,
    options: IRemoteFetchOptions = {}
  ): Promise<IRemoteOperationResult> {
    validateOptions(options, [
      'remoteName',
      'signal',
      'progressCallback',
      'isBackgroundTask',
      'recurseSubmodules',
    ])
    return this.run(repository, options.remoteName, async remote => {
      await fetch(
        repository,
        remote,
        progressObserver(options.progressCallback),
        options.isBackgroundTask ?? false,
        options.recurseSubmodules ?? true,
        this.resolveProxy,
        options.signal
      )
    })
  }

  public async pull(
    repository: Repository,
    options: IRemotePullOptions = {}
  ): Promise<IRemoteOperationResult> {
    validateOptions(options, [
      'remoteName',
      'signal',
      'progressCallback',
      'recurseSubmodules',
      'onHookProgress',
      'onHookFailure',
      'onTerminalOutputAvailable',
    ])
    return this.run(repository, options.remoteName, async (remote, state) => {
      assertTracking(state, remote)
      await pull(repository, remote, {
        ...hookOptions(options),
        progressCallback: progressObserver(options.progressCallback),
        recurseSubmodules: options.recurseSubmodules,
        noAutoStash: true,
        signal: options.signal,
        resolveProxy: this.resolveProxy,
      })
    })
  }

  public async push(
    repository: Repository,
    options: IRemotePushOptions = {}
  ): Promise<IRemoteOperationResult> {
    validateOptions(options, [
      'remoteName',
      'signal',
      'progressCallback',
      'onHookProgress',
      'onHookFailure',
      'onTerminalOutputAvailable',
    ])
    return this.run(repository, options.remoteName, async (remote, state) => {
      const { localRef, remoteRef } = assertTracking(state, remote)
      const valid = await git(
        ['check-ref-format', remoteRef],
        repository.path,
        'validateRemoteTracking',
        { successExitCodes: new Set([0, 1]) }
      )
      if (valid.exitCode !== 0) {
        throw new RemoteOperationError(
          'invalid-tracking',
          'Tracking must target a valid remote branch'
        )
      }
      // Git, not our URL parser, applies pushurl and pushInsteadOf. The URL is
      // only used for proxy resolution; push still targets the configured name.
      const urls = await git(
        ['remote', 'get-url', '--push', '--all', '--', remote.name],
        repository.path,
        'getOrdinaryPushURLs'
      )
      const pushURLs = urls.stdout.slice(0, -1).split('\n')
      if (pushURLs.length !== 1 || pushURLs[0].length === 0) {
        throw new RemoteOperationError(
          'unsupported-push-urls',
          'Ordinary native push requires one configured push URL'
        )
      }
      await push(
        repository,
        { name: remote.name, url: pushURLs[0] },
        localRef,
        remoteRef,
        null,
        {
          ...hookOptions(options),
          ordinary: true,
          signal: options.signal,
          resolveProxy: this.resolveProxy,
        },
        progressObserver(options.progressCallback)
      )
    })
  }

  private async run(
    repository: Repository,
    remoteName: string | undefined,
    operation: (remote: IRemote, state: IRemoteState) => Promise<void>
  ): Promise<IRemoteOperationResult> {
    const path = resolve(repository.path)
    const key = __WIN32__ ? path.toLowerCase() : path
    if (this.activeRepositories.has(key)) {
      throw new RemoteOperationError(
        'busy',
        'A remote operation is already in progress'
      )
    }
    // Match Desktop's network-operation admission in addition to the shared Git
    // scope. Neither failed validation nor a failed Git subprocess retains it.
    this.activeRepositories.add(key)
    try {
      return await withRepositoryGitOperation(
        repository.path,
        'mutation',
        async () => {
          const state = await readState(repository)
          const remote = selectRemote(state, remoteName)
          await assertNoOperation(repository, state.status)
          await operation(remote, state)
          return {
            remoteName: remote.name,
            state: information(await readState(repository)),
          }
        }
      )
    } catch (error) {
      throw operationError(error)
    } finally {
      this.activeRepositories.delete(key)
    }
  }
}

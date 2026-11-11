import { projectKernelMediaOwner } from './media-presentation'
import { shell } from '../lib/app-shell'
import { getDotComAPIEndpoint } from '../lib/api'
import type { SignInResult } from '../lib/stores/sign-in-store'
import type { AppRuntime } from '../ui/app-runtime'
import type { IAppState } from '../lib/app-state'
import { Account, IAccountIdentity, isSameAccount } from '../models/account'
import { Popup, PopupType } from '../models/popup'
import { getRepositoryAccountIdentity } from '../models/repository'
import { FetchType } from '../models/fetch'
import { RetryActionType } from '../models/retry-actions'
import { getAccountForCommitMessageGeneration } from '../lib/get-account-for-repository'
import { enableCopilotSdkCommitMessageGeneration } from '../lib/feature-flag'
import { ErrorWithMetadata, getErrorCauses } from '../lib/error-with-metadata'
import { CopilotError, getCopilotErrorDisplayInfo } from '../lib/copilot-error'
import { GitError, TerminalOutput, isAuthFailureError } from '../lib/git/core'
import { coerceToString } from '../lib/git/coerce-to-string'
import { getFileFromExceedsError } from '../lib/helpers/regex'
import { isRecord } from '../lib/is-record'
import { GitError as DugiteError } from 'dugite'

import type {
  NativeDialogPresentation,
  NativeDialogReply,
} from './dialog-module'
import {
  cancelNativeDialog,
  isNativeDialog,
  nativeDialogTypes,
  parseNativeDialogReply,
  projectNativeDialog,
  replyNativeDialog,
} from './dialog-presenter'

// Sign-in navigation uses the original SignInStore; OAuth state never enters a DTO.

/** This is a bounded presenter, not coverage of the entire upstream popup union. */
export const supportedKernelPopupTypes = [
  PopupType.AddSSHHost,
  PopupType.SSHKeyPassphrase,
  PopupType.SSHUserPassword,
  PopupType.GenericGitAuthentication,
  PopupType.HookFailed,
  PopupType.GenerateCommitMessageOverrideWarning,
  PopupType.GenerateCommitMessageDisclaimer,
  PopupType.RepositoryAccount,
  PopupType.ConfirmForcePush,
  PopupType.PushNeedsPull,
  PopupType.Error,
  PopupType.SignIn,
] as const

export const supportedNativePopupTypes: ReadonlyArray<PopupType> = [
  ...supportedKernelPopupTypes,
  ...nativeDialogTypes,
]

export type SupportedKernelPopupType = typeof supportedKernelPopupTypes[number]
export type KernelHookResult = Parameters<
  Extract<Popup, { type: PopupType.HookFailed }>['resolve']
>[0]
export type KernelPopupRuntime = Pick<AppRuntime, 'appStore' | 'dispatcher'>
type PopupState = Pick<IAppState, 'currentPopup' | 'accounts'>
type PresentationState = Pick<
  IAppState,
  'accounts' | 'askForConfirmationOnForcePush' | 'commitGeneration'
>

interface PopupDTO<T extends PopupType> {
  /** The original AppStore id is opaque to the frontend; it is not a callback id. */
  readonly popupID: number | null
  readonly type: T
  readonly supported: true
}

interface ErrorContentDTO {
  readonly message: string
  readonly terminal: boolean
  readonly oversizedFiles?: ReadonlyArray<string>
  readonly retryAfterMessage?: string
  readonly actionText?: string
  readonly actionURL?: string
}

type ErrorAction = 'close' | 'retry' | 'openPreferences' | 'openAction'

/** No raw Popup, Repository, Error, Account, Buffer or callback crosses the wire. */
export type KernelPopupDTO =
  | (Omit<NativeDialogPresentation, 'type'> & {
      readonly type: Exclude<PopupType, SupportedKernelPopupType>
    })
  | PopupDTO<PopupType.SignIn>
  | (PopupDTO<PopupType.AddSSHHost> & {
      readonly host: string
      readonly ip: string
      readonly keyType: string
      readonly fingerprint: string
    })
  | (PopupDTO<PopupType.SSHKeyPassphrase> & { readonly keyPath: string })
  | (PopupDTO<PopupType.SSHUserPassword> & { readonly username: string })
  | (PopupDTO<PopupType.GenericGitAuthentication> & {
      readonly remoteUrl: string
      /** null means editable; even an empty predetermined username is locked. */
      readonly username: string | null
    })
  | (PopupDTO<PopupType.HookFailed> & {
      readonly hookName: string
      readonly terminalOutput: string
      readonly choices: ReadonlyArray<KernelHookResult>
    })
  | (PopupDTO<PopupType.GenerateCommitMessageOverrideWarning> & {
      readonly showCopilotInstructionsTip: boolean
    })
  | (PopupDTO<PopupType.GenerateCommitMessageDisclaimer> & {
      readonly generationSource: 'copilot' | 'custom'
    })
  | (PopupDTO<PopupType.RepositoryAccount> & {
      readonly repositoryName: string
      readonly endpoint: string
      readonly assignedLogin: string | null
      readonly requiresSignIn: boolean
      readonly selectedAccount?: IAccountIdentity
      readonly accounts: ReadonlyArray<IAccountIdentity>
    })
  | (PopupDTO<PopupType.ConfirmForcePush> & {
      readonly upstreamBranch: string
      readonly askForConfirmationOnForcePush: boolean
    })
  | PopupDTO<PopupType.PushNeedsPull>
  | (PopupDTO<PopupType.Error> & {
      readonly title: string
      readonly content: ErrorContentDTO
      readonly causes: ReadonlyArray<ErrorContentDTO>
      readonly retryCloneName: string | null
      readonly choices: ReadonlyArray<ErrorAction>
    })
  | {
      readonly popupID: number | null
      readonly type: PopupType
      readonly supported: false
    }

interface Reply<T extends SupportedKernelPopupType> {
  readonly popupID: number
  readonly type: T
}

export type KernelPopupReply =
  | (Reply<SupportedKernelPopupType> & { readonly action: 'cancel' })
  | (Reply<PopupType.AddSSHHost> & {
      readonly action: 'submit'
      readonly addHost: boolean
    })
  | (Reply<PopupType.SSHKeyPassphrase> & {
      readonly action: 'submit'
      readonly passphrase: string
      readonly storePassphrase: boolean
    })
  | (Reply<PopupType.SSHUserPassword> & {
      readonly action: 'submit'
      readonly password: string
      readonly storePassword: boolean
    })
  | (Reply<PopupType.GenericGitAuthentication> & {
      readonly action: 'submit'
      readonly username: string
      readonly password: string
    })
  | (Reply<PopupType.HookFailed> & {
      readonly action: 'submit'
      readonly result: KernelHookResult
    })
  | (Reply<PopupType.GenerateCommitMessageOverrideWarning> & {
      readonly action: 'submit'
      readonly confirmCommitMessageOverride: boolean
    })
  | (Reply<PopupType.GenerateCommitMessageDisclaimer> & {
      readonly action: 'submit'
    })
  | (Reply<PopupType.RepositoryAccount> & {
      readonly action: 'sign-in' | 'choose-another'
    })
  | (Reply<PopupType.RepositoryAccount> & {
      readonly action: 'submit'
      readonly account: IAccountIdentity
    })
  | (Reply<PopupType.ConfirmForcePush> & {
      readonly action: 'submit'
      readonly askForConfirmationOnForcePush: boolean
    })
  | (Reply<PopupType.PushNeedsPull> & { readonly action: 'submit' })
  | (Reply<PopupType.Error> & {
      readonly action: ErrorAction
      readonly contentIndex?: number
    })

/** Diagnostics deliberately contain neither wire values nor credential-bearing objects. */
export class KernelPopupError extends Error {
  public constructor(
    public readonly reason: 'invalid-reply' | 'unavailable' | 'unsupported'
  ) {
    super(
      reason === 'invalid-reply'
        ? 'Invalid popup reply'
        : reason === 'unavailable'
        ? 'This popup is no longer current'
        : 'This popup is not supported by the native presenter'
    )
    this.name = 'KernelPopupError'
  }
}

function supported(type: PopupType): type is SupportedKernelPopupType {
  return supportedKernelPopupTypes.some(candidate => candidate === type)
}

function base<T extends PopupType>(popup: Popup & { type: T }): PopupDTO<T> {
  return { popupID: popup.id ?? null, type: popup.type, supported: true }
}

function terminalText(output: TerminalOutput): string {
  // Decode together so a UTF-8 character split across native chunks is retained.
  return Array.isArray(output)
    ? Buffer.concat(output).toString('utf8')
    : coerceToString(output)
}

function underlyingError(error: Error): Error {
  return (
    getErrorCauses(error).find(
      (cause): cause is CopilotError => cause instanceof CopilotError
    ) ?? (error instanceof ErrorWithMetadata ? error.underlyingError : error)
  )
}

function errorContent(error: Error): ErrorContentDTO {
  const underlying = underlyingError(error)
  if (underlying instanceof GitError && underlying.isRawMessage) {
    return { message: underlying.message, terminal: true }
  }
  if (
    underlying instanceof GitError &&
    underlying.result.gitError === DugiteError.PushWithFileSizeExceedingLimit
  ) {
    return {
      message: error.message,
      terminal: false,
      oversizedFiles: getFileFromExceedsError(
        coerceToString(underlying.result.stderr)
      ),
    }
  }
  const copilot =
    underlying instanceof CopilotError
      ? getCopilotErrorDisplayInfo(underlying)
      : null
  return {
    message: copilot?.message ?? underlying.message,
    terminal: false,
    ...(copilot === null
      ? {}
      : {
          ...(copilot.retryAfterMessage === undefined
            ? {}
            : { retryAfterMessage: copilot.retryAfterMessage }),
          ...(copilot.actionText === undefined
            ? {}
            : { actionText: copilot.actionText }),
          ...(copilot.actionURL === undefined
            ? {}
            : { actionURL: copilot.actionURL }),
        }),
  }
}

function errorTitle(error: Error): string {
  const underlying = underlyingError(error)
  if (underlying instanceof CopilotError) {
    const info = getCopilotErrorDisplayInfo(underlying)
    if (info !== null) return info.title
  }
  if (
    underlying instanceof GitError &&
    underlying.result.gitError === DugiteError.PushWithFileSizeExceedingLimit
  ) {
    return 'File size limit exceeded'
  }
  if (error instanceof ErrorWithMetadata) {
    switch (error.metadata.retryAction?.type) {
      case RetryActionType.Clone:
        return 'Clone failed'
      case RetryActionType.Push:
        return 'Failed to push'
    }
    switch (error.metadata.gitContext?.kind) {
      case 'create-repository':
        return 'Failed creating repository'
      case 'commit':
        return 'Commit failed'
    }
  }
  return 'Error'
}

function errorCauseContents(popup: Extract<Popup, { type: PopupType.Error }>) {
  return popup.showCauses
    ? getErrorCauses(popup.error)
        .filter(
          (cause): cause is Error =>
            cause instanceof Error &&
            !(cause instanceof ErrorWithMetadata) &&
            cause !== underlyingError(popup.error)
        )
        .map(errorContent)
    : []
}

function errorChoices(error: Error): ReadonlyArray<ErrorAction> {
  // Match AppError's footer precedence: only clone errors offer Retry.
  if (
    error instanceof ErrorWithMetadata &&
    error.metadata.retryAction?.type === RetryActionType.Clone
  ) {
    return ['close', 'retry']
  }
  const underlying = underlyingError(error)
  return underlying instanceof GitError &&
    underlying.result.gitError !== null &&
    isAuthFailureError(underlying.result.gitError)
    ? ['close', 'openPreferences']
    : ['close']
}

// The original dialog's local selection is not a repository-account setting.
// Weak popup identity rejects late sign-in callbacks after dismissal/replacement.
const repositoryAccountForms = new WeakMap<
  Popup,
  { choosingAnother: boolean; selected?: Account }
>()

function repositoryAccounts(
  popup: Extract<Popup, { type: PopupType.RepositoryAccount }>,
  accounts: ReadonlyArray<Account>
): ReadonlyArray<Account> {
  const accountsForEndpoint = (popup.accounts ?? accounts).filter(
    account => account.endpoint === popup.repository.gitHubRepository.endpoint
  )
  const selected = repositoryAccountForms.get(popup)?.selected
  return selected !== undefined &&
    !accountsForEndpoint.some(a => isSameAccount(a, selected))
    ? [...accountsForEndpoint, selected]
    : accountsForEndpoint
}

/** Project only fields read by the verified upstream components; unknown types stay visible. */
export function projectKernelPopup(
  popup: Popup | null,
  state: PresentationState
): KernelPopupDTO | null {
  if (popup === null) return null
  projectKernelMediaOwner(popup)
  switch (popup.type) {
    case PopupType.SignIn:
      return base(popup)
    case PopupType.AddSSHHost:
      return {
        ...base(popup),
        host: popup.host,
        ip: popup.ip,
        keyType: popup.keyType,
        fingerprint: popup.fingerprint,
      }
    case PopupType.SSHKeyPassphrase:
      return { ...base(popup), keyPath: popup.keyPath }
    case PopupType.SSHUserPassword:
      return { ...base(popup), username: popup.username }
    case PopupType.GenericGitAuthentication:
      return {
        ...base(popup),
        remoteUrl: popup.remoteUrl,
        username: popup.username ?? null,
      }
    case PopupType.HookFailed:
      return {
        ...base(popup),
        hookName: popup.hookName,
        terminalOutput: terminalText(popup.terminalOutput),
        // Current upstream has abort/ignore, not a retry or always-ignore policy.
        choices: ['abort', 'ignore'],
      }
    case PopupType.GenerateCommitMessageOverrideWarning: {
      const account = getAccountForCommitMessageGeneration(
        state.accounts,
        popup.repository
      )
      return {
        ...base(popup),
        showCopilotInstructionsTip:
          state.commitGeneration?.source !== 'custom' &&
          account !== undefined &&
          enableCopilotSdkCommitMessageGeneration(account),
      }
    }
    case PopupType.GenerateCommitMessageDisclaimer:
      return {
        ...base(popup),
        generationSource: state.commitGeneration?.source ?? 'copilot',
      }
    case PopupType.PushNeedsPull:
      // Their repository/file objects are continuation inputs, not displayed data.
      return base(popup)
    case PopupType.RepositoryAccount: {
      const identity = getRepositoryAccountIdentity(popup.repository)
      return {
        ...base(popup),
        repositoryName: popup.repository.name,
        endpoint: popup.repository.gitHubRepository.endpoint,
        assignedLogin: popup.repository.login,
        requiresSignIn:
          !repositoryAccountForms.get(popup)?.choosingAnother &&
          identity !== null &&
          !(popup.accounts ?? state.accounts).some(account =>
            isSameAccount(account, identity)
          ),
        ...(repositoryAccountForms.get(popup)?.selected === undefined
          ? {}
          : {
              selectedAccount: {
                endpoint: repositoryAccountForms.get(popup)!.selected!.endpoint,
                login: repositoryAccountForms.get(popup)!.selected!.login,
              },
            }),
        accounts: repositoryAccounts(popup, state.accounts).map(account => ({
          endpoint: account.endpoint,
          login: account.login,
        })),
      }
    }
    case PopupType.ConfirmForcePush:
      return {
        ...base(popup),
        upstreamBranch: popup.upstreamBranch,
        askForConfirmationOnForcePush: state.askForConfirmationOnForcePush,
      }
    case PopupType.Error: {
      const retry =
        popup.error instanceof ErrorWithMetadata
          ? popup.error.metadata.retryAction
          : undefined
      return {
        ...base(popup),
        title: errorTitle(popup.error),
        content: errorContent(popup.error),
        causes: errorCauseContents(popup),
        retryCloneName:
          retry?.type === RetryActionType.Clone ? retry.name : null,
        choices: errorChoices(popup.error),
      }
    }
    default:
      if (isNativeDialog(popup.type)) {
        // The original renderer supplies IAppState. The narrower parameter also
        // keeps the isolated legacy popup projection contract usable in tests.
        return projectNativeDialog(popup, state as IAppState) as KernelPopupDTO
      }
      return {
        popupID: popup.id ?? null,
        type: popup.type,
        supported: false,
      }
  }
}

function only(
  value: Readonly<Record<string, unknown>>,
  keys: ReadonlyArray<string>
) {
  return Object.keys(value).every(key => keys.includes(key))
}

function nonemptySingleLine(value: unknown): value is string {
  // These are single-line TextBox/PasswordTextBox inputs. Preserve whitespace.
  return (
    typeof value === 'string' && value.length > 0 && !/[\0\r\n]/.test(value)
  )
}

function popupID(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

function currentPopup(state: PopupState, id: number): Popup {
  const popup = state.currentPopup
  // Upstream DialogStackContext makes covered dialogs inert. An old id must
  // never settle a later prompt (or a prompt underneath an Error/SignIn).
  if (popup === null || popup.id !== id) {
    throw new KernelPopupError('unavailable')
  }
  if (!supported(popup.type)) throw new KernelPopupError('unsupported')
  return popup
}

function parseReply(state: PopupState, value: unknown): KernelPopupReply {
  if (!isRecord(value) || !popupID(value.popupID)) {
    throw new KernelPopupError('invalid-reply')
  }
  const popup = currentPopup(state, value.popupID)
  if (value.type !== popup.type) throw new KernelPopupError('invalid-reply')
  const exact = (fields: ReadonlyArray<string>) =>
    only(value, ['popupID', 'type', 'action', ...fields])
  if (value.action === 'cancel' && exact([])) {
    return value as unknown as KernelPopupReply
  }
  let valid = false
  if (value.action === 'submit') {
    switch (popup.type) {
      case PopupType.AddSSHHost:
        valid = exact(['addHost']) && typeof value.addHost === 'boolean'
        break
      case PopupType.SSHKeyPassphrase:
        valid =
          exact(['passphrase', 'storePassphrase']) &&
          nonemptySingleLine(value.passphrase) &&
          typeof value.storePassphrase === 'boolean'
        break
      case PopupType.SSHUserPassword:
        valid =
          exact(['password', 'storePassword']) &&
          nonemptySingleLine(value.password) &&
          typeof value.storePassword === 'boolean'
        break
      case PopupType.GenericGitAuthentication:
        valid =
          exact(['username', 'password']) &&
          nonemptySingleLine(value.username) &&
          nonemptySingleLine(value.password) &&
          (popup.username === undefined || value.username === popup.username)
        break
      case PopupType.HookFailed:
        valid =
          exact(['result']) &&
          (value.result === 'abort' || value.result === 'ignore')
        break
      case PopupType.GenerateCommitMessageOverrideWarning:
        valid =
          exact(['confirmCommitMessageOverride']) &&
          typeof value.confirmCommitMessageOverride === 'boolean'
        break
      case PopupType.GenerateCommitMessageDisclaimer:
      case PopupType.PushNeedsPull:
        valid = exact([])
        break
      case PopupType.RepositoryAccount:
        valid =
          exact(['account']) &&
          isRecord(value.account) &&
          only(value.account, ['endpoint', 'login']) &&
          nonemptySingleLine(value.account.endpoint) &&
          nonemptySingleLine(value.account.login) &&
          repositoryAccounts(popup, state.accounts).some(account =>
            isSameAccount(account, value.account as unknown as IAccountIdentity)
          )
        break
      case PopupType.ConfirmForcePush:
        valid =
          exact(['askForConfirmationOnForcePush']) &&
          typeof value.askForConfirmationOnForcePush === 'boolean'
        break
    }
  } else if (popup.type === PopupType.RepositoryAccount) {
    valid =
      exact([]) &&
      (value.action === 'sign-in' || value.action === 'choose-another')
  } else if (popup.type === PopupType.Error) {
    if (value.action === 'openAction') {
      const contents = [errorContent(popup.error), ...errorCauseContents(popup)]
      const index = value.contentIndex
      valid =
        exact(['contentIndex']) &&
        typeof index === 'number' &&
        Number.isSafeInteger(index) &&
        index >= 0 &&
        index < contents.length &&
        !!contents[index].actionText &&
        !!contents[index].actionURL
    } else {
      valid =
        exact([]) &&
        errorChoices(popup.error).some(action => action === value.action)
    }
  }
  if (!valid) throw new KernelPopupError('invalid-reply')
  return value as unknown as KernelPopupReply
}

/** The sole wire-field validation boundary, including fixed usernames and account choices. */
export function validateKernelPopupReply(
  runtime: KernelPopupRuntime,
  value: unknown
): KernelPopupReply | NativeDialogReply {
  const popup = runtime.appStore.getState().currentPopup
  if (popup !== null && isNativeDialog(popup.type)) {
    return parseNativeReply(popup, value)
  }
  return parseReply(runtime.appStore.getState(), value)
}

function parseNativeReply(popup: Popup, value: unknown): NativeDialogReply {
  if (!isRecord(value) || !popupID(value.popupID)) {
    throw new KernelPopupError('invalid-reply')
  }
  if (value.popupID !== popup.id) throw new KernelPopupError('unavailable')
  try {
    return parseNativeDialogReply(popup, value)
  } catch {
    throw new KernelPopupError('invalid-reply')
  }
}

function original<T extends SupportedKernelPopupType>(
  state: PopupState,
  reply: Reply<T>
): Extract<Popup, { type: T }> {
  // parseReply already checked the id and discriminant in this same snapshot.
  return state.currentPopup as Extract<Popup, { type: T }>
}

function cancelPopup(runtime: KernelPopupRuntime, popup: Popup, id: number) {
  switch (popup.type) {
    case PopupType.SignIn:
      runtime.dispatcher.resetSignInState()
      break
    case PopupType.AddSSHHost:
      popup.onSubmit(false)
      break
    case PopupType.SSHKeyPassphrase:
    case PopupType.SSHUserPassword:
      popup.onSubmit(undefined, false)
      break
    case PopupType.GenericGitAuthentication:
      popup.onDismiss()
      break
    case PopupType.HookFailed:
      popup.resolve('abort')
      break
    case PopupType.RepositoryAccount:
      popup.onComplete(undefined)
      break
  }
  // Hook resolution can remove its own popup and present the next owner. Close
  // only the old id: _closePopupById's settleDismissed must not abort that owner.
  runtime.dispatcher.closePopupById(id)
}

/** Cancel/close is a real upstream decision, not a generic stack removal. */
export function closeKernelPopup(
  runtime: KernelPopupRuntime,
  id: unknown
): void | Promise<void> {
  if (!popupID(id)) throw new KernelPopupError('invalid-reply')
  const state = runtime.appStore.getState()
  const popup = state.currentPopup
  if (popup === null || popup.id !== id)
    throw new KernelPopupError('unavailable')
  if (isNativeDialog(popup.type)) {
    return cancelNativeDialog(runtime as AppRuntime, popup)
  }
  cancelPopup(runtime, currentPopup(state, id), id)
}

/** Resolve the current original object and invoke only explicit, typed continuations. */
export async function replyToKernelPopup(
  runtime: KernelPopupRuntime,
  value: unknown
): Promise<void> {
  const state = runtime.appStore.getState()
  if (state.currentPopup !== null && isNativeDialog(state.currentPopup.type)) {
    const reply = parseNativeReply(state.currentPopup, value)
    await replyNativeDialog(runtime as AppRuntime, state.currentPopup, reply)
    return
  }
  const reply = parseReply(state, value)
  const dispatcher = runtime.dispatcher
  const close = () => dispatcher.closePopupById(reply.popupID)
  if (
    reply.type === PopupType.RepositoryAccount &&
    (reply.action === 'sign-in' || reply.action === 'choose-another')
  ) {
    const popup = original(state, reply)
    const form = repositoryAccountForms.get(popup) ?? { choosingAnother: false }
    repositoryAccountForms.set(popup, form)
    if (reply.action === 'choose-another') {
      form.choosingAnother = true
      return
    }
    const endpoint = popup.repository.gitHubRepository.endpoint
    const identity = getRepositoryAccountIdentity(popup.repository)
    const assigned =
      !form.choosingAnother &&
      identity !== null &&
      !repositoryAccounts(popup, state.accounts).some(a =>
        isSameAccount(a, identity)
      )
    const receive = (result: SignInResult) => {
      // Original SignIn callbacks may finish after navigation/cancel. A stale
      // result must not close a different sign-in or settle a different owner.
      if (
        result.kind !== 'success' ||
        !runtime.appStore.getState().allPopups.includes(popup)
      )
        return
      if (
        assigned
          ? !isSameAccount(result.account, identity)
          : result.account.endpoint !== endpoint
      )
        return
      form.selected = result.account
      dispatcher.closePopup(PopupType.SignIn)
      if (assigned) {
        popup.onComplete(result.account)
        dispatcher.closePopupById(popup.id!)
      }
    }
    if (assigned && popup.repository.login !== null) {
      await dispatcher.showAccountSignInDialog(
        endpoint,
        popup.repository.login,
        receive
      )
    } else if (endpoint === getDotComAPIEndpoint()) {
      await dispatcher.showDotComSignInDialog(receive)
    } else {
      await dispatcher.showEnterpriseSignInDialog(endpoint, receive)
    }
    return
  }
  if (reply.action === 'cancel') {
    cancelPopup(runtime, state.currentPopup!, reply.popupID)
    return
  }
  switch (reply.type) {
    case PopupType.AddSSHHost:
      original(state, reply).onSubmit(reply.addHost)
      close()
      return
    case PopupType.SSHKeyPassphrase:
      original(state, reply).onSubmit(reply.passphrase, reply.storePassphrase)
      close()
      return
    case PopupType.SSHUserPassword:
      original(state, reply).onSubmit(reply.password, reply.storePassword)
      close()
      return
    case PopupType.GenericGitAuthentication: {
      const popup = original(state, reply)
      popup.onSubmit(reply.username, reply.password)
      // GenericGitAuthentication.save invokes the same dismissal path after save.
      popup.onDismiss()
      close()
      return
    }
    case PopupType.HookFailed:
      original(state, reply).resolve(reply.result)
      close()
      return
    case PopupType.GenerateCommitMessageOverrideWarning: {
      const popup = original(state, reply)
      if (!reply.confirmCommitMessageOverride) {
        await dispatcher.setConfirmCommitMessageOverrideSetting(false)
      }
      const generation = dispatcher.generateCommitMessage(
        popup.repository,
        popup.filesSelected
      )
      close()
      await generation
      return
    }
    case PopupType.GenerateCommitMessageDisclaimer: {
      const popup = original(state, reply)
      dispatcher.updateCommitMessageGenerationDisclaimerLastSeen()
      const generation = dispatcher.generateCommitMessage(
        popup.repository,
        popup.filesSelected
      )
      close()
      await generation
      return
    }
    case PopupType.RepositoryAccount: {
      if (reply.action !== 'submit') throw new KernelPopupError('invalid-reply')
      const popup = original(state, reply)
      const account = repositoryAccounts(popup, state.accounts).find(account =>
        isSameAccount(account, reply.account)
      )!
      // Return the kernel's Account, never reconstruct it from a frontend DTO.
      popup.onComplete(account)
      close()
      return
    }
    case PopupType.ConfirmForcePush: {
      const popup = original(state, reply)
      dispatcher.setConfirmForcePushSetting(reply.askForConfirmationOnForcePush)
      close()
      await dispatcher.performForcePush(popup.repository)
      return
    }
    case PopupType.PushNeedsPull: {
      const popup = original(state, reply)
      await dispatcher.fetch(popup.repository, FetchType.UserInitiatedTask)
      close()
      return
    }
    case PopupType.Error: {
      const popup = original(state, reply)
      if (reply.action === 'openAction') {
        // Resolve the current original error URL, never a URL supplied by Go.
        // Like LinkButton, opening help does not dismiss the recovery dialog.
        const content = [
          errorContent(popup.error),
          ...errorCauseContents(popup),
        ][reply.contentIndex!]
        await shell.openExternal(content.actionURL!)
        return
      }
      close()
      if (
        reply.action === 'retry' &&
        popup.error instanceof ErrorWithMetadata
      ) {
        // The validator admitted only the original clone retry, not wire metadata.
        await dispatcher.performRetry(popup.error.metadata.retryAction!)
      } else if (reply.action === 'openPreferences') {
        // React's exit-transition timeout is view-only. The native host still
        // closes the Error first and opens the very same upstream Preferences.
        await dispatcher.showPopup({ type: PopupType.Preferences })
      }
      return
    }
  }
}

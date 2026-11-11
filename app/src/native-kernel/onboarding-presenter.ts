import { Emitter } from 'event-kit'
import type { AppRuntime } from '../ui/app-runtime'
import { WelcomeStep } from '../ui/welcome/welcome-step'
import { getGlobalConfigValue } from '../lib/git'
import { lookupPreferredEmail } from '../lib/email'
import { isDotComAccount } from '../models/account'
import { PopupType } from '../models/popup'
import { SignInStep, SignInState } from '../lib/stores/sign-in-store'
import {
  deleteGitAuthorConfigLock,
  saveConfiguredGitAuthor,
} from '../ui/lib/git-author-configuration'

export interface KernelSignInView {
  readonly kind: 'EndpointEntry' | 'Authentication' | 'Success'
  readonly loading: boolean
  readonly error: string | null
  readonly endpoint: string | null
  readonly expectedLogin: string | null
  readonly unrecognizedEnterpriseServer: boolean
}

/** Neither OAuth state, callbacks, account objects nor tokens are view data. */
export function projectSignInView(
  state: SignInState | null
): KernelSignInView | null {
  if (state === null) {
    return null
  }
  if (state.kind === SignInStep.Success) {
    return {
      kind: 'Success',
      loading: false,
      error: null,
      endpoint: null,
      expectedLogin: null,
      unrecognizedEnterpriseServer: false,
    }
  }
  return {
    kind: state.kind,
    loading: state.loading,
    error: state.error?.message ?? null,
    endpoint: state.kind === SignInStep.Authentication ? state.endpoint : null,
    expectedLogin:
      state.kind === SignInStep.Authentication
        ? state.expectedLogin ?? null
        : null,
    unrecognizedEnterpriseServer:
      state.kind === SignInStep.Authentication &&
      state.isUnrecognizedEnterpriseServer === true,
  }
}

export interface KernelOnboardingView {
  readonly step: WelcomeStep
  readonly loading: boolean
  readonly globalName: string | null
  readonly globalEmail: string | null
  readonly lockFilePath: string | null
  readonly account: {
    readonly name: string
    readonly preferredEmail: string
    readonly emails: ReadonlyArray<string>
    readonly enterprise: boolean
  } | null
}

/**
 * Native presentation of the original Welcome sequence. Only wizard navigation
 * and its temporary config-lock prompt are owned here. The original stores,
 * Dispatcher, Git author save helper and browser authentication retain authority.
 */
export class NativeOnboardingPresenter {
  private readonly emitter = new Emitter()
  private step = WelcomeStep.Start
  private loading = false
  private globalName: string | undefined
  private globalEmail: string | undefined
  private lockFilePath: string | undefined
  private initiated = false
  private previousSignInKind: SignInStep | undefined

  public constructor(private readonly runtime: AppRuntime) {}

  public onDidUpdate(listener: () => void) {
    return this.emitter.on('update', listener)
  }

  private emit(): void {
    this.emitter.emit('update')
  }

  public getState(): KernelOnboardingView | null {
    const state = this.runtime.appStore.getState()
    if (!state.showWelcomeFlow) {
      return null
    }
    const account = state.accounts[0]
    return {
      step: this.step,
      loading: this.loading,
      globalName: this.globalName ?? null,
      globalEmail: this.globalEmail ?? null,
      lockFilePath: this.lockFilePath ?? null,
      account:
        account === undefined
          ? null
          : {
              name: account.name || account.login,
              preferredEmail: lookupPreferredEmail(account),
              emails: account.emails.map(email => email.email),
              enterprise: !isDotComAccount(account),
            },
    }
  }

  public async start(): Promise<void> {
    if (!this.runtime.appStore.getState().showWelcomeFlow || this.initiated) {
      return
    }
    this.initiated = true
    this.runtime.dispatcher.recordWelcomeWizardInitiated()
    await this.refreshGlobalAuthor()
  }

  private async refreshGlobalAuthor(): Promise<void> {
    this.loading = true
    this.emit()
    try {
      const [name, email] = await Promise.all([
        getGlobalConfigValue('user.name'),
        getGlobalConfigValue('user.email'),
      ])
      this.globalName = name ?? undefined
      this.globalEmail = email ?? undefined
    } catch (error) {
      // Match Welcome.refreshGlobalGitAuthorInfo: a read error is logged, not a
      // reason to invent identity values or bypass the subsequent author form.
      log.error('[Welcome] error while fetching global user config', error)
    } finally {
      this.loading = false
      this.emit()
    }
  }

  public async advance(step: WelcomeStep): Promise<void> {
    if (!this.runtime.appStore.getState().showWelcomeFlow) {
      throw new Error('The Welcome flow is no longer active')
    }
    if (step === WelcomeStep.SignInToEnterprise) {
      this.runtime.dispatcher.beginEnterpriseSignIn()
    }
    this.step = step
    this.emit()
    if (step === WelcomeStep.ConfigureGit) {
      await this.refreshGlobalAuthor()
    }
    if (step === WelcomeStep.SignInToDotComWithBrowser) {
      this.runtime.dispatcher.requestBrowserAuthenticationToDotcom()
    }
  }

  /** The same transition check as Welcome.advanceOnSuccessfulSignIn. */
  public observeSignIn(): void {
    const signIn = this.runtime.signInStore.getState()
    const kind = signIn?.kind
    const previous = this.previousSignInKind
    this.previousSignInKind = kind
    if (
      previous !== kind &&
      kind === SignInStep.Success &&
      this.runtime.appStore
        .getState()
        .allPopups.some(popup => popup.type === PopupType.SignIn)
    ) {
      // Match SignIn.componentWillReceiveProps: its result callback may have
      // closed a parent already, so remove only the original SignIn popup type.
      void this.runtime.dispatcher.closePopup(PopupType.SignIn)
    }
    if (
      previous === undefined ||
      previous === kind ||
      kind !== SignInStep.Success ||
      (this.step !== WelcomeStep.SignInToDotComWithBrowser &&
        this.step !== WelcomeStep.SignInToEnterprise)
    ) {
      return
    }
    void this.advance(WelcomeStep.ConfigureGit)
    void this.runtime.dispatcher.resetSignInState()
  }

  public async configureAuthor(
    name: string,
    email: string,
    useAccount: boolean
  ): Promise<void> {
    const state = this.getState()
    if (
      state === null ||
      state.step !== WelcomeStep.ConfigureGit ||
      this.loading
    ) {
      throw new Error('Git author configuration is not the active Welcome step')
    }
    if (useAccount) {
      if (state.account === null || !state.account.emails.includes(email)) {
        throw new Error('The account author selection is no longer available')
      }
      name = state.account.name
    }
    this.loading = true
    this.emit()
    try {
      const result = await saveConfiguredGitAuthor(
        name,
        email,
        this.globalName,
        this.globalEmail
      )
      if (result.kind === 'locked') {
        this.lockFilePath = result.path
        return
      }
      await this.runtime.dispatcher.endWelcomeFlow()
    } finally {
      this.loading = false
      this.emit()
    }
  }

  public async deleteConfigLock(): Promise<void> {
    // A path is resolved from the original Git failure, never supplied by a
    // wire request. Only the user's explicit Delete action reaches unlink.
    if (
      this.lockFilePath === undefined ||
      this.step !== WelcomeStep.ConfigureGit
    ) {
      throw new Error('There is no current Git configuration lock prompt')
    }
    const path = this.lockFilePath
    try {
      await deleteGitAuthorConfigLock(path)
    } finally {
      this.lockFilePath = undefined
      this.emit()
    }
  }
}

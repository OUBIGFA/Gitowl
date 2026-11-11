import {
  CopilotClient,
  CopilotSession,
  RuntimeConnection,
  type CopilotClientOptions,
  type SessionConfig,
} from '@github/copilot-sdk'
import { access, mkdir } from 'fs/promises'
import { join, resolve } from 'path'
import { getCopilotRuntimePath } from '../lib/copilot-runtime'
import {
  createCopilotInMemorySessionFsProvider,
  getCopilotInMemorySessionFsConfig,
  type ICopilotInMemorySessionFsProvider,
} from '../lib/copilot-in-memory-session-fs-provider'
import {
  awaitCancellableCopilotOperation,
  getCopilotMessageSession,
  sendCopilotMessageRequest,
} from '../lib/copilot-operation'
import type { Model } from '../lib/copilot/types'

type AccountsRPC = CopilotSession['rpc']['accounts']
type SDKAccount = Extract<
  Awaited<ReturnType<AccountsRPC['enumerate']>>,
  { kind: 'accounts' }
>['items'][number]

/** Only runtime-issued routing/display metadata may leave the native service. */
export type CopilotGatewayAccount = Pick<
  SDKAccount,
  | 'selectionId'
  | 'host'
  | 'login'
  | 'kind'
  | 'active'
  | 'authSource'
  | 'derivedFrom'
>

/** SDK-owned login steps; a completed step can still require human consent. */
export type CopilotLoginStep = Awaited<
  ReturnType<AccountsRPC['login']['advance']>
>

export type CopilotLoginFlow = {
  readonly flowId: string
  readonly step: CopilotLoginStep
}

/** Authentication is not a promise of persistence, entitlement or remaining quota. */
export type CopilotGatewayStatus = {
  readonly isAuthenticated: boolean
  readonly activeAccount: CopilotGatewayAccount | null
  readonly accountCount: number
  readonly copilotPlan: string | null
  readonly errors: ReadonlyArray<{
    readonly message: string
    readonly githubMessage?: string
  }>
}

/** The locked SDK cannot namespace its interactive GitHub credential store. */
export class CopilotLoginPersistenceBlockedError extends Error {
  public constructor() {
    super(
      'Gitowl cannot safely persist a Copilot login with this SDK: its system keychain is shared with other clients. Plaintext storage is not permitted.'
    )
    this.name = 'CopilotLoginPersistenceBlockedError'
  }
}

export type CopilotGatewayOptions = {
  readonly runtimeRoot: string
  /** Gitowl-owned directory, never the user's existing Copilot or gh home. */
  readonly dataDirectory: string
}

export type CopilotGenerateRequest = {
  /** Opaque runtime selectionId, not a Git author or a provider account fallback. */
  readonly accountId: string
  readonly modelId: string
  readonly repositoryPath: string
  readonly systemPrompt: string
  readonly userPrompt: string
  readonly signal?: AbortSignal
}

// Keep the upstream commit-generation timeout without importing renderer stores.
const CommitMessageRequestTimeoutMs = 60000

function accountMetadata(account: SDKAccount): CopilotGatewayAccount {
  return {
    selectionId: account.selectionId,
    host: account.host,
    login: account.login,
    kind: account.kind,
    active: account.active,
    authSource: account.authSource,
    derivedFrom: account.derivedFrom,
  }
}

/** Project known fields explicitly: never spread authInfo or future secret fields. */
function loginStepMetadata(step: CopilotLoginStep): CopilotLoginStep {
  switch (step.kind) {
    case 'open-url':
      return { kind: step.kind, url: step.url }
    case 'input-required':
      return { kind: step.kind, prompt: step.prompt }
    case 'awaiting':
    case 'needs-interaction':
      return { kind: step.kind }
    case 'error':
      return { kind: step.kind, message: step.message }
    case 'completed':
      return {
        kind: step.kind,
        result: {
          status: step.result.status,
          host: step.result.host,
          login: step.result.login,
          accounts: step.result.accounts?.map(account => ({
            selectionId: account.selectionId,
            host: account.host,
            login: account.login,
            kind: account.kind,
          })),
        },
      }
  }
}

/**
 * Own the official SDK stdio runtime for native commit generation and login.
 *
 * No ambient tokens, gh login, host tools or repository config are admitted.
 * The locked SDK's interactive login writes a global host+login keychain slot;
 * there is no documented namespace/secret-store callback. Keep the verified
 * isolated, non-persisting runtime for status, login preparation/cancel and the
 * generation contract. Interactive completion stays explicitly blocked, not
 * silently downgraded to plaintext or shared-login import. A separately approved
 * authentication integration is still required for live authenticated generation.
 * Unsupported auth/selection/model APIs fail with their real runtime errors.
 */
export class CopilotGateway {
  private readonly runtimeRoot: string
  private readonly dataDirectory: string
  private client: CopilotClient | undefined
  private clientPromise: Promise<CopilotClient> | undefined
  private controlSession: Promise<CopilotSession> | undefined
  private readonly fileSystems = new Map<
    CopilotSession,
    ICopilotInMemorySessionFsProvider
  >()
  private readonly loginSteps = new Map<string, CopilotLoginStep>()
  private readonly generations = new Map<AbortController, string>()
  private closed = false
  private closing: Promise<void> | undefined

  public constructor(options: CopilotGatewayOptions) {
    this.runtimeRoot = resolve(options.runtimeRoot)
    this.dataDirectory = resolve(options.dataDirectory)
  }

  /** Status and validation errors contain metadata only, never authInfo. */
  public async status(): Promise<CopilotGatewayStatus> {
    const session = await this.getControlSession()
    const [status, activeAccount, errors] = await Promise.all([
      session.rpc.accounts.get({ query: { kind: 'status' } }),
      this.getActiveAccount(session),
      session.rpc.accounts.get({ query: { kind: 'lastErrors' } }),
    ])
    if (status.kind !== 'status' || errors.kind !== 'lastErrors') {
      throw new Error('Unexpected Copilot authentication status response')
    }
    return {
      isAuthenticated: status.status.isAuthenticated,
      activeAccount,
      accountCount: status.status.accountCount,
      copilotPlan: status.status.copilotPlan ?? null,
      errors: errors.errors.map(error => ({
        message: error.message,
        githubMessage: error.githubMessage,
      })),
    }
  }

  /** The opaque selectionId must be used unchanged for models and generation. */
  public async accounts(): Promise<ReadonlyArray<CopilotGatewayAccount>> {
    const session = await this.getControlSession()
    const result = await session.rpc.accounts.enumerate({
      query: { kind: 'accounts' },
    })
    if (result.kind !== 'accounts') {
      throw new Error('Unexpected Copilot account roster response')
    }
    return result.items.map(accountMetadata)
  }

  /**
   * Start the SDK's browser/PKCE flow; never open a browser or implement OAuth.
   * The locked github.com provider rejects selectAccount, so request the supported
   * default flow directly without a retained accountId or a silent fallback.
   */
  public async beginLogin(): Promise<CopilotLoginFlow> {
    const session = await this.getControlSession()
    const result = await session.rpc.accounts.login.begin({
      kind: 'githubDotCom',
    })
    return {
      flowId: result.flowId,
      step: this.recordLoginStep(result.flowId, result.step),
    }
  }

  /**
   * Do not advance an owned browser flow into a shared-keychain write or plaintext
   * fallback. Unknown/completed IDs still go to the SDK for its real flow error.
   */
  public async advanceLogin(
    flowId: string,
    input?: string
  ): Promise<CopilotLoginStep> {
    const session = await this.getControlSession()
    if (this.loginSteps.has(flowId)) {
      throw new CopilotLoginPersistenceBlockedError()
    }
    const step = await session.rpc.accounts.login.advance({ flowId, input })
    return this.recordLoginStep(flowId, step)
  }

  /** The runtime, not Gitowl, owns the meaning and lifetime of a login flow ID. */
  public async cancelLogin(flowId: string): Promise<void> {
    const session = await this.getControlSession()
    await session.rpc.accounts.login.cancel({ flowId })
    this.loginSteps.delete(flowId)
  }

  /** Retire in-flight work for this identity; never log out an implicit first account. */
  public async logout(selectionId: string): Promise<void> {
    this.requireSelection(selectionId)
    for (const [controller, accountId] of this.generations) {
      if (accountId === selectionId) {
        controller.abort()
      }
    }
    const session = await this.getControlSession()
    const result = await session.rpc.accounts.set({
      command: { kind: 'logout', selectionId },
    })
    if (!result.ok) {
      throw new Error('Copilot did not log out the selected account')
    }
  }

  /** Do not use the SDK's connection-wide model cache across different accounts. */
  public async models(selectionId: string): Promise<ReadonlyArray<Model>> {
    this.requireSelection(selectionId)
    const client = await this.getClient()
    const result = await client.rpc.models.list({ selectionId })
    return result.models
  }

  /**
   * Return complete raw text for the caller's upstream commit-message parser.
   * Each request owns a separate, tool-free session and an exact runtime identity.
   * Authentication, policy, billing and quota errors are never bypassed or retried
   * with a different account/model/provider.
   */
  public async generate(request: CopilotGenerateRequest): Promise<string> {
    this.requireSelection(request.accountId)
    if (request.modelId.length === 0) {
      throw new Error('A Copilot model selection is required')
    }
    this.assertOpen()
    const controller = new AbortController()
    const signal = request.signal
      ? AbortSignal.any([request.signal, controller.signal])
      : controller.signal
    let timedOut = false
    const cancellationError = () =>
      timedOut
        ? new Error('Copilot commit message generation timed out')
        : new DOMException(
            'Commit message generation was cancelled',
            'AbortError'
          )
    const cancellation = { signal, cancellationError }
    this.generations.set(controller, request.accountId)
    const timer = setTimeout(() => {
      timedOut = true
      controller.abort()
    }, CommitMessageRequestTimeoutMs)
    let session: CopilotSession | undefined
    let failure: unknown
    try {
      const client = await awaitCancellableCopilotOperation(
        () => this.getClient(),
        cancellation
      )
      const models = await awaitCancellableCopilotOperation(
        () => this.models(request.accountId),
        cancellation
      )
      const model = models.find(candidate => candidate.id === request.modelId)
      if (model === undefined) {
        throw new Error(
          'The selected Copilot model is no longer available for this account'
        )
      }
      session = await awaitCancellableCopilotOperation(
        () =>
          this.createSession(client, {
            model: request.modelId,
            workingDirectory: request.repositoryPath,
            systemMessage: { mode: 'append', content: request.systemPrompt },
          }),
        {
          ...cancellation,
          disposeLateValue: lateSession => this.disposeSession(lateSession),
        }
      )
      const selectedSession = session
      const selected = await awaitCancellableCopilotOperation(
        () =>
          selectedSession.rpc.accounts.set({
            command: { kind: 'switchActive', selectionId: request.accountId },
          }),
        cancellation
      )
      if (!selected.ok) {
        throw new Error('Copilot did not select the requested account')
      }
      await awaitCancellableCopilotOperation(
        () => this.assertSessionAccount(selectedSession, request.accountId),
        cancellation
      )
      // requireAvailable keeps the SDK's policy/entitlement gate and prevents
      // initial-model fallback; providerId pins the exact returned catalog entry.
      await awaitCancellableCopilotOperation(
        () =>
          selectedSession.rpc.model.switchTo({
            modelId: request.modelId,
            providerId: model.provider?.id,
            requireAvailable: true,
          }),
        cancellation
      )
      const current = await awaitCancellableCopilotOperation(
        () => selectedSession.rpc.model.getCurrent(),
        cancellation
      )
      if (
        current.modelId !== request.modelId ||
        (model.provider !== undefined &&
          current.providerId !== model.provider.id)
      ) {
        throw new Error('Copilot did not select the requested model')
      }
      const response = await sendCopilotMessageRequest(
        getCopilotMessageSession(session),
        { prompt: request.userPrompt },
        CommitMessageRequestTimeoutMs,
        cancellation
      )
      await awaitCancellableCopilotOperation(
        () => this.assertSessionAccount(selectedSession, request.accountId),
        cancellation
      )
      if (!response?.data.content) {
        throw new Error('No response from Copilot')
      }
      return response.data.content
    } catch (error) {
      failure = signal.aborted ? cancellationError() : error
      throw failure
    } finally {
      clearTimeout(timer)
      this.generations.delete(controller)
      try {
        if (session !== undefined) {
          await this.disposeSession(session)
        }
      } catch (error) {
        throw new AggregateError(
          failure === undefined ? [error] : [failure, error],
          'Copilot commit session cleanup failed'
        )
      }
    }
  }

  /** Close the owned process, including OAuth callback listeners and pending turns. */
  public close(): Promise<void> {
    if (this.closing !== undefined) {
      return this.closing
    }
    this.closed = true
    for (const controller of this.generations.keys()) {
      controller.abort()
    }
    this.closing = (async () => {
      try {
        // There is no Git/tool work to join. Force-stop the exclusively owned
        // stdio process so a pending login/response cannot hold service shutdown.
        await this.client?.forceStop()
      } finally {
        this.loginSteps.clear()
        for (const fs of this.fileSystems.values()) {
          fs.dispose()
        }
        this.fileSystems.clear()
      }
    })()
    return this.closing
  }

  /** A narrow construction boundary also permits real-SDK protocol fixtures. */
  protected createClient(options: CopilotClientOptions): CopilotClient {
    return new CopilotClient(options)
  }

  private assertOpen(): void {
    if (this.closed) {
      throw new Error('Copilot gateway is closed')
    }
  }

  private requireSelection(selectionId: string): void {
    if (selectionId.length === 0) {
      throw new Error('A Copilot account selection is required')
    }
  }

  private getClient(): Promise<CopilotClient> {
    this.assertOpen()
    return (this.clientPromise ??= this.startClient())
  }

  private async startClient(): Promise<CopilotClient> {
    const runtimePath = getCopilotRuntimePath(this.runtimeRoot)
    await access(runtimePath).catch(() => {
      throw new Error('Copilot runtime entry point not found')
    })
    const home = join(this.dataDirectory, 'home')
    const gh = join(home, 'gh')
    await mkdir(gh, { recursive: true })
    this.assertOpen()
    this.client = this.createClient({
      connection: RuntimeConnection.forStdio({ path: runtimePath }),
      mode: 'empty',
      baseDirectory: this.dataDirectory,
      workingDirectory: this.dataDirectory,
      useLoggedInUser: false,
      logLevel: 'none',
      clientInfo: {
        applicationName: 'Gitowl',
        integrationName: 'commit-message-generation',
      },
      // The SDK replaces env, rather than merging it. Keep transport necessities,
      // not ambient auth, Copilot overrides, Git helpers, or repository commands.
      env: {
        ...Object.fromEntries(
          [
            'PATH',
            'SystemRoot',
            'WINDIR',
            'COMSPEC',
            'TEMP',
            'TMP',
            'TMPDIR',
            'NODE_EXTRA_CA_CERTS',
            'SSL_CERT_FILE',
            'SSL_CERT_DIR',
            'HTTPS_PROXY',
            'HTTP_PROXY',
            'ALL_PROXY',
            'NO_PROXY',
          ].map(key => [key, process.env[key]])
        ),
        HOME: home,
        USERPROFILE: home,
        APPDATA: join(home, 'appdata'),
        LOCALAPPDATA: join(home, 'localappdata'),
        XDG_CONFIG_HOME: home,
        XDG_DATA_HOME: home,
        GH_CONFIG_DIR: gh,
        GH_HOST: 'github.com',
        GH_TOKEN: '',
        GITHUB_TOKEN: '',
        COPILOT_GITHUB_TOKEN: '',
        COPILOT_SDK_AUTH_TOKEN: '',
      },
      sessionFs: getCopilotInMemorySessionFsConfig(
        this.dataDirectory,
        process.platform === 'win32' ? 'windows' : 'posix'
      ),
    })
    await this.client.start()
    this.assertOpen()
    return this.client
  }

  private getControlSession(): Promise<CopilotSession> {
    this.assertOpen()
    return (this.controlSession ??= this.getClient().then(client =>
      this.createSession(client, { workingDirectory: this.dataDirectory })
    ))
  }

  private async createSession(
    client: CopilotClient,
    config: Pick<SessionConfig, 'model' | 'workingDirectory' | 'systemMessage'>
  ): Promise<CopilotSession> {
    this.assertOpen()
    let allocatedSession: CopilotSession | undefined
    try {
      return await client.createSession({
        ...config,
        clientName: 'gitowl:commit-message-generation',
        availableTools: [],
        tools: [],
        coauthorEnabled: false,
        enableFileHooks: false,
        enableConfigDiscovery: false,
        enableSkills: false,
        enableSessionStore: false,
        enableHostGitOperations: false,
        enableOnDemandInstructionDiscovery: false,
        skipCustomInstructions: true,
        infiniteSessions: { enabled: false },
        memory: { enabled: false },
        configDirectory: this.dataDirectory,
        createSessionFsProvider: session => {
          allocatedSession = session
          const fs = createCopilotInMemorySessionFsProvider()
          this.fileSystems.set(session, fs)
          if (this.closed) {
            fs.dispose()
          }
          return fs
        },
        onPermissionRequest: async () => ({ kind: 'reject' }),
      })
    } catch (error) {
      if (allocatedSession !== undefined) {
        this.fileSystems.get(allocatedSession)?.dispose()
        this.fileSystems.delete(allocatedSession)
      }
      throw error
    }
  }

  private async disposeSession(session: CopilotSession): Promise<void> {
    try {
      await session.disconnect()
    } finally {
      this.fileSystems.get(session)?.dispose()
      this.fileSystems.delete(session)
    }
  }

  private async getActiveAccount(
    session: CopilotSession
  ): Promise<CopilotGatewayAccount | null> {
    const result = await session.rpc.accounts.get({
      query: { kind: 'activeAccount' },
    })
    if (result.kind !== 'activeAccount') {
      throw new Error('Unexpected Copilot active account response')
    }
    return result.account ? accountMetadata(result.account) : null
  }

  private async assertSessionAccount(
    session: CopilotSession,
    selectionId: string
  ): Promise<void> {
    const account = await this.getActiveAccount(session)
    if (account?.selectionId !== selectionId) {
      throw new Error(
        'Copilot session identity changed from the selected account'
      )
    }
  }

  private recordLoginStep(
    flowId: string,
    step: CopilotLoginStep
  ): CopilotLoginStep {
    const metadata = loginStepMetadata(step)
    if (
      metadata.kind === 'error' ||
      (metadata.kind === 'completed' &&
        (metadata.result.status === 'completed' ||
          metadata.result.status === 'declined'))
    ) {
      this.loginSteps.delete(flowId)
    } else {
      this.loginSteps.set(flowId, metadata)
    }
    return metadata
  }
}

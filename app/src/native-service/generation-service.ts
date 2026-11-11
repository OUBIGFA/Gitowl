import { ProviderStore } from '../lib/commit-generation/provider-store'
import {
  discoverProviderModels,
  generateWithProvider,
} from '../lib/commit-generation/provider-adapters'
import {
  ConfiguredProviderModel,
  ProviderSettings,
  validateProviderSettings,
} from '../lib/commit-generation/provider-types'
import {
  buildCommitMessageSystemPrompt,
  buildCommitMessageUserPrompt,
  generateCommitMessagePromptTags,
} from '../lib/commit-generation/commit-prompts'
import { parseCopilotCommitMessage } from '../lib/copilot-commit-message'
import { getStatus } from '../lib/git/status'
import { getWorkingDirectoryDiff } from '../lib/git/diff'
import { withRepositoryGitOperation } from '../lib/git/repository-operation'
import { DiffType } from '../models/diff'
import { Repository } from '../models/repository'
import { isRecord } from '../lib/is-record'
import { IDataStore } from '../lib/stores/stores'
import { CopilotGateway, CopilotGatewayAccount } from './copilot-gateway'
import { ServiceError } from './service-error'

export type GenerationSelection =
  | {
      readonly kind: 'custom'
      readonly providerId: string
      readonly modelId: string
    }
  | {
      readonly kind: 'copilot'
      readonly accountId: string
      readonly modelId: string
    }

type CopilotCatalog = {
  readonly account: CopilotGatewayAccount
  readonly models: ReadonlyArray<ConfiguredProviderModel>
}
type Preferences = {
  readonly version: 1
  readonly language: string
  readonly copilotCatalogs: ReadonlyArray<CopilotCatalog>
  readonly copilotSelection: Extract<
    GenerationSelection,
    { kind: 'copilot' }
  > | null
  readonly consents: ReadonlyArray<string>
}
const PreferencesKey = 'gitowl-generation-preferences'

function stringValue(p: Record<string, unknown>, key: string): string {
  const value = p[key]
  if (
    typeof value !== 'string' ||
    value.trim() === '' ||
    value.includes('\0')
  ) {
    throw new ServiceError(-32602, `Expected ${key}`)
  }
  return value
}

function languageTag(value: unknown): string {
  try {
    if (typeof value === 'string' && value.length > 0) {
      const [tag] = Intl.getCanonicalLocales(value)
      if (tag) {
        return tag
      }
    }
  } catch {
    /* Reject invalid input without embedding it in a trusted prompt. */
  }
  throw new ServiceError(-32602, 'Expected a BCP47 output language')
}

function normalizedPreferences(state: Preferences): Preferences {
  const s = state.copilotSelection
  const valid =
    s === null ||
    state.copilotCatalogs.some(
      c =>
        c.account.selectionId === s.accountId &&
        c.models.some(m => m.id === s.modelId && m.enabled && m.available)
    )
  return valid ? state : { ...state, copilotSelection: null }
}

function loadPreferences(storage: IDataStore): Preferences {
  const raw = storage.getItem(PreferencesKey)
  if (raw === null) {
    return {
      version: 1,
      language: 'en',
      copilotCatalogs: [],
      copilotSelection: null,
      consents: [],
    }
  }
  try {
    const p: unknown = JSON.parse(raw)
    if (
      !isRecord(p) ||
      p.version !== 1 ||
      !Array.isArray(p.copilotCatalogs) ||
      !Array.isArray(p.consents) ||
      !p.consents.every(x => typeof x === 'string')
    ) {
      throw new Error('shape')
    }
    const catalogs = p.copilotCatalogs.map((entry: unknown): CopilotCatalog => {
      if (
        !isRecord(entry) ||
        !isRecord(entry.account) ||
        !Array.isArray(entry.models)
      ) {
        throw new Error('catalog')
      }
      const a = entry.account
      const account: CopilotGatewayAccount = {
        selectionId: stringValue(a, 'selectionId'),
        host: stringValue(a, 'host'),
        login: stringValue(a, 'login'),
        kind: a.kind as CopilotGatewayAccount['kind'],
        active: a.active === true,
      }
      if (typeof a.kind !== 'string') {
        throw new Error('account')
      }
      const models = entry.models.map((m: unknown): ConfiguredProviderModel => {
        if (
          !isRecord(m) ||
          typeof m.enabled !== 'boolean' ||
          typeof m.available !== 'boolean'
        ) {
          throw new Error('model')
        }
        return {
          id: stringValue(m, 'id'),
          name: stringValue(m, 'name'),
          enabled: m.enabled,
          available: m.available,
        }
      })
      return { account, models }
    })
    let selection: Preferences['copilotSelection'] = null
    if (p.copilotSelection !== null) {
      if (
        !isRecord(p.copilotSelection) ||
        p.copilotSelection.kind !== 'copilot'
      ) {
        throw new Error('selection')
      }
      selection = {
        kind: 'copilot',
        accountId: stringValue(p.copilotSelection, 'accountId'),
        modelId: stringValue(p.copilotSelection, 'modelId'),
      }
    }
    return normalizedPreferences({
      version: 1,
      language: languageTag(p.language),
      copilotCatalogs: catalogs,
      copilotSelection: selection,
      consents: p.consents,
    })
  } catch {
    throw new ServiceError(
      -32021,
      'Saved generation preferences are invalid; no settings were discarded'
    )
  }
}

/** Model selection, consent and language are shared by every native generation source. */
export class GenerationService {
  private state: Preferences
  public constructor(
    private readonly providers: ProviderStore,
    private readonly copilot: CopilotGateway,
    private readonly repository: (id: unknown) => Repository,
    private readonly storage: IDataStore = localStorage
  ) {
    this.state = loadPreferences(storage)
  }

  private save(state: Preferences) {
    const next = normalizedPreferences(state)
    this.storage.setItem(PreferencesKey, JSON.stringify(next))
    this.state = next
  }

  public settings() {
    const models = [
      ...this.providers
        .getEnabledModels()
        .map(m => ({
          selection: {
            kind: 'custom' as const,
            providerId: m.providerId,
            modelId: m.modelId,
          },
          sourceName: m.providerName,
          modelName: m.modelName,
        })),
      ...this.state.copilotCatalogs.flatMap(c =>
        c.models
          .filter(m => m.enabled && m.available)
          .map(m => ({
            selection: {
              kind: 'copilot' as const,
              accountId: c.account.selectionId,
              modelId: m.id,
            },
            sourceName: `GitHub Copilot · ${c.account.login}`,
            modelName: m.name,
          }))
      ),
    ]
    const custom = this.providers.currentSelection
    const candidate: GenerationSelection | null =
      custom === null
        ? this.state.copilotSelection
        : { kind: 'custom', ...custom }
    const selection =
      candidate !== null &&
      models.some(m => sameSelection(m.selection, candidate))
        ? candidate
        : null
    return {
      providers: this.providers.getProviders(),
      copilotCatalogs: this.state.copilotCatalogs,
      models,
      selection,
      language: this.state.language,
    }
  }

  public async invoke(
    method: string,
    raw: unknown,
    signal?: AbortSignal
  ): Promise<unknown> {
    if (!isRecord(raw)) {
      throw new ServiceError(-32602, 'Expected parameters')
    }
    const p = raw
    switch (method) {
      case 'generation.settings':
        return this.settings()
      case 'providers.save': {
        validateProviderSettings(p.provider)
        if (p.secret !== undefined && typeof p.secret !== 'string') {
          throw new ServiceError(-32602, 'Expected a credential string')
        }
        const input = p.provider as ProviderSettings & { id?: string }
        if (input.id !== undefined && typeof input.id !== 'string') {
          throw new ServiceError(-32602, 'Expected a provider ID')
        }
        await this.providers.saveProvider(input, p.secret)
        return this.settings()
      }
      case 'providers.rename':
        await this.providers.renameProvider(
          stringValue(p, 'id'),
          stringValue(p, 'name')
        )
        return this.settings()
      case 'providers.delete':
        await this.providers.deleteProvider(stringValue(p, 'id'))
        return this.settings()
      case 'providers.models.refresh': {
        const provider = this.requireProvider(stringValue(p, 'id'))
        const secret = await this.providers.getSecret(provider.id)
        const models = await discoverProviderModels(provider, secret, signal)
        // A changed origin/format must not receive a catalog fetched from its predecessor.
        if (this.providers.getProvider(provider.id) !== provider) {
          throw new ServiceError(
            -32024,
            'Provider changed while discovering models; refresh again'
          )
        }
        await this.providers.updateModels(provider.id, models)
        return this.settings()
      }
      case 'providers.models.setEnabled':
        if (typeof p.enabled !== 'boolean') {
          throw new ServiceError(-32602, 'Expected enabled')
        }
        await this.providers.setModelEnabled(
          stringValue(p, 'id'),
          stringValue(p, 'modelId'),
          p.enabled
        )
        return this.settings()
      case 'providers.models.add': {
        const provider = this.requireProvider(stringValue(p, 'id'))
        const id = stringValue(p, 'modelId')
        const name =
          typeof p.name === 'string' && p.name.trim() !== '' ? p.name : id
        const catalog = provider.models
          .filter(m => m.available && m.id !== id)
          .map(m => ({ id: m.id, name: m.name }))
        await this.providers.updateModels(provider.id, [
          ...catalog,
          { id, name },
        ])
        return this.settings()
      }
      case 'generation.language':
        this.save({ ...this.state, language: languageTag(p.language) })
        return this.settings()
      case 'generation.select': {
        const s = p.selection
        if (s === null) {
          await this.providers.setCurrentSelection(null)
          this.save({ ...this.state, copilotSelection: null })
        } else if (isRecord(s) && s.kind === 'custom') {
          await this.providers.setCurrentSelection({
            providerId: stringValue(s, 'providerId'),
            modelId: stringValue(s, 'modelId'),
          })
          this.save({ ...this.state, copilotSelection: null })
        } else if (isRecord(s) && s.kind === 'copilot') {
          const selected = {
            kind: 'copilot' as const,
            accountId: stringValue(s, 'accountId'),
            modelId: stringValue(s, 'modelId'),
          }
          if (
            !this.settings().models.some(m =>
              sameSelection(m.selection, selected)
            )
          ) {
            throw new ServiceError(-32022, 'Choose an enabled, available model')
          }
          await this.providers.setCurrentSelection(null)
          this.save({ ...this.state, copilotSelection: selected })
        } else {
          throw new ServiceError(-32602, 'Invalid model selection')
        }
        return this.settings()
      }
      case 'copilot.status':
        return this.copilot.status()
      case 'copilot.accounts': {
        const accounts = await this.copilot.accounts()
        const ids = new Set(accounts.map(a => a.selectionId))
        this.save({
          ...this.state,
          copilotCatalogs: this.state.copilotCatalogs.map(c => ({
            account:
              accounts.find(a => a.selectionId === c.account.selectionId) ??
              c.account,
            models: c.models.map(m => ({
              ...m,
              available: m.available && ids.has(c.account.selectionId),
            })),
          })),
        })
        return { accounts }
      }
      case 'copilot.login.begin':
        return this.copilot.beginLogin()
      case 'copilot.login.advance':
        return this.copilot.advanceLogin(
          stringValue(p, 'flowId'),
          typeof p.input === 'string' && p.input !== '' ? p.input : undefined
        )
      case 'copilot.login.cancel':
        await this.copilot.cancelLogin(stringValue(p, 'flowId'))
        return { cancelled: true }
      case 'copilot.logout': {
        const id = stringValue(p, 'selectionId')
        await this.copilot.logout(id)
        this.save({
          ...this.state,
          copilotCatalogs: this.state.copilotCatalogs.filter(
            c => c.account.selectionId !== id
          ),
          copilotSelection:
            this.state.copilotSelection?.accountId === id
              ? null
              : this.state.copilotSelection,
        })
        return this.settings()
      }
      case 'copilot.models.refresh': {
        const id = stringValue(p, 'selectionId')
        const account = (await this.copilot.accounts()).find(
          a => a.selectionId === id
        )
        if (!account) {
          throw new ServiceError(
            -32023,
            'The selected GitHub account is not signed in'
          )
        }
        const models = await this.copilot.models(id)
        const previous =
          this.state.copilotCatalogs.find(c => c.account.selectionId === id)
            ?.models ?? []
        const current = models.map(m => ({
          id: m.id,
          name: m.name,
          enabled: previous.find(old => old.id === m.id)?.enabled ?? false,
          available: m.policy?.state !== 'disabled',
        }))
        const missing = previous
          .filter(m => !models.some(n => n.id === m.id))
          .map(m => ({ ...m, available: false }))
        this.save({
          ...this.state,
          copilotCatalogs: [
            ...this.state.copilotCatalogs.filter(
              c => c.account.selectionId !== id
            ),
            { account, models: [...current, ...missing] },
          ],
        })
        return this.settings()
      }
      case 'copilot.models.setEnabled': {
        const id = stringValue(p, 'selectionId'),
          modelId = stringValue(p, 'modelId')
        if (typeof p.enabled !== 'boolean') {
          throw new ServiceError(-32602, 'Expected enabled')
        }
        const enabled = p.enabled
        const model = this.state.copilotCatalogs
          .find(c => c.account.selectionId === id)
          ?.models.find(m => m.id === modelId)
        if (!model || (enabled && !model.available)) {
          throw new ServiceError(-32022, 'Model is not available')
        }
        this.save({
          ...this.state,
          copilotCatalogs: this.state.copilotCatalogs.map(c =>
            c.account.selectionId !== id
              ? c
              : {
                  ...c,
                  models: c.models.map(m =>
                    m.id === modelId ? { ...m, enabled } : m
                  ),
                }
          ),
        })
        return this.settings()
      }
      case 'repository.generateMessage':
        return this.generate(p, signal)
      default:
        throw new ServiceError(-32601, `Unknown method: ${method}`)
    }
  }

  private requireProvider(id: string) {
    const provider = this.providers.getProvider(id)
    if (!provider) {
      throw new ServiceError(-32022, 'Provider is no longer configured')
    }
    return provider
  }

  private async generate(p: Record<string, unknown>, signal?: AbortSignal) {
    const repository = this.repository(p.id)
    if (
      !Array.isArray(p.paths) ||
      p.paths.length === 0 ||
      !p.paths.every(
        (path): path is string => typeof path === 'string' && path.length > 0
      ) ||
      new Set(p.paths).size !== p.paths.length
    ) {
      throw new ServiceError(-32602, 'Choose the changed files to summarize')
    }
    const paths = p.paths
    const { selection, language } = this.settings()
    if (!selection) {
      throw new ServiceError(
        -32022,
        'Choose an enabled model before generating'
      )
    }
    const provider =
      selection.kind === 'custom'
        ? this.requireProvider(selection.providerId)
        : null
    const secret =
      provider === null ? null : await this.providers.getSecret(provider.id)
    if (
      provider !== null &&
      this.providers.getProvider(provider.id) !== provider
    ) {
      throw new ServiceError(
        -32024,
        'Provider changed before generation; select it again'
      )
    }
    const consentKey =
      selection.kind === 'custom'
        ? `custom:${selection.providerId}:${provider!.baseUrl}`
        : `copilot:${selection.accountId}`
    if (!this.state.consents.includes(consentKey)) {
      if (p.consent !== true) {
        throw new ServiceError(
          -32020,
          'Confirm sending the selected changes to this generation source'
        )
      }
      this.save({
        ...this.state,
        consents: [...this.state.consents, consentKey],
      })
    }
    // Unlike getFilesDiffText, this read-only selection path never clears or
    // stages the user's real index just to obtain model context.
    const diff = await withRepositoryGitOperation(
      repository.path,
      'read',
      async () => {
        const status = await getStatus(repository, true, true)
        const sections: string[] = []
        let bytes = 0
        for (const path of paths) {
          signal?.throwIfAborted()
          const file = status.workingDirectory.files.find(f => f.path === path)
          if (!file) {
            throw new ServiceError(
              -32003,
              'A selected file is no longer changed; refresh before generating'
            )
          }
          const change = await getWorkingDirectoryDiff(repository, file)
          const metadata = JSON.stringify({
            path: file.path,
            status: file.status.kind,
            ...('oldPath' in file.status
              ? { oldPath: file.status.oldPath }
              : {}),
          })
          const text =
            change.kind === DiffType.Text || change.kind === DiffType.LargeText
              ? change.text
              : JSON.stringify({
                  kind: DiffType[change.kind],
                  ...(change.kind === DiffType.Submodule
                    ? { oldSHA: change.oldSHA, newSHA: change.newSHA }
                    : {}),
                })
          const section = `File metadata: ${metadata}\n${text}\n`
          bytes += Buffer.byteLength(section)
          if (bytes > 10 * 1024 * 1024) {
            throw new ServiceError(
              -32025,
              'The selected diff is too large; choose fewer files'
            )
          }
          sections.push(section)
        }
        return sections.join('\n')
      }
    )
    signal?.throwIfAborted()
    const tags = generateCommitMessagePromptTags()
    const systemPrompt = buildCommitMessageSystemPrompt(false, tags, language)
    const userPrompt = buildCommitMessageUserPrompt(diff, tags)
    if (
      provider !== null &&
      this.providers.getProvider(provider.id) !== provider
    ) {
      throw new ServiceError(
        -32024,
        'Provider changed before generation; select it again'
      )
    }
    const content =
      selection.kind === 'custom'
        ? await generateWithProvider(
            provider!,
            selection.modelId,
            secret,
            systemPrompt,
            userPrompt,
            signal
          )
        : await this.copilot.generate({
            accountId: selection.accountId,
            modelId: selection.modelId,
            repositoryPath: repository.path,
            systemPrompt,
            userPrompt,
            signal,
          })
    signal?.throwIfAborted()
    if (
      !sameSelection(this.settings().selection, selection) ||
      this.state.language !== language ||
      (provider !== null &&
        this.providers.getProvider(provider.id) !== provider)
    ) {
      throw new ServiceError(
        -32024,
        'Generation selection changed; the draft was not applied'
      )
    }
    return { ...parseCopilotCommitMessage(content), selection, language }
  }

  public close() {
    return this.copilot.close()
  }
}

function sameSelection(
  a: GenerationSelection | null,
  b: GenerationSelection | null
): boolean {
  if (a === null || b === null) {
    return a === b
  }
  return a.kind === 'custom' && b.kind === 'custom'
    ? a.providerId === b.providerId && a.modelId === b.modelId
    : a.kind === 'copilot' &&
        b.kind === 'copilot' &&
        a.accountId === b.accountId &&
        a.modelId === b.modelId
}

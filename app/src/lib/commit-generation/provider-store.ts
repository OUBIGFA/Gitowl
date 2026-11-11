import { randomUUID } from 'crypto'
import type { IDataStore, ISecureStore } from '../stores/stores'
import { TokenStore } from '../stores/token-store'
import {
  ConfiguredProvider,
  ConfiguredProviderModel,
  DiscoveredProviderModel,
  EnabledProviderModel,
  ProviderError,
  ProviderModelSelection,
  ProviderSettings,
  requiresNewProviderSecret,
  validateProviderSecret,
  validateProviderSettings,
} from './provider-types'

export const ProviderStorageKey = 'gitowl-commit-generation-providers'
export const ProviderKeychainService = 'Gitowl'

const UUIDPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

type ProviderState = {
  readonly version: 1
  readonly providers: ReadonlyArray<ConfiguredProvider>
  readonly currentSelection: ProviderModelSelection | null
}

function nativeStorage(): IDataStore {
  // Electron supplies the original renderer localStorage. The retained Node
  // research entry must opt in with --experimental-webstorage and a userData
  // --localstorage-file. Neither entry substitutes successful memory storage.
  try {
    if (typeof localStorage !== 'undefined') {
      return localStorage
    }
  } catch {
    throw new ProviderError('storage-unavailable')
  }
  throw new ProviderError('storage-unavailable')
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasOnlyKeys(
  value: Record<string, unknown>,
  keys: ReadonlyArray<string>
) {
  return Object.keys(value).every(key => keys.includes(key))
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0
}

function settingsOnly(settings: ProviderSettings): ProviderSettings {
  // Whitelist fields: an IPC caller adding a secret property cannot persist it.
  return {
    name: settings.name.trim(),
    format: settings.format,
    baseUrl: new URL(settings.baseUrl).href.replace(/\/+$/, ''),
    authKind: settings.authKind,
    ...(settings.requestTimeoutSeconds === undefined
      ? {}
      : { requestTimeoutSeconds: settings.requestTimeoutSeconds }),
  }
}

function freezeProvider(provider: ConfiguredProvider): ConfiguredProvider {
  return Object.freeze({
    ...provider,
    models: Object.freeze(
      provider.models.map(model => Object.freeze({ ...model }))
    ),
  })
}

function validSelection(
  providers: ReadonlyArray<ConfiguredProvider>,
  selection: ProviderModelSelection | null
): ProviderModelSelection | null {
  if (selection === null) {
    return null
  }
  const provider = providers.find(p => p.id === selection.providerId)
  return provider?.models.some(
    m => m.id === selection.modelId && m.enabled && m.available
  )
    ? Object.freeze({
        providerId: selection.providerId,
        modelId: selection.modelId,
      })
    : null
}

function parseState(raw: string | null): ProviderState {
  if (raw === null) {
    return { version: 1, providers: Object.freeze([]), currentSelection: null }
  }
  try {
    const parsed: unknown = JSON.parse(raw)
    if (
      !isRecord(parsed) ||
      !hasOnlyKeys(parsed, ['version', 'providers', 'currentSelection']) ||
      parsed.version !== 1 ||
      !Array.isArray(parsed.providers)
    ) {
      throw new ProviderError('storage-corrupt')
    }
    const ids = new Set<string>()
    const providers = parsed.providers.map((p: unknown) => {
      validateProviderSettings(p)
      const entry = p as ProviderSettings & Record<string, unknown>
      if (
        !hasOnlyKeys(entry, [
          'id',
          'name',
          'format',
          'baseUrl',
          'authKind',
          'requestTimeoutSeconds',
          'models',
        ]) ||
        typeof entry.id !== 'string' ||
        !UUIDPattern.test(entry.id) ||
        ids.has(entry.id) ||
        !Array.isArray(entry.models)
      ) {
        throw new ProviderError('storage-corrupt')
      }
      ids.add(entry.id)
      const modelIds = new Set<string>()
      const models = entry.models.map((m: unknown): ConfiguredProviderModel => {
        if (
          !isRecord(m) ||
          !hasOnlyKeys(m, ['id', 'name', 'enabled', 'available']) ||
          !isNonEmptyString(m.id) ||
          !isNonEmptyString(m.name) ||
          typeof m.enabled !== 'boolean' ||
          typeof m.available !== 'boolean' ||
          modelIds.has(m.id)
        ) {
          throw new ProviderError('storage-corrupt')
        }
        modelIds.add(m.id)
        return {
          id: m.id,
          name: m.name,
          enabled: m.enabled,
          available: m.available,
        }
      })
      return freezeProvider({ ...settingsOnly(p), id: entry.id, models })
    })
    const selected = parsed.currentSelection
    if (
      selected !== null &&
      (!isRecord(selected) ||
        !hasOnlyKeys(selected, ['providerId', 'modelId']) ||
        typeof selected.providerId !== 'string' ||
        !UUIDPattern.test(selected.providerId) ||
        !isNonEmptyString(selected.modelId))
    ) {
      throw new ProviderError('storage-corrupt')
    }
    return {
      version: 1,
      providers: Object.freeze(providers),
      currentSelection: validSelection(
        providers,
        selected as ProviderModelSelection | null
      ),
    }
  } catch {
    // Never silently filter damaged entries or turn a parse failure into [];
    // never include the raw JSON (which may contain an accidentally pasted key).
    throw new ProviderError('storage-corrupt')
  }
}

function secretKey(providerId: string): string {
  return `commit-generation-provider:${providerId}`
}

function secretBinding(provider: ProviderSettings): string {
  // The OS store and localStorage are not transactional. Keep this binding in
  // the secure value, not JSON: after a failed metadata write getSecret cannot
  // send a newly saved credential to the old origin/protocol/authentication.
  return `gitowl-provider-v1\n${provider.format}\n${provider.authKind}\n${
    new URL(provider.baseUrl).origin
  }\n`
}

/**
 * Stable UUID + independently editable name + OS credential separation follows
 * upstream/Desktop Plus BYOK. Discovery and enabled/available state are new;
 * the reference fork has neither model discovery nor enabled flags.
 */
export class ProviderStore {
  private state: ProviderState
  private pendingChanges: Promise<void> = Promise.resolve()

  public constructor(
    private readonly secureStore: ISecureStore = TokenStore,
    private readonly storage: IDataStore = nativeStorage()
  ) {
    let raw: string | null
    try {
      raw = storage.getItem(ProviderStorageKey)
    } catch {
      throw new ProviderError('storage-unavailable')
    }
    this.state = parseState(raw)
  }

  public getProviders(): ReadonlyArray<ConfiguredProvider> {
    return this.state.providers
  }

  public getProvider(providerId: string): ConfiguredProvider | null {
    return this.state.providers.find(p => p.id === providerId) ?? null
  }

  /** No default model or provider fallback when the stored selection expires. */
  public get currentSelection(): ProviderModelSelection | null {
    return validSelection(this.state.providers, this.state.currentSelection)
  }

  public getEnabledModels(): ReadonlyArray<EnabledProviderModel> {
    return this.state.providers.flatMap(provider =>
      provider.models
        .filter(model => model.enabled && model.available)
        .map(model => ({
          providerId: provider.id,
          providerName: provider.name,
          modelId: model.id,
          modelName: model.name,
        }))
    )
  }

  /** Bind credential reads to an optional immutable request snapshot. */
  public async getSecret(
    providerId: string,
    expected?: ConfiguredProvider
  ): Promise<string | null> {
    await this.pendingChanges
    const provider = this.requireProvider(providerId)
    if (expected !== undefined && provider !== expected) {
      throw new ProviderError('invalid-configuration')
    }
    const secret = await this.readBoundSecret(provider)
    // A save can run while OS keychain I/O is pending. Do not combine the new
    // key with a caller's old host/protocol, even when that save succeeded.
    if (expected !== undefined && this.getProvider(providerId) !== expected) {
      throw new ProviderError('invalid-configuration')
    }
    return secret
  }

  /** Create/edit settings. Omit secret only to retain an unchanged credential. */
  public saveProvider(
    input: ProviderSettings & { readonly id?: string },
    secret?: string
  ): Promise<ConfiguredProvider> {
    return this.change(async () => {
      validateProviderSettings(input)
      const settings = settingsOnly(input)
      const existing =
        input.id === undefined ? null : this.requireProvider(input.id)
      const id = existing?.id ?? randomUUID()
      if (settings.authKind === 'none') {
        if (secret !== undefined && secret !== '') {
          throw new ProviderError('invalid-configuration')
        }
        if (existing !== null) {
          await this.removeSecret(id)
        }
      } else if (secret !== undefined) {
        validateProviderSecret(secret)
        try {
          await this.secureStore.setItem(
            ProviderKeychainService,
            secretKey(id),
            secretBinding(settings) + secret
          )
        } catch {
          throw new ProviderError('secret-storage-failed')
        }
      } else if (
        requiresNewProviderSecret(settings, existing) ||
        (await this.readBoundSecret(existing!)) === null
      ) {
        throw new ProviderError('secret-required')
      }
      const changedEndpoint =
        existing !== null &&
        (existing.format !== settings.format ||
          existing.baseUrl !== settings.baseUrl ||
          existing.authKind !== settings.authKind)
      const provider = freezeProvider({
        ...settings,
        id,
        models: (existing?.models ?? []).map(model =>
          changedEndpoint ? { ...model, available: false } : model
        ),
      })
      this.replaceProvider(provider)
      return provider
    })
  }

  /** Renaming changes only presentation, never identity, catalog or credentials. */
  public renameProvider(
    providerId: string,
    name: string
  ): Promise<ConfiguredProvider> {
    return this.change(() => {
      if (!isNonEmptyString(name)) {
        throw new ProviderError('invalid-configuration')
      }
      const provider = freezeProvider({
        ...this.requireProvider(providerId),
        name: name.trim(),
      })
      this.replaceProvider(provider)
      return provider
    })
  }

  /** Secure deletion must succeed before forgetting the manageable record. */
  public deleteProvider(providerId: string): Promise<void> {
    return this.change(async () => {
      this.requireProvider(providerId)
      await this.removeSecret(providerId)
      this.persist(this.state.providers.filter(p => p.id !== providerId))
    })
  }

  /**
   * Apply a complete discovery result (or explicitly entered Azure deployments).
   * New entries are disabled. Missing entries keep their flags but are unavailable.
   * Call only after discovery fully succeeds, never with a partial failed list.
   */
  public updateModels(
    providerId: string,
    discovered: ReadonlyArray<DiscoveredProviderModel>,
    expected?: ConfiguredProvider,
    signal?: AbortSignal
  ): Promise<ConfiguredProvider> {
    return this.change(() => {
      const provider = this.requireProvider(providerId)
      // Check inside the serialized mutation, not before joining its queue:
      // a queued connection/key edit must win over an old catalog response.
      if (signal?.aborted) {
        throw new ProviderError('cancelled')
      }
      if (expected !== undefined && provider !== expected) {
        throw new ProviderError('invalid-configuration')
      }
      const ids = new Set<string>()
      const previous = new Map(provider.models.map(m => [m.id, m]))
      const models = discovered.map(model => {
        if (
          !isNonEmptyString(model.id) ||
          !isNonEmptyString(model.name) ||
          ids.has(model.id)
        ) {
          throw new ProviderError('invalid-response')
        }
        ids.add(model.id)
        return {
          id: model.id,
          name: model.name,
          enabled: previous.get(model.id)?.enabled ?? false,
          available: true,
        }
      })
      models.push(
        ...provider.models
          .filter(m => !ids.has(m.id))
          .map(m => ({ ...m, available: false }))
      )
      const updated = freezeProvider({ ...provider, models })
      this.replaceProvider(updated)
      return updated
    })
  }

  public setModelEnabled(
    providerId: string,
    modelId: string,
    enabled: boolean
  ): Promise<void> {
    return this.change(() => {
      const provider = this.requireProvider(providerId)
      if (
        typeof enabled !== 'boolean' ||
        !provider.models.some(m => m.id === modelId)
      ) {
        throw new ProviderError('model-not-found')
      }
      this.replaceProvider(
        freezeProvider({
          ...provider,
          models: provider.models.map(m =>
            m.id === modelId ? { ...m, enabled } : m
          ),
        })
      )
    })
  }

  public setCurrentSelection(
    selection: ProviderModelSelection | null
  ): Promise<void> {
    return this.change(() => {
      const selected = validSelection(this.state.providers, selection)
      if (selection !== null && selected === null) {
        throw new ProviderError('invalid-selection')
      }
      this.persist(this.state.providers, selected)
    })
  }

  private requireProvider(providerId: string): ConfiguredProvider {
    const provider = this.getProvider(providerId)
    if (provider === null) {
      throw new ProviderError('provider-not-found')
    }
    return provider
  }

  private async readBoundSecret(
    provider: ConfiguredProvider
  ): Promise<string | null> {
    if (provider.authKind === 'none') {
      return null
    }
    let stored: string | null
    try {
      stored = await this.secureStore.getItem(
        ProviderKeychainService,
        secretKey(provider.id)
      )
    } catch {
      throw new ProviderError('secret-storage-failed')
    }
    if (stored === null) {
      return null
    }
    const prefix = secretBinding(provider)
    if (!stored.startsWith(prefix)) {
      throw new ProviderError('secret-required')
    }
    const secret = stored.slice(prefix.length)
    validateProviderSecret(secret)
    return secret
  }

  private async removeSecret(providerId: string): Promise<void> {
    try {
      const key = secretKey(providerId)
      // keytar returns false for an absent password, not only a failed deletion.
      // Check absence explicitly so users can also remove a record with a lost key.
      const stored = await this.secureStore.getItem(
        ProviderKeychainService,
        key
      )
      if (
        stored !== null &&
        !(await this.secureStore.deleteItem(ProviderKeychainService, key))
      ) {
        throw new ProviderError('secret-storage-failed')
      }
    } catch {
      throw new ProviderError('secret-storage-failed')
    }
  }

  private replaceProvider(provider: ConfiguredProvider): void {
    const exists = this.state.providers.some(p => p.id === provider.id)
    this.persist(
      exists
        ? this.state.providers.map(p => (p.id === provider.id ? provider : p))
        : [...this.state.providers, provider]
    )
  }

  private persist(
    providers: ReadonlyArray<ConfiguredProvider>,
    selection: ProviderModelSelection | null = this.state.currentSelection
  ): void {
    const state: ProviderState = {
      version: 1,
      providers: Object.freeze([...providers]),
      currentSelection: validSelection(providers, selection),
    }
    try {
      this.storage.setItem(ProviderStorageKey, JSON.stringify(state))
    } catch {
      throw new ProviderError('storage-unavailable')
    }
    this.state = state
  }

  private change<T>(action: () => T | Promise<T>): Promise<T> {
    // Serializing metadata with async keychain operations prevents overlapping
    // saves/deletes from associating one operation's secret with another's origin.
    const result = this.pendingChanges.then(action)
    this.pendingChanges = result.then(
      () => {},
      () => {}
    )
    return result
  }
}

import { PopupType } from '../models/popup'
import { PreferencesTab } from '../models/preferences'
import type { AppRuntime } from '../ui/app-runtime'
import type { IAppState } from '../lib/app-state'
import type {
  ProviderSettings,
  ProviderModelSelection,
  DiscoveredProviderModel,
} from '../lib/commit-generation/provider-types'
import {
  ProviderFormats,
  getSupportedProviderAuthKinds,
} from '../lib/commit-generation/provider-types'
import { isRecord } from '../lib/is-record'

export type KernelGenerationSettings = NonNullable<
  IAppState['commitGeneration']
>
export type NativeGenerationCommand =
  | { readonly name: 'generation.copilot.settings' }
  | {
      readonly name: 'generation.provider.save'
      readonly provider: ProviderSettings & { readonly id?: string }
      readonly secret?: string
    }
  | {
      readonly name: 'generation.provider.rename'
      readonly id: string
      readonly providerName: string
    }
  | { readonly name: 'generation.provider.delete'; readonly id: string }
  | { readonly name: 'generation.provider.models.refresh'; readonly id: string }
  | {
      readonly name: 'generation.provider.models.enabled'
      readonly providerId: string
      readonly modelId: string
      readonly enabled: boolean
    }
  | {
      readonly name: 'generation.provider.deployments.set'
      readonly id: string
      readonly models: ReadonlyArray<DiscoveredProviderModel>
    }
  | {
      readonly name: 'generation.select'
      readonly source: 'copilot' | 'custom'
      readonly selection: ProviderModelSelection | null
    }
  | { readonly name: 'generation.language'; readonly language: string }

const text = (value: unknown): value is string =>
  typeof value === 'string' && !value.includes('\0')
const only = (value: Record<string, unknown>, keys: ReadonlyArray<string>) =>
  Object.keys(value).every(key => keys.includes(key))

/** Admission shape only. Original ProviderStore validates settings and identities. */
export function parseGenerationCommand(
  value: Record<string, unknown>
): NativeGenerationCommand | null {
  const exact = (keys: ReadonlyArray<string>) => only(value, ['name', ...keys])
  let valid = false
  switch (value.name) {
    case 'generation.copilot.settings':
      valid = exact([])
      break
    case 'generation.provider.save': {
      const provider = value.provider
      valid =
        exact(['provider', 'secret']) &&
        (value.secret === undefined || text(value.secret)) &&
        isRecord(provider) &&
        only(provider, [
          'id',
          'name',
          'format',
          'baseUrl',
          'authKind',
          'requestTimeoutSeconds',
        ]) &&
        (provider.id === undefined || text(provider.id)) &&
        text(provider.name) &&
        text(provider.baseUrl) &&
        ProviderFormats.some(
          format =>
            format === provider.format &&
            getSupportedProviderAuthKinds(format).some(
              auth => auth === provider.authKind
            )
        ) &&
        (provider.requestTimeoutSeconds === undefined ||
          (typeof provider.requestTimeoutSeconds === 'number' &&
            Number.isFinite(provider.requestTimeoutSeconds)))
      break
    }
    case 'generation.provider.rename':
      valid =
        exact(['id', 'providerName']) &&
        text(value.id) &&
        text(value.providerName)
      break
    case 'generation.provider.delete':
    case 'generation.provider.models.refresh':
      valid = exact(['id']) && text(value.id)
      break
    case 'generation.provider.models.enabled':
      valid =
        exact(['providerId', 'modelId', 'enabled']) &&
        text(value.providerId) &&
        text(value.modelId) &&
        typeof value.enabled === 'boolean'
      break
    case 'generation.provider.deployments.set':
      valid =
        exact(['id', 'models']) &&
        text(value.id) &&
        Array.isArray(value.models) &&
        value.models.every(
          model =>
            isRecord(model) &&
            only(model, ['id', 'name']) &&
            text(model.id) &&
            text(model.name)
        )
      break
    case 'generation.select': {
      const selection = value.selection
      valid =
        exact(['source', 'selection']) &&
        (value.source === 'copilot' || value.source === 'custom') &&
        (selection === null ||
          (isRecord(selection) &&
            only(selection, ['providerId', 'modelId']) &&
            text(selection.providerId) &&
            text(selection.modelId)))
      break
    }
    case 'generation.language':
      valid = exact(['language']) && text(value.language)
      break
    default:
      return null
  }
  if (!valid) throw new Error('Invalid generation command parameters')
  return value as unknown as NativeGenerationCommand
}

export function projectGenerationSettings(
  settings: IAppState['commitGeneration']
): KernelGenerationSettings | null {
  if (settings === undefined) return null
  return {
    providers: settings.providers.map(provider => ({
      id: provider.id,
      name: provider.name,
      format: provider.format,
      baseUrl: provider.baseUrl,
      authKind: provider.authKind,
      ...(provider.requestTimeoutSeconds === undefined
        ? {}
        : { requestTimeoutSeconds: provider.requestTimeoutSeconds }),
      models: provider.models.map(model => ({
        id: model.id,
        name: model.name,
        enabled: model.enabled,
        available: model.available,
      })),
    })),
    enabledModels: settings.enabledModels.map(model => ({
      providerId: model.providerId,
      modelId: model.modelId,
      providerName: model.providerName,
      modelName: model.modelName,
    })),
    selection:
      settings.selection === null
        ? null
        : {
            providerId: settings.selection.providerId,
            modelId: settings.selection.modelId,
          },
    source: settings.source,
    language: settings.language,
  }
}

/** All mutations remain explicit original Dispatcher extensions, never a service proxy. */
export async function executeGenerationCommand(
  runtime: AppRuntime,
  command: NativeGenerationCommand
) {
  const dispatcher = runtime.dispatcher
  switch (command.name) {
    case 'generation.copilot.settings':
      await dispatcher.showPopup({
        type: PopupType.Preferences,
        initialSelectedTab: PreferencesTab.Copilot,
      })
      return
    case 'generation.provider.save':
      await dispatcher.saveCommitGenerationProvider(
        command.provider,
        command.secret
      )
      return
    case 'generation.provider.rename':
      await dispatcher.renameCommitGenerationProvider(
        command.id,
        command.providerName
      )
      return
    case 'generation.provider.delete':
      await dispatcher.deleteCommitGenerationProvider(command.id)
      return
    case 'generation.provider.models.refresh':
      await dispatcher.refreshCommitGenerationModels(command.id)
      return
    case 'generation.provider.models.enabled':
      await dispatcher.setCommitGenerationModelEnabled(
        command.providerId,
        command.modelId,
        command.enabled
      )
      return
    case 'generation.provider.deployments.set':
      await dispatcher.setCommitGenerationDeployments(
        command.id,
        command.models
      )
      return
    case 'generation.select':
      await dispatcher.selectCommitGenerationModel(
        command.source,
        command.selection
      )
      return
    case 'generation.language':
      dispatcher.setCommitGenerationLanguage(command.language)
      return
  }
}

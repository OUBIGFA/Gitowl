import { isIPv4 } from 'net'

/** Wire protocols, not provider brands or an assumed OpenAI-compatible API. */
export const ProviderFormats = [
  'openai-responses',
  'openai-chat',
  'anthropic-messages',
  'gemini',
  'azure-openai-v1',
  'ollama',
] as const

export type ProviderFormat = typeof ProviderFormats[number]
export type ProviderAuthKind = 'apiKey' | 'bearer' | 'none'

/** Editable metadata only. Credentials must never be added to this object. */
export type ProviderSettings = {
  readonly name: string
  readonly format: ProviderFormat
  /**
   * OpenAI/Azure: API root including /v1 or /openai/v1. Anthropic/Gemini/
   * Ollama: service root; their official SDK adds the protocol's API path.
   */
  readonly baseUrl: string
  readonly authKind: ProviderAuthKind
  readonly requestTimeoutSeconds?: number
}

/** A provider's exact model ID (an explicitly entered deployment name on Azure). */
export type DiscoveredProviderModel = {
  readonly id: string
  readonly name: string
}

export type ConfiguredProviderModel = DiscoveredProviderModel & {
  readonly enabled: boolean
  /** Disappearing models remain manageable, but cannot be selected or used. */
  readonly available: boolean
}

/** UUID identity survives renames. This is safe to persist in localStorage. */
export type ConfiguredProvider = ProviderSettings & {
  readonly id: string
  readonly models: ReadonlyArray<ConfiguredProviderModel>
}

export type ProviderModelSelection = {
  readonly providerId: string
  readonly modelId: string
}

/** The only custom models offered on the home page. */
export type EnabledProviderModel = ProviderModelSelection & {
  readonly providerName: string
  readonly modelName: string
}

export const DefaultProviderRequestTimeoutSeconds = 60

export type ProviderErrorCode =
  | 'invalid-configuration'
  | 'unsupported-auth'
  | 'secret-required'
  | 'provider-not-found'
  | 'model-not-found'
  | 'invalid-selection'
  | 'stale-request'
  | 'storage-corrupt'
  | 'storage-unavailable'
  | 'secret-storage-failed'
  | 'discovery-unsupported'
  | 'request-failed'
  | 'cancelled'
  | 'timeout'
  | 'invalid-response'
  | 'truncated-response'
  | 'refused-response'
  | 'tool-response'
  | 'unexpected-origin'

const ErrorMessages: Readonly<Record<ProviderErrorCode, string>> = {
  'invalid-configuration': 'The provider configuration is invalid.',
  'unsupported-auth':
    'This protocol does not support the selected authentication mode.',
  'secret-required':
    'Enter a credential for this provider, authentication mode and origin.',
  'provider-not-found': 'The configured provider no longer exists.',
  'model-not-found': 'The configured model no longer exists.',
  'invalid-selection': 'Select an explicitly enabled, available model.',
  'stale-request':
    'The generation settings or selected input changed. Generate again explicitly.',
  'storage-corrupt':
    'The saved provider configuration is damaged or has an unsupported version.',
  'storage-unavailable':
    'Provider local storage is unavailable; no configuration was saved.',
  'secret-storage-failed':
    'The provider credential could not be read, saved or deleted in the OS secure store.',
  'discovery-unsupported':
    'Azure v1 model catalogs do not identify your deployments. Enter a deployment name explicitly.',
  'request-failed': 'The provider request failed.',
  cancelled: 'The provider request was cancelled.',
  timeout: 'The provider request timed out.',
  'invalid-response':
    'The provider did not return a complete text response in the selected protocol.',
  'truncated-response': 'The provider stopped before completing the response.',
  'refused-response': 'The provider refused the request.',
  'tool-response':
    'The provider returned a tool call instead of a text-only response.',
  'unexpected-origin': 'The SDK request targeted an unexpected origin.',
}

/**
 * Only fixed diagnostics and an HTTP status escape this boundary. SDK/keychain
 * errors may echo credentials, prompts or response bodies, so never attach them
 * as a message, cause, property or log argument.
 */
export class ProviderError extends Error {
  public readonly status: number | undefined

  public constructor(public readonly code: ProviderErrorCode, status?: number) {
    const safeStatus =
      Number.isInteger(status) && status! >= 100 && status! <= 599
        ? status
        : undefined
    super(
      ErrorMessages[code] +
        (safeStatus === undefined ? '' : ` (HTTP ${safeStatus})`)
    )
    this.name = 'ProviderError'
    this.status = safeStatus
  }
}

export function isProviderFormat(value: unknown): value is ProviderFormat {
  return ProviderFormats.some(format => format === value)
}

const AllAuthKinds: ReadonlyArray<ProviderAuthKind> = Object.freeze([
  'apiKey',
  'bearer',
  'none',
])
const GeminiAuthKinds: ReadonlyArray<ProviderAuthKind> = Object.freeze([
  'apiKey',
])

/** Gemini here is the Developer API, not a pretend Vertex/IAM integration. */
export function getSupportedProviderAuthKinds(
  format: ProviderFormat
): ReadonlyArray<ProviderAuthKind> {
  return format === 'gemini' ? GeminiAuthKinds : AllAuthKinds
}

/** Keep the upstream BYOK HTTPS/loopback policy; disallow credentials in URLs. */
export function isValidProviderBaseUrl(value: unknown): value is string {
  if (typeof value !== 'string') {
    return false
  }
  try {
    const url = new URL(value)
    if (url.username || url.password || url.search || url.hash) {
      return false
    }
    const local =
      url.hostname === 'localhost' ||
      url.hostname === '[::1]' ||
      url.hostname === '::1' ||
      (isIPv4(url.hostname) && url.hostname.startsWith('127.'))
    return url.protocol === 'https:' || (url.protocol === 'http:' && local)
  } catch {
    return false
  }
}

/** Validate untrusted settings once, before persisting or instantiating an SDK. */
export function validateProviderSettings(
  value: unknown
): asserts value is ProviderSettings {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new ProviderError('invalid-configuration')
  }
  const p = value as Record<string, unknown>
  if (
    typeof p.name !== 'string' ||
    p.name.trim().length === 0 ||
    !isProviderFormat(p.format) ||
    !isValidProviderBaseUrl(p.baseUrl) ||
    (p.authKind !== 'apiKey' &&
      p.authKind !== 'bearer' &&
      p.authKind !== 'none') ||
    (p.requestTimeoutSeconds !== undefined &&
      (typeof p.requestTimeoutSeconds !== 'number' ||
        !Number.isFinite(p.requestTimeoutSeconds) ||
        p.requestTimeoutSeconds <= 0 ||
        p.requestTimeoutSeconds * 1000 > 2_147_483_647))
  ) {
    throw new ProviderError('invalid-configuration')
  }
  if (!getSupportedProviderAuthKinds(p.format).includes(p.authKind)) {
    throw new ProviderError('unsupported-auth')
  }
}

/** A rename or timeout edit cannot accidentally move a credential to a new host. */
export function requiresNewProviderSecret(
  settings: ProviderSettings,
  existing: ConfiguredProvider | null
): boolean {
  return (
    settings.authKind !== 'none' &&
    (existing === null ||
      settings.authKind !== existing.authKind ||
      settings.format !== existing.format ||
      new URL(settings.baseUrl).origin !== new URL(existing.baseUrl).origin)
  )
}

/** Canonical BCP47 only; arbitrary instruction text can never enter a trusted system prompt. */
export function canonicalCommitGenerationLanguage(value: string): string {
  if (value === 'auto') {
    return value
  }
  try {
    const language = Intl.getCanonicalLocales(value)[0]
    if (language === undefined) {
      throw new Error('empty language')
    }
    return language
  } catch {
    throw new ProviderError('invalid-configuration')
  }
}

/** A blank or multiline value is not a usable HTTP credential. Never echo it. */
export function validateProviderSecret(
  secret: unknown
): asserts secret is string {
  if (
    typeof secret !== 'string' ||
    secret.trim().length === 0 ||
    /[\r\n]/.test(secret)
  ) {
    throw new ProviderError('secret-required')
  }
}

import OpenAI from 'openai'
import Anthropic from '@anthropic-ai/sdk'
import {
  ApiError,
  BlockedReason,
  FinishReason,
  GoogleGenAI,
} from '@google/genai'
import type { GenerateContentResponse } from '@google/genai'
import { Ollama } from 'ollama'
import type { ChatResponse } from 'ollama'
import {
  ConfiguredProvider,
  DefaultProviderRequestTimeoutSeconds,
  DiscoveredProviderModel,
  ProviderError,
  validateProviderSecret,
  validateProviderSettings,
} from './provider-types'

/**
 * The documented fetch customization is only a transport boundary: the SDK
 * still owns paths, authentication, serialization, pagination and API errors.
 * Bind real HTTP/body cancellation to the operation deadline (including Ollama
 * non-stream calls, which its abort() does not cancel). Do not follow redirects
 * carrying a provider credential, or inherit an environment key in none mode.
 */
function requestFetch(
  provider: ConfiguredProvider,
  signal: AbortSignal
): typeof fetch {
  const origin = new URL(provider.baseUrl).origin
  return (input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input))
    if (url.origin !== origin) {
      throw new ProviderError('unexpected-origin')
    }
    const headers = new Headers(init?.headers)
    if (provider.authKind === 'none') {
      for (const name of [
        'authorization',
        'api-key',
        'x-api-key',
        'x-goog-api-key',
      ]) {
        headers.delete(name)
      }
    }
    return fetch(input, {
      ...init,
      headers,
      redirect: 'error',
      signal: init?.signal ? AbortSignal.any([signal, init.signal]) : signal,
    })
  }
}

function httpStatus(error: unknown): number | undefined {
  if (
    error instanceof OpenAI.APIError ||
    error instanceof Anthropic.APIError ||
    error instanceof ApiError
  ) {
    return error.status
  }
  // Ollama's ResponseError is not exported; status_code is its public field.
  if (
    typeof error === 'object' &&
    error !== null &&
    'status_code' in error &&
    typeof error.status_code === 'number'
  ) {
    return error.status_code
  }
  return undefined
}

async function withRequest<T>(
  provider: ConfiguredProvider,
  secret: string | null,
  signal: AbortSignal | undefined,
  action: (signal: AbortSignal, timeoutMs: number) => Promise<T>
): Promise<T> {
  if (signal?.aborted) {
    throw new ProviderError('cancelled')
  }
  validateProviderSettings(provider)
  if (provider.authKind !== 'none') {
    validateProviderSecret(secret)
  }
  const timeoutMs = Math.ceil(
    (provider.requestTimeoutSeconds ?? DefaultProviderRequestTimeoutSeconds) *
      1000
  )
  const controller = new AbortController()
  let timedOut = false
  const onAbort = () => controller.abort()
  signal?.addEventListener('abort', onAbort, { once: true })
  const timer = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, timeoutMs)
  try {
    if (signal?.aborted) {
      controller.abort()
    }
    const result = await action(controller.signal, timeoutMs)
    if (controller.signal.aborted) {
      throw new ProviderError(timedOut ? 'timeout' : 'cancelled')
    }
    return result
  } catch (error) {
    if (controller.signal.aborted) {
      throw new ProviderError(timedOut ? 'timeout' : 'cancelled')
    }
    if (error instanceof ProviderError) {
      throw error
    }
    if (
      error instanceof OpenAI.APIConnectionTimeoutError ||
      error instanceof Anthropic.APIConnectionTimeoutError
    ) {
      throw new ProviderError('timeout')
    }
    // In particular, never let an upstream 401 that echoes a key into an error,
    // cause or logger. Only the HTTP status is safe to propagate.
    throw new ProviderError('request-failed', httpStatus(error))
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', onAbort)
  }
}

function openAIClient(
  provider: ConfiguredProvider,
  secret: string | null,
  signal: AbortSignal,
  timeout: number
): OpenAI {
  const azureKey =
    provider.format === 'azure-openai-v1' && provider.authKind === 'apiKey'
  return new OpenAI({
    // The original Desktop kernel is a trusted Node-enabled Electron renderer,
    // not a website. Secrets remain in its OS-backed store and never reach MyGo.
    dangerouslyAllowBrowser:
      typeof process !== 'undefined' && process.versions.electron !== undefined,
    baseURL: provider.baseUrl,
    // The SDK constructor requires credentials even for an explicitly
    // unauthenticated custom endpoint. This non-secret placeholder is omitted
    // using its documented null-header option and never goes on the wire.
    apiKey: provider.authKind === 'none' ? 'unused' : secret,
    adminAPIKey: null,
    organization: null,
    project: null,
    webhookSecret: null,
    maxRetries: 0,
    timeout,
    logLevel: 'off',
    defaultHeaders: {
      Authorization:
        provider.authKind === 'none' || azureKey ? null : `Bearer ${secret}`,
      // Azure v1 REST docs accept api-key; bearer is reserved for token auth.
      'api-key': azureKey ? secret : null,
      'x-api-key': null,
      'x-goog-api-key': null,
    },
    fetch: requestFetch(provider, signal),
  })
}

function anthropicClient(
  provider: ConfiguredProvider,
  secret: string | null,
  signal: AbortSignal,
  timeout: number
): Anthropic {
  return new Anthropic({
    dangerouslyAllowBrowser:
      typeof process !== 'undefined' && process.versions.electron !== undefined,
    baseURL: provider.baseUrl,
    apiKey:
      provider.authKind === 'bearer'
        ? null
        : provider.authKind === 'none'
        ? 'unused'
        : secret,
    authToken: provider.authKind === 'bearer' ? secret : null,
    webhookKey: null,
    maxRetries: 0,
    timeout,
    logLevel: 'off',
    openTelemetry: false,
    // SDK 1.6.0 / API 2026-10-21 use Bearer for both API keys and tokens.
    // Explicit options suppress environment credentials and lazy auth chains.
    defaultHeaders: {
      Authorization: provider.authKind === 'none' ? null : `Bearer ${secret}`,
      'x-api-key': null,
      'api-key': null,
      'x-goog-api-key': null,
    },
    fetch: requestFetch(provider, signal),
  })
}

function geminiClient(
  provider: ConfiguredProvider,
  secret: string | null,
  signal: AbortSignal
): GoogleGenAI {
  return new GoogleGenAI({
    apiKey: secret!,
    enterprise: false,
    vertexai: false,
    // genai 3.0.0's documented stable Developer API default, made explicit so
    // an environment variable cannot silently select a different API/backend.
    apiVersion: 'v1',
    httpOptions: {
      baseUrl: provider.baseUrl,
      retryOptions: { attempts: 1 },
      // One operation deadline above also bounds pagination/body reading;
      // there is no separate per-page timeout or abandoned Promise race.
      fetch: requestFetch(provider, signal),
    },
  })
}

function ollamaClient(
  provider: ConfiguredProvider,
  secret: string | null,
  signal: AbortSignal
): Ollama {
  return new Ollama({
    host: provider.baseUrl,
    headers:
      provider.authKind === 'none' ? {} : { Authorization: `Bearer ${secret}` },
    fetch: requestFetch(provider, signal),
  })
}

/**
 * Fetch the complete catalog using each protocol's actual official SDK. Azure
 * inference model catalogs are not deployment discovery; ARM deployment listing
 * needs separate resource configuration/permissions, so explicitly fail there.
 */
export async function discoverProviderModels(
  provider: ConfiguredProvider,
  secret: string | null,
  signal?: AbortSignal
): Promise<ReadonlyArray<DiscoveredProviderModel>> {
  if (provider.format === 'azure-openai-v1') {
    throw new ProviderError('discovery-unsupported')
  }
  return withRequest(
    provider,
    secret,
    signal,
    async (requestSignal, timeout) => {
      const models: DiscoveredProviderModel[] = []
      const ids = new Set<string>()
      const add = (id: unknown, name: unknown) => {
        if (
          typeof id !== 'string' ||
          id.trim().length === 0 ||
          typeof name !== 'string' ||
          name.trim().length === 0 ||
          ids.has(id)
        ) {
          throw new ProviderError('invalid-response')
        }
        ids.add(id)
        models.push({ id, name })
      }
      switch (provider.format) {
        case 'openai-responses':
        case 'openai-chat': {
          const page = await openAIClient(
            provider,
            secret,
            requestSignal,
            timeout
          ).models.list({ signal: requestSignal })
          // Official OpenAI Models.list is currently a single Page, not a cursor
          // API. Do not invent has_more/after pagination for compatible servers.
          if (page.object !== 'list' || !Array.isArray(page.data)) {
            throw new ProviderError('invalid-response')
          }
          for (const model of page.data) {
            add(model.id, model.id)
          }
          break
        }
        case 'anthropic-messages': {
          const client = anthropicClient(
            provider,
            secret,
            requestSignal,
            timeout
          )
          let page = await client.models.list(
            { limit: 100 },
            { signal: requestSignal }
          )
          const cursors = new Set<string>()
          while (true) {
            if (
              !Array.isArray(page.data) ||
              typeof page.has_more !== 'boolean'
            ) {
              throw new ProviderError('invalid-response')
            }
            for (const model of page.data) {
              if (model.lifecycle !== 'retired') {
                add(model.id, model.display_name)
              }
            }
            if (!page.has_more) {
              break
            }
            if (!page.last_id || cursors.has(page.last_id)) {
              throw new ProviderError('invalid-response')
            }
            cursors.add(page.last_id)
            page = await page.getNextPage()
          }
          break
        }
        case 'gemini': {
          const pager = await geminiClient(
            provider,
            secret,
            requestSignal
          ).models.list({
            config: { pageSize: 100, abortSignal: requestSignal },
          })
          const cursors = new Set<string>()
          // Use the public page/nextPage API, including empty intermediate pages,
          // and reject a looping token rather than claiming a partial catalog.
          while (true) {
            if (!Array.isArray(pager.page)) {
              throw new ProviderError('invalid-response')
            }
            for (const model of pager.page) {
              if (model.supportedActions?.includes('generateContent')) {
                add(model.name, model.displayName ?? model.name)
              }
            }
            if (!pager.hasNextPage()) {
              break
            }
            const cursor = pager.params.config?.pageToken
            if (
              typeof cursor !== 'string' ||
              cursor.length === 0 ||
              cursors.has(cursor)
            ) {
              throw new ProviderError('invalid-response')
            }
            cursors.add(cursor)
            await pager.nextPage()
          }
          break
        }
        case 'ollama': {
          const response = await ollamaClient(
            provider,
            secret,
            requestSignal
          ).list()
          if (!Array.isArray(response.models)) {
            throw new ProviderError('invalid-response')
          }
          for (const model of response.models) {
            add(model.model ?? model.name, model.name)
          }
          break
        }
        case 'azure-openai-v1':
          throw new ProviderError('discovery-unsupported')
      }
      return models
    }
  )
}

function requireText(text: unknown): string {
  if (typeof text !== 'string' || text.trim().length === 0) {
    throw new ProviderError('invalid-response')
  }
  return text
}

function responsesText(response: OpenAI.Responses.Response): string {
  if (response.incomplete_details?.reason === 'content_filter') {
    throw new ProviderError('refused-response')
  }
  if (response.status === 'incomplete' || response.incomplete_details != null) {
    throw new ProviderError('truncated-response')
  }
  if (
    response.status !== 'completed' ||
    response.error != null ||
    !Array.isArray(response.output)
  ) {
    throw new ProviderError('invalid-response')
  }
  let text = ''
  for (const item of response.output) {
    if (item.type === 'reasoning') {
      if (item.status !== undefined && item.status !== 'completed') {
        throw new ProviderError('truncated-response')
      }
      continue
    }
    if (item.type !== 'message') {
      throw new ProviderError('tool-response')
    }
    if (item.status !== 'completed') {
      throw new ProviderError('truncated-response')
    }
    if (item.role !== 'assistant' || !Array.isArray(item.content)) {
      throw new ProviderError('invalid-response')
    }
    for (const part of item.content) {
      if (part.type === 'refusal') {
        throw new ProviderError('refused-response')
      }
      if (part.type !== 'output_text' || typeof part.text !== 'string') {
        throw new ProviderError('invalid-response')
      }
      // Reasoning and intermediate commentary are not the final answer. Concatenating
      // text fragments without adding separators preserves JSON split across blocks.
      if (item.phase !== 'commentary') {
        text += part.text
      }
    }
  }
  return requireText(text)
}

function chatText(response: OpenAI.Chat.Completions.ChatCompletion): string {
  if (!Array.isArray(response.choices) || response.choices.length !== 1) {
    throw new ProviderError('invalid-response')
  }
  const choice = response.choices[0]
  const message = choice.message
  if (choice.finish_reason === 'content_filter' || message?.refusal != null) {
    throw new ProviderError('refused-response')
  }
  if (
    choice.finish_reason === 'tool_calls' ||
    choice.finish_reason === 'function_call' ||
    message?.tool_calls?.length ||
    message?.function_call != null
  ) {
    throw new ProviderError('tool-response')
  }
  if (choice.finish_reason === 'length') {
    throw new ProviderError('truncated-response')
  }
  if (
    choice.finish_reason !== 'stop' ||
    message?.role !== 'assistant' ||
    message.audio != null
  ) {
    throw new ProviderError('invalid-response')
  }
  // Chat Completions has a string/null response content, not Responses blocks.
  return requireText(message.content)
}

function anthropicText(message: Anthropic.Message): string {
  if (
    message.stop_reason === 'refusal' ||
    message.stop_details?.type === 'refusal'
  ) {
    throw new ProviderError('refused-response')
  }
  if (message.stop_reason === 'tool_use') {
    throw new ProviderError('tool-response')
  }
  if (
    message.stop_reason === 'max_tokens' ||
    message.stop_reason === 'model_context_window_exceeded' ||
    message.stop_reason === 'pause_turn' ||
    message.stop_reason === 'stop_sequence'
  ) {
    throw new ProviderError('truncated-response')
  }
  if (
    message.stop_reason !== 'end_turn' ||
    message.role !== 'assistant' ||
    !Array.isArray(message.content)
  ) {
    throw new ProviderError('invalid-response')
  }
  let text = ''
  for (const block of message.content) {
    if (block.type === 'thinking' || block.type === 'redacted_thinking') {
      continue
    }
    if (block.type !== 'text') {
      throw new ProviderError('tool-response')
    }
    if (typeof block.text !== 'string') {
      throw new ProviderError('invalid-response')
    }
    text += block.text
  }
  return requireText(text)
}

function geminiText(response: GenerateContentResponse): string {
  if (
    response.promptFeedback?.blockReason &&
    response.promptFeedback.blockReason !==
      BlockedReason.BLOCKED_REASON_UNSPECIFIED
  ) {
    throw new ProviderError('refused-response')
  }
  if (!Array.isArray(response.candidates) || response.candidates.length !== 1) {
    throw new ProviderError('invalid-response')
  }
  const candidate = response.candidates[0]
  if (
    candidate.finishReason === FinishReason.MAX_TOKENS ||
    candidate.finishReason === FinishReason.CONTINUATION
  ) {
    throw new ProviderError('truncated-response')
  }
  if (
    candidate.finishReason === FinishReason.UNEXPECTED_TOOL_CALL ||
    candidate.finishReason === FinishReason.MALFORMED_FUNCTION_CALL ||
    candidate.finishReason === FinishReason.TOO_MANY_TOOL_CALLS
  ) {
    throw new ProviderError('tool-response')
  }
  if (
    candidate.safetyRatings?.some(rating => rating.blocked) ||
    [
      FinishReason.SAFETY,
      FinishReason.RECITATION,
      FinishReason.BLOCKLIST,
      FinishReason.PROHIBITED_CONTENT,
      FinishReason.SPII,
    ].includes(candidate.finishReason!)
  ) {
    throw new ProviderError('refused-response')
  }
  if (
    candidate.finishReason !== FinishReason.STOP ||
    candidate.content?.role !== 'model' ||
    !Array.isArray(candidate.content.parts)
  ) {
    throw new ProviderError('invalid-response')
  }
  let text = ''
  for (const part of candidate.content.parts) {
    if (
      part.functionCall != null ||
      part.functionResponse != null ||
      part.toolCall != null ||
      part.toolResponse != null ||
      part.executableCode != null ||
      part.codeExecutionResult != null
    ) {
      throw new ProviderError('tool-response')
    }
    if (part.thought) {
      continue
    }
    if (
      part.inlineData != null ||
      part.fileData != null ||
      typeof part.text !== 'string'
    ) {
      throw new ProviderError('invalid-response')
    }
    text += part.text
  }
  // Do not use response.text: its convenience getter discards non-text parts
  // and warns instead of failing for an unexpected tool/multimodal response.
  return requireText(text)
}

function ollamaText(response: ChatResponse): string {
  if (!response.done || response.done_reason === 'length') {
    throw new ProviderError('truncated-response')
  }
  if (response.message?.tool_calls?.length) {
    throw new ProviderError('tool-response')
  }
  if (
    response.done_reason !== 'stop' ||
    response.message?.role !== 'assistant' ||
    response.message.images?.length
  ) {
    throw new ProviderError('invalid-response')
  }
  // Ollama has no standardized refusal flag. Never guess from natural language;
  // the caller's upstream JSON commit parser still rejects a textual refusal.
  return requireText(response.message.content)
}

/**
 * Generate complete raw text with the selected official SDK. The caller reuses
 * the upstream JSON commit parser and existing prompt/diff boundaries. No model
 * fallback, tool execution, retries, JSON repair or synthetic summary here.
 */
export async function generateWithProvider(
  provider: ConfiguredProvider,
  modelId: string,
  secret: string | null,
  systemPrompt: string,
  userPrompt: string,
  signal?: AbortSignal
): Promise<string> {
  return withRequest(
    provider,
    secret,
    signal,
    async (requestSignal, timeout) => {
      if (
        !provider.models.some(
          model =>
            model.id === modelId &&
            model.enabled === true &&
            model.available === true
        )
      ) {
        throw new ProviderError('invalid-selection')
      }
      switch (provider.format) {
        case 'openai-responses':
        case 'azure-openai-v1': {
          // Azure v1 uses standard OpenAI, not the dated AzureOpenAI client;
          // modelId is the user-configured deployment name, never a catalog alias.
          const response = await openAIClient(
            provider,
            secret,
            requestSignal,
            timeout
          ).responses.create(
            {
              model: modelId,
              instructions: systemPrompt,
              input: userPrompt,
              store: false,
              stream: false,
            },
            { signal: requestSignal }
          )
          return responsesText(response)
        }
        case 'openai-chat': {
          const response = await openAIClient(
            provider,
            secret,
            requestSignal,
            timeout
          ).chat.completions.create(
            {
              model: modelId,
              messages: [
                { role: 'system', content: systemPrompt },
                { role: 'user', content: userPrompt },
              ],
              stream: false,
            },
            { signal: requestSignal }
          )
          return chatText(response)
        }
        case 'anthropic-messages': {
          const response = await anthropicClient(
            provider,
            secret,
            requestSignal,
            timeout
          ).messages.create(
            {
              model: modelId,
              system: systemPrompt,
              messages: [{ role: 'user', content: userPrompt }],
              // Required by Messages; do not impose unsupported reasoning/sampling
              // parameters. Reaching this output budget fails instead of using a fragment.
              max_tokens: 4096,
              stream: false,
            },
            { signal: requestSignal }
          )
          return anthropicText(response)
        }
        case 'gemini': {
          const response = await geminiClient(
            provider,
            secret,
            requestSignal
          ).models.generateContent({
            model: modelId,
            contents: [{ role: 'user', parts: [{ text: userPrompt }] }],
            config: {
              systemInstruction: systemPrompt,
              abortSignal: requestSignal,
              // genai 3.0.0 defaults to additional continuation requests. Disable
              // that client feature and fail an incomplete response explicitly.
              automaticContinuation: false,
            },
          })
          return geminiText(response)
        }
        case 'ollama': {
          const response = await ollamaClient(
            provider,
            secret,
            requestSignal
          ).chat({
            model: modelId,
            messages: [
              { role: 'system', content: systemPrompt },
              { role: 'user', content: userPrompt },
            ],
            stream: false,
          })
          return ollamaText(response)
        }
      }
    }
  )
}

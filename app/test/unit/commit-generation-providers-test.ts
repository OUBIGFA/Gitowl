import { listenOnLoopback } from '../helpers/http-server'
import assert from 'node:assert/strict'
import { after, afterEach, before, describe, it, TestContext } from 'node:test'
import { createServer, IncomingHttpHeaders, ServerResponse } from 'http'
import { AddressInfo } from 'net'
import { execFile } from 'child_process'
import { promisify } from 'util'
import { mkdir, mkdtemp, rm } from 'fs/promises'
import { join, resolve } from 'path'
import { AsyncInMemoryStore, InMemoryStore } from '../helpers/stores'
import {
  discoverProviderModels,
  generateWithProvider,
} from '../../src/lib/commit-generation/provider-adapters'
import {
  ProviderKeychainService,
  ProviderStorageKey,
  ProviderStore,
} from '../../src/lib/commit-generation/provider-store'
import {
  ConfiguredProvider,
  ProviderAuthKind,
  ProviderError,
  ProviderErrorCode,
  ProviderFormat,
  ProviderSettings,
} from '../../src/lib/commit-generation/provider-types'

// Public wire fixtures only; these credentials are deliberately fake and every
// SDK request goes to a loopback HTTP server, never a user's or paid endpoint.
const FakeKey = 'fake-key-for-local-provider-tests'
const SystemPrompt =
  'Return title and description as JSON. Output language: 中文.'
const UserPrompt = '<diff-test>+ localized commit form</diff-test>'
const RawText = '{"title":"改进提交","description":"保留原始 diff"}'
const ProviderId = '6419e1c7-68a7-4ef3-9131-822da9527f08'
const DefaultSettings: ProviderSettings = {
  name: 'Custom provider',
  format: 'openai-responses',
  baseUrl: 'https://provider.example/v1',
  authKind: 'apiKey',
}

function hasCode(code: ProviderErrorCode, status?: number) {
  return (error: unknown) => {
    assert.ok(error instanceof ProviderError)
    assert.equal(error.code, code)
    if (status !== undefined) {
      assert.equal(error.status, status)
    }
    assert.ok(!error.message.includes(FakeKey))
    assert.ok(!error.stack?.includes(FakeKey))
    assert.ok(!JSON.stringify(error).includes(FakeKey))
    assert.ok(!('cause' in error))
    return true
  }
}

type WireRequest = {
  readonly method: string
  readonly url: string
  readonly headers: IncomingHttpHeaders
  readonly body: Record<string, any> | undefined
}

async function wireServer(
  t: TestContext,
  respond: (
    request: WireRequest,
    response: ServerResponse
  ) => void | Promise<void>
) {
  const requests: WireRequest[] = []
  const errors: unknown[] = []
  const server = createServer(async (request, response) => {
    try {
      const chunks: Buffer[] = []
      for await (const chunk of request) {
        chunks.push(Buffer.from(chunk))
      }
      const body = Buffer.concat(chunks).toString('utf8')
      const recorded = {
        method: request.method ?? '',
        url: request.url ?? '',
        headers: request.headers,
        body: body === '' ? undefined : JSON.parse(body),
      }
      requests.push(recorded)
      await respond(recorded, response)
    } catch (error) {
      errors.push(error)
      if (!response.headersSent) {
        json(response, { error: 'Local fixture failed' }, 500)
      } else {
        response.destroy()
      }
    }
  })
  await listenOnLoopback(server)
  t.after(async () => {
    server.closeAllConnections()
    await new Promise<void>(resolve => server.close(() => resolve()))
    assert.deepEqual(errors, [])
  })
  return {
    baseUrl: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
    requests,
  }
}

function json(response: ServerResponse, body: unknown, status = 200) {
  // Each case owns a short-lived server. Do not leave a pooled keep-alive
  // socket addressing a closed fixture port that Windows may immediately reuse.
  response.writeHead(status, {
    'Content-Type': 'application/json',
    Connection: 'close',
  })
  response.end(JSON.stringify(body))
}

function responsesBody(output?: unknown[]) {
  return {
    id: 'resp_fixture',
    object: 'response',
    created_at: 1,
    model: 'fixture',
    status: 'completed',
    error: null,
    incomplete_details: null,
    output: output ?? [
      {
        type: 'reasoning',
        id: 'reasoning_fixture',
        status: 'completed',
        summary: [{ type: 'summary_text', text: 'not the answer' }],
      },
      {
        type: 'message',
        id: 'message_fixture',
        role: 'assistant',
        status: 'completed',
        content: [
          { type: 'output_text', text: RawText.slice(0, 12), annotations: [] },
          { type: 'output_text', text: RawText.slice(12), annotations: [] },
        ],
      },
    ],
  }
}

function chatBody(
  message: Record<string, unknown> = {
    role: 'assistant',
    content: RawText,
    refusal: null,
  },
  finishReason = 'stop'
) {
  return {
    id: 'chatcmpl_fixture',
    object: 'chat.completion',
    created: 1,
    model: 'fixture',
    choices: [{ index: 0, finish_reason: finishReason, message }],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
  }
}

function anthropicBody(
  content: unknown[] = [
    { type: 'thinking', thinking: 'not the answer', signature: 'fixture' },
    { type: 'text', text: RawText.slice(0, 12) },
    { type: 'text', text: RawText.slice(12) },
  ],
  stopReason = 'end_turn'
) {
  return {
    id: 'msg_fixture',
    type: 'message',
    role: 'assistant',
    model: 'fixture',
    content,
    stop_reason: stopReason,
    stop_sequence: null,
    stop_details: null,
    usage: { input_tokens: 1, output_tokens: 1 },
  }
}

function geminiBody(
  parts: unknown[] = [
    { thought: true, text: 'not the answer' },
    { text: RawText.slice(0, 12) },
    { text: RawText.slice(12) },
  ],
  finishReason = 'STOP'
) {
  return {
    candidates: [{ index: 0, content: { role: 'model', parts }, finishReason }],
    usageMetadata: {
      promptTokenCount: 1,
      candidatesTokenCount: 1,
      totalTokenCount: 2,
    },
    modelVersion: 'fixture',
  }
}

function ollamaBody(
  message: Record<string, unknown> = {
    role: 'assistant',
    content: RawText,
    thinking: 'not the answer',
  },
  doneReason = 'stop',
  done = true
) {
  return {
    model: 'fixture:latest',
    created_at: '2026-01-01T00:00:00Z',
    message,
    done,
    done_reason: doneReason,
    total_duration: 1,
    load_duration: 1,
    prompt_eval_count: 1,
    prompt_eval_duration: 1,
    eval_count: 1,
    eval_duration: 1,
  }
}

type WireCase = {
  readonly format: ProviderFormat
  readonly root: string
  readonly path: string
  readonly model: string
  readonly authKind: ProviderAuthKind
  readonly response: () => unknown
}
const WireCases: ReadonlyArray<WireCase> = [
  {
    format: 'openai-responses',
    root: '/v1',
    path: '/v1/responses',
    model: 'fixture-openai',
    authKind: 'apiKey',
    response: responsesBody,
  },
  {
    format: 'openai-chat',
    root: '/v1',
    path: '/v1/chat/completions',
    model: 'fixture-chat',
    authKind: 'apiKey',
    response: chatBody,
  },
  {
    format: 'anthropic-messages',
    root: '',
    path: '/v1/messages',
    model: 'fixture-claude',
    authKind: 'apiKey',
    response: anthropicBody,
  },
  {
    format: 'gemini',
    root: '',
    path: '/v1/models/fixture-gemini:generateContent',
    model: 'models/fixture-gemini',
    authKind: 'apiKey',
    response: geminiBody,
  },
  {
    format: 'azure-openai-v1',
    root: '/openai/v1',
    path: '/openai/v1/responses',
    model: 'my-deployment',
    authKind: 'apiKey',
    response: responsesBody,
  },
  {
    format: 'ollama',
    root: '',
    path: '/api/chat',
    model: 'fixture:latest',
    authKind: 'none',
    response: ollamaBody,
  },
]

function configured(
  testCase: WireCase,
  baseUrl: string,
  patch: Partial<ConfiguredProvider> = {}
): ConfiguredProvider {
  return {
    ...DefaultSettings,
    id: ProviderId,
    format: testCase.format,
    baseUrl: baseUrl + testCase.root,
    authKind: testCase.authKind,
    models: [
      {
        id: testCase.model,
        name: testCase.model,
        enabled: true,
        available: true,
      },
    ],
    ...patch,
  }
}

function generate(
  provider: ConfiguredProvider,
  model: string,
  signal?: AbortSignal
) {
  return generateWithProvider(
    provider,
    model,
    provider.authKind === 'none' ? null : FakeKey,
    SystemPrompt,
    UserPrompt,
    signal
  )
}

function setEnvironment(t: TestContext, values: Record<string, string>) {
  for (const [name, value] of Object.entries(values)) {
    const previous = process.env[name]
    process.env[name] = value
    t.after(() => {
      if (previous === undefined) {
        delete process.env[name]
      } else {
        process.env[name] = previous
      }
    })
  }
}

describe('commit generation official SDK wire adapters', () => {
  // The normal unit harness supplies jsdom for renderer tests. These SDKs must
  // run as Node clients, not enable dangerouslyAllowBrowser in production.
  const windowDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'window')
  before(() => {
    Reflect.deleteProperty(globalThis, 'window')
  })
  after(() => {
    if (windowDescriptor !== undefined) {
      Object.defineProperty(globalThis, 'window', windowDescriptor)
    }
  })

  for (const testCase of WireCases) {
    it(`${testCase.format}: native path/auth/prompt body and complete raw output`, async t => {
      const server = await wireServer(t, (request, response) => {
        assert.equal(request.method, 'POST')
        assert.equal(request.url, testCase.path)
        assert.ok(
          request.headers['content-type']?.startsWith('application/json')
        )
        if (testCase.format === 'gemini') {
          assert.equal(request.headers['x-goog-api-key'], FakeKey)
          assert.equal(request.headers.authorization, undefined)
          assert.deepEqual(request.body?.contents, [
            { role: 'user', parts: [{ text: UserPrompt }] },
          ])
          assert.deepEqual(request.body?.systemInstruction.parts, [
            { text: SystemPrompt },
          ])
          assert.equal(request.body?.messages, undefined)
          assert.equal(request.body?.automaticContinuation, undefined)
        } else {
          assert.equal(request.body?.model, testCase.model)
          if (testCase.format === 'azure-openai-v1') {
            assert.equal(request.headers['api-key'], FakeKey)
            assert.equal(request.headers.authorization, undefined)
          } else if (testCase.authKind === 'none') {
            assert.equal(request.headers.authorization, undefined)
          } else {
            assert.equal(request.headers.authorization, `Bearer ${FakeKey}`)
          }
          if (
            testCase.format === 'openai-responses' ||
            testCase.format === 'azure-openai-v1'
          ) {
            assert.deepEqual(request.body, {
              model: testCase.model,
              instructions: SystemPrompt,
              input: UserPrompt,
              store: false,
              stream: false,
            })
          } else if (testCase.format === 'anthropic-messages') {
            assert.equal(request.headers['anthropic-version'], '2026-10-21')
            assert.deepEqual(request.body, {
              model: testCase.model,
              system: SystemPrompt,
              messages: [{ role: 'user', content: UserPrompt }],
              max_tokens: 4096,
              stream: false,
            })
          } else {
            assert.deepEqual(request.body, {
              model: testCase.model,
              messages: [
                { role: 'system', content: SystemPrompt },
                { role: 'user', content: UserPrompt },
              ],
              stream: false,
            })
          }
        }
        assert.ok(!request.url.includes(FakeKey))
        json(response, testCase.response())
      })
      assert.equal(
        await generate(configured(testCase, server.baseUrl), testCase.model),
        RawText
      )
      assert.equal(server.requests.length, 1)
    })

    for (const status of [401, 429, 500]) {
      it(`${testCase.format}: HTTP ${status} fails once without leaking the echoed credential`, async t => {
        const server = await wireServer(t, (_, response) =>
          json(
            response,
            testCase.format === 'ollama'
              ? { error: FakeKey }
              : {
                  type: 'error',
                  error: {
                    code: status,
                    type: 'fixture_error',
                    message: FakeKey,
                    status: 'INTERNAL',
                  },
                },
            status
          )
        )
        await assert.rejects(
          generate(configured(testCase, server.baseUrl), testCase.model),
          hasCode('request-failed', status)
        )
        assert.equal(
          server.requests.length,
          1,
          'no automatic SDK retries or fallback provider'
        )
      })
    }

    for (const phase of ['before-headers', 'during-body'] as const) {
      it(`${testCase.format}: cancellation closes HTTP ${phase}`, async t => {
        const controller = new AbortController()
        let markClosed: () => void = () => {}
        const closed = new Promise<void>(resolve => {
          markClosed = resolve
        })
        const server = await wireServer(t, (_, response) => {
          response.once('close', () => {
            assert.equal(response.writableEnded, false)
            markClosed()
          })
          if (phase === 'during-body') {
            response.writeHead(200, { 'Content-Type': 'application/json' })
            response.write('{')
          }
          setTimeout(() => controller.abort(), 20)
        })
        await assert.rejects(
          generate(
            configured(testCase, server.baseUrl),
            testCase.model,
            controller.signal
          ),
          hasCode('cancelled')
        )
        await closed
        assert.equal(server.requests.length, 1)
      })
    }

    it(`${testCase.format}: timeout really aborts body consumption`, async t => {
      let markClosed: () => void = () => {}
      const closed = new Promise<void>(resolve => {
        markClosed = resolve
      })
      const server = await wireServer(t, (_, response) => {
        response.once('close', markClosed)
        response.writeHead(200, { 'Content-Type': 'application/json' })
        response.write('{')
      })
      await assert.rejects(
        generate(
          configured(testCase, server.baseUrl, { requestTimeoutSeconds: 0.1 }),
          testCase.model
        ),
        hasCode('timeout')
      )
      await closed
      assert.equal(server.requests.length, 1)
    })

    it(`${testCase.format}: already cancelled and unavailable selections make no request`, async t => {
      const server = await wireServer(t, (_, response) =>
        json(response, testCase.response())
      )
      const controller = new AbortController()
      controller.abort()
      const provider = configured(testCase, server.baseUrl)
      await assert.rejects(
        generate(provider, testCase.model, controller.signal),
        hasCode('cancelled')
      )
      await assert.rejects(
        generate(
          { ...provider, models: [{ ...provider.models[0], enabled: false }] },
          testCase.model
        ),
        hasCode('invalid-selection')
      )
      await assert.rejects(
        generate(
          {
            ...provider,
            models: [{ ...provider.models[0], available: false }],
          },
          testCase.model
        ),
        hasCode('invalid-selection')
      )
      await assert.rejects(
        generate(provider, 'not-selected'),
        hasCode('invalid-selection')
      )
      assert.equal(server.requests.length, 0)
    })
  }

  for (const format of [
    'openai-responses',
    'openai-chat',
    'anthropic-messages',
    'ollama',
    'azure-openai-v1',
  ] as const) {
    for (const authKind of ['bearer', 'none'] as const) {
      it(`${format}: explicit ${authKind} ignores environment credentials`, async t => {
        setEnvironment(t, {
          OPENAI_API_KEY: 'fake-environment-openai',
          OPENAI_ADMIN_KEY: 'fake-environment-admin',
          ANTHROPIC_API_KEY: 'fake-environment-anthropic',
          ANTHROPIC_AUTH_TOKEN: 'fake-environment-token',
          OLLAMA_API_KEY: 'fake-environment-ollama',
        })
        const testCase = WireCases.find(c => c.format === format)!
        const server = await wireServer(t, (request, response) => {
          assert.equal(
            request.headers.authorization,
            authKind === 'none' ? undefined : `Bearer ${FakeKey}`
          )
          assert.equal(request.headers['api-key'], undefined)
          assert.equal(request.headers['x-api-key'], undefined)
          json(response, testCase.response())
        })
        assert.equal(
          await generate(
            configured(testCase, server.baseUrl, { authKind }),
            testCase.model
          ),
          RawText
        )
      })
    }
  }

  it('Gemini does not silently select Vertex, an environment key or an environment endpoint', async t => {
    setEnvironment(t, {
      GOOGLE_GENAI_USE_VERTEXAI: 'true',
      GOOGLE_GENAI_USE_ENTERPRISE: 'true',
      GOOGLE_API_KEY: 'fake-environment-google',
      GOOGLE_GENAI_API_VERSION: 'v1alpha',
      GOOGLE_GEMINI_BASE_URL: 'https://must-not-be-called.invalid',
    })
    const testCase = WireCases.find(c => c.format === 'gemini')!
    const server = await wireServer(t, (request, response) => {
      assert.equal(request.headers['x-goog-api-key'], FakeKey)
      assert.equal(request.url, testCase.path)
      json(response, geminiBody())
    })
    assert.equal(
      await generate(configured(testCase, server.baseUrl), testCase.model),
      RawText
    )
    for (const authKind of ['bearer', 'none'] as const) {
      await assert.rejects(
        generate(
          configured(testCase, server.baseUrl, { authKind }),
          testCase.model
        ),
        hasCode('unsupported-auth')
      )
    }
    assert.equal(server.requests.length, 1)
  })

  it('does not follow credential-bearing HTTP redirects', async t => {
    const other = await wireServer(t, (_, response) =>
      json(response, responsesBody())
    )
    const server = await wireServer(t, (_, response) => {
      response.writeHead(307, { Location: other.baseUrl + '/responses' })
      response.end()
    })
    const testCase = WireCases[0]
    await assert.rejects(
      generate(configured(testCase, server.baseUrl), testCase.model),
      hasCode('request-failed')
    )
    assert.equal(server.requests.length, 1)
    assert.equal(other.requests.length, 0)
  })

  const BadResponses: ReadonlyArray<{
    format: ProviderFormat
    name: string
    body: () => unknown
    code: ProviderErrorCode
  }> = [
    {
      format: 'openai-responses',
      name: 'token cap',
      body: () => ({
        ...responsesBody(),
        status: 'incomplete',
        incomplete_details: { reason: 'max_output_tokens' },
      }),
      code: 'truncated-response',
    },
    {
      format: 'openai-responses',
      name: 'refusal block alongside text',
      body: () =>
        responsesBody([
          {
            type: 'message',
            id: 'm',
            role: 'assistant',
            status: 'completed',
            content: [
              { type: 'output_text', text: RawText },
              { type: 'refusal', refusal: FakeKey },
            ],
          },
        ]),
      code: 'refused-response',
    },
    {
      format: 'openai-responses',
      name: 'tool call alongside text',
      body: () => ({
        ...responsesBody(),
        output: [
          ...responsesBody().output,
          {
            type: 'function_call',
            name: 'do_not_execute',
            call_id: 'c',
            arguments: '{}',
          },
        ],
      }),
      code: 'tool-response',
    },
    {
      format: 'openai-responses',
      name: 'failed response',
      body: () => ({
        ...responsesBody(),
        status: 'failed',
        error: { message: FakeKey, code: 'server_error' },
      }),
      code: 'invalid-response',
    },
    {
      format: 'openai-responses',
      name: 'forged convenience text without output',
      body: () => ({ ...responsesBody([]), output_text: RawText }),
      code: 'invalid-response',
    },
    {
      format: 'openai-chat',
      name: 'token cap',
      body: () => chatBody(undefined, 'length'),
      code: 'truncated-response',
    },
    {
      format: 'openai-chat',
      name: 'refusal with text',
      body: () =>
        chatBody({ role: 'assistant', content: RawText, refusal: FakeKey }),
      code: 'refused-response',
    },
    {
      format: 'openai-chat',
      name: 'tool call with a stop reason',
      body: () =>
        chatBody({
          role: 'assistant',
          content: RawText,
          tool_calls: [
            {
              id: 'c',
              type: 'function',
              function: { name: 'never_run', arguments: '{}' },
            },
          ],
        }),
      code: 'tool-response',
    },
    {
      format: 'openai-chat',
      name: 'empty content',
      body: () => chatBody({ role: 'assistant', content: null }),
      code: 'invalid-response',
    },
    {
      format: 'openai-chat',
      name: 'Responses blocks are not Chat content',
      body: () =>
        chatBody({
          role: 'assistant',
          content: [{ type: 'output_text', text: RawText }],
        }),
      code: 'invalid-response',
    },
    {
      format: 'anthropic-messages',
      name: 'token cap',
      body: () => anthropicBody(undefined, 'max_tokens'),
      code: 'truncated-response',
    },
    {
      format: 'anthropic-messages',
      name: 'refusal stop reason',
      body: () => anthropicBody(undefined, 'refusal'),
      code: 'refused-response',
    },
    {
      format: 'anthropic-messages',
      name: 'tool call even with end_turn',
      body: () =>
        anthropicBody([
          { type: 'text', text: RawText },
          { type: 'tool_use', id: 't', name: 'never_run', input: {} },
        ]),
      code: 'tool-response',
    },
    {
      format: 'anthropic-messages',
      name: 'thinking only',
      body: () =>
        anthropicBody([
          { type: 'thinking', thinking: RawText, signature: 'fake' },
        ]),
      code: 'invalid-response',
    },
    {
      format: 'anthropic-messages',
      name: 'paused turn',
      body: () => anthropicBody(undefined, 'pause_turn'),
      code: 'truncated-response',
    },
    {
      format: 'gemini',
      name: 'token cap',
      body: () => geminiBody(undefined, 'MAX_TOKENS'),
      code: 'truncated-response',
    },
    {
      format: 'gemini',
      name: 'continuation does not trigger an extra paid request',
      body: () => ({
        ...geminiBody(undefined, 'CONTINUATION'),
        continuationToken: 'fake-continuation',
        candidates: [
          {
            ...geminiBody().candidates[0],
            finishReason: 'CONTINUATION',
            continuationToken: 'fake-continuation',
          },
        ],
      }),
      code: 'truncated-response',
    },
    {
      format: 'gemini',
      name: 'blocked prompt',
      body: () => ({ promptFeedback: { blockReason: 'SAFETY' } }),
      code: 'refused-response',
    },
    {
      format: 'gemini',
      name: 'safety finish reason',
      body: () => geminiBody(undefined, 'SAFETY'),
      code: 'refused-response',
    },
    {
      format: 'gemini',
      name: 'tool call alongside text',
      body: () =>
        geminiBody([
          { text: RawText },
          { functionCall: { name: 'never_run', args: {} } },
        ]),
      code: 'tool-response',
    },
    {
      format: 'gemini',
      name: 'no candidates',
      body: () => ({ candidates: [] }),
      code: 'invalid-response',
    },
    {
      format: 'gemini',
      name: 'non-text part alongside text',
      body: () =>
        geminiBody([
          { text: RawText },
          { inlineData: { mimeType: 'image/png', data: 'ZmFrZQ==' } },
        ]),
      code: 'invalid-response',
    },
    {
      format: 'gemini',
      name: 'thoughts only',
      body: () => geminiBody([{ thought: true, text: RawText }]),
      code: 'invalid-response',
    },
    {
      format: 'ollama',
      name: 'token cap',
      body: () => ollamaBody(undefined, 'length'),
      code: 'truncated-response',
    },
    {
      format: 'ollama',
      name: 'unfinished response',
      body: () => ollamaBody(undefined, 'stop', false),
      code: 'truncated-response',
    },
    {
      format: 'ollama',
      name: 'tool call alongside text',
      body: () =>
        ollamaBody({
          role: 'assistant',
          content: RawText,
          tool_calls: [{ function: { name: 'never_run', arguments: {} } }],
        }),
      code: 'tool-response',
    },
    {
      format: 'ollama',
      name: 'empty message',
      body: () => ollamaBody({ role: 'assistant', content: '' }),
      code: 'invalid-response',
    },
  ]
  for (const invalid of BadResponses) {
    it(`${invalid.format}: rejects ${invalid.name}`, async t => {
      const testCase = WireCases.find(c => c.format === invalid.format)!
      const server = await wireServer(t, (_, response) =>
        json(response, invalid.body())
      )
      await assert.rejects(
        generate(configured(testCase, server.baseUrl), testCase.model),
        hasCode(invalid.code)
      )
      assert.equal(server.requests.length, 1)
    })
  }

  it('OpenAI discovery is one native /models call, without invented pagination', async t => {
    const server = await wireServer(t, (request, response) => {
      assert.equal(request.method, 'GET')
      assert.equal(request.url, '/custom/v1/models')
      assert.equal(request.headers.authorization, `Bearer ${FakeKey}`)
      json(response, {
        object: 'list',
        data: [
          { id: 'fixture-a', object: 'model', created: 1, owned_by: 'fixture' },
          { id: 'fixture-b', object: 'model', created: 1, owned_by: 'fixture' },
        ],
        has_more: true,
      })
    })
    for (const testCase of WireCases.filter(c =>
      c.format.startsWith('openai-')
    )) {
      assert.deepEqual(
        await discoverProviderModels(
          configured(testCase, server.baseUrl, {
            baseUrl: server.baseUrl + '/custom/v1',
          }),
          FakeKey
        ),
        [
          { id: 'fixture-a', name: 'fixture-a' },
          { id: 'fixture-b', name: 'fixture-b' },
        ]
      )
    }
    assert.equal(server.requests.length, 2)
  })

  it('Anthropic discovery exhausts native after_id pages including an empty middle page', async t => {
    const server = await wireServer(t, (request, response) => {
      const url = new URL(request.url, 'http://fixture')
      assert.equal(url.pathname, '/v1/models')
      assert.equal(url.searchParams.get('limit'), '100')
      assert.equal(request.headers.authorization, `Bearer ${FakeKey}`)
      assert.equal(request.headers['anthropic-version'], '2026-10-21')
      const cursor = url.searchParams.get('after_id')
      if (cursor === null) {
        json(response, {
          data: [
            {
              id: 'claude-a',
              type: 'model',
              display_name: 'Claude A',
              created_at: '2026-01-01T00:00:00Z',
              lifecycle: 'active',
            },
          ],
          has_more: true,
          first_id: 'claude-a',
          last_id: 'claude-a',
        })
      } else if (cursor === 'claude-a') {
        json(response, {
          data: [],
          has_more: true,
          first_id: null,
          last_id: 'empty-page',
        })
      } else {
        assert.equal(cursor, 'empty-page')
        json(response, {
          data: [
            {
              id: 'claude-b',
              type: 'model',
              display_name: 'Claude B',
              created_at: '2026-01-01T00:00:00Z',
              lifecycle: 'deprecated',
            },
          ],
          has_more: false,
          first_id: 'claude-b',
          last_id: 'claude-b',
        })
      }
    })
    const testCase = WireCases.find(c => c.format === 'anthropic-messages')!
    assert.deepEqual(
      await discoverProviderModels(
        configured(testCase, server.baseUrl),
        FakeKey
      ),
      [
        { id: 'claude-a', name: 'Claude A' },
        { id: 'claude-b', name: 'Claude B' },
      ]
    )
    assert.equal(server.requests.length, 3)
  })

  it('Gemini discovery exhausts pageToken pages and filters by native generation capability', async t => {
    const server = await wireServer(t, (request, response) => {
      const url = new URL(request.url, 'http://fixture')
      assert.equal(url.pathname, '/v1/models')
      assert.equal(url.searchParams.get('pageSize'), '100')
      assert.equal(request.headers['x-goog-api-key'], FakeKey)
      const cursor = url.searchParams.get('pageToken')
      if (cursor === null) {
        json(response, {
          models: [
            {
              name: 'models/gemini-a',
              displayName: 'Gemini A',
              supportedGenerationMethods: ['generateContent'],
            },
            {
              name: 'models/embed',
              displayName: 'Embedding',
              supportedGenerationMethods: ['embedContent'],
            },
          ],
          nextPageToken: 'second',
        })
      } else if (cursor === 'second') {
        json(response, { models: [], nextPageToken: 'third' })
      } else {
        assert.equal(cursor, 'third')
        json(response, {
          models: [
            {
              name: 'models/gemini-b',
              supportedGenerationMethods: ['generateContent', 'countTokens'],
            },
          ],
        })
      }
    })
    const testCase = WireCases.find(c => c.format === 'gemini')!
    assert.deepEqual(
      await discoverProviderModels(
        configured(testCase, server.baseUrl),
        FakeKey
      ),
      [
        { id: 'models/gemini-a', name: 'Gemini A' },
        { id: 'models/gemini-b', name: 'models/gemini-b' },
      ]
    )
    assert.equal(server.requests.length, 3)
  })

  it('Ollama discovery uses native /api/tags, not OpenAI /v1/models', async t => {
    const server = await wireServer(t, (request, response) => {
      assert.equal(request.method, 'GET')
      assert.equal(request.url, '/api/tags')
      assert.equal(request.headers.authorization, `Bearer ${FakeKey}`)
      json(response, {
        models: [
          {
            model: 'fixture:latest',
            name: 'Local fixture',
            modified_at: '2026-01-01T00:00:00Z',
            size: 1,
            digest: 'fixture',
            details: {
              format: 'gguf',
              family: 'fixture',
              parameter_size: '1B',
            },
          },
        ],
      })
    })
    const testCase = WireCases.find(c => c.format === 'ollama')!
    assert.deepEqual(
      await discoverProviderModels(
        configured(testCase, server.baseUrl, { authKind: 'apiKey' }),
        FakeKey
      ),
      [{ id: 'fixture:latest', name: 'Local fixture' }]
    )
    assert.equal(server.requests.length, 1)
  })

  it('Azure explicitly rejects discovery without treating a model catalog as deployments', async t => {
    const server = await wireServer(t, (_, response) =>
      json(response, {
        object: 'list',
        data: [{ id: 'catalog-model-not-a-deployment' }],
      })
    )
    const testCase = WireCases.find(c => c.format === 'azure-openai-v1')!
    await assert.rejects(
      discoverProviderModels(configured(testCase, server.baseUrl), FakeKey),
      hasCode('discovery-unsupported')
    )
    assert.equal(server.requests.length, 0)
  })

  for (const format of ['anthropic-messages', 'gemini'] as const) {
    it(`${format}: a later-page failure rejects instead of returning a partial catalog`, async t => {
      const server = await wireServer(t, (request, response) => {
        const url = new URL(request.url, 'http://fixture')
        const later = url.searchParams.has(
          format === 'gemini' ? 'pageToken' : 'after_id'
        )
        if (later) {
          json(
            response,
            {
              error: {
                message: FakeKey,
                type: 'authentication_error',
                code: 401,
                status: 'UNAUTHENTICATED',
              },
            },
            401
          )
        } else if (format === 'gemini') {
          json(response, {
            models: [
              {
                name: 'models/first',
                supportedGenerationMethods: ['generateContent'],
              },
            ],
            nextPageToken: 'second',
          })
        } else {
          json(response, {
            data: [
              {
                id: 'first',
                display_name: 'First',
                type: 'model',
                lifecycle: 'active',
              },
            ],
            has_more: true,
            first_id: 'first',
            last_id: 'first',
          })
        }
      })
      const testCase = WireCases.find(c => c.format === format)!
      await assert.rejects(
        discoverProviderModels(configured(testCase, server.baseUrl), FakeKey),
        hasCode('request-failed', 401)
      )
      assert.equal(server.requests.length, 2)
    })

    it(`${format}: a repeated paging cursor fails explicitly`, async t => {
      const server = await wireServer(t, (_, response) =>
        json(
          response,
          format === 'gemini'
            ? { models: [], nextPageToken: 'loop' }
            : { data: [], has_more: true, first_id: null, last_id: 'loop' }
        )
      )
      const testCase = WireCases.find(c => c.format === format)!
      await assert.rejects(
        discoverProviderModels(configured(testCase, server.baseUrl), FakeKey),
        hasCode('invalid-response')
      )
      assert.equal(server.requests.length, 2)
    })

    it(`${format}: abort cancels discovery's next page, not just generation`, async t => {
      const controller = new AbortController()
      let markClosed: () => void = () => {}
      const closed = new Promise<void>(resolve => {
        markClosed = resolve
      })
      const server = await wireServer(t, (request, response) => {
        const later = new URL(request.url, 'http://fixture').searchParams.has(
          format === 'gemini' ? 'pageToken' : 'after_id'
        )
        if (later) {
          response.once('close', markClosed)
          response.writeHead(200, { 'Content-Type': 'application/json' })
          response.write('{')
          setTimeout(() => controller.abort(), 20)
        } else {
          json(
            response,
            format === 'gemini'
              ? { models: [], nextPageToken: 'second' }
              : { data: [], has_more: true, first_id: null, last_id: 'first' }
          )
        }
      })
      const testCase = WireCases.find(c => c.format === format)!
      await assert.rejects(
        discoverProviderModels(
          configured(testCase, server.baseUrl),
          FakeKey,
          controller.signal
        ),
        hasCode('cancelled')
      )
      await closed
      assert.equal(server.requests.length, 2)
    })
  }
})

describe('commit generation ProviderStore public behavior', () => {
  afterEach(() => localStorage.clear())

  it('creates UUID metadata, renames only the name and retains the isolated credential', async t => {
    const secure = new AsyncInMemoryStore()
    const writes = t.mock.method(secure, 'setItem')
    const deletes = t.mock.method(secure, 'deleteItem')
    const store = new ProviderStore(secure)
    assert.deepEqual(store.getProviders(), [])
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    assert.match(
      provider.id,
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    )
    assert.equal(writes.mock.calls[0].arguments[0], ProviderKeychainService)
    assert.ok(writes.mock.calls[0].arguments[1].includes(provider.id))
    const renamed = await store.renameProvider(provider.id, '重命名供应商')
    assert.deepEqual(renamed, { ...provider, name: '重命名供应商' })
    assert.equal(await store.getSecret(provider.id), FakeKey)
    assert.equal(writes.mock.callCount(), 1)
    assert.equal(deletes.mock.callCount(), 0)
    assert.ok(!localStorage.getItem(ProviderStorageKey)!.includes(FakeKey))
    const reloaded = new ProviderStore(secure)
    assert.deepEqual(reloaded.getProviders(), store.getProviders())
    assert.equal(await reloaded.getSecret(provider.id), FakeKey)
  })

  it('new models default disabled; refresh preserves flags, names and unavailable entries', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    await store.updateModels(provider.id, [
      { id: 'a', name: 'A' },
      { id: 'b', name: 'B' },
    ])
    assert.deepEqual(store.getEnabledModels(), [])
    await store.setModelEnabled(provider.id, 'a', true)
    await store.setCurrentSelection({ providerId: provider.id, modelId: 'a' })
    await store.updateModels(provider.id, [
      { id: 'a', name: 'A renamed' },
      { id: 'c', name: 'C' },
    ])
    assert.deepEqual(store.getProvider(provider.id)!.models, [
      { id: 'a', name: 'A renamed', enabled: true, available: true },
      { id: 'c', name: 'C', enabled: false, available: true },
      { id: 'b', name: 'B', enabled: false, available: false },
    ])
    assert.deepEqual(store.getEnabledModels(), [
      {
        providerId: provider.id,
        providerName: provider.name,
        modelId: 'a',
        modelName: 'A renamed',
      },
    ])
    await store.updateModels(provider.id, [{ id: 'c', name: 'C' }])
    assert.equal(store.currentSelection, null)
    assert.deepEqual(store.getEnabledModels(), [])
    assert.equal(
      store.getProvider(provider.id)!.models.find(m => m.id === 'a')!.enabled,
      true
    )
    assert.equal(
      store.getProvider(provider.id)!.models.find(m => m.id === 'a')!.available,
      false
    )
    await store.updateModels(provider.id, [{ id: 'a', name: 'A returned' }])
    assert.equal(store.getEnabledModels().length, 1)
    assert.equal(
      store.currentSelection,
      null,
      'reappearance does not auto-select a model'
    )
  })

  it('home choices are compound identities and only enabled && available; invalid selections never fallback', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const first = await store.saveProvider(DefaultSettings, FakeKey)
    const second = await store.saveProvider(
      { ...DefaultSettings, name: 'Second' },
      'another-fake-key'
    )
    for (const provider of [first, second]) {
      await store.updateModels(provider.id, [
        { id: 'same', name: 'Shared ID' },
        { id: 'off', name: 'Disabled' },
      ])
      await store.setModelEnabled(provider.id, 'same', true)
    }
    assert.deepEqual(
      store.getEnabledModels().map(m => m.providerId),
      [first.id, second.id]
    )
    await assert.rejects(
      store.setCurrentSelection({ providerId: first.id, modelId: 'off' }),
      hasCode('invalid-selection')
    )
    assert.equal(store.currentSelection, null)
    await store.setCurrentSelection({ providerId: first.id, modelId: 'same' })
    await store.setModelEnabled(first.id, 'same', false)
    assert.equal(store.currentSelection, null)
    assert.equal(
      new ProviderStore(new AsyncInMemoryStore()).currentSelection,
      null
    )
    await store.setCurrentSelection({ providerId: second.id, modelId: 'same' })
    await store.deleteProvider(second.id)
    assert.equal(store.currentSelection, null)
    assert.deepEqual(store.getEnabledModels(), [])
  })

  it('origin/auth/protocol changes require a fresh secret and leave existing metadata untouched on refusal', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    for (const patch of [
      { baseUrl: 'https://other.example/v1' },
      { authKind: 'bearer' as const },
      { format: 'openai-chat' as const },
    ]) {
      await assert.rejects(
        store.saveProvider({ ...provider, ...patch }),
        hasCode('secret-required')
      )
      assert.deepEqual(store.getProvider(provider.id), provider)
      assert.equal(await store.getSecret(provider.id), FakeKey)
    }
    const changed = await store.saveProvider(
      { ...provider, baseUrl: 'https://other.example/v1' },
      'new-fake-key'
    )
    assert.equal(changed.id, provider.id)
    assert.equal(await store.getSecret(provider.id), 'new-fake-key')
  })

  it('same-origin path changes may retain the secret, but invalidate the previous catalog/selection', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    await store.updateModels(provider.id, [{ id: 'a', name: 'A' }])
    await store.setModelEnabled(provider.id, 'a', true)
    await store.setCurrentSelection({ providerId: provider.id, modelId: 'a' })
    const changed = await store.saveProvider({
      ...provider,
      baseUrl: 'https://provider.example/other/v1',
    })
    assert.deepEqual(changed.models, [
      { id: 'a', name: 'A', enabled: true, available: false },
    ])
    assert.equal(await store.getSecret(provider.id), FakeKey)
    assert.equal(store.currentSelection, null)
    assert.deepEqual(store.getEnabledModels(), [])
  })

  it('failed secret writes never add or change metadata and do not leak the OS error', async t => {
    const secure = new AsyncInMemoryStore()
    const store = new ProviderStore(secure)
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    const raw = localStorage.getItem(ProviderStorageKey)
    t.mock.method(secure, 'setItem', async () => {
      throw new Error(FakeKey)
    })
    await assert.rejects(
      store.saveProvider({ ...DefaultSettings, name: 'Not added' }, FakeKey),
      hasCode('secret-storage-failed')
    )
    await assert.rejects(
      store.saveProvider({ ...provider, name: 'Not changed' }, FakeKey),
      hasCode('secret-storage-failed')
    )
    assert.equal(localStorage.getItem(ProviderStorageKey), raw)
    assert.deepEqual(store.getProviders(), [provider])
  })

  for (const failure of ['throw', 'false'] as const) {
    it(`failed secret deletion (${failure}) retains the provider and its current selection`, async t => {
      const secure = new AsyncInMemoryStore()
      const store = new ProviderStore(secure)
      const provider = await store.saveProvider(DefaultSettings, FakeKey)
      await store.updateModels(provider.id, [{ id: 'a', name: 'A' }])
      await store.setModelEnabled(provider.id, 'a', true)
      const selection = { providerId: provider.id, modelId: 'a' }
      await store.setCurrentSelection(selection)
      t.mock.method(secure, 'deleteItem', async () => {
        if (failure === 'throw') {
          throw new Error(FakeKey)
        }
        return false
      })
      const raw = localStorage.getItem(ProviderStorageKey)
      await assert.rejects(
        store.deleteProvider(provider.id),
        hasCode('secret-storage-failed')
      )
      await assert.rejects(
        store.saveProvider({ ...provider, authKind: 'none' }),
        hasCode('secret-storage-failed')
      )
      assert.equal(localStorage.getItem(ProviderStorageKey), raw)
      assert.deepEqual(store.currentSelection, selection)
      assert.equal(await store.getSecret(provider.id), FakeKey)
    })
  }

  it('switching to none securely deletes a previous credential; a lost key still leaves a removable record', async t => {
    const secure = new AsyncInMemoryStore()
    const store = new ProviderStore(secure)
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    const deletes = t.mock.method(secure, 'deleteItem')
    await store.saveProvider({ ...provider, authKind: 'none' })
    assert.equal(deletes.mock.callCount(), 1)
    assert.equal(await store.getSecret(provider.id), null)
    await assert.rejects(
      store.saveProvider({ ...provider, authKind: 'bearer' }),
      hasCode('secret-required')
    )
    await store.deleteProvider(provider.id)
    assert.deepEqual(store.getProviders(), [])
    const other = await store.saveProvider(DefaultSettings, FakeKey)
    t.mock.method(secure, 'getItem', async () => null)
    await store.deleteProvider(other.id)
    assert.deepEqual(store.getProviders(), [])
  })

  it('keychain reads fail explicitly without removing manageable metadata', async t => {
    const secure = new AsyncInMemoryStore()
    const store = new ProviderStore(secure)
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    t.mock.method(secure, 'getItem', async () => {
      throw new Error(FakeKey)
    })
    await assert.rejects(
      store.getSecret(provider.id),
      hasCode('secret-storage-failed')
    )
    await assert.rejects(
      store.deleteProvider(provider.id),
      hasCode('secret-storage-failed')
    )
    assert.deepEqual(store.getProviders(), [provider])
  })

  it('failed metadata writes do not expose a new-origin credential to the old origin', async t => {
    const storage = new InMemoryStore()
    storage.setItem(
      ProviderStorageKey,
      JSON.stringify({ version: 1, providers: [], currentSelection: null })
    )
    const secure = new AsyncInMemoryStore()
    const store = new ProviderStore(secure, storage)
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    t.mock.method(storage, 'setItem', () => {
      throw new Error(FakeKey)
    })
    await assert.rejects(
      store.saveProvider(
        { ...provider, baseUrl: 'https://other.example/v1' },
        'other-origin-fake-key'
      ),
      hasCode('storage-unavailable')
    )
    assert.deepEqual(store.getProviders(), [provider])
    await assert.rejects(
      store.getSecret(provider.id),
      hasCode('secret-required')
    )
    await assert.rejects(
      new ProviderStore(secure, storage).getSecret(provider.id),
      hasCode('secret-required')
    )
  })

  it('untrusted extra secret fields never enter persisted metadata', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const provider = await store.saveProvider(
      { ...DefaultSettings, secret: FakeKey } as ProviderSettings,
      FakeKey
    )
    await store.updateModels(provider.id, [{ id: 'a', name: 'A' }])
    await store.setModelEnabled(provider.id, 'a', true)
    await store.setCurrentSelection({
      providerId: provider.id,
      modelId: 'a',
      secret: FakeKey,
    } as Parameters<ProviderStore['setCurrentSelection']>[0])
    assert.ok(!localStorage.getItem(ProviderStorageKey)!.includes(FakeKey))
    assert.deepEqual(store.currentSelection, {
      providerId: provider.id,
      modelId: 'a',
    })
  })

  it('rejects a duplicate discovered ID atomically rather than dropping a checked model', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const provider = await store.saveProvider(DefaultSettings, FakeKey)
    await store.updateModels(provider.id, [{ id: 'a', name: 'A' }])
    await store.setModelEnabled(provider.id, 'a', true)
    const raw = localStorage.getItem(ProviderStorageKey)
    await assert.rejects(
      store.updateModels(provider.id, [
        { id: 'a', name: 'A' },
        { id: 'a', name: 'Duplicate' },
      ]),
      hasCode('invalid-response')
    )
    assert.equal(localStorage.getItem(ProviderStorageKey), raw)
    assert.equal(store.getEnabledModels().length, 1)
  })

  it('valid stale selection metadata resolves to null rather than the first enabled model', async () => {
    localStorage.setItem(
      ProviderStorageKey,
      JSON.stringify({
        version: 1,
        providers: [
          {
            ...DefaultSettings,
            id: ProviderId,
            models: [
              { id: 'available', name: 'A', enabled: true, available: true },
            ],
          },
        ],
        currentSelection: { providerId: ProviderId, modelId: 'gone' },
      })
    )
    const store = new ProviderStore(new AsyncInMemoryStore())
    assert.equal(store.currentSelection, null)
    assert.equal(store.getEnabledModels().length, 1)
  })

  const GoodState = {
    version: 1,
    providers: [
      {
        ...DefaultSettings,
        id: ProviderId,
        models: [{ id: 'a', name: 'A', enabled: true, available: true }],
      },
    ],
    currentSelection: null,
  }
  for (const [name, value] of [
    ['invalid JSON', `{${FakeKey}`],
    ['array root', '[]'],
    ['unknown version', JSON.stringify({ ...GoodState, version: 2 })],
    ['missing selection field', JSON.stringify({ version: 1, providers: [] })],
    [
      'invalid provider',
      JSON.stringify({
        ...GoodState,
        providers: [...GoodState.providers, { name: 'damaged' }],
      }),
    ],
    [
      'non UUID identity',
      JSON.stringify({
        ...GoodState,
        providers: [
          { ...GoodState.providers[0], id: 'display-name-not-an-id' },
        ],
      }),
    ],
    [
      'duplicate identity',
      JSON.stringify({
        ...GoodState,
        providers: [GoodState.providers[0], GoodState.providers[0]],
      }),
    ],
    [
      'missing enabled flag',
      JSON.stringify({
        ...GoodState,
        providers: [
          {
            ...GoodState.providers[0],
            models: [{ id: 'a', name: 'A', available: true }],
          },
        ],
      }),
    ],
    [
      'string enabled flag',
      JSON.stringify({
        ...GoodState,
        providers: [
          {
            ...GoodState.providers[0],
            models: [{ id: 'a', name: 'A', enabled: 'true', available: true }],
          },
        ],
      }),
    ],
    [
      'secret in metadata',
      JSON.stringify({
        ...GoodState,
        providers: [{ ...GoodState.providers[0], secret: FakeKey }],
      }),
    ],
    [
      'malformed selection',
      JSON.stringify({ ...GoodState, currentSelection: 'a' }),
    ],
  ]) {
    it(`damaged saved state (${name}) is an explicit error, never a successful empty list`, () => {
      localStorage.setItem(ProviderStorageKey, value)
      assert.throws(
        () => new ProviderStore(new AsyncInMemoryStore()),
        hasCode('storage-corrupt')
      )
      assert.equal(localStorage.getItem(ProviderStorageKey), value)
    })
  }

  it('rejects credential-bearing URLs, remote plaintext, invalid timeouts and missing authenticated secrets', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    for (const baseUrl of [
      'https://user:password@example.com/v1',
      'https://example.com/v1?key=fake',
      'https://example.com/v1#fake',
      'http://public.example/v1',
      'file:///tmp/fake',
    ]) {
      await assert.rejects(
        store.saveProvider({ ...DefaultSettings, baseUrl }, FakeKey),
        hasCode('invalid-configuration')
      )
    }
    for (const requestTimeoutSeconds of [0, -1, Infinity, NaN, 3_000_000]) {
      await assert.rejects(
        store.saveProvider(
          { ...DefaultSettings, requestTimeoutSeconds },
          FakeKey
        ),
        hasCode('invalid-configuration')
      )
    }
    await assert.rejects(
      store.saveProvider(DefaultSettings),
      hasCode('secret-required')
    )
    await assert.rejects(
      store.saveProvider(DefaultSettings, ''),
      hasCode('secret-required')
    )
    assert.deepEqual(store.getProviders(), [])
  })

  it('serializes async credential saves with subsequent rename/model changes', async t => {
    const secure = new AsyncInMemoryStore()
    const write = secure.setItem.bind(secure)
    let release: () => void = () => {}
    const wait = new Promise<void>(resolve => {
      release = resolve
    })
    t.mock.method(
      secure,
      'setItem',
      async (...args: Parameters<typeof write>) => {
        await wait
        return write(...args)
      }
    )
    const store = new ProviderStore(secure)
    const saving = store.saveProvider(DefaultSettings, FakeKey)
    release()
    const provider = await saving
    await Promise.all([
      store.renameProvider(provider.id, 'Renamed'),
      store.updateModels(provider.id, [{ id: 'a', name: 'A' }]),
      store.setModelEnabled(provider.id, 'a', true),
    ])
    assert.equal(store.getEnabledModels()[0].providerName, 'Renamed')
    assert.equal(await store.getSecret(provider.id), FakeKey)
  })

  it('uses real native Node localStorage with the default constructor and reloads enabled selection', async t => {
    const root = resolve('_temp/provider-research')
    await mkdir(root, { recursive: true })
    const directory = await mkdtemp(join(root, 'native-storage-test-'))
    t.after(() => rm(directory, { recursive: true, force: true }))
    const storeModule = resolve(
      'app/src/lib/commit-generation/provider-store.ts'
    )
    const helperModule = resolve(
      'app/test/helpers/stores/async-in-memory-store.ts'
    )
    const script = `
      import assert from 'node:assert/strict';
      import { createRequire } from 'node:module';
      const require = createRequire(import.meta.url);
      const { ProviderStore, ProviderStorageKey } = require(${JSON.stringify(
        storeModule
      )});
      const { AsyncInMemoryStore } = require(${JSON.stringify(helperModule)});
      assert.ok(globalThis.localStorage);
      const secure = new AsyncInMemoryStore();
      const store = new ProviderStore(secure);
      const provider = await store.saveProvider(${JSON.stringify(
        DefaultSettings
      )}, 'fake-native-key');
      await store.updateModels(provider.id, [{ id: 'native-model', name: 'Native model' }]);
      await store.setModelEnabled(provider.id, 'native-model', true);
      await store.setCurrentSelection({ providerId: provider.id, modelId: 'native-model' });
      const reloaded = new ProviderStore(secure);
      assert.deepEqual(reloaded.getProviders(), store.getProviders());
      assert.deepEqual(reloaded.currentSelection, store.currentSelection);
      assert.equal(reloaded.getEnabledModels().length, 1);
      assert.equal(await reloaded.getSecret(provider.id), 'fake-native-key');
      assert.ok(!localStorage.getItem(ProviderStorageKey).includes('fake-native-key'));
      console.log('native-localStorage-ok');
    `
    const { stdout } = await promisify(execFile)(
      process.execPath,
      [
        '--experimental-webstorage',
        `--localstorage-file=${join(directory, 'providers.db')}`,
        '--import',
        'tsx',
        '--input-type=module',
        '--eval',
        script,
      ],
      { cwd: resolve('.') }
    )
    assert.equal(stdout.trim(), 'native-localStorage-ok')
  })
})

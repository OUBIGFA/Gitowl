import assert from 'node:assert/strict'
import { describe, it, type TestContext } from 'node:test'
import { mkdir, mkdtemp, readFile, rm } from 'fs/promises'
import { join, resolve } from 'path'
import {
  CopilotClient,
  RuntimeConnection,
  type CopilotClientOptions,
  type SessionConfig,
  type SessionFsProvider,
} from '@github/copilot-sdk'
import {
  CopilotGateway,
  type CopilotGenerateRequest,
} from '../../src/native-service/copilot-gateway'
import { CopilotError } from '../../src/lib/copilot-error'

const accountId = 'opaque:fixture/alpha+?='
const otherAccountId = 'opaque:fixture/bravo+?='
const rawResponse = '{"title":"Fixture commit","description":""}'

type Trace = { method: string; params: any }

class FixtureGateway extends CopilotGateway {
  public readonly sessionFileSystems: SessionFsProvider[] = []
  public sdk: CopilotClient | undefined

  public constructor(
    root: string,
    private readonly trace: string,
    private readonly scenario: string
  ) {
    super({
      runtimeRoot: resolve('out/copilot'),
      dataDirectory: join(root, 'data'),
    })
  }

  protected override createClient(
    options: CopilotClientOptions
  ): CopilotClient {
    // Real SDK and stdio framing; only the backend is a controlled protocol
    // fixture. These tests do NOT establish live Copilot authentication.
    const client = super.createClient({
      ...options,
      connection: RuntimeConnection.forStdio({
        path: resolve('app/test/native-service/copilot-fixture-runtime.js'),
      }),
      useLoggedInUser: false,
      env: {
        ...options.env,
        COPILOT_FIXTURE_TRACE: this.trace,
        COPILOT_FIXTURE_SCENARIO: this.scenario,
      },
    })
    this.sdk = client
    const createSession = client.createSession.bind(client)
    client.createSession = (config: SessionConfig) =>
      createSession({
        ...config,
        createSessionFsProvider: session => {
          const fs = config.createSessionFsProvider!(session)
          this.sessionFileSystems.push(fs)
          return fs
        },
      })
    return client
  }
}

async function fixture(t: TestContext, scenario = 'success') {
  const scratch = resolve('_temp/copilot-gateway-tests')
  await mkdir(scratch, { recursive: true })
  const root = await mkdtemp(join(scratch, 'protocol-'))
  const path = join(root, 'trace.jsonl')
  const instance = new FixtureGateway(root, path, scenario)
  t.after(async () => {
    await instance.close()
    await rm(root, { recursive: true, force: true })
  })
  const trace = async (): Promise<Trace[]> => {
    const text = await readFile(path, 'utf8').catch(error => {
      if (error.code === 'ENOENT') {
        return ''
      }
      throw error
    })
    return text.trim().length === 0
      ? []
      : text
          .trim()
          .split('\n')
          .map(line => JSON.parse(line))
  }
  const waitFor = async (method: string) => {
    const deadline = Date.now() + 5000
    while (Date.now() < deadline) {
      const found = (await trace()).find(entry => entry.method === method)
      if (found !== undefined) {
        return found
      }
      await new Promise(resolve => setTimeout(resolve, 10))
    }
    throw new Error(`Fixture did not receive ${method}`)
  }
  const request: CopilotGenerateRequest = {
    accountId,
    modelId: 'fixture-model',
    repositoryPath: root,
    systemPrompt: 'Trusted fixture system prompt',
    userPrompt: '<diff-fixture>\n+const identifier = "中文"\n</diff-fixture>',
  }
  return { instance, trace, waitFor, request, root }
}

describe(
  'Copilot gateway generation contract through the real SDK and a protocol fixture',
  { timeout: 20000 },
  () => {
    it('pins explicit account/model, forwards only supplied prompts, and disposes its session FS', async t => {
      const { instance, trace, request } = await fixture(t)
      assert.equal(await instance.generate(request), rawResponse)
      const calls = await trace()
      const create = calls.find(
        entry => entry.method === 'session.create'
      )!.params
      assert.equal(create.workingDirectory, request.repositoryPath)
      assert.equal(create.systemMessage.content, request.systemPrompt)
      assert.deepEqual(create.availableTools, [])
      assert.deepEqual(create.tools, [])
      assert.equal(create.requestPermission, true)
      for (const flag of [
        'enableFileHooks',
        'enableConfigDiscovery',
        'enableSkills',
        'enableSessionStore',
        'enableHostGitOperations',
        'enableOnDemandInstructionDiscovery',
      ]) {
        assert.equal(create[flag], false)
      }
      assert.deepEqual(create.infiniteSessions, { enabled: false })
      assert.deepEqual(create.memory, { enabled: false })
      assert.equal(create.gitHubToken, undefined)
      assert.equal(create.provider, undefined)
      const options = calls.find(
        entry => entry.method === 'session.options.update'
      )!.params
      assert.equal(options.skipCustomInstructions, true)
      assert.equal(options.coauthorEnabled, false)
      const selected = calls.find(
        entry => entry.method === 'session.accounts.set'
      )!.params.command
      assert.deepEqual(selected, {
        kind: 'switchActive',
        selectionId: accountId,
      })
      const model = calls.find(
        entry => entry.method === 'session.model.switchTo'
      )!.params
      assert.equal(model.modelId, request.modelId)
      assert.equal(model.providerId, 'fixture-copilot')
      assert.equal(model.requireAvailable, true)
      const send = calls.find(
        entry => entry.method === 'session.sendAndWait'
      )!.params
      assert.equal(send.prompt, request.userPrompt)
      assert.equal(send.attachments, undefined)
      assert.equal(
        calls.filter(entry => entry.method === 'session.detach').length,
        1
      )
      assert.equal(instance.sessionFileSystems.length, 1)
      await assert.rejects(
        instance.sessionFileSystems[0].exists('state'),
        /filesystem is disposed/
      )
    })

    it('projects credential-free account/status/login metadata and blocks unsafe login persistence', async t => {
      const { instance, trace } = await fixture(t)
      const accounts = await instance.accounts()
      const status = await instance.status()
      const login = await instance.beginLogin()
      assert.equal(accounts[0].selectionId, accountId)
      assert.equal(accounts[1].selectionId, otherAccountId)
      assert.ok(
        !JSON.stringify({ accounts, status, login }).includes('FIXTURE SECRET')
      )
      assert.ok(!('authInfo' in status))
      assert.equal(login.flowId, 'opaque:fixture/login+?=/')
      for (const input of [undefined, 'yes', 'true', 'accept']) {
        await assert.rejects(instance.advanceLogin(login.flowId, input), {
          name: 'CopilotLoginPersistenceBlockedError',
        })
      }
      assert.equal(
        (await trace()).filter(
          entry => entry.method === 'session.accounts.login.advance'
        ).length,
        0
      )
      await instance.cancelLogin(login.flowId)
      const cancel = (await trace()).find(
        entry => entry.method === 'session.accounts.login.cancel'
      )!
      assert.equal(cancel.params.flowId, login.flowId)
    })

    for (const scenario of [
      'account-refused',
      'account-mismatch',
      'model-fallback',
    ]) {
      it(`fails before sending a diff on ${scenario}, without account/model fallback`, async t => {
        const { instance, trace, request } = await fixture(t, scenario)
        await assert.rejects(
          instance.generate(request),
          /Copilot (did not select|session identity changed)/
        )
        const calls = await trace()
        assert.equal(
          calls.filter(entry => entry.method === 'session.sendAndWait').length,
          0
        )
        assert.equal(
          calls.filter(entry => entry.method === 'session.accounts.set').length,
          1
        )
        assert.equal(
          calls.filter(entry => entry.method === 'session.detach').length,
          1
        )
        await assert.rejects(
          instance.sessionFileSystems[0].exists('state'),
          /filesystem is disposed/
        )
      })
    }

    it('rejects SDK permission requests even with an empty tool allowlist', async t => {
      const { instance, trace, request } = await fixture(t, 'permission')
      assert.equal(await instance.generate(request), rawResponse)
      const response = (await trace()).find(
        entry =>
          entry.method === 'session.permissions.handlePendingPermissionRequest'
      )!
      assert.deepEqual(response.params.result, { kind: 'reject' })
    })

    it('preserves upstream billing/quota metadata rather than replacing it with a transport error', async t => {
      const { instance, trace, request } = await fixture(t, 'billing')
      await assert.rejects(instance.generate(request), error => {
        assert.ok(error instanceof CopilotError)
        assert.equal(error.isPaymentRequiredError, true)
        assert.equal(error.code, 'quota_exceeded')
        assert.equal(error.message, 'fixture quota exhausted')
        return true
      })
      assert.equal(
        (await trace()).filter(entry => entry.method === 'session.detach')
          .length,
        1
      )
    })

    it('does not turn an empty assistant response into a generated commit', async t => {
      const { instance, request } = await fixture(t, 'empty')
      await assert.rejects(
        instance.generate(request),
        /No response from Copilot/
      )
      await assert.rejects(
        instance.sessionFileSystems[0].exists('state'),
        /filesystem is disposed/
      )
    })

    it('cancels a pending SDK response, clears its timers and disposes the session', async t => {
      const { instance, trace, waitFor, request } = await fixture(t, 'pending')
      await instance.status()
      const scheduled = t.mock.method(globalThis, 'setTimeout')
      const cleared = t.mock.method(globalThis, 'clearTimeout')
      const controller = new AbortController()
      const operation = instance.generate({
        ...request,
        signal: controller.signal,
      })
      await waitFor('session.sendAndWait')
      controller.abort()
      await assert.rejects(operation, { name: 'AbortError' })
      await waitFor('fixture.request-cancelled')
      const waits = scheduled.mock.calls.filter(
        call => call.arguments[1] === 60000
      )
      assert.ok(waits.length >= 2)
      for (const wait of waits) {
        assert.ok(
          cleared.mock.calls.some(call => call.arguments[0] === wait.result)
        )
      }
      assert.equal(
        (await trace()).filter(entry => entry.method === 'session.detach')
          .length,
        1
      )
      await assert.rejects(
        instance.sessionFileSystems[1].exists('state'),
        /filesystem is disposed/
      )
      assert.equal(instance['generations'].size, 0)
    })

    it('disposes a session arriving after cancellation exactly once without sending a prompt', async t => {
      const { instance, trace, waitFor, request } = await fixture(
        t,
        'late-create'
      )
      const controller = new AbortController()
      const operation = instance.generate({
        ...request,
        signal: controller.signal,
      })
      await waitFor('session.create')
      controller.abort()
      await assert.rejects(operation, { name: 'AbortError' })
      await waitFor('session.detach')
      const calls = await trace()
      assert.equal(
        calls.filter(entry => entry.method === 'session.detach').length,
        1
      )
      assert.equal(
        calls.filter(entry => entry.method === 'session.sendAndWait').length,
        0
      )
      await assert.rejects(
        instance.sessionFileSystems[0].exists('state'),
        /filesystem is disposed/
      )
    })
  }
)

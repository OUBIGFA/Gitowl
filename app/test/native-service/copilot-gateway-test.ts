import assert from 'node:assert/strict'
import { describe, it, type TestContext } from 'node:test'
import { mkdir, mkdtemp, readdir, rm } from 'fs/promises'
import { join, resolve } from 'path'
import { CopilotClient, type CopilotClientOptions } from '@github/copilot-sdk'
import { CopilotGateway } from '../../src/native-service/copilot-gateway'

class ObservedGateway extends CopilotGateway {
  public options: CopilotClientOptions | undefined
  public sdk: CopilotClient | undefined

  protected override createClient(
    options: CopilotClientOptions
  ): CopilotClient {
    this.options = options
    this.sdk = super.createClient(options)
    return this.sdk
  }
}

async function directory(): Promise<string> {
  const scratch = resolve('_temp/copilot-gateway-tests')
  await mkdir(scratch, { recursive: true })
  return mkdtemp(join(scratch, 'isolated-'))
}

async function gateway(t: TestContext) {
  const root = await directory()
  const dataDirectory = join(root, 'gitowl-copilot')
  const instance = new ObservedGateway({
    runtimeRoot: resolve('out/copilot'),
    dataDirectory,
  })
  t.after(async () => {
    // Windows keeps the runtime's cwd open; close before removing its directory.
    await instance.close()
    await rm(root, { recursive: true, force: true })
  })
  return { instance, root, dataDirectory }
}

describe(
  'Copilot gateway with the packaged runtime, no credentials',
  { timeout: 30000 },
  () => {
    it('lazily starts the real isolated SDK runtime and reports unauthenticated metadata', async t => {
      const { instance, dataDirectory } = await gateway(t)
      assert.equal(instance['client'], undefined)
      assert.deepEqual(await instance.status(), {
        isAuthenticated: false,
        activeAccount: null,
        accountCount: 0,
        copilotPlan: null,
        errors: [],
      })
      assert.deepEqual(await instance.accounts(), [])
      assert.ok(instance.sdk)
      const runtimeStatus = await instance.sdk.getStatus()
      assert.equal(runtimeStatus.protocolVersion, 3)
      assert.ok(runtimeStatus.version.length > 0)
      assert.deepEqual(await instance.sdk.getAuthStatus(), {
        isAuthenticated: false,
        statusMessage: 'Not authenticated',
      })
      const options = instance.options
      assert.ok(options)
      assert.equal(options.useLoggedInUser, false)
      assert.equal(options.mode, 'empty')
      assert.equal(options.baseDirectory, dataDirectory)
      assert.equal(options.env?.HOME, join(dataDirectory, 'home'))
      assert.equal(options.env?.USERPROFILE, join(dataDirectory, 'home'))
      assert.equal(
        options.env?.GH_CONFIG_DIR,
        join(dataDirectory, 'home', 'gh')
      )
      for (const key of [
        'GH_TOKEN',
        'GITHUB_TOKEN',
        'COPILOT_GITHUB_TOKEN',
        'COPILOT_SDK_AUTH_TOKEN',
      ]) {
        assert.equal(options.env?.[key], '')
      }
      assert.equal(options.env?.GIT_ASKPASS, undefined)
      assert.equal(options.env?.COPILOT_API_TOKEN, undefined)

      const pid = instance.sdk['cliProcess']?.pid
      assert.ok(pid)
      const fileSystems = [...instance['fileSystems'].values()]
      assert.ok(fileSystems.length > 0)
      await instance.close()
      assert.throws(() => process.kill(pid, 0), { code: 'ESRCH' })
      for (const fs of fileSystems) {
        await assert.rejects(fs.exists('state'), /filesystem is disposed/)
      }
      const files = await readdir(dataDirectory, { recursive: true })
      assert.ok(
        !files.some(path =>
          /session-state|session-store\.db|config\.json|settings\.json/.test(
            path
          )
        )
      )
      await assert.rejects(instance.status(), /gateway is closed/)
      await instance.close()
    })

    it('preserves real authentication/model/selection errors instead of claiming availability', async t => {
      const { instance, root } = await gateway(t)
      await instance.status()
      assert.ok(instance.sdk)
      const unauthenticated = (error: unknown) => {
        assert.ok(error instanceof Error)
        assert.ok('code' in error && error.code === -32603)
        assert.match(error.message, /Not authenticated/)
        return true
      }
      await assert.rejects(instance.sdk.rpc.models.list({}), unauthenticated)
      const control = await instance['controlSession']
      assert.ok(control)
      await assert.rejects(control.rpc.model.list(), unauthenticated)
      await assert.rejects(
        instance.models('fixture-missing-account'),
        /Selected authentication is no longer available/
      )
      await assert.rejects(
        instance.generate({
          accountId: 'fixture-missing-account',
          modelId: 'auto',
          repositoryPath: root,
          systemPrompt: 'fixture system prompt',
          userPrompt: 'fixture diff',
        }),
        /Selected authentication is no longer available/
      )
      assert.equal(instance['generations'].size, 0)
      assert.deepEqual(await instance.accounts(), [])
      assert.equal((await instance.status()).isAuthenticated, false)
    })

    it('uses SDK-owned OAuth/PKCE start and opaque cancellation without logging in', async t => {
      const { instance } = await gateway(t)
      const login = await instance.beginLogin()
      assert.ok(login.flowId.length > 0)
      assert.equal(login.step.kind, 'open-url')
      if (login.step.kind !== 'open-url') {
        throw new Error(
          'Packaged github.com login did not return an authorize URL'
        )
      }
      const url = new URL(login.step.url)
      assert.equal(url.origin, 'https://github.com')
      assert.equal(url.pathname, '/login/oauth/authorize')
      assert.equal(url.searchParams.get('code_challenge_method'), 'S256')
      assert.ok(url.searchParams.get('state'))
      assert.ok(url.searchParams.get('code_challenge'))
      assert.equal(
        new URL(url.searchParams.get('redirect_uri')!).hostname,
        '127.0.0.1'
      )
      // Preparing a URL is not a completed login: the locked SDK cannot safely
      // namespace persistence, and neither "yes" nor polling may accept plaintext.
      await assert.rejects(instance.advanceLogin(login.flowId), {
        name: 'CopilotLoginPersistenceBlockedError',
      })
      await assert.rejects(instance.advanceLogin(login.flowId, 'yes'), {
        name: 'CopilotLoginPersistenceBlockedError',
      })
      // Browser opening and OAuth completion are deliberately not part of offline tests.
      await instance.cancelLogin(login.flowId)
      await assert.rejects(
        instance.advanceLogin(login.flowId),
        /Unknown or completed login flow/
      )
      await instance.cancelLogin(login.flowId)
      assert.equal((await instance.status()).isAuthenticated, false)
      assert.deepEqual(await instance.accounts(), [])
      const control = await instance['controlSession']
      assert.ok(control)
      await assert.rejects(
        control.rpc.accounts.login.begin({
          kind: 'githubDotCom',
          mode: 'selectAccount',
        }),
        /This login provider does not support identity selection/
      )
    })

    it('does not spawn or load credentials for already cancelled or incomplete selections', async t => {
      const { instance, root } = await gateway(t)
      const controller = new AbortController()
      controller.abort()
      await assert.rejects(
        instance.generate({
          accountId: 'fixture-account',
          modelId: 'auto',
          repositoryPath: root,
          systemPrompt: 'fixture system prompt',
          userPrompt: 'fixture diff',
          signal: controller.signal,
        }),
        { name: 'AbortError' }
      )
      await assert.rejects(instance.models(''), /account selection is required/)
      await assert.rejects(instance.logout(''), /account selection is required/)
      assert.equal(instance.sdk, undefined)
      assert.equal(instance['generations'].size, 0)
      assert.equal(instance['fileSystems'].size, 0)
    })

    it('is lazy when the runtime is missing and does not invent an authenticated state', async t => {
      const root = await directory()
      const instance = new ObservedGateway({
        runtimeRoot: join(root, 'missing-runtime'),
        dataDirectory: join(root, 'data'),
      })
      t.after(async () => {
        await instance.close()
        await rm(root, { recursive: true, force: true })
      })
      assert.equal(instance.sdk, undefined)
      await assert.rejects(instance.status(), /runtime entry point not found/)
      assert.equal(instance.sdk, undefined)
      await assert.rejects(readdir(join(root, 'data')), { code: 'ENOENT' })
    })
  }
)

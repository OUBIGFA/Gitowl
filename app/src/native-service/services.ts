import { AsyncLocalStorage } from 'async_hooks'
import { isAbsolute, join } from 'path'
import { ProviderStore } from '../lib/commit-generation/provider-store'
import { getRemotes } from '../lib/git/remote'
import { isRecord } from '../lib/is-record'
import { RepositoryService } from './repository-service'
import { GenerationService } from './generation-service'
import { CopilotGateway } from './copilot-gateway'
import { RemoteOperations } from './remote-operations'
import { NativeRPCService } from './protocol'
import { ServiceError } from './service-error'

interface ProxyContext {
  readonly origin: string | null
  readonly proxy: string | null
}

/** Compose upstream Git, official model SDKs and native-host platform services. */
export class NativeServices implements NativeRPCService {
  private readonly repositories = new RepositoryService()
  private generation: GenerationService | undefined
  private readonly proxyContexts = new AsyncLocalStorage<ProxyContext>()
  private readonly remote = new RemoteOperations(async url => {
    const context = this.proxyContexts.getStore()
    if (!context || new URL(url).origin !== context.origin) {
      throw new ServiceError(
        -32030,
        'Remote origin changed before proxy resolution; refresh and retry'
      )
    }
    return context.proxy ?? undefined
  })

  private generators(): GenerationService {
    if (this.generation === undefined) {
      const data = process.env.GITOWL_USER_DATA
      if (!data || !isAbsolute(data)) {
        throw new ServiceError(
          -32021,
          'The native host did not supply its user data directory'
        )
      }
      this.generation = new GenerationService(
        new ProviderStore(),
        new CopilotGateway({
          runtimeRoot: join(__dirname, 'copilot'),
          dataDirectory: join(data, 'copilot'),
        }),
        id => this.repositories.getRepository(id)
      )
    }
    return this.generation
  }

  public async invoke(
    method: string,
    rawParams: unknown,
    signal?: AbortSignal
  ): Promise<unknown> {
    if (method === 'system.info') {
      const info = (await this.repositories.invoke(method, rawParams)) as {
        protocol: number
        version: string
        capabilities: string[]
      }
      return {
        ...info,
        capabilities: [
          ...info.capabilities,
          'generation.settings',
          'providers.manage',
          'copilot.accounts',
          'repository.generateMessage',
          'repository.remotes',
          'repository.fetch',
          'repository.pull',
          'repository.push',
        ],
      }
    }
    if (
      method.startsWith('providers.') ||
      method.startsWith('generation.') ||
      method.startsWith('copilot.') ||
      method === 'repository.generateMessage'
    ) {
      return this.generators().invoke(method, rawParams, signal)
    }
    if (
      [
        'repository.remotes',
        'repository.remoteNetwork',
        'repository.fetch',
        'repository.pull',
        'repository.push',
      ].includes(method)
    ) {
      if (!isRecord(rawParams)) {
        throw new ServiceError(-32602, 'Expected repository parameters')
      }
      const p = rawParams
      const repository = this.repositories.getRepository(p.id)
      if (method === 'repository.remotes') {
        return this.remote.getRemoteInfo(repository)
      }
      if (method === 'repository.remoteNetwork') {
        const info = await this.remote.getRemoteInfo(repository)
        const remotes = await getRemotes(repository)
        // The Go networking layer needs only origins to resolve OS/PAC settings.
        // Userinfo, paths, query strings and Git credentials never leave here.
        return {
          currentRemoteName: info.currentRemoteName,
          remotes: remotes.map(remote => {
            let origin: string | null = null
            try {
              const url = new URL(remote.url)
              if (url.protocol === 'https:' || url.protocol === 'http:') {
                origin = url.origin
              }
            } catch {
              /* SSH/local remotes do not use HTTP proxies. */
            }
            return { name: remote.name, origin }
          }),
        }
      }
      if (p.remoteName !== undefined && typeof p.remoteName !== 'string') {
        throw new ServiceError(-32602, 'Expected a configured remote name')
      }
      if (
        (p.proxyOrigin !== null && typeof p.proxyOrigin !== 'string') ||
        (p.proxy !== null && typeof p.proxy !== 'string')
      ) {
        throw new ServiceError(
          -32602,
          'The native host must supply the resolved proxy context'
        )
      }
      if (typeof p.proxy === 'string') {
        let proxy: URL
        try {
          proxy = new URL(p.proxy)
        } catch {
          throw new ServiceError(-32602, 'Invalid proxy URL')
        }
        if (!['http:', 'socks5:', 'socks5h:'].includes(proxy.protocol)) {
          throw new ServiceError(
            -32602,
            'This Git runtime does not support the resolved proxy scheme'
          )
        }
      }
      const options = { remoteName: p.remoteName, signal }
      return this.proxyContexts.run(
        { origin: p.proxyOrigin, proxy: p.proxy },
        async () => {
          if (method === 'repository.fetch') {
            return this.remote.fetch(repository, options)
          }
          if (method === 'repository.pull') {
            return this.remote.pull(repository, {
              ...options,
              onHookFailure: async () => 'abort',
            })
          }
          return this.remote.push(repository, {
            ...options,
            onHookFailure: async () => 'abort',
          })
        }
      )
    }
    return this.repositories.invoke(method, rawParams)
  }

  public async close(): Promise<void> {
    await this.generation?.close()
  }
}

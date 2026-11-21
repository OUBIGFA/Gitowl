import {
  Agent,
  Dispatcher,
  ProxyAgent,
  Socks5ProxyAgent,
  WebSocket,
  Headers,
  setGlobalDispatcher,
} from 'undici'
import { getDesktopHost } from '../lib/desktop-host'
import { getAliveOrigin } from '../lib/alive-origin'

/** StableSocket uses the standard global constructor. Undici owns the full
 * WebSocket implementation; this adapter supplies only Desktop's Origin rule.
 */
export class NativeWebSocket extends WebSocket {
  public constructor(
    url: string | URL,
    protocols?: ConstructorParameters<typeof WebSocket>[1]
  ) {
    const options =
      typeof protocols === 'object' && !Array.isArray(protocols)
        ? protocols
        : { protocols }
    const headers = new Headers(options?.headers)
    const origin = getAliveOrigin(url)
    if (origin !== undefined) {
      headers.set('Origin', origin)
    }
    super(url, { ...options, headers })
  }
}

/** Resolve through MyGo's maintained system/PAC implementation; delegate HTTP,
 * TLS and SOCKS protocols to Node's maintained Undici clients. No PAC parser,
 * socket protocol or silent direct fallback is implemented by Gitowl.
 */
export class NativeProxyDispatcher extends Dispatcher {
  private readonly direct = new Agent()
  private readonly proxies = new Map<string, Dispatcher>()
  public constructor(
    private readonly resolve: (url: string) => Promise<string | null>
  ) {
    super()
  }

  public dispatch(
    options: Dispatcher.DispatchOptions,
    handler: Dispatcher.DispatchHandler
  ): boolean {
    const url = new URL(options.path, String(options.origin)).href
    void this.resolve(url)
      .then(proxy => {
        let dispatcher: Dispatcher = this.direct
        if (proxy !== null) {
          dispatcher = this.proxies.get(proxy)!
          if (dispatcher === undefined) {
            const scheme = new URL(proxy).protocol
            dispatcher =
              scheme === 'socks5:' || scheme === 'socks5h:'
                ? new Socks5ProxyAgent(proxy)
                : new ProxyAgent(proxy)
            this.proxies.set(proxy, dispatcher)
          }
        }
        dispatcher.dispatch(options, handler)
      })
      .catch(error => {
        // Undici's own DispatcherBase passes null when failure precedes request
        // admission; its published controller type has not represented null yet.
        handler.onResponseError?.(
          null as unknown as Dispatcher.DispatchController,
          error
        )
      })
    return true
  }

  public override destroy(): Promise<void> {
    return Promise.all(
      [this.direct, ...this.proxies.values()].map(agent => agent.destroy())
    ).then(() => {})
  }
}

export function installNativeNetwork(): NativeProxyDispatcher {
  const dispatcher = new NativeProxyDispatcher(
    url =>
      getDesktopHost().invoke('resolve-proxy-url', [url]) as Promise<
        string | null
      >
  )
  setGlobalDispatcher(dispatcher)
  globalThis.WebSocket = NativeWebSocket as typeof globalThis.WebSocket
  return dispatcher
}

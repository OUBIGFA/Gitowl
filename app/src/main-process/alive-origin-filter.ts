import { OrderedWebRequest } from './ordered-webrequest'
import { getAliveOrigin } from '../lib/alive-origin'

/**
 * Installs a web request filter to override the default Origin used to connect
 * to Alive web sockets
 */
export function installAliveOriginFilter(orderedWebRequest: OrderedWebRequest) {
  orderedWebRequest.onBeforeSendHeaders.addEventListener(async details => {
    const origin = getAliveOrigin(details.url)
    if (origin === undefined) {
      return {}
    }
    return {
      requestHeaders: {
        ...details.requestHeaders,
        Origin: origin,
      },
    }
  })
}

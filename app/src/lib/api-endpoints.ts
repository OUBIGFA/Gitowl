import { URL } from 'url'

// Git's proxy environment also needs endpoint-to-origin mapping. Keep this
// boundary independent of HTTP clients, account stores, and renderer globals.
const envEndpoint = process.env['DESKTOP_GITHUB_DOTCOM_API_ENDPOINT']
const envHTMLURL = process.env['DESKTOP_GITHUB_DOTCOM_HTML_URL']

/** Whether or not the given endpoint URI is under the ghe.com domain. */
export const isGHE = (ep: string) => new URL(ep).hostname.endsWith('.ghe.com')

/** Get github.com's API endpoint. */
export function getDotComAPIEndpoint(): string {
  // Only override this when developing against a local GitHub API.
  if (envEndpoint && envEndpoint.length > 0) {
    return envEndpoint
  }

  return 'https://api.github.com'
}

/** Map an API endpoint to its HTML site, retaining Enterprise ports. */
export function getHTMLURL(endpoint: string): string {
  if (envHTMLURL !== undefined) {
    return envHTMLURL
  }

  // GitHub.com uses the parent domain, whereas Enterprise uses the API origin.
  if (endpoint === getDotComAPIEndpoint() && !envEndpoint) {
    return 'https://github.com'
  } else {
    if (isGHE(endpoint)) {
      const url = new URL(endpoint)

      url.pathname = '/'

      if (url.hostname.startsWith('api.')) {
        url.hostname = url.hostname.replace(/^api\./, '')
      }

      return url.toString()
    }

    return new URL(endpoint).origin
  }
}

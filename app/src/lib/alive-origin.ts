/** Preserve Desktop's Alive service origin policy across presentation hosts. */
export function getAliveOrigin(url: string | URL): string | undefined {
  const { protocol, host } = new URL(url)
  if (
    protocol !== 'wss:' ||
    (!/^alive\.github\.com$/.test(host) &&
      !/^alive\.(.*)\.ghe\.com$/.test(host))
  ) {
    return undefined
  }
  return `https://${host.replace('alive.', 'desktop.')}`
}

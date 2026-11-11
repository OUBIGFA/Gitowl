import type { Account } from '../../models/account'

/** Display-only URL projection. The original URL stays in the controller for
 * continuations, so redacting a saved remote never silently changes Git auth. */
export function displayDialogURL(value: string): string {
  const relative = value.startsWith('//')
  const hasScheme =
    /^[a-z][a-z\d+.-]*:/i.test(value) && !/^[a-z]:[\\/]/i.test(value)
  if (hasScheme || relative) {
    try {
      const url = new URL(
        value,
        relative ? 'https://display.invalid' : undefined
      )
      if (url.host.length === 0 && value.includes('@')) {
        return ''
      }
      url.username = ''
      url.password = ''
      url.search = ''
      url.hash = ''
      return relative ? `//${url.host}${url.pathname}` : url.href
    } catch {
      // An unfinished URL in an input still must not reflect userinfo/query.
      return value
        .replace(/^(\w[\w+.-]*:(?:\/\/)?|\/\/)[^/]*@/, '$1')
        .split(/[?#]/, 1)[0]
    }
  }
  // Git's scp-style URL has no URL-parser representation. No userinfo is
  // needed to identify the displayed host/path; Windows/local paths stay exact.
  return value.replace(/^[^/\\\s]+@(?=[^/\\\s]+:)/, '').split(/[?#]/, 1)[0]
}

/** Diagnostics are technical original text, not Error/Account serialization.
 * Strip credential-bearing URLs and the exact currently known account tokens. */
export function displayDialogDiagnostic(
  message: string,
  accounts: ReadonlyArray<Account>
): string {
  let text = message.replace(
    /\b(?:[a-z][a-z\d+.-]*:\/\/|(?:https?|ssh|git):)[^\s<>"'`]+/gi,
    displayDialogURL
  )
  for (const account of accounts) {
    if (account.token.length > 0) {
      text = text.split(account.token).join('[redacted]')
    }
  }
  return text
}

import { AsyncLocalStorage } from 'async_hooks'

const documents = new AsyncLocalStorage<Document>()

/** Each inert parsing document owns its nodes. Async contexts prevent two
 * concurrent dialog filters from borrowing one another's document or base URL.
 */
export function withMarkdownDocument<T>(doc: Document, run: () => T): T {
  return documents.run(doc, run)
}

export function markdownDocument(): Document {
  const doc = documents.getStore()
  if (doc !== undefined) {return doc}
  if (typeof document !== 'undefined') {return document}
  throw new Error('Markdown parsing requires an owned document')
}

export function markdownNodeFilter(): typeof NodeFilter {
  return markdownDocument().defaultView?.NodeFilter ?? globalThis.NodeFilter
}

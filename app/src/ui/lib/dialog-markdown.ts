import { marked } from 'marked'
import DOMPurify from 'dompurify'
import {
  buildCustomMarkDownNodeFilterPipe,
  ICustomMarkdownFilterOptions,
} from '../../lib/markdown-filters/node-filter'

/** Original SandboxedMarkdown GFM/breaks and sanitizer, shared without rendering HTML natively. */
export function sanitizeDialogMarkdown(markdown: string) {
  return DOMPurify.sanitize(marked(markdown, { gfm: true, breaks: true }))
}

/** Original filter ordering and per-node cancellation; React owns height/tooltip work. */
export async function applyDialogMarkdownFilters(
  doc: Document,
  options: ICustomMarkdownFilterOptions,
  cancelled: () => boolean,
  onMutation: () => void
) {
  for (const nodeFilter of buildCustomMarkDownNodeFilterPipe(options)) {
    let mutated = false
    const walker = nodeFilter.createFilterTreeWalker(doc)
    let node = walker.nextNode()
    while (node !== null) {
      const replacements = await nodeFilter.filter(node)
      if (cancelled()) {
        return
      }
      const current = node
      node = walker.nextNode()
      if (replacements === null) {
        continue
      }
      mutated = true
      for (const replacement of replacements) {
        current.parentNode?.insertBefore(replacement, current)
      }
      current.parentNode?.removeChild(current)
    }
    if (mutated) {
      onMutation()
    }
  }
}

/**
 * Resolve only links produced by the original sanitized/filter pipeline. This
 * document is a kernel parsing workspace, never a visible Web/HTML frontend.
 */
export async function dialogMarkdownLinks(
  markdown: string,
  options: ICustomMarkdownFilterOptions,
  baseHref?: string,
  cancelled: () => boolean = () => false
) {
  const doc = document.implementation.createHTMLDocument('')
  if (baseHref !== undefined) {
    const base = doc.createElement('base')
    base.href = baseHref
    doc.head.append(base)
  }
  doc.body.innerHTML = sanitizeDialogMarkdown(markdown)
  await applyDialogMarkdownFilters(doc, options, cancelled, () => {})
  return Array.from(doc.querySelectorAll('a'))
    .filter(a => /^https?:/.test(a.protocol))
    .map(a => ({ label: a.textContent ?? a.href, url: a.href }))
}

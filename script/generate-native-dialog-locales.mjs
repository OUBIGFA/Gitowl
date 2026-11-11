import { createHash } from 'node:crypto'
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// Dialog label fixtures are plain text, with data in separate DTO fields. Quote
// literal braces/apostrophes for MyGo's ICU parser (e.g. {targetPath} help text).
const literalMessage = text =>
  text.replaceAll("'", "''").replace(/[{}]/g, brace => `'${brace}'`)
const catalogKey = key => `dialogLabel_${createHash('sha256').update(key).digest('hex').slice(0, 16)}`
const root = fileURLToPath(new URL('../', import.meta.url))
const sources = readdirSync(resolve(root, 'references'))
  .filter(name => /^native-ui-.*-labels\.json$/.test(name))
  .sort()
  .map(name => `references/${name}`)
const labels = new Map()
for (const source of sources) {
  const content = JSON.parse(readFileSync(resolve(root, source), 'utf8'))
  for (const [key, translations] of Object.entries(content)) {
    if (labels.has(key)) throw new Error(`Duplicate dialog label: ${key}`)
    labels.set(key, translations)
  }
}
const prefixes = ['aux.', 'operation.', 'repositoryDialog.', 'settings.', 'aux_', 'operation_', 'repositoryDialog_', 'settings_', 'dialogLabel_']
const check = process.argv.includes('--check')
for (const locale of ['en', 'zh-Hans', 'zh-Hant']) {
  const path = resolve(root, `app/native/locales/${locale}.json`)
  const current = readFileSync(path, 'utf8')
  const catalog = JSON.parse(current)
  for (const key of Object.keys(catalog)) {
    if (prefixes.some(prefix => key.startsWith(prefix))) delete catalog[key]
  }
  for (const [key, translations] of labels) {
    const value = translations[locale]
    if (typeof value !== 'string') {
      throw new Error(`Missing ${locale} translation for ${key}`)
    }
    // MyGo keys are flat and generated accessor names are case-normalized.
    // A short checked digest retains the semantic wire ID without collisions
    // between titles/actions (CreateBranch/createBranch) or reserved namespaces.
    const id = catalogKey(key)
    if (Object.hasOwn(catalog, id)) throw new Error(`Dialog catalog key collision: ${key}`)
    catalog[id] = literalMessage(value)
  }
  const generated = `${JSON.stringify(catalog, null, 2)}\n`
  if (check && generated !== current) {
    throw new Error(`Native dialog catalog is stale: ${path}`)
  }
  if (!check) writeFileSync(path, generated)
}
console.log(`Native dialog catalogs ${check ? 'checked' : 'generated'}: ${labels.size} labels in 3 languages`)

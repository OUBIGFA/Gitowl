import { createRequire } from 'node:module'
import { mkdirSync, mkdtempSync } from 'node:fs'
import { resolve, join } from 'node:path'

// Reuse the existing database/store regression suites against the production
// SQLite driver after their common test environment has initialized.
const require = createRequire(import.meta.url)
const { installNodeDatabase } = require('../../src/native-kernel/node-database') as typeof import('../../src/native-kernel/node-database')
const base = resolve('_temp/electron-removal-20261121/sqlite-regression')
mkdirSync(base, { recursive: true })
installNodeDatabase(mkdtempSync(join(base, 'process-')))

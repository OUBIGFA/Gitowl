import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync } from 'node:fs'
import { resolve, join } from 'node:path'

function fixture(action: string, directory: string) {
  const result = spawnSync(
    process.execPath,
    [
      '--conditions=import',
      '--import',
      'tsx',
      resolve('app/test/native-kernel/node-storage-fixture.ts'),
      directory,
      action,
    ],
    { cwd: resolve('.'), encoding: 'utf8', timeout: 30000 }
  )
  assert.equal(result.status, 0, result.stderr + result.stdout)
  assert.match(result.stdout, /storage contract passed/)
}

describe('plain Node production database', () => {
  const root = resolve('_temp/electron-removal-20261121/storage')
  mkdirSync(root, { recursive: true })

  it('persists actual repository records across processes and rolls back a failed transaction', () => {
    const profile = mkdtempSync(join(root, 'durable-'))
    fixture('write', profile)
    fixture('read', profile)
  })

  it('runs the existing account-isolation schema migration against SQLite', () => {
    fixture('upgrade', mkdtempSync(join(root, 'upgrade-')))
  })
  it('keeps a read transaction alive through native async record conversion', () => {
    fixture('async-read', mkdtempSync(join(root, 'async-read-')))
  })
})

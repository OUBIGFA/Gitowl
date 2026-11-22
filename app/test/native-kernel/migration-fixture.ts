import assert from 'node:assert/strict'
import { join } from 'node:path'
import { readFile } from 'node:fs/promises'
import Dexie from 'dexie'
import { installNodeDatabase } from '../../src/native-kernel/node-database'
import { importLegacyExport } from '../../src/native-kernel/import-legacy'

async function main() {
  const [source, destination] = process.argv.slice(2)
  installNodeDatabase(destination)
  await importLegacyExport(source, join(destination, 'migration.json'))
  const db = new Dexie('MigrationFixture')
  db.version(2).stores({ items: '++id, &[owner+name]', launches: '++' })
  const record = await db.table('items').get(7)
  assert.equal(record.owner, '账户')
  assert.equal(record.name, '项目')
  assert.equal(record.date.toISOString(), '2026-01-02T03:04:05.000Z')
  assert.equal(
    await db
      .table('items')
      .where('[owner+name]')
      .equals(['账户', '项目'])
      .count(),
    1
  )
  assert.equal(await db.table('items').add({ owner: 'next', name: 'next' }), 16)
  assert.equal((await db.table('launches').get(1)).mainReadyTime, 120)
  assert.equal(await db.table('launches').add({ mainReadyTime: 360 }), 3)
  assert.equal(localStorage.getItem('theme'), 'dark')
  assert.equal(
    JSON.parse(localStorage.getItem('providers')!).selected,
    'stable-id'
  )
  assert.equal(
    JSON.parse(await readFile(join(destination, 'migration.json'), 'utf8'))
      .format,
    1
  )
  db.close()
}
void main().catch(error => {
  console.error(error)
  process.exitCode = 1
})

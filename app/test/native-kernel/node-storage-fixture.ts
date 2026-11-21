import assert from 'node:assert/strict'
import { join } from 'node:path'
import { readdirSync, readFileSync } from 'node:fs'
import { installNodeDatabase } from '../../src/native-kernel/node-database'
import { RepositoriesDatabase } from '../../src/lib/databases/repositories-database'

// A separate plain Node process proves durability and prevents the unit runner's
// in-memory IndexedDB/browser test environment from satisfying this contract.
async function main() {
  const [directory, action] = process.argv.slice(2)
  assert.equal(typeof globalThis.window, 'undefined')
  assert.equal(typeof globalThis.document, 'undefined')
  Object.assign(globalThis, { log: console })
  installNodeDatabase(directory)

  const db = new RepositoriesDatabase(
    'Database',
    action === 'upgrade' ? 9 : undefined
  )
  if (action === 'write') {
    const id = await db.repositories.add({
      path: 'C:\\项目\\with spaces',
      alias: '保留名称',
      missing: false,
      gitHubRepositoryID: null,
      login: null,
    })
    await assert.rejects(
      db.repositories.add({
        path: 'C:\\项目\\with spaces',
        alias: null,
        missing: false,
        gitHubRepositoryID: null,
      }),
      { name: 'ConstraintError' }
    )
    await assert.rejects(
      db.transaction('rw', db.repositories, async () => {
        await db.repositories.update(id, { alias: '应回滚' })
        throw new Error('rollback probe')
      }),
      /rollback probe/
    )
    await db.protectedBranches.put({ repoId: 3, name: 'main' })
    await db.protectedBranches.put({ repoId: 3, name: 'release' })
    await db.protectedBranches.put({ repoId: 4, name: 'main' })
    assert.equal(
      await db.protectedBranches.where('repoId').equals(3).count(),
      2
    )
  } else if (action === 'read') {
    const records = await db.repositories.toArray()
    assert.equal(records.length, 1)
    assert.equal(records[0].alias, '保留名称')
    assert.equal(records[0].path, 'C:\\项目\\with spaces')
    assert.equal(
      (await db.protectedBranches.get([3, 'release']))?.name,
      'release'
    )
    const files = readdirSync(join(directory, 'databases')).filter(f =>
      f.endsWith('.sqlite')
    )
    assert.ok(files.length > 0)
    assert.ok(
      files.some(
        f =>
          readFileSync(join(directory, 'databases', f))
            .subarray(0, 16)
            .toString() === 'SQLite format 3\0'
      )
    )
  } else if (action === 'upgrade') {
    await db.gitHubRepositories.add({
      name: 'example',
      ownerID: 1,
      parentID: null,
      private: false,
      htmlURL: null,
      cloneURL: null,
      lastPruneDate: null,
      permissions: 'admin',
    })
    await db.protectedBranches.put({ repoId: 1, name: 'main' })
    db.close()
    const upgraded = new RepositoriesDatabase('Database')
    const records = await upgraded.gitHubRepositories.toArray()
    assert.equal(records[0].accountLogin, '')
    assert.equal(records[0].permissions, null)
    assert.equal(await upgraded.protectedBranches.count(), 0)
    upgraded.close()
  } else if (action === 'async-read') {
    await db.repositories.bulkAdd(
      Array.from({ length: 30 }, (_, index) => ({
        path: `C:\\projects\\${index}`,
        alias: null,
        missing: false,
        gitHubRepositoryID: null,
        login: null,
      }))
    )
    Object.assign(globalThis, {
      __WIN32__: true,
      __DARWIN__: false,
      __LINUX__: false,
      __DEV__: false,
      __TEST__: true,
      __DEV_SECRETS__: false,
    })
    const { RepositoriesStore } = await import(
      '../../src/lib/stores/repositories-store'
    )
    assert.equal((await new RepositoriesStore(db).getAll()).length, 30)
  } else {
    throw new Error('Unknown fixture action')
  }
  db.close()
  console.log('storage contract passed')
}
void main().catch(error => {
  console.error(error)
  process.exitCode = 1
})

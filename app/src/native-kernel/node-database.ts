import { mkdirSync } from 'fs'
import { isAbsolute, join } from 'path'
import Dexie from 'dexie'
import setGlobalVars from 'indexeddbshim'

/** Keep the existing schemas, transactions and migrations on the maintained
 * SQLite-backed IndexedDB implementation. No browser or in-memory substitute
 * is installed: only Dexie's documented database dependencies are supplied.
 */
export function installNodeDatabase(userData: string): void {
  if (!isAbsolute(userData)) {
    throw new Error('The database requires an absolute application data path')
  }
  const directory = join(userData, 'databases')
  mkdirSync(directory, { recursive: true })
  const api = setGlobalVars(
    {},
    {
      checkOrigin: false,
      databaseBasePath: directory,
      sysDatabaseBasePath: directory,
      addSQLiteExtension: true,
      useSQLiteIndexes: true,
      addNonIDBGlobals: false,
      replaceNonIDBGlobals: false,
    }
  )
  if (api.indexedDB === undefined || api.IDBKeyRange === undefined) {
    throw new Error(
      'SQLite IndexedDB initialization did not provide the required API'
    )
  }
  Dexie.dependencies.indexedDB = api.indexedDB as IDBFactory
  Dexie.dependencies.IDBKeyRange = api.IDBKeyRange
}

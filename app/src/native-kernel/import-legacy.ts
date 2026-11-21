import { readFile, writeFile } from 'fs/promises'
import { Typeson, structuredCloningForStorage } from 'typeson-registry'
import Dexie from 'dexie'

export interface ILegacyExport {
  readonly format: 1
  readonly settings: Record<string, string>
  readonly databases: ReadonlyArray<{
    name: string
    version: number
    stores: ReadonlyArray<{
      name: string
      keyPath: string | string[] | null
      autoIncrement: boolean
      indexes: ReadonlyArray<{
        name: string
        keyPath: string | string[]
        unique: boolean
        multiEntry: boolean
      }>
      keys: IDBValidKey[]
      values: unknown[]
      nextKey?: number
    }>
  }>
}

const request = <T>(value: IDBRequest<T>) =>
  new Promise<T>((resolve, reject) => {
    value.onsuccess = () => resolve(value.result)
    value.onerror = () => reject(value.error)
  })

/** Import into a new staging profile only. Callers publish that entire profile
 * after success; a failed transaction never replaces the user's old database.
 */
export async function importLegacyExport(
  source: string,
  receipt: string
): Promise<void> {
  const data = (await new Typeson()
    .register(structuredCloningForStorage)
    .parse(await readFile(source, 'utf8'))) as ILegacyExport
  if (
    data.format !== 1 ||
    typeof data.databases !== 'object' ||
    typeof data.settings !== 'object'
  ) {
    throw new Error('Unsupported legacy export format')
  }
  const factory = Dexie.dependencies.indexedDB
  for (const database of data.databases) {
    const opening = factory.open(database.name, database.version)
    opening.onupgradeneeded = () => {
      for (const store of database.stores) {
        const target = opening.result.createObjectStore(store.name, {
          keyPath: store.keyPath,
          autoIncrement: store.autoIncrement,
        })
        for (const index of store.indexes)
          {target.createIndex(index.name, index.keyPath, index)}
      }
    }
    const db = await request(opening)
    try {
      if (database.stores.length) {
        await new Promise<void>((resolve, reject) => {
          const tx = db.transaction(
            database.stores.map(store => store.name),
            'readwrite'
          )
          tx.oncomplete = () => resolve()
          tx.onabort = () =>
            reject(tx.error ?? new Error('Legacy import transaction aborted'))
          tx.onerror = () => {}
          for (const store of database.stores) {
            if (store.keys.length !== store.values.length) {
              tx.abort()
              return
            }
            const target = tx.objectStore(store.name)
            store.values.forEach((value, index) => {
              if (store.keyPath === null) {target.add(value, store.keys[index])}
              else {target.add(value)}
            })
            // Adding and deleting a high-water record uses the standard IDB
            // generator semantics, preserving IDs after the former maximum
            // record was deleted. Existing values/keys remain untouched.
            if (
              store.autoIncrement &&
              store.nextKey !== undefined &&
              store.nextKey > 1
            ) {
              const lastKey = store.nextKey - 1
              if (!store.keys.includes(lastKey)) {
                const marker = {}
                if (store.keyPath !== null)
                  {Dexie.setByKeyPath(marker, store.keyPath, lastKey)}
                if (store.keyPath === null) {target.add(marker, lastKey)}
                else {target.add(marker)}
                target.delete(lastKey)
              }
            }
          }
        })
      }
    } finally {
      db.close()
    }
  }
  for (const [key, value] of Object.entries(data.settings))
    {localStorage.setItem(key, value)}
  await writeFile(
    receipt,
    JSON.stringify({
      format: 1,
      databases: data.databases.map(d => d.name),
      settings: Object.keys(data.settings).length,
    }) + '\n',
    { encoding: 'utf8', mode: 0o600 }
  )
}

// Reproducible, synthetic fixture written by the actual retired dependencies.
// Run with Node WebStorage directed at an isolated settings.sqlite file.
const { createRequire } = require('node:module')
const { resolve, join } = require('node:path')
const { mkdirSync } = require('node:fs')
const appRequire = createRequire(resolve(__dirname, '../app/package.json'))
const setGlobalVars = appRequire('indexeddbshim').default
const root = resolve(process.argv[2])
mkdirSync(join(root, 'backend-data/databases'), { recursive: true })
const api = setGlobalVars(
  {},
  {
    checkOrigin: false,
    databaseBasePath: join(root, 'backend-data/databases'),
    sysDatabaseBasePath: join(root, 'backend-data/databases'),
    addSQLiteExtension: true,
    useSQLiteIndexes: true,
    addNonIDBGlobals: false,
    replaceNonIDBGlobals: false,
  }
)
const request = value =>
  new Promise((resolve, reject) => {
    value.onsuccess = () => resolve(value.result)
    value.onerror = () => reject(value.error)
  })
;(async () => {
  localStorage.setItem(
    'users',
    JSON.stringify([
      {
        login: 'fixture',
        endpoint: 'https://api.github.com',
        id: 42,
        name: 'Fixture',
      },
    ])
  )
  localStorage.setItem('has-shown-welcome-flow', '1')
  localStorage.setItem('theme', 'dark')
  const open = api.indexedDB.open('Database', 1)
  open.onupgradeneeded = () => {
    for (const name of [
      'repositories',
      'gitHubRepositories',
      'owners',
      'structuredRecords',
    ])
      open.result.createObjectStore(name, { keyPath: 'id' })
  }
  const db = await request(open)
  const tx = db.transaction([...db.objectStoreNames], 'readwrite')
  const complete = new Promise((resolve, reject) => {
    tx.oncomplete = resolve
    tx.onerror = () => reject(tx.error)
    tx.onabort = () => reject(tx.error)
  })
  tx.objectStore('repositories').put({
    id: 1,
    path: 'C:/fixture/中文^name',
    alias: '名称^0',
    missing: false,
    login: 'fixture',
    gitHubRepositoryID: 2,
  })
  tx.objectStore('gitHubRepositories').put({ id: 2, ownerID: 3 })
  tx.objectStore('owners').put({ id: 3, endpoint: 'https://api.github.com' })
  tx.objectStore('structuredRecords').put({
    id: 4,
    date: new Date('2026-01-02T03:04:05Z'),
    bytes: new Uint8Array([0, 1, 255]),
    value: '^\u0000',
  })
  await complete
  db.close()
  console.log('Wrote synthetic legacy storage with locked Node/IndexedDBShim')
})().catch(error => {
  console.error(error)
  process.exitCode = 1
})

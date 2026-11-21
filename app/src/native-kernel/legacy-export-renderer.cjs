// Read the actual legacy schema, keys and structured-clone values. Exporting
// never opens a non-existent database or changes the existing schema/version.
const { ipcRenderer } = require('electron')
const { Typeson, structuredCloningForStorage } = require('typeson-registry')
const request = value => new Promise((resolve, reject) => {
  value.onsuccess = () => resolve(value.result)
  value.onerror = () => reject(value.error)
})
;(async () => {
  const databases = []
  for (const info of await indexedDB.databases()) {
    const db = await request(indexedDB.open(info.name, info.version))
    try {
      const names = Array.from(db.objectStoreNames)
      const stores = []
      if (names.length) {
        const tx = db.transaction(names, 'readonly')
        await Promise.all(names.map(async name => {
          const store = tx.objectStore(name)
          const indexes = Array.from(store.indexNames, name => {
            const index = store.index(name)
            return { name, keyPath: index.keyPath, unique: index.unique, multiEntry: index.multiEntry }
          })
          const [keys, values] = await Promise.all([request(store.getAllKeys()), request(store.getAll())])
          stores.push({ name, keyPath: store.keyPath, autoIncrement: store.autoIncrement, indexes, keys, values })
        }))
      }
      // IndexedDB does not expose an auto-increment generator directly. A
      // standard aborted transaction observes its next key without committing
      // a record or consuming that key in the original database.
      for (const store of stores.filter(store => store.autoIncrement)) {
        store.nextKey = await new Promise((resolve, reject) => {
          const tx = db.transaction(store.name, 'readwrite')
          const probe = tx.objectStore(store.name).add({})
          let key
          probe.onsuccess = () => { key = probe.result; tx.abort() }
          probe.onerror = () => reject(probe.error)
          tx.onabort = () => key === undefined ? reject(tx.error) : resolve(key)
        })
      }
      databases.push({ name: info.name, version: db.version, stores })
    } finally { db.close() }
  }
  const settings = Object.fromEntries(Array.from({ length: localStorage.length }, (_, i) => {
    const key = localStorage.key(i)
    return [key, localStorage.getItem(key)]
  }))
  // Typeson's published structured-clone preset crosses V8 versions; native
  // V8 serialization from a newer Chromium cannot be read by older Node V8.
  const encoded = await new Typeson().register(structuredCloningForStorage)
    .stringify({ format: 1, databases, settings })
  await ipcRenderer.invoke('gitowl-export', encoded)
})().catch(error => ipcRenderer.send('gitowl-export-failed', { name: error.name, message: error.message }))

const { app, BrowserWindow } = require('electron')
const { join } = require('node:path')
app.setPath('userData', join(process.env.GITOWL_USER_DATA, 'desktop-kernel'))
app.whenReady().then(async () => {
  const window = new BrowserWindow({ show: false })
  await window.loadFile(join(__dirname, 'legacy-seed.html'))
  await window.webContents.executeJavaScript(`(async () => {
    localStorage.setItem('theme', 'dark');
    localStorage.setItem('providers', JSON.stringify({ selected: 'stable-id' }));
    await new Promise((resolve, reject) => {
      const request = indexedDB.open('MigrationFixture', 20);
      request.onupgradeneeded = () => {
        const store = request.result.createObjectStore('items', { keyPath: 'id', autoIncrement: true });
        store.createIndex('ownerName', ['owner', 'name'], { unique: true });
      };
      request.onerror = () => reject(request.error);
      request.onsuccess = () => {
        const db = request.result;
        const transaction = db.transaction('items', 'readwrite');
        transaction.objectStore('items').put({ id: 7, owner: '账户', name: '项目', date: new Date('2026-01-02T03:04:05Z') });
        transaction.objectStore('items').put({ id: 15, owner: 'removed', name: 'removed' });
        transaction.objectStore('items').delete(15);
        transaction.oncomplete = () => { db.close(); resolve(); };
        transaction.onabort = () => reject(transaction.error);
      };
    });
  })()`)
  app.exit(0)
}).catch(() => app.exit(1))

// One-time upgrade reader, run only by an already installed legacy Electron.
// It uses Chromium's own IndexedDB/localStorage APIs and never parses LevelDB.
// No Electron executable is distributed by the new package.
const { app, BrowserWindow, ipcMain } = require('electron')
const { join, isAbsolute } = require('node:path')
const { writeFileSync, renameSync, mkdirSync } = require('node:fs')
const profile = process.env.GITOWL_USER_DATA
const destination = process.env.GITOWL_LEGACY_EXPORT
if (!profile || !destination || !isAbsolute(profile) || !isAbsolute(destination)) {
  throw new Error('Legacy export requires explicit owned paths')
}
app.setPath('userData', join(profile, 'desktop-kernel'))
app.whenReady().then(async () => {
  const window = new BrowserWindow({ show: false, webPreferences: {
    nodeIntegration: true, contextIsolation: false,
  } })
  ipcMain.handle('gitowl-export', (_, data) => {
    mkdirSync(join(destination, '..'), { recursive: true })
    const pending = destination + '.pending'
    writeFileSync(pending, data, { encoding: 'utf8', mode: 0o600 })
    renameSync(pending, destination)
    setImmediate(() => app.exit(0))
  })
  ipcMain.on('gitowl-export-failed', (_, failure) => {
    console.error('Legacy export failed:', failure?.name ?? 'Error')
    app.exit(1)
  })
  window.webContents.on('console-message', (_, level, message) => {
    if (level >= 2) console.error('Legacy export renderer:', message)
  })
  await window.loadFile(join(__dirname, 'legacy-export.html'))
}).catch(() => app.exit(1))

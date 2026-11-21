import assert from 'node:assert/strict'
import { it } from 'node:test'
import { spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, existsSync } from 'node:fs'
import { resolve, join } from 'node:path'

it(
  'exports the actual legacy Chromium store and imports keys, dates, indexes and preferences into SQLite',
  { timeout: 60000 },
  () => {
    const root = resolve('_temp/electron-removal-20261121/migration')
    mkdirSync(root, { recursive: true })
    const profile = mkdtempSync(join(root, 'profile-'))
    const exported = join(profile, 'export.bin')
    const destination = join(profile, 'new-data')
    mkdirSync(destination)
    const env = {
      ...process.env,
      GITOWL_USER_DATA: profile,
      GITOWL_LEGACY_EXPORT: exported,
      ELECTRON_NO_ATTACH_CONSOLE: '1',
    }
    const electron = resolve('node_modules/electron/dist/electron.exe')
    assert.ok(
      existsSync(electron),
      'The isolated migration test requires the cached previous runtime'
    )
    for (const entry of [
      'app/test/native-kernel/legacy-seed.cjs',
      'app/src/native-kernel/legacy-export.cjs',
    ]) {
      const child = spawnSync(electron, [resolve(entry)], {
        env,
        encoding: 'utf8',
        timeout: 20000,
        windowsHide: true,
      })
      assert.equal(child.status, 0, child.stderr)
    }
    const child = spawnSync(
      process.execPath,
      [
        '--conditions=import',
        '--experimental-webstorage',
        `--localstorage-file=${join(destination, 'settings.sqlite')}`,
        '--import',
        'tsx',
        resolve('app/test/native-kernel/migration-fixture.ts'),
        exported,
        destination,
      ],
      { encoding: 'utf8', timeout: 20000, windowsHide: true }
    )
    assert.equal(child.status, 0, child.stderr)
    assert.ok(
      existsSync(join(profile, 'desktop-kernel/IndexedDB')),
      'Original data must remain available for recovery'
    )
  }
)

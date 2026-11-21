import assert from 'node:assert/strict'
import { it } from 'node:test'
import { readFile } from 'node:fs/promises'
import { join, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import { inventory } from './prepare-native-runtime'
import '../app/test/native-kernel/node-runtime-test'

it('ships the complete Node resources, verified codecs/storage and no Electron executable', async () => {
  const runtime = resolve(
    process.env.GITOWL_NODE_RUNTIME ?? 'output/native-runtime/backend'
  )
  const receipt = JSON.parse(
    await readFile(join(runtime, '../runtime-receipt.json'), 'utf8')
  )
  assert.equal(receipt.host, 'node')
  const files = await inventory(runtime, true)
  assert.deepEqual(files, receipt.files)
  const paths = files.map(file => file.path)
  for (const name of [
    'node.exe',
    'main.js',
    'migration.js',
    'cli.js',
    'install-cli.js',
    'image-worker.js',
    'git/cmd/git.exe',
    'git/ucrt64/libexec/git-core/git-credential-desktop.exe',
    'desktop-trampoline/desktop-askpass-trampoline.exe',
    'copilot/prebuilds/win32-x64/copilot-runtime.exe',
    'copilot/prebuilds/win32-x64/runtime.node',
    'static/licenses.json',
    'emoji.json',
  ]) {
    assert.ok(paths.includes(name), `Missing production resource: ${name}`)
  }
  assert.ok(
    !paths.some(path =>
      /(^|\/)(electron\.exe|native-kernel\.html|renderer\.js|chrome.*\.pak)$/.test(
        path
      )
    )
  )
  const git = spawnSync(join(runtime, 'git/cmd/git.exe'), ['--version'], {
    encoding: 'utf8',
    windowsHide: true,
  })
  assert.equal(git.status, 0, git.stderr)
  assert.match(git.stdout, /git version/)
  const native = spawnSync(
    join(runtime, 'node.exe'),
    [
      '--conditions=import',
      '-e',
      `
    if (process.versions.electron !== undefined) throw new Error('Electron runtime');
    require('./keytar.node'); require('./koffi.node');
    const Database = require('better-sqlite3'); const db = new Database(':memory:');
    if (db.prepare('select 42 as answer').get().answer !== 42) throw new Error('SQLite'); db.close();
    require('@imagemagick/magick-wasm'); require('upng-js'); require('jsdom');
  `,
    ],
    {
      cwd: runtime,
      env: { ...process.env, NODE_PATH: '', NODE_OPTIONS: '' },
      encoding: 'utf8',
      windowsHide: true,
    }
  )
  assert.equal(native.status, 0, native.stderr)
  const codec = spawnSync(
    join(runtime, 'node.exe'),
    [
      '-e',
      `
    const { Worker } = require('node:worker_threads');
    const worker = new Worker(require('node:path').resolve('image-worker.js'));
    worker.once('error', error => { console.error(error); process.exitCode = 1; });
    worker.once('message', result => {
      if (result.error || result.frame.width !== 17 || result.frame.data.length !== 884) {
        console.error(result.error || 'Invalid decoded frame'); process.exitCode = 1;
      }
      worker.terminate();
    });
    worker.postMessage({ id: 1, index: 0, mediaType: 'image/png', data: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAABEAAAANCAYAAABPeYUaAAAAIElEQVR4nGNkYPjfwEAhYKLUABAYNQQTjIYJJhhmYQIAalsBmdcOMzAAAAAASUVORK5CYII=', 'base64') });
  `,
    ],
    {
      cwd: runtime,
      env: { ...process.env, NODE_PATH: '', NODE_OPTIONS: '' },
      encoding: 'utf8',
      timeout: 20000,
      windowsHide: true,
    }
  )
  assert.equal(codec.status, 0, codec.stderr)
})

import assert from 'node:assert/strict'
import { it } from 'node:test'
import { createHash } from 'node:crypto'
import { createServer } from 'node:http'
import { mkdir, mkdtemp, readFile, rename, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { execFileSync } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'
import { PassThrough } from 'node:stream'
import { NodeKernelTransport } from '../../src/native-kernel/node-transport'

async function fixture() {
  const root = resolve('_temp/copilot-runtime-tests')
  await mkdir(root, { recursive: true })
  const work = await mkdtemp(join(root, 'run-'))
  const files = [
    [
      'package.json',
      '{"name":"@github/copilot-sdk-win32-x64","version":"test"}',
    ],
    ['LICENSE', 'Fixture license'],
    ['prebuilds/win32-x64/copilot-runtime.exe', 'fixture entry'],
    ['prebuilds/win32-x64/runtime.node', 'fixture library'],
  ]
  for (const [path, data] of files) {
    const file = join(work, 'source/package', path)
    await mkdir(dirname(file), { recursive: true })
    await writeFile(file, data)
  }
  const archivePath = join(work, 'runtime.tgz')
  execFileSync(
    'tar.exe',
    ['-czf', archivePath, '-C', join(work, 'source'), 'package'],
    { windowsHide: true }
  )
  const archive = await readFile(archivePath)
  const manifest = {
    schemaVersion: 1,
    package: '@github/copilot-sdk-win32-x64',
    version: 'test',
    platform: 'win32',
    arch: 'x64',
    url: 'https://registry.npmjs.org/@github/copilot-sdk-win32-x64/-/runtime.tgz',
    integrity: `sha512-${createHash('sha512')
      .update(archive)
      .digest('base64')}`,
    files: files.map(([path, data]) => ({
      path,
      size: Buffer.byteLength(data),
      sha256: createHash('sha256').update(data).digest('hex'),
    })),
  }
  const manifestPath = join(work, 'copilot-runtime.json')
  await writeFile(manifestPath, JSON.stringify(manifest))
  // Test-owned retired attempts are moved aside, never permanently deleted.
  let retired = 0
  const recycle = async (path: string) => {
    await rename(path, join(work, `retired-${++retired}`))
  }
  const { CopilotRuntimeDownload } = await import(
    '../../src/lib/copilot-runtime-download'
  )
  const cache = join(work, 'cache')
  const make = (fetchArchive: typeof fetch) =>
    new CopilotRuntimeDownload(manifestPath, cache, fetchArchive, recycle)
  return { work, archive, manifest, manifestPath, make }
}

it('downloads only on demand, verifies the official layout and reuses it offline across restarts', async t => {
  const f = await fixture()
  let requests = 0
  const server = createServer((_req, response) => {
    requests++
    response.writeHead(200, { 'Content-Length': f.archive.length })
    response.end(f.archive)
  })
  await new Promise<void>(resolveListen =>
    server.listen(0, '127.0.0.1', resolveListen)
  )
  t.after(() => {
    server.closeAllConnections()
    server.close()
  })
  const address = server.address() as { port: number }
  const installer = f.make((_url, options) =>
    fetch(`http://127.0.0.1:${address.port}/runtime.tgz`, options)
  )
  assert.equal(await installer.isInstalled(), false)
  assert.equal(requests, 0)
  const phases: string[] = []
  installer.events.on('change', () => {
    if (installer.status) { phases.push(installer.status.phase) }
  })
  const entry = await installer.prepare()
  assert.equal(await readFile(entry, 'utf8'), 'fixture entry')
  assert.equal(await installer.isInstalled(), true)
  assert.equal(requests, 1)
  assert.ok(
    phases.includes('downloading') &&
      phases.includes('verifying') &&
      phases.includes('extracting')
  )
  assert.equal(installer.status, null)
  const offline = f.make(async () => {
    throw new Error('Network must not be used')
  })
  assert.equal(await offline.prepare(), entry)
})

it('rejects changed archives before extraction and allows a clean retry', async () => {
  const f = await fixture()
  let corrupted = true
  const installer = f.make(
    async () => new Response(corrupted ? new Uint8Array([1, 2, 3]) : new Uint8Array(f.archive))
  )
  await assert.rejects(installer.prepare(), /integrity/i)
  assert.equal(await installer.isInstalled(), false)
  assert.equal(installer.status?.phase, 'error')
  corrupted = false
  await installer.prepare()
  assert.equal(await installer.isInstalled(), true)
})

it('checks every extracted file and repairs a corrupt cached package without reusing it', async () => {
  const f = await fixture()
  let requests = 0
  const fetchArchive: typeof fetch = async () => {
    requests++
    return new Response(new Uint8Array(f.archive))
  }
  const entry = await f.make(fetchArchive).prepare()
  await writeFile(join(dirname(entry), 'runtime.node'), 'corruption')
  await f.make(fetchArchive).prepare()
  assert.equal(requests, 2)
  assert.equal(
    await readFile(join(dirname(entry), 'runtime.node'), 'utf8'),
    'fixture library'
  )
  f.manifest.files[1].sha256 = '0'.repeat(64)
  await writeFile(f.manifestPath, JSON.stringify(f.manifest))
  await assert.rejects(f.make(fetchArchive).prepare(), /file verification/i)
})

it('shares one download; cancelling one caller leaves another caller active', async () => {
  const f = await fixture()
  let release!: () => void
  const waiting = new Promise<void>(resolveWait => {
    release = resolveWait
  })
  let requests = 0
  const installer = f.make(async () => {
    requests++
    await waiting
    return new Response(new Uint8Array(f.archive))
  })
  const controller = new AbortController()
  const first = installer.prepare(controller.signal)
  const second = installer.prepare()
  controller.abort()
  await assert.rejects(first, { name: 'AbortError' })
  release()
  await second
  assert.equal(requests, 1)
})

it('aborts the last cancelled download, leaves no usable partial package and can retry', async () => {
  const f = await fixture()
  let wasAborted = false
  let wait = true
  const installer = f.make(async (_url, options) => {
    if (wait) {
      try {
        await delay(30000, undefined, { signal: options?.signal ?? undefined })
      } catch (error) {
        wasAborted = true
        throw error
      }
    }
    return new Response(new Uint8Array(f.archive))
  })
  const pending = installer.prepare()
  while (installer.status?.phase !== 'downloading') { await delay(5) }
  installer.cancel()
  await assert.rejects(pending, { name: 'AbortError' })
  assert.equal(wasAborted, true)
  assert.equal(await installer.isInstalled(), false)
  wait = false
  await installer.prepare()
  assert.equal(await installer.isInstalled(), true)
})

it('rejects non-registry manifests and unsafe paths without making a request', async () => {
  const f = await fixture()
  const fetchArchive: typeof fetch = async () => {
    throw new Error('Unexpected request')
  }
  f.manifest.url = 'https://example.com/runtime.tgz'
  await writeFile(f.manifestPath, JSON.stringify(f.manifest))
  await assert.rejects(f.make(fetchArchive).prepare(), /manifest/i)
  f.manifest.url =
    'https://registry.npmjs.org/@github/copilot-sdk-win32-x64/-/runtime.tgz'
  f.manifest.files[0].path = '../escape'
  await writeFile(f.manifestPath, JSON.stringify(f.manifest))
  await assert.rejects(f.make(fetchArchive).prepare(), /manifest/i)
})

it('accepts the native cancellation event and announces host closure before pending actions settle', async () => {
  const input = new PassThrough(), output = new PassThrough()
  const transport = new NodeKernelTransport(input, output, () => {})
  let cancelled = false, closed = false
  transport.events.on('copilot-runtime-cancel', () => { cancelled = true })
  transport.events.on('host-closed', () => { closed = true })
  input.write(JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'kernel.platformEvent', params: { name: 'copilot-runtime-cancel', args: [] } }) + '\n')
  input.end()
  await delay(10)
  assert.equal(cancelled, true)
  assert.equal(closed, true)
})

it('prepares the pinned official archive and starts the real SDK runtime without an account', {
  skip: !process.env.GITOWL_COPILOT_ARCHIVE,
}, async () => {
  const { CopilotRuntimeDownload } = await import('../../src/lib/copilot-runtime-download')
  const { CopilotClient, RuntimeConnection } = await import('@github/copilot-sdk')
  const root = resolve('_temp/copilot-runtime-tests')
  await mkdir(root, { recursive: true })
  const work = await mkdtemp(join(root, 'official-'))
  const archive = await readFile(process.env.GITOWL_COPILOT_ARCHIVE!)
  let retired = 0
  const installer = new CopilotRuntimeDownload(
    resolve('output/native-runtime/backend/copilot-runtime.json'), join(work, 'cache'),
    async () => new Response(new Uint8Array(archive)),
    async path => { await rename(path, join(work, `retired-${++retired}`)) }
  )
  const entry = await installer.prepare()
  const client = new CopilotClient({
    connection: RuntimeConnection.forStdio({ path: entry }),
    baseDirectory: join(work, 'home'),
    useLoggedInUser: false,
  })
  try {
    await client.start()
    const pong = await client.ping('gitowl-first-use')
    assert.equal(pong.message, 'pong: gitowl-first-use')
  } finally { await client.stop() }
})

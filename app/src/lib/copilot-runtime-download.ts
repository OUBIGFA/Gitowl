import { createHash } from 'crypto'
import { createReadStream, createWriteStream } from 'fs'
import {
  cp,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rename,
} from 'fs/promises'
import { dirname, join } from 'path'
import { execFile } from 'child_process'
import { promisify } from 'util'
import { EventEmitter } from 'events'
import { Readable, Transform } from 'stream'
import { pipeline } from 'stream/promises'
import { getCopilotRuntimePath } from './copilot-runtime'

export interface ICopilotRuntimeManifest {
  readonly schemaVersion: 1
  readonly package: string
  readonly version: string
  readonly platform: NodeJS.Platform
  readonly arch: string
  readonly url: string
  readonly integrity: string
  readonly files: ReadonlyArray<{
    readonly path: string
    readonly size: number
    readonly sha256: string
  }>
}

export interface ICopilotRuntimeDownloadStatus {
  readonly phase: 'downloading' | 'verifying' | 'extracting' | 'error'
  readonly received: number
  readonly total: number
  readonly error?: string
}

const runFile = promisify(execFile)

async function exists(path: string): Promise<boolean> {
  try {
    await lstat(path)
    return true
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
      return false
    }
    throw error
  }
}

async function digest(path: string, algorithm: string): Promise<string> {
  const hash = createHash(algorithm)
  for await (const chunk of createReadStream(path)) {
    hash.update(chunk)
  }
  return hash.digest('hex')
}

/** Acquire the exact official SDK platform package. Node streams the download
 * through the existing system-proxy dispatcher; Windows' libarchive owns tar
 * extraction. No npm install scripts, unpinned releases or alternate mirrors.
 */
export class CopilotRuntimeDownload {
  public readonly events = new EventEmitter()
  public status: ICopilotRuntimeDownloadStatus | null = null
  private pending?: {
    promise: Promise<string>
    controller: AbortController
    users: number
  }
  private verifiedPath?: string

  public constructor(
    private readonly manifestPath: string,
    private readonly cache: string,
    private readonly fetchArchive: typeof fetch,
    private readonly recycle: (path: string) => Promise<void>
  ) {}

  private update(status: ICopilotRuntimeDownloadStatus | null) {
    this.status = status
    this.events.emit('change')
  }

  private async manifest(): Promise<ICopilotRuntimeManifest> {
    const value: ICopilotRuntimeManifest = JSON.parse(
      await readFile(this.manifestPath, 'utf8')
    )
    if (
      value.schemaVersion !== 1 ||
      value.platform !== 'win32' ||
      value.arch !== 'x64' ||
      value.package !== '@github/copilot-sdk-win32-x64' ||
      !/^[\w.+-]+$/.test(value.version) ||
      !value.url.startsWith(
        'https://registry.npmjs.org/@github/copilot-sdk-win32-x64/-/'
      ) ||
      !/^sha512-[A-Za-z0-9+/]{86}==$/.test(value.integrity) ||
      !Array.isArray(value.files) ||
      value.files.length === 0 ||
      value.files.some(
        (file: ICopilotRuntimeManifest['files'][number]) =>
          !file.path ||
          file.path
            .split('/')
            .some(part => !part || part === '.' || part === '..') ||
          /[\\:\0]/.test(file.path) ||
          !/^[a-f0-9]{64}$/.test(file.sha256) ||
          !Number.isSafeInteger(file.size) ||
          file.size < 0
      )
    ) {
      throw new Error('Invalid Copilot runtime manifest')
    }
    return value
  }

  private directory(manifest: ICopilotRuntimeManifest) {
    // Include the pinned archive identity, not just a mutable version label.
    const key = createHash('sha256')
      .update(manifest.integrity)
      .digest('hex')
      .slice(0, 16)
    return join(
      this.cache,
      `${manifest.platform}-${manifest.arch}-${manifest.version}-${key}`
    )
  }

  /** Passive account/model refresh must not start the first download. */
  public async isInstalled(): Promise<boolean> {
    const manifest = await this.manifest()
    return exists(
      getCopilotRuntimePath(
        this.directory(manifest),
        manifest.platform,
        manifest.arch
      )
    )
  }

  public cancel(): void {
    this.pending?.controller.abort()
    if (!this.pending) {
      this.update(null)
    }
  }

  public prepare(signal?: AbortSignal): Promise<string> {
    if (signal?.aborted) {
      return Promise.reject(signal.reason)
    }
    if (this.pending?.controller.signal.aborted) {
      return this.pending.promise
        .catch(() => undefined)
        .then(() => this.prepare(signal))
    }
    if (this.pending === undefined) {
      const controller = new AbortController()
      const pending = { controller, users: 0, promise: Promise.resolve('') }
      pending.promise = this.install(controller.signal)
        .then(
          path => {
            this.update(null)
            return path
          },
          error => {
            this.update(
              controller.signal.aborted
                ? null
                : {
                    phase: 'error',
                    received: 0,
                    total: 0,
                    error:
                      error instanceof Error ? error.message : String(error),
                  }
            )
            throw error
          }
        )
        .finally(() => {
          if (this.pending === pending) {
            this.pending = undefined
          }
        })
      this.pending = pending
    }
    const pending = this.pending
    pending.users++
    // Cancelling one request must not interrupt another feature using the same
    // download. The explicit download Cancel button cancels all waiters.
    return new Promise((resolveWait, reject) => {
      let settled = false
      const finish = (error: unknown, path?: string) => {
        if (settled) {
          return
        }
        settled = true
        signal?.removeEventListener('abort', abort)
        pending.users--
        if (pending.users === 0 && this.pending === pending) {
          pending.controller.abort()
        }
        if (error !== undefined) {
          reject(error)
        } else {
          resolveWait(path!)
        }
      }
      const abort = () => finish(signal!.reason)
      signal?.addEventListener('abort', abort, { once: true })
      pending.promise.then(
        path => finish(undefined, path),
        error => finish(error)
      )
    })
  }

  private async verify(
    directory: string,
    manifest: ICopilotRuntimeManifest,
    signal: AbortSignal
  ) {
    const expected = new Map(manifest.files.map(file => [file.path, file]))
    let count = 0
    const walk = async (relative = ''): Promise<void> => {
      const base = join(directory, relative)
      if (
        !(await lstat(base)).isDirectory() ||
        (await lstat(base)).isSymbolicLink()
      ) {
        throw new Error('Copilot runtime file verification failed: directory')
      }
      for (const entry of await readdir(base, { withFileTypes: true })) {
        signal.throwIfAborted()
        const path = relative ? `${relative}/${entry.name}` : entry.name
        if (entry.isDirectory()) {
          await walk(path)
          continue
        }
        const file = expected.get(path)
        if (
          !entry.isFile() ||
          !file ||
          (await lstat(join(directory, path))).size !== file.size ||
          (await digest(join(directory, path), 'sha256')) !== file.sha256
        ) {
          throw new Error(`Copilot runtime file verification failed: ${path}`)
        }
        count++
      }
    }
    await walk()
    if (count !== expected.size) {
      throw new Error('Copilot runtime file verification failed: missing files')
    }
  }

  private async install(signal: AbortSignal): Promise<string> {
    const manifest = await this.manifest()
    const destination = this.directory(manifest)
    const entry = getCopilotRuntimePath(
      destination,
      manifest.platform,
      manifest.arch
    )
    signal.throwIfAborted()
    if (this.verifiedPath === entry && (await exists(entry))) {
      return entry
    }
    if (await exists(destination)) {
      try {
        await this.verify(destination, manifest, signal)
        this.verifiedPath = entry
        return entry
      } catch {
        signal.throwIfAborted()
        // Preserve an invalid cached package in the recycle bin before repair.
        // A recycle failure is actionable and must not be bypassed with rm.
        await this.recycle(destination)
      }
    }
    await mkdir(join(this.cache, '_temp'), { recursive: true })
    const work = await mkdtemp(join(this.cache, '_temp', 'download-'))
    const staging = join(work, 'package')
    try {
      // An upgraded install may still have the exact old bundled package. Copy
      // only after verification; credentials and Copilot sessions are elsewhere.
      const bundled = join(dirname(this.manifestPath), 'copilot')
      let reuseBundled = false
      if (await exists(bundled)) {
        try {
          await this.verify(bundled, manifest, signal)
          reuseBundled = true
        } catch {
          signal.throwIfAborted()
          log.info(
            'The older bundled Copilot component does not match this version; downloading the pinned package'
          )
        }
      }
      if (reuseBundled) {
        await cp(bundled, staging, { recursive: true })
      } else {
        this.update({ phase: 'downloading', received: 0, total: 0 })
        const requestSignal = AbortSignal.any([
          signal,
          AbortSignal.timeout(10 * 60 * 1000),
        ])
        const response = await this.fetchArchive(manifest.url, {
          signal: requestSignal,
          redirect: 'error',
        })
        if (!response.ok || !response.body) {
          throw new Error(
            `Copilot component download failed: HTTP ${response.status}`
          )
        }
        const total = Number(response.headers.get('content-length')) || 0
        let received = 0,
          published = 0
        const hash = createHash('sha512')
        const archive = join(work, 'runtime.tgz')
        await pipeline(
          Readable.fromWeb(
            response.body as Parameters<typeof Readable.fromWeb>[0]
          ),
          new Transform({
            transform: (chunk: Buffer, _encoding, done) => {
              received += chunk.length
              hash.update(chunk)
              if (Date.now() - published >= 200) {
                published = Date.now()
                this.update({ phase: 'downloading', received, total })
              }
              done(null, chunk)
            },
          }),
          createWriteStream(archive, { flags: 'wx' }),
          { signal: requestSignal }
        )
        this.update({ phase: 'verifying', received, total })
        if (`sha512-${hash.digest('base64')}` !== manifest.integrity) {
          throw new Error('Copilot component download integrity check failed')
        }
        signal.throwIfAborted()
        await mkdir(staging)
        this.update({ phase: 'extracting', received, total })
        // System32 avoids a project/PATH executable shadowing Windows bsdtar.
        await runFile(
          join(process.env.SystemRoot!, 'System32', 'tar.exe'),
          [
            '-xf',
            archive,
            '-C',
            staging,
            '--strip-components=1',
            '--no-same-owner',
            '--no-same-permissions',
          ],
          { windowsHide: true, signal, timeout: 120000 }
        )
      }
      await this.verify(staging, manifest, signal)
      signal.throwIfAborted()
      await rename(staging, destination)
      this.verifiedPath = entry
      return entry
    } finally {
      // Shutdown may close the native trash service first. Keep failed cleanup
      // visible in diagnostics and leave the task-owned directory recoverable.
      await this.recycle(work).catch(error =>
        log.warn('Could not recycle Copilot download files', error)
      )
    }
  }
}

let nativeDownload: CopilotRuntimeDownload | undefined

/** One owner per backend; callers and the native progress view share it. */
export function getNativeCopilotRuntime(
  userData: string
): CopilotRuntimeDownload {
  if (nativeDownload === undefined) {
    nativeDownload = new CopilotRuntimeDownload(
      join(__dirname, 'copilot-runtime.json'),
      join(userData, 'components', 'copilot'),
      (...args) => fetch(...args),
      async path => {
        const { moveItemToTrash } = await import('../ui/main-process-proxy')
        await moveItemToTrash(path)
      }
    )
  }
  return nativeDownload
}

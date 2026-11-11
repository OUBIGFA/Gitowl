import { execFile } from 'child_process'
import { lstat, readFile, realpath } from 'fs/promises'
import { isAbsolute, relative, resolve, sep } from 'path'
import { promisify } from 'util'
import { hash, inventory } from './prepare-native-runtime'

export type Artifact = {
  readonly path: string
  readonly sha256: string
}

export type SourceTreeEvidence = {
  readonly schemaVersion: 1
  readonly revision: string
  /** The repository gate reads this exact index tree, not workspace bytes. */
  readonly sourceTree: string
  readonly files: ReadonlyArray<Artifact>
  readonly submodules: ReadonlyArray<{ path: string; revision: string }>
  readonly environmentSha256: string
  readonly sourceDigest: string
  /** Review/capture artifacts are bound separately, not added to shipping source. */
  readonly artifacts: ReadonlyArray<Artifact>
  readonly evidenceDigest: string
}

export function requireCondition(
  value: unknown,
  message: string
): asserts value {
  if (!value) {
    throw new Error(message)
  }
}

export const jsonHash = (value: unknown) => hash(JSON.stringify(value))
export const isSHA = (value: unknown): value is string =>
  typeof value === 'string' && /^[a-f0-9]{64}$/.test(value)

/** Receipts contain repository-relative paths only. Reject traversal, junctions
 * and symlinks, including a linked ancestor, before reading any evidence. */
export async function safePath(root: string, name: string): Promise<string> {
  requireCondition(
    typeof name === 'string' &&
      name.length > 0 &&
      !isAbsolute(name) &&
      !/[\\:\x00-\x1f]/.test(name) &&
      name.split('/').every(p => p !== '' && p !== '.' && p !== '..'),
    `Unsafe evidence path: ${name}`
  )
  const base = await realpath(root)
  let current = base
  for (const part of name.split('/')) {
    current = resolve(current, part)
    const entry = await lstat(current)
    requireCondition(!entry.isSymbolicLink(), `Linked evidence path: ${name}`)
    const actual = await realpath(current)
    const within = relative(base, actual)
    requireCondition(
      within !== '..' && !within.startsWith(`..${sep}`) && !isAbsolute(within),
      `Evidence escapes repository: ${name}`
    )
  }
  return current
}

export function isEvidencePath(name: string) {
  return /^(?:_temp|output)\//.test(name)
}

export async function artifact(root: string, name: string): Promise<Artifact> {
  const file = await safePath(root, name)
  requireCondition((await lstat(file)).isFile(), `Not a file: ${name}`)
  const bytes = await readFile(file)
  requireCondition(bytes.length > 0, `Empty evidence: ${name}`)
  return { path: name, sha256: hash(bytes) }
}

export async function verifyArtifact(root: string, item: Artifact) {
  requireCondition(item && isSHA(item.sha256), 'Invalid artifact SHA-256')
  const actual = await artifact(root, item.path)
  requireCondition(
    actual.sha256 === item.sha256,
    `Changed artifact: ${item.path}`
  )
  return actual
}

export async function evidenceJSON(root: string, item: Artifact) {
  requireCondition(
    isEvidencePath(item.path),
    'Evidence must be in ignored output/ or _temp/'
  )
  await verifyArtifact(root, item)
  return JSON.parse(await readFile(await safePath(root, item.path), 'utf8'))
}

export async function git(root: string, args: ReadonlyArray<string>) {
  const { stdout } = await promisify(execFile)('git', [...args], {
    cwd: root,
    maxBuffer: 32 * 1024 * 1024,
    encoding: 'utf8',
  })
  return stdout
}

/** Use Git's input set, not a second runtime source walk. Raw bytes also catch
 * CRLF changes. Untracked public inputs count before staging; ignored private
 * profiles never do. prepare-native-runtime remains the runtime input authority. */
export async function captureSourceTreeEvidence(
  root: string,
  reviewArtifacts: ReadonlyArray<Artifact> = []
): Promise<SourceTreeEvidence> {
  const files: Artifact[] = []
  const submodules: Array<{ path: string; revision: string }> = []
  async function collect(directory: string, prefix: string) {
    const names = [
      ...new Set(
        (
          await git(directory, [
            'ls-files',
            '-z',
            '--cached',
            '--others',
            '--exclude-standard',
          ])
        )
          .split('\0')
          .filter(Boolean)
      ),
    ].sort()
    requireCondition(names.length > 0, `No source inputs: ${prefix || '.'}`)
    const gitlinks = new Set(
      (await git(directory, ['ls-files', '--stage', '-z']))
        .split('\0')
        .map(line => /^160000 [a-f0-9]+ 0\t(.+)$/.exec(line)?.[1])
        .filter(Boolean)
    )
    for (const name of names) {
      const path = prefix + name
      requireCondition(
        !/^docs\//.test(path) &&
          !/(^|\/)(?:\.local|\.git|node_modules|output|_temp|\.agents)(\/|$)/.test(
            path
          ) &&
          !/(^|\/)(?:AGENTS|CLAUDE)\.md$/.test(path),
        `Private/generated input in source set: ${path}`
      )
      const absolute = await safePath(root, path)
      if (gitlinks.has(name)) {
        // Runtime inputs include three actual upstream submodules. Hash their
        // Git input sets too, including uncommitted bytes, not just gitlinks.
        const revision = (await git(absolute, ['rev-parse', 'HEAD'])).trim()
        requireCondition(
          /^[a-f0-9]{40,64}$/.test(revision),
          `Invalid submodule: ${path}`
        )
        submodules.push({ path, revision })
        await collect(absolute, path + '/')
      } else {
        requireCondition(
          (await lstat(absolute)).isFile(),
          `Not a regular source input: ${path}`
        )
        // Empty source files are legal; empty evidence receipts are not.
        files.push({ path, sha256: hash(await readFile(absolute)) })
      }
    }
  }
  await collect(root, '')
  files.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
  const revision = (await git(root, ['rev-parse', 'HEAD'])).trim()
  requireCondition(/^[a-f0-9]{40,64}$/.test(revision), 'Invalid Git revision')
  const sourceTree = (await git(root, ['write-tree'])).trim()
  requireCondition(/^[a-f0-9]{40,64}$/.test(sourceTree), 'Invalid Git source tree')
  const inputs = {
    schemaVersion: 1 as const,
    revision,
    sourceTree,
    files,
    submodules,
    // Same original OAuth overrides as prepare-native-runtime. Never record
    // credentials or require a dedicated OAuth client instead of upstream's.
    environmentSha256: jsonHash([
      process.env.DESKTOP_OAUTH_CLIENT_ID ?? '',
      process.env.DESKTOP_OAUTH_CLIENT_SECRET ?? '',
    ]),
  }
  const artifacts = []
  for (const item of reviewArtifacts) {
    requireCondition(
      isEvidencePath(item.path),
      'Review artifacts must stay outside shipping inputs'
    )
    artifacts.push(await verifyArtifact(root, item))
  }
  const sourceDigest = jsonHash(inputs)
  return {
    ...inputs,
    sourceDigest,
    artifacts,
    evidenceDigest: jsonHash({ sourceDigest, artifacts }),
  }
}

/** Reuse the runtime's canonical inventory for assets, checking path boundaries
 * first. inventory itself rejects non-file entries inside the directory. */
export async function treeArtifact(
  root: string,
  name: string
): Promise<Artifact> {
  const files = await inventory(await safePath(root, name))
  requireCondition(files.length > 0, `Empty asset directory: ${name}`)
  for (const file of files) {
    await safePath(root, `${name}/${file.path}`)
  }
  return { path: name, sha256: jsonHash(files) }
}

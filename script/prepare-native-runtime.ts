/// <reference path="./globals.d.ts" />

import { createHash } from 'crypto'
import { execFile, spawn } from 'child_process'
import { promisify } from 'util'
import { cp, mkdir, readFile, readdir, rm, writeFile } from 'fs/promises'
import { basename, join, resolve } from 'path'
import { getNpmCommand } from './npm'
import { getSHA } from '../app/git-info'

const root = resolve(__dirname, '..')
const output = join(root, 'output', 'native-runtime')
const kernel = join(output, 'desktop-kernel')
export const hash = (data: Buffer | string) =>
  createHash('sha256').update(data).digest('hex')

type FileRecord = { path: string; sha256: string }

/** Ask the original DefinePlugin input in the exact compiler environment. Only
 * its callback scheme is copied to MyGo, never OAuth credentials or tokens.
 * Installers do not claim both Desktop schemes: the running MyGo owner
 * registers only the scheme selected by the unchanged __DEV_SECRETS__ rule. */
export async function nativeRuntimeLaunchConfig(env: NodeJS.ProcessEnv) {
  const { stdout } = await promisify(execFile)(
    process.execPath,
    [
      '-r',
      'ts-node/register',
      '-e',
      `const r = require('./app/app-info').getReplacements(); process.stdout.write(JSON.stringify({oauthCallbackScheme: r.__DEV_SECRETS__ ? 'x-github-desktop-dev-auth' : 'x-github-desktop-auth'}))`,
    ],
    { cwd: root, env: { ...env, TS_NODE_PROJECT: 'script/tsconfig.json' } }
  )
  return JSON.parse(stdout) as { readonly oauthCallbackScheme: string }
}

/** Source walks omit Git metadata; exact runtime walks must detect every file.
 * Canonical path order is shared with the Python dependency registration. */
export async function inventory(
  directory: string,
  includeGitMetadata = false
): Promise<FileRecord[]> {
  const records: FileRecord[] = []
  async function walk(relative: string) {
    for (const item of await readdir(join(directory, relative), {
      withFileTypes: true,
    })) {
      if (item.name === '.git' && !includeGitMetadata) {
        continue
      }
      const name = join(relative, item.name)
      if (item.isDirectory()) {
        await walk(name)
      } else if (item.isFile()) {
        records.push({
          path: name.replaceAll('\\', '/'),
          sha256: hash(await readFile(join(directory, name))),
        })
      } else {
        throw new Error(`Unsupported runtime entry: ${name}`)
      }
    }
  }
  await walk('')
  return records.sort((a, b) =>
    a.path < b.path ? -1 : a.path > b.path ? 1 : 0
  )
}

/** A receipt never authorizes reuse of changed output, including licenses. */
export async function reusableRuntime(
  receiptPath: string,
  directory: string,
  inputHash: string
): Promise<boolean> {
  try {
    const receipt = JSON.parse(await readFile(receiptPath, 'utf8'))
    return (
      receipt.inputHash === inputHash &&
      JSON.stringify(receipt.files) ===
        JSON.stringify(await inventory(directory, true))
    )
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
      throw error
    }
    return false
  }
}

function run(executable: string, args: readonly string[], env = process.env) {
  return new Promise<void>((resolve, reject) => {
    const child = spawn(executable, args, { cwd: root, env, stdio: 'inherit' })
    child.once('error', reject)
    child.once('exit', code =>
      code === 0
        ? resolve()
        : reject(new Error(`${executable} ${args.join(' ')} failed (${code})`))
    )
  })
}

async function prepare() {
  if (process.platform !== 'win32' || process.arch !== 'x64') {
    throw new Error('This runtime adapter currently validates Windows x64 only')
  }
  const manifest = JSON.parse(
    await readFile(join(root, 'third_party/native-runtimes.json'), 'utf8')
  )
  if (
    manifest.schemaVersion !== 2 ||
    manifest.node.role !== 'build-tool-only'
  ) {
    throw new Error(
      'Register the original Electron runtime, not a Node service'
    )
  }
  if (
    hash(await readFile(process.execPath)) !== manifest.node.executableSha256
  ) {
    throw new Error('Use the registered Node build tool through npm')
  }
  for (const lock of manifest.lockfiles) {
    // Normalize only lockfile text, as in record_native_dependencies.py.
    const text = (await readFile(join(root, lock.path), 'utf8')).replaceAll(
      '\r\n',
      '\n'
    )
    if (hash(text) !== lock.sha256) {
      throw new Error(`Stale native dependency registration: ${lock.path}`)
    }
  }
  const electron = join(root, 'node_modules/electron/dist')
  const electronFiles = await inventory(electron)
  if (
    hash(JSON.stringify(electronFiles)) !== manifest.electron.distTreeSha256
  ) {
    throw new Error(
      'Electron dist differs from the registered official archive'
    )
  }
  for (const input of manifest.bundledInputs) {
    const source = join(root, input.path)
    const actual = input.treeSha256
      ? hash(JSON.stringify(await inventory(source)))
      : hash(await readFile(source))
    if (actual !== (input.treeSha256 ?? input.sha256)) {
      throw new Error(
        `Bundled runtime differs from registration: ${input.path}`
      )
    }
  }
  for (const license of manifest.electron.licenses) {
    const data = license.parts
      ? Buffer.concat(
          await Promise.all(
            license.parts.map((part: { path: string }) =>
              readFile(join(root, part.path))
            )
          )
        )
      : await readFile(join(root, license.path))
    if (hash(data) !== license.sha256) {
      throw new Error(
        `Electron license differs from registration: ${license.runtimePath}`
      )
    }
  }

  // Hash source/build inputs as well as locked binaries. A seeded dev bundle in
  // out/ is never an input: a cache miss performs the original production build.
  const directories = [
    'app/src',
    'app/test',
    'gemoji',
    'vendor',
    'app/static',
    'references',
    'third_party/licenses/native',
    'script/licenses',
    'script/validate-sass',
  ]
  const buildFiles = [
    'package.json',
    'app/package.json',
    'app/app-info.ts',
    'app/package-info.ts',
    'app/git-info.ts',
    'tsconfig.json',
    'app/webpack.common.ts',
    'app/webpack.production.ts',
    'script/build.ts',
    'script/copilot.ts',
    'script/webpack-koffi.ts',
    'script/dist-info.ts',
    'script/build-platforms.ts',
    'script/npm.ts',
    'script/globals.d.ts',
    'script/tsconfig.json',
    'script/prepare-native-runtime.ts',
    'script/native-windows-cli.bat',
    'script/native-windows-cli.sh',
    'app/native/mygo.json',
    'LICENSE',
  ]
  const sourceSnapshot = async () => ({
    sources: await Promise.all(
      directories.map(async path => ({
        path,
        files: await inventory(join(root, path)),
      }))
    ),
    files: await Promise.all(
      buildFiles.map(async path => ({
        path,
        sha256: hash(await readFile(join(root, path))),
      }))
    ),
    manifest: JSON.parse(
      await readFile(join(root, 'third_party/native-runtimes.json'), 'utf8')
    ),
    // Credentials affect DefinePlugin output, but must never enter a receipt.
    oauthIdentityHash: hash(
      JSON.stringify([
        process.env.DESKTOP_OAUTH_CLIENT_ID ?? '',
        process.env.DESKTOP_OAUTH_CLIENT_SECRET ?? '',
      ])
    ),
    revision: getSHA(),
    build:
      'webpack.production.ts + script/build.ts; DESKTOP_SKIP_PACKAGE=1; GITOWL_NATIVE_BUILD=1; heap=8192',
  })
  const inputs = await sourceSnapshot()
  const inputHash = hash(JSON.stringify(inputs))
  const receiptPath = join(output, 'runtime-receipt.json')
  const reuse = await reusableRuntime(receiptPath, kernel, inputHash)
  if (!reuse) {
    // Use the unified compiler and upstream build.ts copy/license rules, including
    // Copilot FFI, Koffi assets, Git credential helpers, emoji and platform resources.
    // Do not import or ship out/native-service or the seed-profile test entry.
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      NODE_ENV: 'production',
      TS_NODE_PROJECT: 'script/tsconfig.json',
      NODE_OPTIONS: '--max-old-space-size=8192',
      DESKTOP_SKIP_PACKAGE: '1',
      RELEASE_CHANNEL: 'research-preview',
      GITOWL_NATIVE_BUILD: '1',
      TARGET_ARCH: 'x64',
    }
    for (const key of [
      'TEST_ENV',
      'DESKTOP_KERNEL_TEST',
      'DESKTOP_E2E',
      'CIRCLE_SHA1',
      'DESKTOP_ERROR_REPORTING_ENDPOINT',
      'DESKTOP_NON_FATAL_ERROR_REPORTING_ENDPOINT',
    ]) {
      delete env[key]
    }
    // The upstream override prevents a preview from targeting Desktop's feed.
    env.DESKTOP_E2E_UPDATES_URL =
      'http://127.0.0.1:9/gitowl-research-preview-no-updates'
    await rm(join(root, 'out'), { recursive: true, force: true })
    // compile:prod hardcodes a 4 GiB cap which OOMs on the six original targets
    // with this SDK. Use its unchanged production config and build.ts entry with
    // an 8 GiB build-tool heap; do not substitute the development kernel config.
    await run(
      process.execPath,
      [
        require.resolve('webpack-cli/bin/cli.js'),
        '--config',
        'app/webpack.production.ts',
      ],
      env
    )
    await run(
      process.execPath,
      ['-r', 'ts-node/register', 'script/build.ts'],
      env
    )
    const compiled = join(root, 'out')
    const kernelMap = JSON.parse(
      await readFile(join(compiled, 'native-kernel.js.map'), 'utf8')
    )
    if (
      kernelMap.sources.some((name: string) =>
        /seed-profile|native-service/.test(name)
      )
    ) {
      throw new Error(
        'A research/test entry cannot be staged as the Desktop kernel'
      )
    }
    await rm(kernel, { recursive: true, force: true })
    await mkdir(kernel, { recursive: true })
    await cp(electron, kernel, { recursive: true })
    const app = join(kernel, 'app')
    // The original Git template copy can carry a submodule's .git pointer.
    // Preserve hidden runtime data, but never package repository metadata.
    await cp(compiled, app, {
      recursive: true,
      filter: path => basename(path) !== '.git',
    })
    await writeFile(
      join(app, 'native-launch.json'),
      JSON.stringify(await nativeRuntimeLaunchConfig(env), null, 2) + '\n'
    )
    // Compatibility UI is compiled by the original build but is not a native
    // package resource. The hidden host loads only native-kernel.html.
    for (const file of [
      'index.html',
      'renderer.js',
      'renderer.js.map',
      'renderer.css',
      'renderer.css.map',
      'renderer.js.LICENSE.txt',
      'renderer.report.html',
    ]) {
      await rm(join(app, file), { force: true })
    }
    // build.ts flattens win32 resources to app/static. Both launchers retain
    // the original CLI parser, whose native target is the MyGo executable.
    for (const extension of ['bat', 'sh']) {
      await writeFile(
        join(app, `static/github.${extension}`),
        await readFile(join(root, `script/native-windows-cli.${extension}`))
      )
    }
    const pkg = JSON.parse(await readFile(join(app, 'package.json'), 'utf8'))
    pkg.name = 'gitowl-desktop-kernel'
    pkg.productName = 'Gitowl'
    pkg.version = '0.0.1'
    pkg.bundleID = 'io.github.oubigfa.gitowl'
    pkg.main = 'main.js'
    await writeFile(
      join(app, 'package.json'),
      JSON.stringify(pkg, null, 2) + '\n'
    )
    const licenses = join(kernel, 'licenses')
    await mkdir(licenses, { recursive: true })
    await cp(join(root, 'LICENSE'), join(licenses, 'Gitowl-LICENSE.txt'))
    await cp(join(app, 'git/LICENSE.txt'), join(licenses, 'Git-LICENSE.txt'))
    await cp(
      join(app, 'static/licenses.json'),
      join(licenses, 'npm-licenses.json')
    )
    await cp(
      join(root, 'third_party/licenses/native'),
      join(licenses, 'native'),
      { recursive: true }
    )
    const finishedInputs = await sourceSnapshot()
    if (hash(JSON.stringify(finishedInputs)) !== inputHash) {
      const changed = finishedInputs.sources
        .filter(
          (item, index) =>
            JSON.stringify(item) !== JSON.stringify(inputs.sources[index])
        )
        .map(item => item.path)
      console.error(
        `Runtime input changes: ${
          changed.join(', ') || 'build files, revision or registration'
        }`
      )
      throw new Error(
        'Runtime source/lock/registration inputs changed during the production build; retry after the owning task settles'
      )
    }
    await writeFile(
      receiptPath,
      JSON.stringify(
        {
          schemaVersion: 2,
          inputHash,
          originalCore: 'desktop/desktop',
          presentationComplete: false,
          channel: 'research-preview',
          compile:
            'production, original webpack.production.ts + build.ts; GITOWL_NATIVE_BUILD=1; no seed-profile',
          buildFlags: {
            GITOWL_NATIVE_BUILD: '1',
            NODE_ENV: 'production',
            RELEASE_CHANNEL: 'research-preview',
          },
          files: await inventory(kernel, true),
        },
        null,
        2
      ) + '\n'
    )
  }
  // Retire the old resource directory, so no caller can accidentally reuse it.
  await rm(join(output, 'service'), { recursive: true, force: true })
  await writeFile(
    join(output, 'build-info.json'),
    JSON.stringify(
      {
        product: 'Gitowl',
        version: '0.0.1',
        identifier: 'io.github.oubigfa.gitowl',
        platform: 'windows-x64',
        originalCore: 'desktop/desktop',
        presentationComplete: false,
        fullFeatureParity: false,
        channel: 'research-preview',
        runtimeInputHash: inputHash,
        sourceSnapshot: 'hashed working-tree runtime/build inputs',
        sourceRevision: inputs.revision,
        authenticodeSigned: false,
        previewReady: manifest.packaging.previewReady,
        blockers: manifest.packaging.blockers,
      },
      null,
      2
    ) + '\n'
  )
  console.log(
    `Original Desktop Electron runtime ${
      reuse ? 'verified and reused' : 'prepared'
    }: ${kernel}`
  )
  const smoke = getNpmCommand([
    'test',
    '--',
    'script/native-runtime-smoke-test.ts',
  ])
  await run(smoke.executable, smoke.args, {
    ...process.env,
    GITOWL_RUNTIME_SMOKE: kernel,
  })
}

if (require.main === module) {
  prepare().catch(error => {
    console.error(error)
    process.exitCode = 1
  })
}

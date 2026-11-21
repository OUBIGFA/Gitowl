/// <reference path="./globals.d.ts" />
import { spawn } from 'child_process'
import { cp, mkdir, readFile, rename, stat, writeFile } from 'fs/promises'
import { basename, dirname, isAbsolute, join, relative, resolve } from 'path'
import { randomUUID } from 'crypto'
import {
  inventory,
  hash,
  nativeRuntimeLaunchConfig,
  reusableRuntime,
  runtimeSourceInventory,
} from './prepare-native-runtime'
import { generateLicenseMetadataFromSource } from './licenses/generate-license-metadata'
import { updateLicenseDump } from './licenses/update-license-dump'
import { getNpmCommand } from './npm'

const root = resolve(__dirname, '..')
const output = join(root, 'output/native-runtime')
const destination = join(output, 'backend')
const externalPackages = [
  'indexeddbshim',
  '@imagemagick/magick-wasm',
  'upng-js',
  'jsdom',
  'undici',
]

function run(executable: string, args: string[], env = process.env) {
  return new Promise<void>((resolveRun, reject) => {
    const child = spawn(executable, args, {
      cwd: root,
      env,
      stdio: 'inherit',
      windowsHide: true,
    })
    child.on('error', reject)
    child.on('exit', code =>
      code === 0
        ? resolveRun()
        : reject(new Error(`Runtime preparation failed (${code})`))
    )
  })
}

async function exists(path: string) {
  try {
    await stat(path)
    return true
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') {return false}
    throw error
  }
}

/** Copy the installed, lockfile-verified dependency closure without running a
 * second package resolution in the output. Relative package layout is retained.
 */
export async function copyRuntimePackages(target: string): Promise<void> {
  const base = join(root, 'app/node_modules')
  const pending = externalPackages.map(name => join(base, name))
  const seen = new Set<string>()
  while (pending.length) {
    const directory = pending.pop()!
    if (seen.has(directory)) {continue}
    seen.add(directory)
    const pkg = JSON.parse(
      await readFile(join(directory, 'package.json'), 'utf8')
    )
    const targetPackage = join(
      target,
      'node_modules',
      relative(base, directory)
    )
    await cp(directory, targetPackage, {
      recursive: true,
      filter: path => {
        const rel = relative(directory, path).replaceAll('\\', '/')
        // The application uses the library's default wasm32 entry and the one
        // Windows x64 SQLite prebuild; other CPU binaries and compiler sources
        // are not consumed at runtime. Published license files are retained.
        if (
          pkg.name === '@imagemagick/magick-wasm' &&
          rel.startsWith('dist/x64')
        )
          {return false}
        if (
          pkg.name === 'better-sqlite3' &&
          (rel.startsWith('deps/') ||
            rel.startsWith('src/') ||
            (rel.startsWith('prebuilds/') &&
              rel !== 'prebuilds/win32-x64.node'))
        )
          {return false}
        return (
          !rel
            .split('/')
            .some(part =>
              ['node_modules', '.git', 'test', 'tests', 'examples'].includes(
                part
              )
            ) && !/\.(pdb|obj|ilk|lib|exp)$/.test(path)
        )
      },
    })
    for (const name of Object.keys({
      ...pkg.dependencies,
      ...pkg.optionalDependencies,
    })) {
      // These published Node entries use no Canvas. JSDOM's optional Canvas
      // renderer is outside the inert Markdown parsing path; all image pixels
      // come from the registered ImageMagick/UPNG worker instead.
      if (name === 'canvas') {continue}
      let parent = directory
      let found = false
      while (parent.startsWith(join(root, 'app'))) {
        const candidate = join(parent, 'node_modules', name)
        if (await exists(join(candidate, 'package.json'))) {
          pending.push(candidate)
          found = true
          break
        }
        const next = dirname(parent)
        if (next === parent) {break}
        parent = next
      }
      if (!found && !pkg.optionalDependencies?.[name])
        {throw new Error(
          `Missing installed runtime dependency: ${pkg.name} -> ${name}`
        )}
    }
  }
}

async function prepare() {
  const manifest = JSON.parse(
    await readFile(join(root, 'third_party/native-runtimes.json'), 'utf8')
  )
  if (
    manifest.schemaVersion !== 3 ||
    manifest.node.role !== 'production-backend-and-build-tool'
  ) {
    throw new Error('Register the complete Node runtime before packaging')
  }
  if (hash(await readFile(process.execPath)) !== manifest.node.executableSha256)
    {throw new Error('Use the registered Node toolchain')}
  for (const lock of manifest.lockfiles) {
    if (
      hash(
        (await readFile(join(root, lock.path), 'utf8')).replaceAll('\r\n', '\n')
      ) !== lock.sha256
    )
      {throw new Error(`Stale dependency lock registration: ${lock.path}`)}
  }
  for (const input of manifest.bundledInputs) {
    const bytes = input.treeSha256
      ? hash(JSON.stringify(await inventory(join(root, input.path))))
      : hash(await readFile(join(root, input.path)))
    if (bytes !== (input.treeSha256 ?? input.sha256))
      {throw new Error(`Registered runtime input changed: ${input.path}`)}
  }
  const inputSnapshot = async () => ({
    sources: await runtimeSourceInventory(root),
    manifest,
    files: await Promise.all(
      [
        'app/webpack.node-kernel.ts',
        'app/app-info.ts',
        'script/prepare-node-runtime.ts',
        'script/native-windows-cli.bat',
        'script/native-windows-cli.sh',
        'app/native/mygo.json',
      ].map(async path => ({
        path,
        sha256: hash(await readFile(join(root, path))),
      }))
    ),
  })
  const inputHash = hash(JSON.stringify(await inputSnapshot()))
  const receipt = join(output, 'runtime-receipt.json')
  if (!(await reusableRuntime(receipt, destination, inputHash))) {
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      NODE_ENV: 'production',
      RELEASE_CHANNEL: 'production',
      TS_NODE_PROJECT: 'script/tsconfig.json',
      NODE_OPTIONS: '--max-old-space-size=8192',
    }
    delete env.TEST_ENV
    await run(
      process.execPath,
      [
        require.resolve('webpack-cli/bin/cli.js'),
        '--config',
        'app/webpack.node-kernel.ts',
      ],
      env
    )
    const work = join(root, '_temp/node-runtime', randomUUID())
    const staging = join(work, 'backend')
    await mkdir(staging, { recursive: true })
    const compiled = join(root, 'out/native-kernel-node')
    const assets: string[] = JSON.parse(
      await readFile(join(compiled, 'node-runtime-assets.json'), 'utf8')
    )
    for (const asset of assets) {
      if (isAbsolute(asset) || asset.split(/[\\/]/).includes('..'))
        {throw new Error('Invalid compiled resource path')}
      await mkdir(dirname(join(staging, asset)), { recursive: true })
      await cp(join(compiled, asset), join(staging, asset))
    }
    await cp(process.execPath, join(staging, 'node.exe'))
    await cp(
      join(dirname(process.execPath), 'LICENSE'),
      join(staging, 'Node-LICENSE.txt')
    )
    await cp(join(root, 'app/node_modules/dugite/git'), join(staging, 'git'), {
      recursive: true,
    })
    await cp(
      join(root, 'app/node_modules/@github/copilot-sdk-win32-x64'),
      join(staging, 'copilot'),
      { recursive: true }
    )
    const trampoline = join(
      root,
      'app/node_modules/desktop-trampoline/build/Release'
    )
    await mkdir(join(staging, 'desktop-trampoline'), { recursive: true })
    await cp(
      join(trampoline, 'desktop-askpass-trampoline.exe'),
      join(staging, 'desktop-trampoline/desktop-askpass-trampoline.exe')
    )
    await cp(
      join(trampoline, 'desktop-credential-helper-trampoline.exe'),
      join(staging, 'git/ucrt64/libexec/git-core/git-credential-desktop.exe')
    )
    for (const [module, method, file] of [
      ['process-proxy', 'getProxyCommandPath', 'process-proxy.exe'],
      ['printenvz', 'getPrintenvzPath', 'printenvz.exe'],
    ]) {
      await cp(require(module)[method](), join(staging, file))
    }
    await cp(join(root, 'gemoji/db/emoji.json'), join(staging, 'emoji.json'))
    await cp(join(root, 'gemoji/images/emoji'), join(staging, 'emoji'), {
      recursive: true,
      filter: path => basename(path) !== 'unicode',
    })
    await cp(join(root, 'app/static/common'), join(staging, 'static'), {
      recursive: true,
      filter: path => !['.git', 'choosealicense.com'].includes(basename(path)),
    })
    generateLicenseMetadataFromSource(
      join(root, 'app/static/common/choosealicense.com'),
      staging
    )
    for (const extension of ['bat', 'sh'])
      {await cp(
        join(root, `script/native-windows-cli.${extension}`),
        join(staging, `static/github.${extension}`)
      )}
    for (const file of [
      'legacy-export.cjs',
      'legacy-export.html',
      'legacy-export-renderer.cjs',
    ])
      {await cp(join(root, 'app/src/native-kernel', file), join(staging, file))}
    await copyRuntimePackages(staging)
    await writeFile(
      join(staging, 'native-launch.json'),
      JSON.stringify(await nativeRuntimeLaunchConfig(env)) + '\n'
    )
    await writeFile(
      join(staging, 'package.json'),
      JSON.stringify({
        name: 'gitowl-backend',
        productName: 'Gitowl',
        version: '0.1.0',
        bundleID: 'io.github.oubigfa.gitowl',
        main: 'main.js',
        private: true,
      }) + '\n'
    )
    await mkdir(join(staging, 'licenses'), { recursive: true })
    await cp(
      join(root, 'LICENSE'),
      join(staging, 'licenses/Gitowl-LICENSE.txt')
    )
    await cp(
      join(root, 'third_party/licenses/native'),
      join(staging, 'licenses/native'),
      {
        recursive: true,
        filter: path => !basename(path).startsWith('electron-'),
      }
    )
    await updateLicenseDump(root, staging)
    if (hash(JSON.stringify(await inputSnapshot())) !== inputHash)
      {throw new Error('Source changed during Node runtime preparation')}
    const files = await inventory(staging, true)
    if (
      files.some(file =>
        /(^|\/)(electron\.exe|chrome.*\.pak|native-kernel\.html|renderer\.js)$/.test(
          file.path
        )
      )
    )
      {throw new Error('Electron/browser resource entered the Node package')}
    await mkdir(output, { recursive: true })
    // Both resolved targets must stay under this workspace before a directory
    // move. Retired task-owned output is kept for recycling at delivery.
    for (const path of [staging, destination]) {
      if (!isAbsolute(path) || relative(root, path).startsWith('..'))
        {throw new Error('Unsafe runtime destination')}
    }
    if (await exists(destination))
      {await rename(
        destination,
        join(work, 'retired')
      )}
    await rename(staging, destination)
    await writeFile(
      receipt,
      JSON.stringify(
        {
          schemaVersion: 3,
          inputHash,
          originalCore: 'desktop/desktop',
          host: 'node',
          buildFlags: {
            NODE_ENV: 'production',
            RELEASE_CHANNEL: 'production',
            host: 'node',
          },
          files,
        },
        null,
        2
      ) + '\n'
    )
  }
  console.log(`Verified Node backend: ${destination}`)
  const validationPath = join(output, 'node-runtime-validation.json')
  if (process.env.GITOWL_SKIP_RUNTIME_SMOKE === '1') {
    console.log('Node runtime smoke skipped by explicit request')
    return
  }
  const validation = (await exists(validationPath))
    ? JSON.parse(await readFile(validationPath, 'utf8'))
    : null
  if (validation?.inputHash !== inputHash || validation?.passed !== true) {
    const test = getNpmCommand([
      'test',
      '--',
      'script/node-runtime-smoke-test.ts',
    ])
    await run(test.executable, [...test.args], {
      ...process.env,
      GITOWL_NODE_RUNTIME: destination,
    })
    await writeFile(
      validationPath,
      JSON.stringify(
        {
          inputHash,
          passed: true,
          checks: [
            'owned-node-startup',
            'resource-inventory',
            'git',
            'native-modules',
            'sqlite',
            'codecs',
            'no-electron',
          ],
        },
        null,
        2
      ) + '\n'
    )
  } else {
    console.log('Reused passing smoke for identical runtime bytes and inputs')
  }
}

if (require.main === module)
  {void prepare().catch(error => {
    console.error(error)
    process.exitCode = 1
  })}

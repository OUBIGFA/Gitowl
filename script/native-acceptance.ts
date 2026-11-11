/// <reference path="./globals.d.ts" />

import { spawn } from 'child_process'
import { lstat, mkdir, readFile, writeFile } from 'fs/promises'
import { dirname, join, resolve } from 'path'
import { getNpmCommand } from './npm'
import { hash } from './prepare-native-runtime'
import {
  Artifact,
  artifact,
  captureSourceTreeEvidence,
  git,
  isEvidencePath,
  requireCondition as check,
  safePath,
} from './native-acceptance-source'
import {
  CommandPlan,
  CommandReceipt,
  Suite,
  commandPlan,
  nativeAssets,
  nativeExecutablePath,
  runtimeReceiptPath,
  suites,
  usesNativeAssets,
  usesRuntime,
  validateAcceptance,
  validatePackagedAssets,
  validateResults,
} from './native-acceptance-policy'

/** Stream real runner output to ignored logs. The caller cannot supply an exit
 * code, result, or a replacement command via the record CLI. Failed runs retain
 * diagnostic receipts but validation will reject them. */
export async function runRecordedCommand(
  executable: string,
  args: ReadonlyArray<string>,
  cwd: string,
  env: NodeJS.ProcessEnv,
  stdoutPath: string,
  stderrPath: string
) {
  // Exclusive creation also prevents a successful old receipt being paired
  // accidentally with a later failed/truncated log.
  const { open } = await import('fs/promises')
  const stdout = await open(stdoutPath, 'wx')
  let stderr: Awaited<ReturnType<typeof open>> | undefined
  try {
    stderr = await open(stderrPath, 'wx')
    const stderrFD = stderr.fd
    return await new Promise<number | null>((resolveRun, reject) => {
      const child = spawn(executable, [...args], {
        cwd,
        env,
        stdio: ['ignore', stdout.fd, stderrFD],
        windowsHide: true,
      })
      child.once('error', reject)
      child.once('close', resolveRun)
    })
  } finally {
    await stdout.close()
    await stderr?.close()
  }
}

async function outputPath(root: string, name: string) {
  check(
    isEvidencePath(name) &&
      !/[\\:\x00-\x1f]/.test(name) &&
      name.split('/').every(p => p && p !== '.' && p !== '..'),
    'Output must be a repository-relative ignored evidence path'
  )
  check(
    (await git(root, ['check-ignore', '--', name])).trim() === name,
    'Evidence output must be ignored by Git'
  )
  let parent = root
  for (const part of dirname(name).split('/')) {
    parent = join(parent, part)
    try {
      await mkdir(parent)
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'EEXIST') {
        throw error
      }
    }
    await safePath(root, parent.slice(root.length + 1).replaceAll('\\', '/'))
  }
  const path = join(root, name)
  try {
    await lstat(path)
    throw new Error(`Evidence is immutable; choose a new path: ${name}`)
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
      throw error
    }
  }
  return path
}

export function commandEnvironment(root: string, suite: Suite) {
  const env = { ...process.env }
  // Do not import private profiles, seeded dev entries, a reporter override, or
  // externally injected Node/Go compile flags into acceptance commands.
  for (const key of Object.keys(env)) {
    if (
      /^(?:TEST_ENV|DESKTOP_KERNEL_TEST|DESKTOP_E2E.*|GITOWL_.*|MYGO_.*|NODE_OPTIONS|NODE_PATH|GOFLAGS|GOENV|GOWORK|GOOS|GOARCH|CGO_ENABLED|CIRCLE_SHA1|GITHUB_ACTIONS|RELEASE_CHANNEL|NODE_ENV|TS_NODE.*|ELECTRON_RUN_AS_NODE|GH_TOKEN|GITHUB_TOKEN|COPILOT_.*TOKEN|DESKTOP_GITHUB_DOTCOM_.*)$/.test(
        key
      )
    ) {
      delete env[key]
    }
  }
  env.GOMODCACHE = join(root, '.local/go-mod')
  env.GOCACHE = join(root, '.local/go-build')
  env.GOTMPDIR = join(root, '_temp/go-build')
  env.GOWORK = 'off'
  env.GOENV = 'off'
  env.GOOS = 'windows'
  env.GOARCH = 'amd64'
  env.NO_COLOR = '1'
  env.FORCE_COLOR = '0'
  if (suite === 'kernel' || suite === 'production-kernel') {
    // -json would ALSO affect the go mod edit inside prepare_mygo.py and
    // corrupt the original PowerShell runner's stdout-only modfile contract.
    // Standard verbose test events retain exact names without that side effect.
    env.GOFLAGS = '-v -count=1'
  }
  if (suite === 'native-build' || suite === 'locales') {
    env.GOFLAGS = `"-modfile=${join(root, '.local/gitowl-native.mod')}"`
  }
  if (suite === 'script') {
    env.GITOWL_RUNTIME_SMOKE = join(
      root,
      'output/native-runtime/desktop-kernel'
    )
  }
  return env
}

function executableCommand(plan: CommandPlan) {
  if (plan.executable === 'npm') {
    return getNpmCommand(plan.args)
  }
  return {
    executable: plan.executable === 'node' ? process.execPath : plan.executable,
    args: plan.args,
  }
}

async function record(root: string, suite: Suite, name: string) {
  check(suites.includes(suite), `Unknown suite: ${suite}`)
  check(
    process.platform === 'win32' && process.arch === 'x64',
    'Native acceptance records require Windows x64'
  )
  const manifest = JSON.parse(
    await readFile(join(root, 'third_party/native-runtimes.json'), 'utf8')
  )
  check(
    hash(await readFile(process.execPath)) === manifest.node.executableSha256,
    'Use the registered Node toolchain, through npm exec'
  )
  const destination = await outputPath(root, name)
  const sourceEvidence = await captureSourceTreeEvidence(root)
  const env = commandEnvironment(root, suite)
  await mkdir(env.GOTMPDIR!, { recursive: true })
  if (usesNativeAssets(suite)) {
    const prepare = await runRecordedCommand(
      'python',
      ['tools/prepare_mygo.py'],
      root,
      env,
      destination + '.prepare.stdout.log',
      destination + '.prepare.stderr.log'
    )
    check(
      prepare === 0,
      'Verified MyGo preparation failed; see adjacent prepare logs'
    )
  }
  const assetsBefore = usesNativeAssets(suite)
    ? await nativeAssets(root)
    : undefined
  const runtimeBefore =
    usesRuntime(suite) && !['runtime', 'production-kernel'].includes(suite)
      ? await artifact(root, runtimeReceiptPath)
      : undefined
  const command = commandPlan(suite, name)
  const executable = executableCommand(command)
  const exitCode = await runRecordedCommand(
    executable.executable,
    executable.args,
    resolve(root, command.cwd),
    env,
    destination + '.stdout.log',
    destination + '.stderr.log'
  )
  const stdout = await readFile(destination + '.stdout.log')
  const stderr = await readFile(destination + '.stderr.log')
  const finished = await captureSourceTreeEvidence(root)
  const runtimeReceipt = usesRuntime(suite)
    ? await artifact(root, runtimeReceiptPath)
    : undefined
  const assetsAfter = usesNativeAssets(suite)
    ? await nativeAssets(root)
    : undefined
  const receipt: CommandReceipt = {
    schemaVersion: 1,
    kind: 'native-acceptance-command',
    suite,
    command,
    sourceEvidence,
    finishedSourceDigest: finished.sourceDigest,
    exitCode,
    stdout: { path: name + '.stdout.log', sha256: hash(stdout) },
    stderr: { path: name + '.stderr.log', sha256: hash(stderr) },
    runtimeReceipt,
    nativeAssets: assetsAfter,
    nativeAssetsBefore: assetsBefore,
    runtimeReceiptBefore: runtimeBefore,
    executable:
      suite === 'native-build' || suite === 'native-ui-build'
        ? await artifact(root, nativeExecutablePath(suite, name))
        : undefined,
  }
  await writeFile(destination, JSON.stringify(receipt, null, 2) + '\n', {
    flag: 'wx',
  })
  check(exitCode === 0, `Recorded command failed (${exitCode}): ${name}`)
  check(
    sourceEvidence.sourceDigest === finished.sourceDigest,
    'Source changed during recorded command'
  )
  check(
    JSON.stringify(assetsBefore) === JSON.stringify(assetsAfter),
    'Native dependency inputs changed during command'
  )
  check(
    !runtimeBefore || runtimeBefore.sha256 === runtimeReceipt?.sha256,
    'Runtime changed during command'
  )
  validateResults(suite, stdout.toString('utf8'), stderr.toString('utf8'))
  console.log(JSON.stringify(await artifact(root, name)))
}

function argumentsFor(args: ReadonlyArray<string>) {
  const options = new Map<string, string[]>()
  for (let i = 0; i < args.length; i += 2) {
    check(
      ['--suite', '--out', '--receipt', '--artifact', '--package'].includes(
        args[i]
      ) &&
        args[i + 1] &&
        !args[i + 1].startsWith('--'),
      `Invalid CLI argument: ${args[i]}`
    )
    options.set(args[i], [...(options.get(args[i]) ?? []), args[i + 1]])
  }
  return options
}

/** snapshot only hashes evidence. record only executes known runners. validate
 * never writes a pass receipt and never packages or installs anything. */
export async function main(args = process.argv.slice(2)) {
  const root = resolve(__dirname, '..')
  const [action, ...rest] = args
  const options = argumentsFor(rest)
  const one = (name: string) => {
    const values = options.get(name)
    check(values?.length === 1, `Specify exactly one ${name}`)
    return values[0]
  }
  if (action === 'record') {
    check(
      [...options.keys()].every(k => ['--suite', '--out'].includes(k)),
      'Unexpected record option'
    )
    await record(root, one('--suite') as Suite, one('--out'))
  } else if (action === 'snapshot') {
    check(
      [...options.keys()].every(k => ['--out', '--artifact'].includes(k)),
      'Unexpected snapshot option'
    )
    const destination = await outputPath(root, one('--out'))
    const artifacts: Artifact[] = []
    for (const name of options.get('--artifact') ?? []) {
      artifacts.push(await artifact(root, name))
    }
    const evidence = await captureSourceTreeEvidence(root, artifacts)
    await writeFile(destination, JSON.stringify(evidence, null, 2) + '\n', {
      flag: 'wx',
    })
    console.log(
      JSON.stringify({
        sourceDigest: evidence.sourceDigest,
        evidenceDigest: evidence.evidenceDigest,
      })
    )
  } else if (action === 'validate') {
    check(
      [...options.keys()].every(k => ['--receipt', '--package'].includes(k)),
      'Unexpected validate option'
    )
    const acceptance = await validateAcceptance(root, one('--receipt'))
    console.log(
      JSON.stringify(
        options.has('--package')
          ? await validatePackagedAssets(root, one('--package'), acceptance)
          : acceptance
      )
    )
  } else {
    throw new Error(
      'Usage: native-acceptance.ts record --suite NAME --out _temp/...json | snapshot --out _temp/...json [--artifact _temp/...md] | validate --receipt _temp/...json'
    )
  }
}

if (require.main === module) {
  main().catch(error => {
    console.error(
      `Native acceptance blocked: ${
        error instanceof Error ? error.message : String(error)
      }`
    )
    process.exitCode = 1
  })
}

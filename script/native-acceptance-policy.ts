import { execFile } from 'child_process'
import { readFile, readdir } from 'fs/promises'
import { promisify } from 'util'
import { crc32 } from 'zlib'
import { hash, inventory, reusableRuntime } from './prepare-native-runtime'
import {
  Artifact,
  SourceTreeEvidence,
  artifact,
  captureSourceTreeEvidence,
  evidenceJSON,
  git,
  isEvidencePath,
  isSHA,
  requireCondition as check,
  safePath,
  treeArtifact,
  verifyArtifact,
} from './native-acceptance-source'

/** These are actual unified runners, not user-provided success commands. */
export const suites = [
  'repository',
  'repository-tests',
  'features',
  'dependencies',
  'go-modules',
  'locales',
  'typecheck',
  'script-typecheck',
  'unit',
  'script',
  'kernel',
  'runtime',
  'production-kernel',
  'native-unit',
  'installer-tests',
  'native-build',
  'native-ui-build',
] as const
export type Suite = typeof suites[number]
export type CommandPlan = {
  readonly executable: 'npm' | 'node' | 'python' | 'go'
  readonly args: ReadonlyArray<string>
  readonly cwd: '.' | 'app/native'
}

export function commandPlan(suite: Suite, receiptPath: string): CommandPlan {
  const npm: Partial<Record<Suite, string>> = {
    features: 'check:features',
    locales: 'check:native-locales',
    'script-typecheck': 'compile:script',
    unit: 'test:unit',
    script: 'test:script',
    kernel: 'test:kernel',
    runtime: 'prepare:native-runtime',
    'production-kernel': 'test:kernel:production',
  }
  if (npm[suite]) {
    // The script suite includes expensive source-graph hashing and native tsc
    // probes. Run its files sequentially so they cannot starve the real
    // Electron smoke's deadline; no test or timeout is removed or weakened.
    return {
      executable: 'npm',
      args: [
        'run',
        npm[suite]!,
        ...(suite === 'script' ? ['--', '--test-concurrency=1'] : []),
      ],
      cwd: '.',
    }
  }
  switch (suite) {
    case 'repository':
      return { executable: 'python', args: ['tools/check_repo.py'], cwd: '.' }
    case 'repository-tests':
      return {
        executable: 'python',
        args: ['-m', 'unittest', 'discover', '-s', 'tests/repository', '-v'],
        cwd: '.',
      }
    case 'dependencies':
      return {
        executable: 'python',
        args: ['tools/record_native_dependencies.py', '--check'],
        cwd: '.',
      }
    case 'typecheck':
      return {
        executable: 'node',
        args: [
          'node_modules/typescript/bin/tsc',
          '--noEmit',
          '-p',
          'tsconfig.json',
        ],
        cwd: '.',
      }
    case 'go-modules':
      return { executable: 'go', args: ['mod', 'verify'], cwd: 'app/native' }
    case 'native-unit':
      return {
        executable: 'go',
        args: [
          'test',
          '-modfile=../../.local/gitowl-native.mod',
          '-json',
          './...',
          '-count=1',
        ],
        cwd: 'app/native',
      }
    case 'installer-tests':
      return {
        executable: 'go',
        args: [
          'test',
          '-modfile=../../.local/gitowl-native.mod',
          '-json',
          'github.com/egoist/mygo/cmd/mygo',
          '-run',
          'Test(GitowlInstallerProcessMessages|GitowlInstallerRemoval|NSISCloseApp|NSISLanguage|NSISSourceLanguage|NSISHooks)$',
          '-count=1',
        ],
        cwd: 'app/native',
      }
    case 'native-build':
      // The exact production MyGo builder, but NO installer before acceptance.
      return {
        executable: 'go',
        args: [
          'tool',
          'mygo',
          'build',
          '-platform',
          'windows/amd64',
          '-skip-installer',
          '-o',
          `../../${receiptPath}.package`,
        ],
        cwd: 'app/native',
      }
    case 'native-ui-build':
      return {
        executable: 'go',
        args: [
          'build',
          '-modfile=../../.local/gitowl-native.mod',
          '-tags=e2e',
          '-o',
          `../../${receiptPath}.exe`,
          '.',
        ],
        cwd: 'app/native',
      }
    default:
      throw new Error(`Unknown acceptance suite: ${suite}`)
  }
}

export type CommandReceipt = {
  readonly schemaVersion: 1
  readonly kind: 'native-acceptance-command'
  readonly suite: Suite
  readonly command: CommandPlan
  readonly sourceEvidence: SourceTreeEvidence
  readonly finishedSourceDigest: string
  readonly exitCode: number | null
  readonly stdout: Artifact
  readonly stderr: Artifact
  readonly runtimeReceipt?: Artifact
  readonly nativeAssets?: ReadonlyArray<Artifact>
  readonly nativeAssetsBefore?: ReadonlyArray<Artifact>
  readonly runtimeReceiptBefore?: Artifact
  readonly executable?: Artifact
}

export const nativeExecutablePath = (suite: Suite, receiptPath: string) =>
  suite === 'native-build'
    ? `${receiptPath}.package/windows-amd64/Gitowl.exe`
    : `${receiptPath}.exe`

export const runtimeReceiptPath = 'output/native-runtime/runtime-receipt.json'
export const runtimePath = 'output/native-runtime/desktop-kernel'
export const usesNativeAssets = (suite: Suite) =>
  [
    'go-modules',
    'locales',
    'kernel',
    'production-kernel',
    'native-unit',
    'installer-tests',
    'native-build',
    'native-ui-build',
  ].includes(suite)
export const usesRuntime = (suite: Suite) =>
  [
    'runtime',
    'production-kernel',
    'script',
    'native-build',
    'native-ui-build',
  ].includes(suite)

export async function nativeAssets(
  root: string
): Promise<ReadonlyArray<Artifact>> {
  const manifest = JSON.parse(
    await readFile(await safePath(root, 'third_party/mygo.json'), 'utf8')
  )
  check(
    /^v\d+\.\d+\.\d+$/.test(manifest.version),
    'Invalid registered MyGo version'
  )
  const registration = JSON.parse(
    await readFile(
      await safePath(root, 'third_party/native-runtimes.json'),
      'utf8'
    )
  )
  check(
    Array.isArray(registration.goModules) && registration.goModules.length > 0,
    'Missing registered native module inputs'
  )
  const modules: Artifact[] = []
  for (const module of registration.goModules) {
    // Go's escaped module directory format (e.g. !azure) is canonical.
    const escaped = `${module.module}@${module.version}`.replace(
      /[A-Z]/g,
      c => `!${c.toLowerCase()}`
    )
    modules.push(await treeArtifact(root, `.local/go-mod/${escaped}`))
  }
  const original = await inventory(
    await safePath(root, `.local/go-mod/${manifest.module}@${manifest.version}`)
  )
  const patchedPath = `.local/mygo-gitowl-${manifest.version.slice(1)}`
  const expected = new Map(original.map(f => [f.path, f.sha256]))
  for (const patch of manifest.patches) {
    for (const file of patch.files) {
      check(
        (expected.get(file.path) ?? null) === file.before,
        'MyGo patch original input changed'
      )
      expected.set(file.path, file.after)
    }
  }
  expected.set(
    '.gitowl-patch',
    hash(
      [
        manifest.archive_sha256,
        ...manifest.patches.map((p: any) => p.sha256),
      ].join(':')
    )
  )
  const patched = await inventory(await safePath(root, patchedPath))
  check(
    patched.length === expected.size &&
      patched.every(f => expected.get(f.path) === f.sha256),
    'Unregistered changes in prepared MyGo sources'
  )
  return [
    await artifact(root, '.local/gitowl-native.mod'),
    await artifact(root, '.local/gitowl-native.sum'),
    await treeArtifact(root, patchedPath),
    ...modules,
  ]
}

/** Parse the repository's Node spec reporter and structured Go -json/-v events.
 * A bare PASS,
 * exit zero, an empty run, or only a type/popup count is not a test result. */
export function extractResults(
  stdout: string,
  stderr: string,
  verboseGo = false
) {
  const text = (stdout + '\n' + stderr).replace(/\x1b\[[0-9;]*m/g, '')
  const node: Array<{
    tests: number
    pass: number
    fail: number
    skipped: number
  }> = []
  const pattern =
    /(?:ℹ|#) tests (\d+)\r?\n(?:ℹ|#) suites \d+\r?\n(?:ℹ|#) pass (\d+)\r?\n(?:ℹ|#) fail (\d+)\r?\n(?:ℹ|#) cancelled (\d+)\r?\n(?:ℹ|#) skipped (\d+)\r?\n(?:ℹ|#) todo (\d+)/g
  for (const match of text.matchAll(pattern)) {
    check(
      Number(match[4]) === 0 && Number(match[6]) === 0,
      'Cancelled/todo Node tests'
    )
    node.push({
      tests: Number(match[1]),
      pass: Number(match[2]),
      fail: Number(match[3]),
      skipped: Number(match[5]),
    })
  }
  const goPass = new Set<string>()
  const goPackages = new Set<string>()
  const goSkip = new Set<string>()
  for (const line of text.split(/\r?\n/)) {
    if (verboseGo) {
      const test = /^\s*--- (PASS|FAIL|SKIP): (\S+) \([\d.]+s\)$/.exec(line)
      if (test) {
        check(test[1] !== 'FAIL', 'Failing Go tests')
        if (test[1] === 'PASS') {
          goPass.add(test[2])
        }
        if (test[1] === 'SKIP') {
          goSkip.add(test[2])
        }
      }
      const pkg = /^ok[ \t]+(\S+)[ \t]+[\d.]+s(?:[ \t].*)?$/.exec(line)
      if (pkg) {
        goPackages.add(pkg[1])
      }
    }
    if (!line.startsWith('{')) {
      continue
    }
    let event: any
    try {
      event = JSON.parse(line)
    } catch {
      continue
    }
    if (typeof event.Package !== 'string' || typeof event.Action !== 'string') {
      continue
    }
    check(
      event.Action !== 'fail' && event.Action !== 'build-fail',
      'Failing Go tests'
    )
    if (event.Action === 'pass') {
      if (event.Test) {
        goPass.add(event.Test)
      } else {
        goPackages.add(event.Package)
      }
    }
    if (event.Action === 'skip' && event.Test) {
      goSkip.add(event.Test)
    }
  }
  const python =
    /Ran ([1-9]\d*) tests? in [\d.]+s\r?\n\r?\nOK(?: \(skipped=\d+\))?(?:\r?\n|$)/.exec(
      text
    )
  return {
    node,
    goPass,
    goPackages,
    goSkip,
    pythonTests: python ? Number(python[1]) : 0,
  }
}

export function validateResults(suite: Suite, stdout: string, stderr: string) {
  const results = extractResults(
    stdout,
    stderr,
    ['kernel', 'production-kernel'].includes(suite)
  )
  if (
    ['unit', 'script', 'kernel', 'runtime', 'production-kernel'].includes(suite)
  ) {
    check(results.node.length > 0, `${suite}: no Node runner summary`)
    check(
      results.node.every(
        n =>
          n.tests > 0 &&
          n.pass > 0 &&
          n.fail === 0 &&
          n.pass + n.skipped === n.tests
      ),
      `${suite}: empty/failing Node tests`
    )
    if (['kernel', 'runtime', 'production-kernel'].includes(suite)) {
      check(
        results.node.every(n => n.skipped === 0),
        `${suite}: required Node tests were skipped`
      )
    }
  }
  if (
    ['kernel', 'production-kernel', 'native-unit', 'installer-tests'].includes(
      suite
    )
  ) {
    check(
      results.goPass.size > 0 && results.goPackages.size > 0,
      `${suite}: no structured Go test/package results`
    )
  }
  if (suite === 'production-kernel') {
    for (const name of [
      'TestNativeViewUsesOriginalDesktopKernel',
      'TestNativeGenerationUsesOriginalDesktopKernel',
      'TestNativeMediaUsesOriginalDesktopKernel',
      'TestOriginalKernelFreshProfileCompletesNativeWelcome',
    ]) {
      check(
        results.goPass.has(name) && !results.goSkip.has(name),
        `Required production test did not run: ${name}`
      )
    }
  }
  if (suite === 'installer-tests') {
    for (const name of [
      'TestGitowlInstallerProcessMessages',
      'TestGitowlInstallerRemoval',
      'TestGitowlInstallerRemoval/unlocked',
      'TestGitowlInstallerRemoval/temporary-lock',
      'TestGitowlInstallerRemoval/persistent-lock',
      'TestNSISHooks',
      'TestNSISCloseApp',
      'TestNSISLanguage',
      'TestNSISSourceLanguage',
    ]) {
      check(results.goPass.has(name), `Missing installer regression: ${name}`)
    }
  }
  if (suite === 'repository-tests') {
    check(results.pythonTests > 0, 'No unittest discovery result')
  }
  return results
}

/** Read even empty stdout/stderr, but never empty JSON receipts or captures. */
async function logText(root: string, item: Artifact) {
  check(
    item && isEvidencePath(item.path) && isSHA(item.sha256),
    'Invalid command log reference'
  )
  const bytes = await readFile(await safePath(root, item.path))
  check(hash(bytes) === item.sha256, `Changed command log: ${item.path}`)
  return bytes.toString('utf8')
}

export async function validateCommand(
  root: string,
  reference: Artifact,
  source: SourceTreeEvidence,
  expectedNativeAssets?: ReadonlyArray<Artifact>
) {
  const receipt = (await evidenceJSON(root, reference)) as CommandReceipt
  check(
    receipt.schemaVersion === 1 && receipt.kind === 'native-acceptance-command',
    'Invalid command receipt'
  )
  check(suites.includes(receipt.suite), 'Unknown command receipt suite')
  check(
    JSON.stringify(receipt.command) ===
      JSON.stringify(commandPlan(receipt.suite, reference.path)),
    'Command does not match the unified runner'
  )
  check(
    receipt.exitCode === 0,
    `${receipt.suite}: command failed or did not finish`
  )
  check(
    receipt.sourceEvidence?.sourceDigest === source.sourceDigest &&
      receipt.finishedSourceDigest === source.sourceDigest,
    `${receipt.suite}: stale/changing source`
  )
  check(
    JSON.stringify(receipt.sourceEvidence) === JSON.stringify(source),
    `${receipt.suite}: source evidence does not match its digest`
  )
  const stdout = await logText(root, receipt.stdout)
  const stderr = await logText(root, receipt.stderr)
  validateResults(receipt.suite, stdout, stderr)
  if (usesRuntime(receipt.suite)) {
    check(
      receipt.runtimeReceipt?.path === runtimeReceiptPath,
      `${receipt.suite}: missing production runtime binding`
    )
    await verifyArtifact(root, receipt.runtimeReceipt)
  }
  if (usesNativeAssets(receipt.suite)) {
    check(
      JSON.stringify(receipt.nativeAssetsBefore) ===
        JSON.stringify(receipt.nativeAssets),
      `${receipt.suite}: native inputs changed during execution`
    )
    check(
      JSON.stringify(receipt.nativeAssets) ===
        JSON.stringify(expectedNativeAssets ?? (await nativeAssets(root))),
      `${receipt.suite}: native asset inputs changed`
    )
  }
  if (
    usesRuntime(receipt.suite) &&
    !['runtime', 'production-kernel'].includes(receipt.suite)
  ) {
    check(
      receipt.runtimeReceiptBefore?.sha256 === receipt.runtimeReceipt?.sha256,
      `${receipt.suite}: runtime inputs changed during execution`
    )
  }
  if (receipt.suite === 'native-build' || receipt.suite === 'native-ui-build') {
    check(
      receipt.executable?.path ===
        nativeExecutablePath(receipt.suite, reference.path),
      'Missing native executable binding'
    )
    await verifyArtifact(root, receipt.executable)
  }
  return receipt
}

/** Runtime validity comes from a source-bound actual prepare:native-runtime run,
 * not from its preview/parity labels. Recheck every staged byte before packaging. */
export async function validateRuntime(root: string, reference: Artifact) {
  check(reference.path === runtimeReceiptPath, 'Wrong runtime receipt path')
  const receipt = await evidenceJSON(root, reference)
  check(
    receipt.schemaVersion === 2 &&
      receipt.originalCore === 'desktop/desktop' &&
      isSHA(receipt.inputHash),
    'Invalid original runtime receipt'
  )
  check(
    receipt.buildFlags?.NODE_ENV === 'production' &&
      receipt.buildFlags.GITOWL_NATIVE_BUILD === '1' &&
      receipt.buildFlags.RELEASE_CHANNEL === 'research-preview',
    'Not the registered isolated production runtime'
  )
  check(
    Array.isArray(receipt.files) && receipt.files.length > 0,
    'Empty runtime inventory'
  )
  for (const file of receipt.files) {
    check(isSHA(file.sha256), 'Invalid runtime file SHA')
    await safePath(root, `${runtimePath}/${file.path}`)
    // Git for Windows ships this system shell startup file, not a user
    // profile. Its bytes remain checked against the original runtime receipt.
    const gitSystemProfile = file.path === 'app/git/etc/profile'
    check(
      gitSystemProfile ||
        !/(?:native-service|seed-profile|(?:^|\/)node\.exe$|^app\/renderer\.|(?:^|\/)(?:test|tests|__tests__|profiles?)(?:\/|$))/i.test(
          file.path
        ),
      `Test/research resource in shipping runtime: ${file.path}`
    )
  }
  check(
    await reusableRuntime(
      await safePath(root, reference.path),
      await safePath(root, runtimePath),
      receipt.inputHash
    ),
    'Runtime bytes differ from production receipt'
  )
  const paths = new Set(receipt.files.map((f: Artifact) => f.path))
  for (const name of [
    'electron.exe',
    'LICENSE',
    'LICENSES.chromium.html',
    'app/main.js',
    'app/native-kernel.js',
    'app/native-kernel.html',
    'app/native-launch.json',
    'app/koffi.node',
    'app/git/cmd/git.exe',
    'app/git/ucrt64/libexec/git-core/git-credential-desktop.exe',
    'app/desktop-trampoline/desktop-askpass-trampoline.exe',
    'app/process-proxy.exe',
    'app/printenvz.exe',
    'app/copilot/prebuilds/win32-x64/copilot-runtime.exe',
    'app/copilot/prebuilds/win32-x64/runtime.node',
    'app/static/licenses.json',
    'app/emoji.json',
    'app/native-kernel.js.map',
    'app/main.js.map',
    'app/package.json',
  ]) {
    check(paths.has(name), `Missing original runtime asset: ${name}`)
  }
  for (const name of ['app/native-kernel.js.map', 'app/main.js.map']) {
    const map = JSON.parse(
      await readFile(await safePath(root, `${runtimePath}/${name}`), 'utf8')
    )
    check(
      Array.isArray(map.sources) && map.sources.length > 0,
      'Missing runtime source map inputs'
    )
    check(
      !map.sources.some((s: string) =>
        /native-service|seed-profile|app\/test\//.test(s)
      ),
      'Production bundle includes a test/research entry'
    )
    // Webpack virtual URLs are valid. Absolute local drive/POSIX paths are not.
    check(
      !map.sources.some((s: string) =>
        /^(?:[a-z]:[\\/]|\/|file:\/\/)/i.test(s)
      ),
      'Machine-local source path in shipping map'
    )
  }
  const pkg = JSON.parse(
    await readFile(
      await safePath(root, `${runtimePath}/app/package.json`),
      'utf8'
    )
  )
  check(
    pkg.productName === 'Gitowl' &&
      pkg.version === '0.0.1' &&
      pkg.bundleID === 'io.github.oubigfa.gitowl' &&
      pkg.main === 'main.js',
    'Runtime identity changed'
  )
  const config = JSON.parse(
    await readFile(await safePath(root, 'app/native/mygo.json'), 'utf8')
  )
  check(
    config.name === 'Gitowl' &&
      config.version === '0.0.1' &&
      config.identifier === 'io.github.oubigfa.gitowl',
    'Fixed native product identity changed'
  )
  check(
    JSON.stringify(config.resources) ===
      JSON.stringify([
        '../static/logos/gitowl/logo.png',
        '../static/logos/gitowl/icons',
        '../../output/native-runtime/desktop-kernel',
        '../../output/native-runtime/build-info.json',
      ]),
    'Unreviewed/private resource in native package'
  )
  return receipt
}

export const requiredUIStates = [
  'empty',
  'loading',
  'busy',
  'disabled',
  'error',
  'success',
  'hover',
  'focus',
  'selected',
] as const
export const requiredUIScenarios = [
  'first-run',
  'overview',
  'diff-lines',
  'long-description',
  'history',
  'branches-worktrees-pr',
  'stash-conflicts',
  'dialogs',
  'menus-shortcuts',
  'image-formats',
  'providers-copilot',
  'fetch-pull-generate-save-push',
  'locale-theme-persistence',
  'cli-oauth-routing',
  'accessibility',
  'accounts-auth',
  'preferences',
  'commit-hooks-lfs',
  'rebase-cherry-pick-revert-reset',
  'recovery-network-errors',
  'platform-lifecycle',
] as const

export type NativeCapture = {
  readonly image: Artifact
  readonly interaction: Artifact
  readonly sourceDigest: string
  readonly runtimeReceiptSha256: string
  readonly executableSha256: string
  readonly kind: 'native-window'
  readonly pid: number
  readonly locale: 'en' | 'zh-Hans' | 'zh-Hant'
  readonly theme: 'light' | 'dark'
  readonly size: 'regular' | 'minimum'
  readonly width: number
  readonly height: number
  readonly windowWidth: number
  readonly windowHeight: number
  readonly keyboard: boolean
  readonly longContent: boolean
  readonly states: ReadonlyArray<string>
  readonly scenarios: ReadonlyArray<string>
}

/** Validate actual PNG framing/CRC, not just a filename or magic-byte header.
 * Content and provenance still require the independent visual reviewer. */
export function pngDimensions(png: Buffer) {
  check(
    png.length > 33 &&
      png.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])),
    'Capture is not PNG'
  )
  let offset = 8
  let width = 0
  let height = 0
  let imageData = false
  while (offset + 12 <= png.length) {
    const length = png.readUInt32BE(offset)
    const end = offset + 12 + length
    check(end <= png.length, 'Truncated PNG capture')
    const type = png.toString('ascii', offset + 4, offset + 8)
    check(
      crc32(png.subarray(offset + 4, end - 4)) === png.readUInt32BE(end - 4),
      'Invalid PNG chunk checksum'
    )
    if (offset === 8) {
      check(type === 'IHDR' && length === 13, 'Missing PNG dimensions')
      width = png.readUInt32BE(offset + 8)
      height = png.readUInt32BE(offset + 12)
    } else {
      check(type !== 'IHDR', 'Duplicate PNG header')
    }
    if (type === 'IDAT' && length > 0) {
      imageData = true
    }
    if (type === 'IEND') {
      check(
        length === 0 &&
          end === png.length &&
          imageData &&
          width > 0 &&
          height > 0,
        'Incomplete PNG capture'
      )
      return { width, height }
    }
    offset = end
  }
  throw new Error('Missing PNG end/image data')
}

/** A qualitative review is intentionally a human trust boundary. It must name
 * an independent reviewer, bind their actual artifacts and close findings with
 * evidence. Structural validation does NOT prove screenshot authenticity, visual
 * quality, completeness of the issue list or a reviewer's honesty. */
export async function validateReview(
  root: string,
  reference: Artifact,
  source: SourceTreeEvidence,
  runtime: Artifact,
  native: Artifact,
  ui: Artifact,
  commandReferences: ReadonlyArray<Artifact>
) {
  const review = await evidenceJSON(root, reference)
  // A single-agent workflow retains every evidence gate without claiming an
  // independent reviewer. The record must name its actual author and reason.
  const singleAgent = review.kind === 'single-agent-native-review'
  check(
    review.schemaVersion === 1 &&
      (singleAgent || review.kind === 'independent-parent-native-review'),
    'Missing native review mode'
  )
  check(
    typeof review.reviewerId === 'string' &&
      review.reviewerId.trim().length > 0 &&
      typeof review.implementationOwnerId === 'string' &&
      review.implementationOwnerId.length > 0,
    'Review must identify its reviewer and implementation owner'
  )
  if (singleAgent) {
    check(
      review.reviewerId === review.implementationOwnerId,
      'Single-agent review must identify the implementation owner'
    )
    check(
      typeof review.reviewModeReason === 'string' &&
        review.reviewModeReason.trim().length > 0,
      'Single-agent review must declare its review mode reason'
    )
  } else {
    check(
      review.reviewerId !== review.implementationOwnerId,
      'Review must identify an independent parent, not the implementation worker'
    )
  }
  check(
    review.sourceEvidence?.sourceDigest === source.sourceDigest,
    'Review is for stale source'
  )
  check(
    review.runtimeReceiptSha256 === runtime.sha256 &&
      review.nativeExecutableSha256 === native.sha256 &&
      review.uiExecutableSha256 === ui.sha256,
    'Review runtime/native bindings changed'
  )
  check(
    Array.isArray(review.rounds) &&
      review.rounds.length >= 3 &&
      review.rounds
        .slice(-3)
        .every(
          (round: any) =>
            round.result === 'passed' &&
            round.sourceDigest === source.sourceDigest
        ) &&
      new Set(review.rounds.slice(-3).map((round: any) => round.artifact?.path))
        .size === 3,
    'Three passed reviews of the current source are required'
  )
  const last = review.rounds[review.rounds.length - 1]
  check(
    review.featureInventory?.path === 'references/upstream-features.json',
    'Missing original feature inventory review basis'
  )
  await verifyArtifact(root, review.featureInventory)
  const boundArtifacts: Artifact[] = [
    singleAgent ? review.sourceReview : review.independentSourceReview,
    review.featureCoverage,
    ...review.rounds.map((r: any) => r.artifact),
  ]
  const reviewedSource = await captureSourceTreeEvidence(root, boundArtifacts)
  check(
    JSON.stringify(review.sourceEvidence) === JSON.stringify(reviewedSource),
    'Review source/artifact evidence was not captured from these bytes'
  )
  check(
    last.sourceDigest === source.sourceDigest &&
      last.methods?.includes('impeccable') &&
      last.methods?.includes('unslop'),
    'Final review must apply Impeccable and unslop to current source'
  )
  check(
    Array.isArray(review.findings) && review.findings.length > 0,
    'Declare the source/UI findings and their closure evidence'
  )
  const ids = new Set()
  check(Array.isArray(last.captures), 'Missing final native captures')
  const currentEvidence: ReadonlyArray<Artifact> = [
    ...commandReferences,
    ...last.captures.flatMap((c: NativeCapture) => [c.image, c.interaction]),
  ]
  for (const finding of review.findings) {
    check(
      typeof finding.id === 'string' &&
        finding.id.length > 0 &&
        !ids.has(finding.id),
      'Invalid/duplicate review finding'
    )
    ids.add(finding.id)
    check(
      ['critical', 'high', 'medium', 'low'].includes(finding.severity),
      'Unknown finding severity'
    )
    check(
      finding.status === 'closed' &&
        Array.isArray(finding.evidence) &&
        finding.evidence.length > 0,
      `Unclosed review finding: ${finding.id}`
    )
    check(
      finding.evidence.some((item: Artifact) =>
        currentEvidence.some(
          current =>
            current.path === item.path && current.sha256 === item.sha256
        )
      ),
      `Finding lacks current regression/capture evidence: ${finding.id}`
    )
    for (const item of finding.evidence) {
      check(
        isEvidencePath(item.path),
        'Finding evidence must be a local ignored artifact'
      )
      await verifyArtifact(root, item)
    }
  }
  check(
    Array.isArray(review.regressions) &&
      commandReferences.every(command =>
        review.regressions.some(
          (r: Artifact) =>
            r.path === command.path && r.sha256 === command.sha256
        )
      ),
    'Reviewer must review all current test/build receipts'
  )
  check(
    Array.isArray(last.captures) && last.captures.length > 0,
    'No actual native window captures'
  )
  const matrix = new Set<string>()
  const states = new Set<string>()
  const scenarios = new Set<string>()
  const images = new Set<string>()
  for (const capture of last.captures as NativeCapture[]) {
    check(
      capture.kind === 'native-window' &&
        Number.isInteger(capture.pid) &&
        capture.pid > 0,
      'Component/EditorTester renders are not desktop acceptance'
    )
    check(
      capture.sourceDigest === source.sourceDigest &&
        capture.runtimeReceiptSha256 === runtime.sha256 &&
        capture.executableSha256 === ui.sha256,
      'Stale desktop capture inputs'
    )
    check(
      ['en', 'zh-Hans', 'zh-Hant'].includes(capture.locale) &&
        ['light', 'dark'].includes(capture.theme) &&
        ['regular', 'minimum'].includes(capture.size),
      'Unknown UI matrix coordinate'
    )
    check(
      capture.keyboard === true && capture.longContent === true,
      'Each matrix cell needs keyboard and long-content exercise'
    )
    check(
      isEvidencePath(capture.image.path) &&
        isEvidencePath(capture.interaction.path),
      'Capture artifacts must remain ignored'
    )
    await verifyArtifact(root, capture.image)
    await verifyArtifact(root, capture.interaction)
    check(
      !images.has(capture.image.sha256),
      'One image cannot stand for multiple UI matrix cells'
    )
    images.add(capture.image.sha256)
    const dimensions = pngDimensions(
      await readFile(await safePath(root, capture.image.path))
    )
    check(
      capture.width === dimensions.width &&
        capture.height === dimensions.height &&
        capture.width >= 600 &&
        capture.height >= 400,
      'Capture dimensions do not match the actual image'
    )
    // PrintWindow can trim nonclient borders and DPI scales pixels. The native
    // interaction transcript supplies logical window dimensions, not PNG size.
    check(
      Number.isFinite(capture.windowWidth) &&
        Number.isFinite(capture.windowHeight) &&
        capture.windowWidth >= 900 &&
        capture.windowHeight >= 620,
      'Missing native logical window geometry'
    )
    check(
      capture.size !== 'minimum' ||
        (capture.windowWidth === 900 && capture.windowHeight === 620),
      'Minimum-window capture was not exercised at 900x620 DIP'
    )
    matrix.add(`${capture.locale}/${capture.theme}/${capture.size}`)
    check(
      Array.isArray(capture.states) && Array.isArray(capture.scenarios),
      'Missing interaction/state coverage'
    )
    capture.states.forEach(state => states.add(state))
    capture.scenarios.forEach(scenario => scenarios.add(scenario))
  }
  for (const locale of ['en', 'zh-Hans', 'zh-Hant']) {
    for (const theme of ['light', 'dark']) {
      for (const size of ['regular', 'minimum']) {
        check(
          matrix.has(`${locale}/${theme}/${size}`),
          `Missing desktop matrix cell: ${locale}/${theme}/${size}`
        )
      }
    }
  }
  check(
    requiredUIStates.every(s => states.has(s)),
    'Desktop review is missing required interaction states'
  )
  check(
    requiredUIScenarios.every(s => scenarios.has(s)),
    'Desktop review is missing required feature scenarios'
  )
}

export type AcceptanceReceipt = {
  readonly schemaVersion: 1
  readonly sourceDigest: string
  readonly commands: ReadonlyArray<Artifact>
  readonly review: Artifact
}

/** No acceptance file is generated here. Successful validation authorizes
 * packaging this source snapshot. */
export async function validateAcceptance(root: string, receiptPath: string) {
  check(isEvidencePath(receiptPath), 'Acceptance receipt must remain ignored')
  const acceptance = (await evidenceJSON(
    root,
    await artifact(root, receiptPath)
  )) as AcceptanceReceipt
  check(acceptance.schemaVersion === 1, 'Unknown acceptance schema')
  const source = await captureSourceTreeEvidence(root)
  check(
    acceptance.sourceDigest === source.sourceDigest,
    'Acceptance source digest is stale'
  )
  check(
    (
      await git(root, ['diff', '--ignore-submodules=none', '--name-only'])
    ).trim() === '' &&
      (
        await git(root, ['ls-files', '--others', '--exclude-standard'])
      ).trim() === '',
    'Stage the exact accepted source before packaging'
  )
  check(
    Array.isArray(acceptance.commands) &&
      acceptance.commands.length === suites.length,
    'Missing required acceptance commands'
  )
  const commands = new Map<Suite, CommandReceipt>()
  const assets = await nativeAssets(root)
  for (const reference of acceptance.commands) {
    const command = await validateCommand(root, reference, source, assets)
    check(
      !commands.has(command.suite),
      `Duplicate acceptance suite: ${command.suite}`
    )
    commands.set(command.suite, command)
  }
  check(
    suites.every(suite => commands.has(suite)),
    'Incomplete acceptance suite matrix'
  )
  const runtimeReference = commands.get('runtime')!.runtimeReceipt!
  const runtime = await validateRuntime(root, runtimeReference)
  for (const command of commands.values()) {
    if (command.runtimeReceipt) {
      check(
        command.runtimeReceipt.sha256 === runtimeReference.sha256,
        'Tests used a different production runtime'
      )
    }
  }
  await validateReview(
    root,
    acceptance.review,
    source,
    runtimeReference,
    commands.get('native-build')!.executable!,
    commands.get('native-ui-build')!.executable!,
    acceptance.commands
  )
  check(
    (await captureSourceTreeEvidence(root)).sourceDigest ===
      source.sourceDigest,
    'Source changed during validation'
  )
  check(
    JSON.stringify(await nativeAssets(root)) === JSON.stringify(assets),
    'Native assets changed during validation'
  )
  const sourceTree = (await git(root, ['write-tree'])).trim()
  check(
    sourceTree === source.sourceTree,
    'Staged source changed during validation'
  )
  return {
    product: 'Gitowl',
    version: '0.0.1',
    identifier: 'io.github.oubigfa.gitowl',
    originalCore: 'desktop/desktop',
    sourceDigest: source.sourceDigest,
    sourceTree,
    acceptanceReceiptSha256: (await artifact(root, receiptPath)).sha256,
    reviewReceiptSha256: acceptance.review.sha256,
    runtimeInputHash: runtime.inputHash,
    runtimeReceiptSha256: runtimeReference.sha256,
    nativeExecutableSha256: commands.get('native-build')!.executable!.sha256,
    uiExecutableSha256: commands.get('native-ui-build')!.executable!.sha256,
    desktopAcceptance: 'source-bound-parent-review',
    // Installed/upgrade lifecycle testing is not a delivery gate (owner decision).
    installedAcceptance: 'not-required',
  }
}

/** MyGo's updater owns this exact list of payload entries, not arbitrary paths. */
export function validateInstalledFileList(text: string) {
  const entries = text.split(/\r?\n/).filter(Boolean).sort()
  check(
    JSON.stringify(entries) ===
      JSON.stringify([
        'Gitowl.exe',
        'build-info.json',
        'desktop-kernel',
        'icons',
        'logo.png',
      ]),
    'Invalid MyGo installed-file list'
  )
}

/** Go deliberately omits -ldflags under -trimpath (Go issue 52372).
 * Read actual Windows version resources; never relax the production tags or
 * path stripping requirements merely because linker flags are absent. */
export async function validatePackagedExecutable(
  bytes: Buffer,
  metadata: string,
  build: string
) {
  const buildTags = metadata.match(/-tags=([^\r\n]*)/)?.[1]?.split(',') ?? []
  check(
    metadata.includes('path\tgithub.com/OUBIGFA/Gitowl/app/native'),
    'Not the Gitowl native executable'
  )
  check(
    buildTags.includes('mygo_noinspector') &&
      !buildTags.includes('e2e') &&
      metadata.includes('-trimpath=true'),
    'Test harness/inspector or machine-local build paths in shipping executable'
  )
  // The resource parser is already locked for the existing Windows packager.
  const { NtExecutable, NtExecutableResource, Resource } = await import(
    'resedit'
  )
  const versions = Resource.VersionInfo.fromEntries(
    NtExecutableResource.from(NtExecutable.from(bytes)).entries
  )
  const values = versions.flatMap(v =>
    v.getAllLanguagesForStringValues().map(l => v.getStringValues(l))
  )
  check(
    values.length > 0 &&
      values.every(
        v =>
          v.ProductName === 'Gitowl' &&
          v.ProductVersion === '0.0.1' &&
          v.OriginalFilename === 'Gitowl.exe' &&
          v.MyGoIdentifier === 'io.github.oubigfa.gitowl'
      ),
    'Packaged Windows identity differs from Gitowl'
  )
  check(
    /^[0-9]{14}$/.test(build) && bytes.includes(Buffer.from(build, 'ascii')),
    'Packaged build number is absent'
  )
}

/** Check the actual MyGo staging directory before copying any fixed installer.
 * The automation/e2e executable is evidence only and must never ship. */
export async function validatePackagedAssets(
  root: string,
  directory: string,
  acceptance: Awaited<ReturnType<typeof validateAcceptance>>
) {
  check(isEvidencePath(directory), 'Package staging must remain ignored')
  const stage = await safePath(root, directory)
  const allowed = new Set([
    'Gitowl.exe',
    'desktop-kernel',
    'icons',
    'logo.png',
    'build-info.json',
    'gitowl-setup-0.0.1-amd64.exe',
    '.mygo-files',
  ])
  for (const name of await readdir(stage)) {
    check(allowed.has(name), `Unreviewed resource in shipping package: ${name}`)
    await safePath(root, `${directory}/${name}`)
  }
  validateInstalledFileList(
    await readFile(await safePath(root, `${directory}/.mygo-files`), 'utf8')
  )
  const runtime = JSON.parse(
    await readFile(await safePath(root, runtimeReceiptPath), 'utf8')
  )
  check(
    await reusableRuntime(
      await safePath(root, runtimeReceiptPath),
      await safePath(root, `${directory}/desktop-kernel`),
      acceptance.runtimeInputHash
    ),
    'Packaged runtime differs from accepted runtime'
  )
  check(
    runtime.inputHash === acceptance.runtimeInputHash,
    'Packaged runtime input identity changed'
  )
  check(
    (await artifact(root, `${directory}/logo.png`)).sha256 ===
      (await artifact(root, 'app/static/logos/gitowl/logo.png')).sha256,
    'Packaged logo differs from source'
  )
  const info = JSON.parse(
    await readFile(await safePath(root, `${directory}/build-info.json`), 'utf8')
  )
  for (const [key, value] of Object.entries(acceptance)) {
    check(
      info[key] === value,
      `Packaged build-info differs from acceptance: ${key}`
    )
  }
  check(
    /^[0-9]{14}$/.test(info.build) &&
      info.channel === 'fixed-test' &&
      info.fullFeatureParity === true &&
      info.presentationComplete === true &&
      info.authenticodeSigned === false,
    'Invalid fixed build metadata'
  )
  const executable = await safePath(root, `${directory}/Gitowl.exe`)
  const { stdout } = await promisify(execFile)(
    'go',
    ['version', '-m', executable],
    { cwd: root }
  )
  await validatePackagedExecutable(
    await readFile(executable),
    stdout,
    info.build
  )
  const installer = await artifact(
    root,
    `${directory}/gitowl-setup-0.0.1-amd64.exe`
  )
  check(
    (await captureSourceTreeEvidence(root)).sourceDigest ===
      acceptance.sourceDigest,
    'Source changed during package verification'
  )
  return {
    ...acceptance,
    packagedExecutableSha256: (await artifact(root, `${directory}/Gitowl.exe`))
      .sha256,
    installerSha256: installer.sha256,
  }
}

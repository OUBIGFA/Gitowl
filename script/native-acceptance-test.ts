import assert from 'node:assert/strict'
import { it } from 'node:test'
import { mkdir, mkdtemp, readFile, rm, symlink, writeFile } from 'fs/promises'
import { dirname, join } from 'path'
import { tmpdir } from 'os'
import { crc32, deflateSync } from 'zlib'
import {
  Artifact,
  artifact,
  captureSourceTreeEvidence,
  git,
} from './native-acceptance-source'
import { hash, inventory } from './prepare-native-runtime'
import {
  commandPlan,
  extractResults,
  nativeAssets,
  nativeExecutablePath,
  pngDimensions,
  requiredUIScenarios,
  requiredUIStates,
  runtimePath,
  runtimeReceiptPath,
  suites,
  usesNativeAssets,
  usesRuntime,
  validateAcceptance,
  validateCommand,
  validatePackagedAssets,
  validateResults,
  validateInstalledFileList,
  validatePackagedExecutable,
  validateReview,
  validateRuntime,
} from './native-acceptance-policy'
import { commandEnvironment, runRecordedCommand } from './native-acceptance'

const nodeSummary =
  'ℹ tests 2\nℹ suites 1\nℹ pass 2\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\n'
const productionTests = [
  'TestNativeViewUsesOriginalDesktopKernel',
  'TestNativeGenerationUsesOriginalDesktopKernel',
  'TestNativeMediaUsesOriginalDesktopKernel',
  'TestOriginalKernelFreshProfileCompletesNativeWelcome',
]
const installerTests = [
  'TestGitowlInstallerProcessMessages',
  'TestGitowlInstallerRemoval',
  'TestGitowlInstallerRemoval/unlocked',
  'TestGitowlInstallerRemoval/temporary-lock',
  'TestGitowlInstallerRemoval/persistent-lock',
  'TestNSISHooks',
  'TestNSISCloseApp',
  'TestNSISLanguage',
  'TestNSISSourceLanguage',
]
function goResults(names: string[]) {
  return (
    [
      ...names.map(Test => ({
        Action: 'pass',
        Package: 'fixture/native',
        Test,
      })),
      { Action: 'pass', Package: 'fixture/native' },
    ]
      .map(e => JSON.stringify(e))
      .join('\n') + '\n'
  )
}

function fixturePNG(label: string) {
  const chunk = (type: string, data: Buffer) => {
    const block = Buffer.alloc(data.length + 12)
    block.writeUInt32BE(data.length)
    block.write(type, 4)
    data.copy(block, 8)
    block.writeUInt32BE(
      crc32(block.subarray(4, block.length - 4)),
      block.length - 4
    )
    return block
  }
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(900)
  ihdr.writeUInt32BE(620, 4)
  ihdr[8] = 8
  ihdr[9] = 6
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk('IHDR', ihdr),
    chunk('tEXt', Buffer.from(`Fixture\0${label}`)),
    chunk('IDAT', deflateSync(Buffer.alloc((900 * 4 + 1) * 620))),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

/** Synthetic receipts are confined to fresh disposable fixture repositories.
 * They test the validator, and are never accepted as real desktop evidence. */
async function fixture(t: { after: (fn: () => Promise<void>) => void }) {
  const root = await mkdtemp(join(tmpdir(), 'gitowl-native-acceptance-'))
  t.after(() =>
    rm(root, { recursive: true, force: true, maxRetries: 10, retryDelay: 50 })
  )
  const file = async (
    path: string,
    content: string | Buffer
  ): Promise<Artifact> => {
    await mkdir(dirname(join(root, path)), { recursive: true })
    await writeFile(join(root, path), content)
    return { path, sha256: hash(content) }
  }
  const json = (path: string, value: unknown) =>
    file(path, JSON.stringify(value))
  await git(root, ['init', '--initial-branch=main'])
  await file('.gitignore', '_temp/\noutput/\n.local/\n')
  await file('src.ts', 'export const fixture = true\n')
  await file('package-lock.json', '{"lockfileVersion":3}\n')
  await json('references/upstream-features.json', {
    fixture: 'public inventory',
  })
  await json('third_party/mygo.json', {
    module: 'github.com/egoist/mygo',
    version: 'v0.8.1',
    archive_sha256: hash('fixture archive'),
    patches: [],
  })
  await json('third_party/native-runtimes.json', {
    goModules: [{ module: 'github.com/egoist/mygo', version: 'v0.8.1' }],
  })
  await json('app/native/mygo.json', {
    name: 'Gitowl',
    version: '0.0.1',
    identifier: 'io.github.oubigfa.gitowl',
    resources: [
      '../static/logos/gitowl/logo.png',
      '../static/logos/gitowl/icons',
      '../../output/native-runtime/desktop-kernel',
      '../../output/native-runtime/build-info.json',
    ],
  })
  await file('app/static/logos/gitowl/logo.png', 'public fixture logo')
  await file('app/static/logos/gitowl/icons/16.png', 'public fixture tray icon')
  await git(root, ['add', '.'])
  await git(root, [
    '-c',
    'user.name=Fixture',
    '-c',
    'user.email=fixture@example.invalid',
    'commit',
    '-m',
    'isolated fixture',
  ])
  for (const path of ['.local/gitowl-native.mod', '.local/gitowl-native.sum']) {
    await file(path, `fixture ${path}`)
  }
  await file(
    '.local/go-mod/github.com/egoist/mygo@v0.8.1/ui.go',
    'fixture original MyGo'
  )
  await file('.local/mygo-gitowl-0.8.1/ui.go', 'fixture original MyGo')
  await file('.local/mygo-gitowl-0.8.1/.gitowl-patch', hash('fixture archive'))
  for (const path of [
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
  ]) {
    await file(`${runtimePath}/${path}`, `fixture ${path}`)
  }
  for (const path of ['app/main.js.map', 'app/native-kernel.js.map']) {
    await json(`${runtimePath}/${path}`, {
      sources: ['webpack://gitowl/./app/src/native-kernel/renderer.ts'],
    })
  }
  await json(`${runtimePath}/app/package.json`, {
    productName: 'Gitowl',
    version: '0.0.1',
    bundleID: 'io.github.oubigfa.gitowl',
    main: 'main.js',
  })
  const runtime = await json(runtimeReceiptPath, {
    schemaVersion: 2,
    originalCore: 'desktop/desktop',
    inputHash: hash('fixture-inputs'),
    buildFlags: {
      NODE_ENV: 'production',
      GITOWL_NATIVE_BUILD: '1',
      RELEASE_CHANNEL: 'research-preview',
    },
    files: await inventory(join(root, runtimePath)),
  })
  const source = await captureSourceTreeEvidence(root)
  const commands: Artifact[] = []
  const receipts = new Map<string, any>()
  for (const suite of suites) {
    const path = `_temp/evidence/${suite}.json`
    let stdout = ''
    let stderr = ''
    if (
      ['unit', 'script', 'kernel', 'runtime', 'production-kernel'].includes(
        suite
      )
    ) {
      stdout += nodeSummary
    }
    if (['kernel', 'production-kernel', 'native-unit'].includes(suite)) {
      stdout += goResults(productionTests)
    }
    if (suite === 'installer-tests') {
      stdout += goResults(installerTests)
    }
    if (suite === 'repository-tests') {
      stderr += 'Ran 2 tests in 0.001s\n\nOK\n'
    }
    const assets = usesNativeAssets(suite)
      ? await nativeAssets(root)
      : undefined
    const receipt = {
      schemaVersion: 1,
      kind: 'native-acceptance-command',
      suite,
      command: commandPlan(suite, path),
      sourceEvidence: source,
      finishedSourceDigest: source.sourceDigest,
      exitCode: 0,
      stdout: await file(path + '.stdout.log', stdout),
      stderr: await file(path + '.stderr.log', stderr),
      runtimeReceipt: usesRuntime(suite) ? runtime : undefined,
      runtimeReceiptBefore:
        usesRuntime(suite) && !['runtime', 'production-kernel'].includes(suite)
          ? runtime
          : undefined,
      nativeAssets: assets,
      nativeAssetsBefore: assets,
      executable: ['native-build', 'native-ui-build'].includes(suite)
        ? await file(
            nativeExecutablePath(suite, path),
            'fixture-not-a-real-executable-' + suite
          )
        : undefined,
    }
    receipts.set(suite, receipt)
    commands.push(await json(path, receipt))
  }
  const baseline = await file(
    '_temp/review/independent.md',
    'Independent original source risk findings: fixture only.'
  )
  const featureCoverage = await file(
    '_temp/review/feature-coverage.md',
    'Independent original feature-by-feature parity review fixture.'
  )
  const first = await file(
    '_temp/review/round1.md',
    'First passed review; fixture only.'
  )
  const second = await file(
    '_temp/review/round2.md',
    'Second passed review; fixture only.'
  )
  const final = await file(
    '_temp/review/round3.md',
    'Independent review of repairs; fixture only.'
  )
  const captures: any[] = []
  for (const locale of ['en', 'zh-Hans', 'zh-Hant']) {
    for (const theme of ['light', 'dark']) {
      for (const size of ['regular', 'minimum']) {
        const label = `${locale}-${theme}-${size}`
        const png = fixturePNG(label)
        captures.push({
          image: await file(`_temp/review/${label}.png`, png),
          interaction: await file(
            `_temp/review/${label}.log`,
            'fixture keyboard/long-content interaction'
          ),
          sourceDigest: source.sourceDigest,
          runtimeReceiptSha256: runtime.sha256,
          executableSha256: receipts.get('native-ui-build').executable.sha256,
          kind: 'native-window',
          pid: 123,
          locale,
          theme,
          size,
          width: 900,
          height: 620,
          windowWidth: size === 'minimum' ? 900 : 1180,
          windowHeight: size === 'minimum' ? 620 : 760,
          keyboard: true,
          longContent: true,
          states: requiredUIStates,
          scenarios: requiredUIScenarios,
        })
      }
    }
  }
  const review = {
    schemaVersion: 1,
    kind: 'independent-parent-native-review',
    reviewerId: 'parent-fixture',
    implementationOwnerId: 'worker-fixture',
    sourceEvidence: await captureSourceTreeEvidence(root, [
      baseline,
      featureCoverage,
      first,
      second,
      final,
    ]),
    runtimeReceiptSha256: runtime.sha256,
    nativeExecutableSha256: receipts.get('native-build').executable.sha256,
    uiExecutableSha256: receipts.get('native-ui-build').executable.sha256,
    independentSourceReview: baseline,
    featureCoverage,
    featureInventory: await artifact(root, 'references/upstream-features.json'),
    rounds: [
      { artifact: first, sourceDigest: source.sourceDigest, result: 'passed' },
      { artifact: second, sourceDigest: source.sourceDigest, result: 'passed' },
      {
        artifact: final,
        sourceDigest: source.sourceDigest,
        result: 'passed',
        methods: ['impeccable', 'unslop'],
        captures,
      },
    ],
    findings: [
      {
        id: 'fixture-P1',
        severity: 'high',
        status: 'closed',
        evidence: [commands[0], captures[0].image],
      },
    ],
    regressions: commands,
  }
  const reviewRef = await json('_temp/evidence/review.json', review)
  const acceptance = {
    schemaVersion: 1,
    sourceDigest: source.sourceDigest,
    commands,
    review: reviewRef,
  }
  const acceptanceRef = await json('_temp/evidence/acceptance.json', acceptance)
  return {
    root,
    file,
    json,
    runtime,
    source,
    commands,
    receipts,
    review,
    reviewRef,
    acceptance,
    acceptanceRef,
  }
}

it('blocks fixed delivery until three reviews have passed for the current source', async t => {
  const f = await fixture(t)
  const validate = async (review: unknown) =>
    validateReview(
      f.root,
      await f.json(f.reviewRef.path, review),
      f.source,
      f.runtime,
      f.receipts.get('native-build').executable,
      f.receipts.get('native-ui-build').executable,
      f.commands
    )
  await assert.rejects(
    validate({ ...f.review, rounds: f.review.rounds.slice(0, 2) }),
    /Three passed reviews/
  )
  for (const change of [
    { result: 'pending' },
    { sourceDigest: hash('old source') },
  ]) {
    await assert.rejects(
      validate({
        ...f.review,
        rounds: [
          { ...f.review.rounds[0], ...change },
          ...f.review.rounds.slice(1),
        ],
      }),
      /Three passed reviews/
    )
  }
})

it('validates the complete isolated source/runtime/command/review evidence graph', async t => {
  const f = await fixture(t)
  const accepted = await validateAcceptance(f.root, f.acceptanceRef.path)
  assert.equal(accepted.sourceDigest, f.source.sourceDigest)
  assert.equal(accepted.sourceTree, (await git(f.root, ['write-tree'])).trim())
  assert.equal(accepted.installedAcceptance, 'not-required')
  assert.equal(accepted.identifier, 'io.github.oubigfa.gitowl')
})

it('records single-agent review honestly while retaining the same evidence gates', async t => {
  const f = await fixture(t)
  const review = {
    ...f.review,
    kind: 'single-agent-native-review',
    reviewerId: f.review.implementationOwnerId,
    reviewModeReason:
      'The user requested review by the implementation owner without additional agents.',
    sourceReview: f.review.independentSourceReview,
  }
  const validate = async (value: unknown) =>
    validateReview(
      f.root,
      await f.json(f.reviewRef.path, value),
      f.source,
      f.runtime,
      f.receipts.get('native-build').executable,
      f.receipts.get('native-ui-build').executable,
      f.commands
    )
  await validate(review)
  await assert.rejects(
    validate({ ...review, reviewModeReason: '' }),
    /review mode reason/
  )
  await assert.rejects(
    validate({ ...review, rounds: review.rounds.slice(0, 2) }),
    /Three passed reviews/
  )
  await assert.rejects(
    validate({ ...review, reviewerId: 'invented-independent-reviewer' }),
    /implementation owner/
  )
})

it('source evidence includes every Git input and independent artifact, never private profiles', async t => {
  const f = await fixture(t)
  await f.file('_temp/private-profile/config.json', 'private fixture')
  assert.equal(
    (await captureSourceTreeEvidence(f.root)).sourceDigest,
    f.source.sourceDigest
  )
  await f.file('new-source.ts', 'new public input')
  assert.notEqual(
    (await captureSourceTreeEvidence(f.root)).sourceDigest,
    f.source.sourceDigest
  )
  await rm(join(f.root, 'new-source.ts'))
  await f.file('package-lock.json', '{"lockfileVersion":4}\n')
  assert.notEqual(
    (await captureSourceTreeEvidence(f.root)).sourceDigest,
    f.source.sourceDigest
  )
  assert.ok(
    f.review.sourceEvidence.artifacts.some(
      a => a.path === f.review.independentSourceReview.path
    )
  )
})

it('rejects receipts after an index-only change and binds the final commit tree', async t => {
  const f = await fixture(t)
  // A mode-only staging change leaves every workspace byte unchanged.
  await git(f.root, ['update-index', '--chmod=+x', 'src.ts'])
  const staged = await captureSourceTreeEvidence(f.root)
  assert.deepEqual(staged.files, f.source.files)
  assert.notEqual(staged.sourceDigest, f.source.sourceDigest)
  await assert.rejects(validateCommand(f.root, f.commands[0], staged))
  const tree = (await git(f.root, ['write-tree'])).trim()
  await git(f.root, [
    '-c',
    'user.name=Fixture',
    '-c',
    'user.email=fixture@example.invalid',
    'commit',
    '-m',
    'mode change',
  ])
  assert.equal((await git(f.root, ['rev-parse', 'HEAD^{tree}'])).trim(), tree)
  assert.notEqual(
    (await captureSourceTreeEvidence(f.root)).sourceDigest,
    staged.sourceDigest
  )
})

it('rejects missing, empty, tampered, stale and unstaged acceptance receipts', async t => {
  const f = await fixture(t)
  await assert.rejects(validateAcceptance(f.root, '_temp/missing.json'))
  await f.file('_temp/empty.json', '')
  await assert.rejects(
    validateAcceptance(f.root, '_temp/empty.json'),
    /Empty evidence/
  )
  await f.file(f.commands[0].path, '{}')
  await assert.rejects(
    validateAcceptance(f.root, f.acceptanceRef.path),
    /Changed artifact/
  )
  await f.json(f.commands[0].path, f.receipts.get('repository'))
  await f.file('src.ts', 'changed source, same mtime irrelevant')
  await assert.rejects(
    validateAcceptance(f.root, f.acceptanceRef.path),
    /digest is stale/
  )
  const source = await captureSourceTreeEvidence(f.root)
  await f.json(f.acceptanceRef.path, {
    ...f.acceptance,
    sourceDigest: source.sourceDigest,
  })
  await assert.rejects(
    validateAcceptance(f.root, f.acceptanceRef.path),
    /Stage the exact/
  )
})

it('rejects runner substitution, failed status, missing suites, changed logs and native inputs', async t => {
  const f = await fixture(t)
  const original = f.receipts.get('native-unit')
  const path = '_temp/evidence/native-unit.json'
  for (const change of [
    {
      command: {
        executable: 'node',
        args: ['-e', 'console.log("PASS")'],
        cwd: '.',
      },
    },
    { exitCode: 1 },
    { finishedSourceDigest: hash('other source') },
  ]) {
    const reference = await f.json(path, { ...original, ...change })
    await assert.rejects(validateCommand(f.root, reference, f.source))
  }
  const reference = await f.json(path, original)
  await f.file(original.stdout.path, 'PASS')
  await assert.rejects(
    validateCommand(f.root, reference, f.source),
    /Changed command log/
  )
  await f.file(original.stdout.path, goResults(productionTests))
  await f.file('.local/mygo-gitowl-0.8.1/ui.go', 'tampered dependency')
  await assert.rejects(
    validateCommand(f.root, reference, f.source),
    /Unregistered changes in prepared MyGo sources/
  )
  await f.json(f.acceptanceRef.path, {
    ...f.acceptance,
    commands: f.commands.slice(1),
  })
  await assert.rejects(
    validateAcceptance(f.root, f.acceptanceRef.path),
    /Missing required acceptance commands/
  )
})

it('extracts actual reporter formats and rejects PASS, empty suites, skips and missing real-kernel tests', () => {
  assert.equal(extractResults(nodeSummary, '').node[0].pass, 2)
  validateResults(
    'production-kernel',
    nodeSummary + goResults(productionTests),
    ''
  )
  validateResults(
    'production-kernel',
    nodeSummary +
      productionTests
        .map(name => `=== RUN   ${name}\n--- PASS: ${name} (0.01s)\n`)
        .join('') +
      'ok\tfixture/native\t0.100s\n',
    ''
  )
  assert.throws(
    () =>
      validateResults('native-unit', 'PASS\nok\tfixture/native\t0.100s\n', ''),
    /no structured Go/
  )
  assert.equal(
    commandEnvironment('fixture', 'production-kernel').GOFLAGS,
    '-v -count=1',
    'Go -json would corrupt prepare_mygo stdout via go mod edit'
  )
  assert.match(commandEnvironment('fixture', 'locales').GOFLAGS!, /-modfile=/)
  validateResults(
    'repository-tests',
    '',
    'Ran 12 tests in 0.021s\r\n\r\nOK\r\n'
  )
  for (const stdout of [
    '',
    'PASS',
    '97/97 popup types',
    nodeSummary.replace('pass 2', 'pass 0').replace('skipped 0', 'skipped 2'),
    nodeSummary.replace('fail 0', 'fail 1'),
  ]) {
    assert.throws(() => validateResults('runtime', stdout, ''))
  }
  assert.throws(
    () =>
      validateResults(
        'production-kernel',
        nodeSummary + goResults(['TestEditorTesterOnly']),
        ''
      ),
    /Required production test/
  )
  assert.throws(
    () =>
      validateResults(
        'native-unit',
        goResults(['SomeTest']) +
          '{"Action":"fail","Package":"fixture/native"}\n',
        ''
      ),
    /Failing Go tests/
  )
})

it('allows the bundled Git system profile while rejecting actual profile directories', async t => {
  const f = await fixture(t)
  await f.file(
    `${runtimePath}/app/git/etc/profile`,
    '# Git system shell startup\n'
  )
  const receipt = JSON.parse(
    await readFile(join(f.root, runtimeReceiptPath), 'utf8')
  )
  receipt.files = await inventory(join(f.root, runtimePath))
  await validateRuntime(f.root, await f.json(runtimeReceiptPath, receipt))
  await f.file(`${runtimePath}/app/profile/credentials.json`, 'private data')
  receipt.files = await inventory(join(f.root, runtimePath))
  await assert.rejects(
    validateRuntime(f.root, await f.json(runtimeReceiptPath, receipt)),
    /Test\/research resource/
  )
})

it('validates every runtime byte and rejects test-seed/harness files even with a rehashed receipt', async t => {
  const f = await fixture(t)
  await validateRuntime(f.root, f.runtime)
  await f.file(`${runtimePath}/app/main.js`, 'tampered output')
  await assert.rejects(
    validateRuntime(f.root, f.runtime),
    /Runtime bytes differ/
  )
  await f.file(`${runtimePath}/app/main.js`, 'fixture app/main.js')
  await f.file(`${runtimePath}/app/seed-profile.js`, 'test seed must not ship')
  const receipt = JSON.parse(
    await readFile(join(f.root, runtimeReceiptPath), 'utf8')
  )
  receipt.files = await inventory(join(f.root, runtimePath))
  const reference = await f.json(runtimeReceiptPath, receipt)
  await assert.rejects(
    validateRuntime(f.root, reference),
    /Test\/research resource/
  )
})

it('rejects absolute, traversing and linked artifact paths', async t => {
  const f = await fixture(t)
  for (const path of [
    '../outside.json',
    '/tmp/private.json',
    'C:/private.json',
    '_temp/../src.ts',
    '_temp\\private.json',
  ]) {
    await assert.rejects(artifact(f.root, path))
  }
  const outside = await mkdtemp(join(tmpdir(), 'gitowl-gate-outside-'))
  t.after(() => rm(outside, { recursive: true, force: true }))
  await writeFile(join(outside, 'private.txt'), 'fixture outside boundary')
  await symlink(
    outside,
    join(f.root, '_temp/linked'),
    process.platform === 'win32' ? 'junction' : 'dir'
  )
  await assert.rejects(
    artifact(f.root, '_temp/linked/private.txt'),
    /Linked evidence path/
  )
})

it('rejects worker self-attestation, pending findings, component screenshots and missing matrix cells', async t => {
  const f = await fixture(t)
  const validate = async (review: any) =>
    validateReview(
      f.root,
      await f.json(f.reviewRef.path, review),
      f.source,
      f.runtime,
      f.receipts.get('native-build').executable,
      f.receipts.get('native-ui-build').executable,
      f.commands
    )
  await assert.rejects(
    validate({ ...f.review, reviewerId: f.review.implementationOwnerId }),
    /independent parent/
  )
  await assert.rejects(
    validate({
      ...f.review,
      findings: [{ ...f.review.findings[0], status: 'pending' }],
    }),
    /Unclosed review finding/
  )
  const changed = () => JSON.parse(JSON.stringify(f.review))
  let review = changed()
  review.rounds[2].captures[0].kind = 'EditorTester'
  await assert.rejects(validate(review), /not desktop acceptance/)
  review = changed()
  review.rounds[2].captures.pop()
  await assert.rejects(validate(review), /Missing desktop matrix cell/)
  review = changed()
  review.rounds[2].captures[0].sourceDigest = hash('stale capture')
  await assert.rejects(validate(review), /Stale desktop capture/)
  await f.file(
    f.review.independentSourceReview.path,
    'tampered parent source review'
  )
  await assert.rejects(validate(f.review), /Changed artifact/)
})

it('validates the exact MyGo installed-file list, including no private or traversing entries', () => {
  const valid = 'Gitowl.exe\nbuild-info.json\ndesktop-kernel\nicons\nlogo.png\n'
  validateInstalledFileList(valid)
  for (const invalid of [
    valid + 'profile\n',
    valid + '../private\n',
    valid + 'logo.png\n',
    valid.replace('logo.png\n', ''),
  ]) {
    assert.throws(
      () => validateInstalledFileList(invalid),
      /installed-file list/
    )
  }
})

it('checks trimmed Windows executable identity without requiring omitted linker flags', async () => {
  const { NtExecutable, NtExecutableResource, Resource } = await import(
    'resedit'
  )
  const make = (name = 'Gitowl', build = '20261109123456') => {
    const exe = NtExecutable.createEmpty(false, false)
    const resources = NtExecutableResource.from(exe)
    const version = Resource.VersionInfo.createEmpty()
    version.setStringValues(
      { lang: 1033, codepage: 1200 },
      {
        ProductName: name,
        ProductVersion: '0.0.1',
        OriginalFilename: 'Gitowl.exe',
        MyGoIdentifier: 'io.github.oubigfa.gitowl',
      }
    )
    version.outputToResourceEntries(resources.entries)
    resources.outputResource(exe)
    exe.setExtraData(Buffer.from(build + '\0'))
    return Buffer.from(exe.generate())
  }
  const metadata =
    'path\tgithub.com/OUBIGFA/Gitowl/app/native\n\tbuild\t-tags=mygo_nodownload,mygo_noinspector\n\tbuild\t-trimpath=true\n'
  await validatePackagedExecutable(make(), metadata, '20261109123456')
  await assert.rejects(
    validatePackagedExecutable(make('Other'), metadata, '20261109123456'),
    /identity/
  )
  await assert.rejects(
    validatePackagedExecutable(
      make(),
      metadata.replace('mygo_noinspector', 'e2e'),
      '20261109123456'
    ),
    /inspector/
  )
  await assert.rejects(
    validatePackagedExecutable(
      make(),
      metadata.replace('-trimpath=true', '-trimpath=false'),
      '20261109123456'
    ),
    /build paths/
  )
  await assert.rejects(
    validatePackagedExecutable(make(), metadata, '20261109123457'),
    /build number/
  )
})

it('rejects extra resources before a shipping directory can authorize an installer', async t => {
  const f = await fixture(t)
  const acceptance = await validateAcceptance(f.root, f.acceptanceRef.path)
  await f.file('_temp/package/seed-profile.js', 'fixture')
  await assert.rejects(
    validatePackagedAssets(f.root, '_temp/package', acceptance),
    /Unreviewed resource/
  )
})

it('hashes actual submodule source bytes as well as its Git revision', async t => {
  const f = await fixture(t)
  const child = join(f.root, 'upstream-fixture')
  await mkdir(child)
  await git(child, ['init', '--initial-branch=main'])
  await f.file('upstream-fixture/upstream.ts', 'original upstream fixture')
  await git(child, ['add', '.'])
  await git(child, [
    '-c',
    'user.name=Fixture',
    '-c',
    'user.email=fixture@example.invalid',
    'commit',
    '-m',
    'fixture upstream',
  ])
  await git(f.root, ['add', 'upstream-fixture'])
  const before = await captureSourceTreeEvidence(f.root)
  assert.equal(before.submodules.length, 1)
  assert.ok(
    before.files.some(file => file.path === 'upstream-fixture/upstream.ts')
  )
  await f.file(
    'upstream-fixture/upstream.ts',
    'changed bytes without changing HEAD'
  )
  assert.notEqual(
    (await captureSourceTreeEvidence(f.root)).sourceDigest,
    before.sourceDigest
  )
})

it('rejects header-only, truncated and checksum-invalid PNG capture artifacts', () => {
  const png = fixturePNG('framing fixture')
  assert.deepEqual(pngDimensions(png), { width: 900, height: 620 })
  assert.throws(() => pngDimensions(png.subarray(0, 32)))
  assert.throws(() => pngDimensions(png.subarray(0, png.length - 1)), /PNG/)
  png[40] ^= 1
  assert.throws(() => pngDimensions(png), /checksum|Truncated/)
})

it('recording runs a real command and captures its actual exit and bytes', async t => {
  const root = await mkdtemp(join(tmpdir(), 'gitowl-gate-command-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const stdout = join(root, 'stdout.log')
  const stderr = join(root, 'stderr.log')
  assert.equal(
    await runRecordedCommand(
      process.execPath,
      [
        '-e',
        'process.stdout.write("actual output"); process.stderr.write("actual error"); process.exitCode=7',
      ],
      root,
      process.env,
      stdout,
      stderr
    ),
    7
  )
  assert.equal(await readFile(stdout, 'utf8'), 'actual output')
  assert.equal(await readFile(stderr, 'utf8'), 'actual error')
  await assert.rejects(
    runRecordedCommand(
      process.execPath,
      ['-e', ''],
      root,
      process.env,
      stdout,
      stderr
    ),
    /EEXIST/
  )
})

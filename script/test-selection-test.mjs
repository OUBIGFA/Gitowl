import assert from 'node:assert/strict'
import { it } from 'node:test'
import { mkdir, writeFile, unlink } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { createRequire } from 'node:module'
import { selectUnitTestFiles } from './test-selection.mjs'

const { createTempDirectory } = createRequire(import.meta.url)(
  '../app/test/helpers/temp.ts'
)

const assisted = [
  'app/test/unit/git/assisted-commit-test.ts',
  'app/test/unit/stores/copilot-assisted-commit-test.ts',
  'app/test/unit/copilot-assisted-commit-sdk-test.ts',
  'app/test/unit/ui/assisted-repository-creation-test.tsx',
]
const native = [
  'app/test/unit/native-commit-assisted-test.ts',
  'app/test/unit/native-workspace-test.ts',
]
const files = [...assisted, ...native]

async function fixture(context) {
  const root = await createTempDirectory(context)
  const sources = {
    'tsconfig.json':
      '{"compilerOptions":{"moduleResolution":"bundler","resolveJsonModule":true},"include":["app/**/*"]}',
    'app/test/globals.mts': "import './helpers/platform'\n",
    'app/test/helpers/platform.ts': 'export {}\n',
    'app/test/helpers/repositories.ts': 'export {}\n',
    'app/src/lib/git/status.ts':
      "import '../../../../references/input.json'\nexport {}\n",
    'app/src/lib/stores/app-store.ts':
      "import '../git/status'\nimport '../../models/repository'\n",
    'app/src/lib/copilot-client.ts': 'export {}\n',
    'app/src/models/repository.ts': 'export {}\n',
    'app/src/ui/dispatcher/dispatcher.ts':
      "import '../../lib/stores/app-store'\n",
    'references/input.json': '{"fixture":true}\n',
    [assisted[0]]:
      "import '../../../src/lib/git/status'\nimport '../../helpers/repositories'\n",
    [assisted[1]]:
      "import '../../../src/lib/stores/app-store'\nimport '../../helpers/repositories'\n",
    [assisted[2]]: "await import('../../src/lib/copilot-client')\n",
    [assisted[3]]: "import '../../../src/ui/dispatcher/dispatcher'\n",
  }
  for (const [path, content] of Object.entries(sources)) {
    await mkdir(dirname(join(root, path)), { recursive: true })
    await writeFile(join(root, path), content)
  }
  return root
}

it('skips the whole upstream assisted family for unrelated layout and same-directory files', async context => {
  const root = await fixture(context)
  for (const path of [
    'app/native/view.go',
    'app/src/lib/stores/sidebar-counter.ts',
    'app/src/models/unrelated.ts',
  ]) {
    assert.deepEqual(selectUnitTestFiles(files, [path], root), native, path)
  }
})

it('selects only the tests reached through real transitive dependencies', async context => {
  const root = await fixture(context)
  for (const path of ['app/src/lib/git/status.ts', 'references/input.json']) {
    assert.deepEqual(
      selectUnitTestFiles(files, [path], root),
      [assisted[0], assisted[1], assisted[3], ...native],
      path
    )
  }
  assert.deepEqual(
    selectUnitTestFiles(files, ['app/src/models/repository.ts'], root),
    [assisted[1], assisted[3], ...native]
  )
  assert.deepEqual(
    selectUnitTestFiles(files, ['app/src/lib/copilot-client.ts'], root),
    [assisted[2], ...native]
  )
  assert.deepEqual(selectUnitTestFiles(files, [assisted[1]], root), [
    assisted[1],
    ...native,
  ])
})

it('keeps shared runner, declarations, fixture and dependency changes fully covered', async context => {
  const root = await fixture(context)
  for (const path of [
    'app/test/globals.mts',
    'app/test/helpers/platform.ts',
    'app/test/fixtures/sample/_git/HEAD',
    'app/src/lib/globals.d.ts',
    'package.json',
    'package-lock.json',
    'app/package.json',
    'app/package-lock.json',
    'tsconfig.json',
    '.test.env',
    'script/test.mjs',
    'script/test-selection.mjs',
  ])
    assert.deepEqual(selectUnitTestFiles(files, [path], root), files, path)
})

it('keeps native generation tests in every default run', () => {
  assert.deepEqual(selectUnitTestFiles(files, []), native)
})

it('fails closed for an unknown change set or an unresolved relative dependency', async context => {
  const root = await fixture(context)
  assert.deepEqual(selectUnitTestFiles(files, null, root), files)
  await unlink(join(root, 'app/src/lib/git/status.ts'))
  assert.deepEqual(
    selectUnitTestFiles(files, ['app/src/lib/git/status.ts'], root),
    files
  )
})

it('handles dependency cycles and Windows path spelling without losing affected tests', async context => {
  const root = await fixture(context)
  await writeFile(
    join(root, 'app/src/models/repository.ts'),
    "import '../lib/stores/app-store'\n"
  )
  const windows = files.map(path => path.replaceAll('/', '\\'))
  assert.deepEqual(
    selectUnitTestFiles(windows, ['app\\src\\models\\repository.ts'], root),
    [assisted[1], assisted[3], ...native].map(path =>
      path.replaceAll('/', '\\')
    )
  )
})

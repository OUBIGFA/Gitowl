import { execFileSync, spawn } from 'child_process'
import { join, resolve } from 'path'
import { readdir, readFile, stat } from 'fs/promises'
import { parseEnv } from 'util'

function reporter(r) {
  return ['--test-reporter', r, '--test-reporter-destination', 'stdout']
}

async function findTestFilesIn(paths) {
  const files = []
  for (const path of paths) {
    const entry = await stat(path)
    if (entry.isFile()) {
      files.push(path)
      continue
    }

    for (const file of await readdir(path, { recursive: true }).then(x =>
      x
        .filter(f => /-test\.(ts|tsx|js|jsx|mts|mjs)$/.test(f))
        .map(f => join(path, f))
    )) {
      files.push(file)
    }
  }
  return files
}

const fileArgs = process.argv.slice(2).filter(a => !a.startsWith('--'))
const switchArgs = process.argv.slice(2).filter(a => a.startsWith('--'))
// Real Git/SDK tests launch their own subprocesses. Bound file concurrency so
// a full run does not exhaust per-test deadlines through process contention.
if (!switchArgs.some(a => a.startsWith('--test-concurrency'))) {
  switchArgs.push('--test-concurrency=4')
}
if (!switchArgs.some(a => a.startsWith('--test-timeout'))) {
  switchArgs.push('--test-timeout=600000')
}

const projectRoot = join(import.meta.dirname, '..')
let files =
  fileArgs.length > 0
    ? await findTestFilesIn(fileArgs)
    : await findTestFilesIn([join(projectRoot, 'app', 'test', 'unit')])

// The upstream assisted-commit suites drive real repositories and remotes for
// about 90% of a full unit run. Run them when the current change set (against
// the last delivered origin/main) touches the code they exercise, or when
// GITOWL_UNIT_ALL=1. Any doubt about the change set runs them.
const assistedTest = /[\\/]assisted-commit[^\\/]*-test\.tsx?$/
const assistedSources = [
  'app/src/lib/assisted-commit/',
  'app/src/lib/git/',
  'app/src/lib/stores/',
  'app/src/lib/copilot',
  'app/src/models/',
  'app/test/helpers/',
  'package-lock.json',
  'app/package.json',
  'app/package-lock.json',
]
function changedPaths() {
  const run = args =>
    execFileSync('git', args, { cwd: projectRoot, encoding: 'utf8' })
      .split('\n')
      .filter(Boolean)
  return [
    ...run(['diff', '--name-only', 'origin/main']),
    ...run(['ls-files', '--others', '--exclude-standard']),
  ]
}
if (fileArgs.length === 0 && process.env.GITOWL_UNIT_ALL !== '1') {
  let changed = null
  try {
    changed = changedPaths()
  } catch {}
  const relevant =
    changed === null ||
    changed.some(
      p => assistedTest.test(p) || assistedSources.some(s => p.startsWith(s))
    )
  if (!relevant) {
    const skipped = files.filter(f => assistedTest.test(f))
    files = files.filter(f => !assistedTest.test(f))
    console.log(
      `Skipping ${skipped.length} upstream assisted-commit test files: no change to the code they exercise (set GITOWL_UNIT_ALL=1 to run them).`
    )
  }
}

// I would _looooove_ to use the `--env-file` option, but it doesn't override
// existing environment variables and we need to override some of them.
const testEnv = parseEnv(await readFile(join(projectRoot, '.test.env'), 'utf8'))
Object.entries(testEnv).forEach(([k, v]) => (process.env[k] = v))

const args = [
  '--disable-warning=ExperimentalWarning',
  '--experimental-test-module-mocks',
  // Allow CJS resolution to find ESM-only packages (e.g. @github/copilot-sdk)
  // whose "exports" only declare an "import" condition with no "require" fallback.
  '--conditions=import',
  ...['--import', 'tsx'],
  ...['--import', './app/test/globals.mts'],
  ...switchArgs,
  '--test',
  ...reporter('spec'),
  ...(process.env.GITHUB_ACTIONS ? reporter('node-test-github-reporter') : []),
  ...files,
]

spawn('node', args, {
  stdio: 'inherit',
  cwd: resolve(import.meta.dirname, '..'),
}).on('exit', process.exit)

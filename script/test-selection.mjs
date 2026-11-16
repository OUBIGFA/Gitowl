import { existsSync, readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, isAbsolute, relative, resolve } from 'node:path'

const require = createRequire(import.meta.url)
const assistedTest =
  /[\\/](?:(?:copilot-)?assisted-commit[^\\/]*|assisted-repository-creation|assisted-worktree-mutations)-test\.tsx?$/
const sharedInputs = new Set([
  'app/test/globals.mts',
  'package.json',
  'package-lock.json',
  'app/package.json',
  'app/package-lock.json',
  'tsconfig.json',
  '.test.env',
  'script/test.mjs',
  'script/test-selection.mjs',
])

/** Follow project imports instead of treating unrelated siblings as dependencies.
 * Dynamic fixture paths and runner inputs are shared; unresolved graphs run all. */
export function selectUnitTestFiles(
  files,
  changed,
  root = resolve(import.meta.dirname, '..')
) {
  if (changed === null) return files
  const changes = changed.map(path => path.replaceAll('\\', '/'))
  if (
    changes.some(
      path =>
        sharedInputs.has(path) ||
        path.startsWith('app/test/fixtures/') ||
        /\.d\.tsx?$/.test(path)
    )
  ) {
    return files
  }
  const heavy = files.filter(file => assistedTest.test(file))
  if (changes.length === 0 || heavy.length === 0) {
    return files.filter(file => !assistedTest.test(file))
  }
  try {
    const ts = require('typescript')
    const config = ts.readConfigFile(
      resolve(root, 'tsconfig.json'),
      ts.sys.readFile
    )
    if (config.error)
      throw new Error('Unable to read test compiler configuration')
    const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, root)
    if (parsed.errors.length)
      throw new Error('Invalid test compiler configuration')
    const canonical = path =>
      process.platform === 'win32' ? path.toLowerCase() : path
    const key = path => canonical(resolve(root, path.replaceAll('\\', '/')))
    const cache = ts.createModuleResolutionCache(
      root,
      canonical,
      parsed.options
    )
    const visited = new Set()
    const parents = new Map()
    function inspect(file) {
      const absolute = resolve(root, file.replaceAll('\\', '/'))
      const current = key(absolute)
      if (visited.has(current)) return
      visited.add(current)
      const source = readFileSync(absolute, 'utf8')
      if (!/\.[cm]?[jt]sx?$/.test(absolute)) return
      const imports = ts.preProcessFile(source, true, true).importedFiles
      for (const item of imports) {
        let dependency = ts.resolveModuleName(
          item.fileName,
          absolute,
          parsed.options,
          ts.sys,
          cache
        ).resolvedModule?.resolvedFileName
        if (!dependency) {
          if (!item.fileName.startsWith('.')) continue
          dependency = resolve(dirname(absolute), item.fileName)
          if (!existsSync(dependency))
            throw new Error('Unresolved project import: ' + item.fileName)
        }
        const name = relative(root, dependency).replaceAll('\\', '/')
        if (
          isAbsolute(name) ||
          name === '..' ||
          name.startsWith('../') ||
          name.split('/').includes('node_modules')
        )
          continue
        const target = key(dependency)
        if (!parents.has(target)) parents.set(target, new Set())
        parents.get(target).add(current)
        inspect(dependency)
      }
    }
    for (const file of heavy) inspect(file)
    const globals = 'app/test/globals.mts'
    inspect(globals)
    const affected = new Set(changes.map(key))
    const pending = [...affected]
    for (let index = 0; index < pending.length; index++) {
      for (const parent of parents.get(pending[index]) ?? []) {
        if (!affected.has(parent)) {
          affected.add(parent)
          pending.push(parent)
        }
      }
    }
    if (affected.has(key(globals))) return files
    return files.filter(
      file => !assistedTest.test(file) || affected.has(key(file))
    )
  } catch (error) {
    console.warn('Running every assisted-commit test: ' + error.message)
    return files
  }
}

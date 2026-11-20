import assert from 'node:assert/strict'
import { it } from 'node:test'
import { readFile, realpath, writeFile } from 'fs/promises'
import { join } from 'path'
import { runInNewContext } from 'vm'
import webpack, { Configuration, Stats } from 'webpack'
import { createTempDirectory } from '../app/test/helpers/temp'

function production(native: boolean): ReadonlyArray<Configuration> {
  const previous = process.env.GITOWL_NATIVE_BUILD
  const modulePath = require.resolve('../app/webpack.production')
  try {
    process.env.GITOWL_NATIVE_BUILD = native ? '1' : '0'
    delete require.cache[modulePath]
    return require(modulePath).default
  } finally {
    delete require.cache[modulePath]
    if (previous === undefined) {
      delete process.env.GITOWL_NATIVE_BUILD
    } else {
      process.env.GITOWL_NATIVE_BUILD = previous
    }
  }
}

it('omits only the compatibility renderer from an explicit native production build', () => {
  const entries = (configs: ReadonlyArray<Configuration>) =>
    configs.flatMap(config => Object.keys(config.entry as object))
  assert.deepEqual(entries(production(false)), [
    'main',
    'renderer',
    'native-kernel',
    'crash',
    'cli',
    'highlighter',
  ])
  assert.deepEqual(entries(production(true)), [
    'main',
    'native-kernel',
    'crash',
    'cli',
    'highlighter',
  ])
})

it('uses distinct persistent caches for each target and build mode', () => {
  const compatibility = production(false)
  const native = production(true)
  const caches = compatibility.map(config => {
    const cache = config.cache
    assert.ok(cache && typeof cache === 'object' && cache.type === 'filesystem')
    return cache
  })
  for (const cache of caches) {
    assert.match(cache.cacheDirectory!, /[\\/]\.local[\\/]webpack-cache$/)
  }
  assert.equal(new Set(caches.map(cache => cache.name)).size, 6)
  assert.notDeepEqual(native[0].cache, compatibility[0].cache)
})

it('reuses compiled modules while invalidating changed source and compile-time identity', async context => {
  // Webpack resolves symlinks; macOS temp paths may start with /var while
  // modules resolve under /private/var. Use the same physical context.
  const root = await realpath(await createTempDirectory(context))
  await writeFile(join(root, 'build.js'), 'module.exports = {}\n')
  await writeFile(join(root, 'entry.js'), 'module.exports = BUILD_ID\n')
  const cache = production(true)[0].cache
  assert.ok(cache && typeof cache === 'object' && cache.type === 'filesystem')
  const persistentCache = cache
  async function compile(identity: string) {
    const compiler = webpack({
      context: root,
      mode: 'development',
      target: 'node',
      devtool: false,
      entry: './entry.js',
      output: {
        path: join(root, 'out'),
        filename: 'bundle.js',
        library: { type: 'commonjs2' },
      },
      cache: {
        ...persistentCache,
        cacheDirectory: join(root, 'cache'),
        buildDependencies: { config: [join(root, 'build.js')] },
      },
      plugins: [
        new webpack.DefinePlugin({ BUILD_ID: JSON.stringify(identity) }),
      ],
    })
    assert.ok(compiler)
    const stats = await new Promise<Stats>((resolve, reject) => {
      compiler.run((error, result) => {
        if (error || !result) {
          reject(error ?? new Error('Missing webpack result'))
        } else {
          resolve(result)
        }
      })
    })
    await new Promise<void>((resolve, reject) =>
      compiler.close(error => (error ? reject(error) : resolve()))
    )
    assert.equal(stats.hasErrors(), false, stats.toString())
    const sandbox = { module: { exports: undefined as unknown } }
    runInNewContext(
      await readFile(join(root, 'out/bundle.js'), 'utf8'),
      sandbox
    )
    const moduleStats = stats.toJson({
      all: false,
      modules: true,
      cachedModules: true,
    }).modules
    return {
      result: sandbox.module.exports,
      built: moduleStats?.find(module => module.name === './entry.js')?.built,
    }
  }
  assert.deepEqual(await compile('first'), { result: 'first', built: true })
  assert.deepEqual(await compile('first'), { result: 'first', built: false })
  assert.equal((await compile('second')).result, 'second')
  await writeFile(
    join(root, 'entry.js'),
    'module.exports = BUILD_ID + " changed"\n'
  )
  assert.equal((await compile('second')).result, 'second changed')
})

import assert from 'node:assert/strict'
import { it } from 'node:test'
import { spawn } from 'child_process'
import { once } from 'events'
import { cp, mkdir, mkdtemp, rm, symlink, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { resolveGitDir } from 'dugite'
import webpack from 'webpack'
import nativeConfig from '../../webpack.native-service'

it(
  'runs remote Git regressions in a plain Node subprocess',
  { timeout: 180000 },
  async () => {
    const scratch = resolve('_temp/provider-research/remote-tests')
    await mkdir(scratch, { recursive: true })
    const runtime = await mkdtemp(join(scratch, 'run-'))
    try {
      const fixture = resolve(
        'app/test/native-service/remote-operations-fixture.ts'
      )
      // Reuse the native service's real loaders/defines, without changing its
      // entry point. Type-check the bundled graph, not unrelated workers' tests.
      const compiler = webpack({
        ...nativeConfig,
        entry: fixture,
        output: { path: runtime, filename: 'remote-fixture.js' },
        module: {
          ...nativeConfig.module,
          rules: nativeConfig.module!.rules!.map(rule =>
            typeof rule === 'object' &&
            rule !== null &&
            'loader' in rule &&
            rule.loader === 'ts-loader'
              ? {
                  ...rule,
                  include: [resolve('app/src'), fixture],
                  options: { onlyCompileBundledFiles: true },
                }
              : rule
          ),
        },
      })
      await new Promise<void>((resolve, reject) => {
        compiler.run((error, stats) => {
          compiler.close(closeError => {
            if (error || closeError || !stats || stats.hasErrors()) {
              reject(
                error ??
                  closeError ??
                  new Error(
                    stats?.toString({
                      colors: false,
                      assets: false,
                      modules: false,
                    }) ?? 'No webpack result'
                  )
              )
            } else {
              resolve()
            }
          })
        })
      })
      const ext = process.platform === 'win32' ? '.exe' : ''
      for (const asset of [
        `process-proxy${ext}`,
        `printenvz${ext}`,
        'desktop-trampoline',
      ]) {
        await cp(resolve('out', asset), join(runtime, asset), {
          recursive: true,
        })
      }
      // Hooks resolve Git beside the bundle, exactly as in the installed service.
      // A junction to the real Git payload is not a credential/helper replacement.
      await symlink(
        resolveGitDir(),
        join(runtime, 'git'),
        process.platform === 'win32' ? 'junction' : 'dir'
      )
      const globalConfig = join(runtime, 'global-config')
      await writeFile(
        globalConfig,
        '[protocol "file"]\n\tallow = always\n[core]\n\tautocrlf = false\n'
      )
      const env: NodeJS.ProcessEnv = {
        ...process.env,
        LOCAL_GIT_DIRECTORY: resolveGitDir(),
        GIT_CONFIG_GLOBAL: globalConfig,
        GIT_CONFIG_PARAMETERS: '',
        GIT_CONFIG_COUNT: '0',
        GITOWL_REMOTE_TEST_DIRECTORY: runtime,
      }
      // Isolate fixture identities and proxy routing from the machine. Keep the
      // production system Git config and credential-helper protection intact.
      for (const key of Object.keys(env)) {
        if (
          /^(?:all_proxy|https?_proxy|no_proxy|GIT_(?:AUTHOR|COMMITTER)_(?:NAME|EMAIL)|GIT_(?:DIR|WORK_TREE|COMMON_DIR|INDEX_FILE|OBJECT_DIRECTORY|ALTERNATE_OBJECT_DIRECTORIES|EXEC_PATH|CONFIG|CONFIG_(?:KEY|VALUE)_\d+)|NODE_OPTIONS|NODE_TEST_CONTEXT)$/i.test(
            key
          )
        ) {
          delete env[key]
        }
      }
      const child = spawn(
        process.execPath,
        [
          '--experimental-webstorage',
          `--localstorage-file=${join(runtime, 'preferences')}`,
          '--test',
          '--test-reporter=spec',
          join(runtime, 'remote-fixture.js'),
        ],
        { cwd: runtime, env, stdio: ['ignore', 'pipe', 'pipe'] }
      )
      let stdout = ''
      let stderr = ''
      child.stdout.on('data', chunk => (stdout += chunk.toString()))
      child.stderr.on('data', chunk => (stderr += chunk.toString()))
      const killTimer = setTimeout(() => child.kill(), 150000)
      try {
        await once(child, 'close')
      } finally {
        clearTimeout(killTimer)
      }
      assert.equal(child.exitCode, 0, `${stdout}\n${stderr}`)
      assert.match(stdout, /fail 0/)
      process.stdout.write(stdout)
    } finally {
      // The child must close before deleting preferences and worktrees on Windows.
      await rm(runtime, { recursive: true, force: true })
    }
  }
)

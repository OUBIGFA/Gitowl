import { join, resolve } from 'path'
import parse from 'minimist'
import { execFile, spawn } from 'child_process'

const run = (...args: Array<string>) => {
  function cb(e: unknown | null, stderr?: string) {
    if (e) {
      console.error(`Error running command ${args}`)
      console.error(stderr ?? `${e}`)
      process.exit(
        typeof e === 'object' && 'code' in e && typeof e.code === 'number'
          ? e.code
          : 1
      )
    }
  }

  if (process.platform === 'darwin') {
    execFile('open', ['-n', join(__dirname, '../../..'), '--args', ...args], cb)
  } else if (process.platform === 'win32') {
    // The packaged CLI still parses the original open/clone grammar, but only
    // the MyGo owner may create or focus the original kernel in native builds.
    const executable =
      __APP_NAME__ === 'Gitowl'
        ? join(__dirname, '../../Gitowl.exe')
        : join(__dirname, `../../GitHubDesktop${__DEV__ ? '-dev' : ''}.exe`)
    spawn(executable, args, {
      detached: true,
      stdio: 'ignore',
    })
      .on('error', cb)
      .on('exit', code => (process.exitCode = code ?? process.exitCode))
      .unref()
  } else {
    throw new Error('Unsupported platform')
  }
}

const args = parse(process.argv.slice(2), {
  alias: { help: 'h', branch: 'b' },
  boolean: ['help'],
})

const commandName = __APP_NAME__ === 'Gitowl' ? 'gitowl' : 'github'

const usage = (exitCode = 1): never => {
  process.stderr.write(
    `${__APP_NAME__} CLI usage: \n` +
      `  ${commandName}                            Open the current directory\n` +
      `  ${commandName} open [path]                Open the provided path\n` +
      `  ${commandName} clone [-b branch] <url>    Clone the repository by url or name/owner\n` +
      '                                    (ex torvalds/linux), optionally checking out\n' +
      '                                    the branch\n'
  )
  process.exit(exitCode)
}

delete process.env.ELECTRON_RUN_AS_NODE

if (args.help || args._.at(0) === 'help') {
  usage(0)
} else if (args._.at(0) === 'clone') {
  const urlArg = args._.at(1)
  // Assume name with owner slug if it looks like it
  const url =
    urlArg && /^[^\/]+\/[^\/]+$/.test(urlArg)
      ? `https://github.com/${urlArg}`
      : urlArg

  if (!url) {
    usage(1)
  } else if (typeof args.branch === 'string') {
    run(`--cli-clone=${url}`, `--cli-branch=${args.branch}`)
  } else {
    run(`--cli-clone=${url}`)
  }
} else {
  const [firstArg, secondArg] = args._
  const pathArg = firstArg === 'open' ? secondArg : firstArg
  const path = resolve(pathArg ?? '.')
  run(`--cli-open=${path}`)
}

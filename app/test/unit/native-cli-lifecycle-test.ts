import assert from 'node:assert/strict'
import { mkdir, mkdtemp, readFile } from 'fs/promises'
import { join, resolve } from 'path'
import { it, mock } from 'node:test'

// Keep the upstream file-writing and PATH update code real. Only the registry
// boundary is isolated so install/uninstall tests cannot edit the user's PATH.
let userPath = 'C:\\Tools;C:\\Desktop\\bin'
let registryFailure = false
mock.module('registry-js', {
  namedExports: {
    HKEY: { HKEY_CURRENT_USER: 'HKCU' },
    RegistryValueType: { REG_SZ: 'REG_SZ', REG_EXPAND_SZ: 'REG_EXPAND_SZ' },
    enumerateValues: () => [
      { name: 'Path', type: 'REG_EXPAND_SZ', data: userPath },
    ],
    setValue: (_hive: string, _key: string, _name: string, _type: string, value: string) => {
      if (registryFailure) throw new Error('registry denied')
      userPath = value
    },
  },
})

it('native install, upgrade and uninstall maintain only the Gitowl CLI entry', async () => {
  const lifecycle = await import('../../src/main-process/squirrel-updater')
  const previousName = __APP_NAME__
  const previousExecutable = process.execPath
  const previousMode = process.env.GITOWL_NATIVE_CORE
  const scratch = resolve('_temp/native-cli-tests')
  await mkdir(scratch, { recursive: true })
  const install = await mkdtemp(join(scratch, 'installation with spaces-'))
  try {
    Object.assign(globalThis, { __APP_NAME__: 'Gitowl' })
    Object.defineProperty(process, 'execPath', {
      value: join(install, 'desktop-kernel/electron.exe'),
    })
    const bin = join(install, 'bin')
    await lifecycle.handleNativeCLIEvent(['app', '--gitowl-install-cli'])
    assert.equal(userPath, `C:\\Tools;C:\\Desktop\\bin;${bin}`)
    const batch = await readFile(join(bin, 'gitowl.bat'), 'utf8')
    const shell = await readFile(join(bin, 'gitowl'), 'utf8')
    assert.match(batch, /desktop-kernel\\app\\static\\github\.bat/)
    assert.match(shell, /desktop-kernel\/app\/static\/github\.sh/)
    assert.doesNotMatch(batch + shell, /GitHubDesktop\.exe|Update\.exe/)
    await lifecycle.handleNativeCLIEvent(['app', '--gitowl-install-cli'])
    assert.equal(userPath, `C:\\Tools;C:\\Desktop\\bin;${bin}`)
    await lifecycle.handleNativeCLIEvent(['app', '--gitowl-uninstall-cli'])
    assert.equal(userPath, 'C:\\Tools;C:\\Desktop\\bin')
    assert.equal(lifecycle.handleNativeCLIEvent(['app', '--unrelated']), null)
    registryFailure = true
    await assert.rejects(
      lifecycle.handleNativeCLIEvent(['app', '--gitowl-install-cli'])!,
      /Could not set the PATH environment variable/
    )
    Object.assign(globalThis, { __APP_NAME__: 'GitHub Desktop' })
    assert.equal(lifecycle.handleNativeCLIEvent(['app', '--gitowl-install-cli']), null)
  } finally {
    registryFailure = false
    Object.assign(globalThis, { __APP_NAME__: previousName })
    Object.defineProperty(process, 'execPath', { value: previousExecutable })
    if (previousMode === undefined) delete process.env.GITOWL_NATIVE_CORE
    else process.env.GITOWL_NATIVE_CORE = previousMode
  }
})

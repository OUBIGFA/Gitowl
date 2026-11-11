import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { getReplacements } from '../app/app-info'
import { nativeCLIPaths } from '../app/src/main-process/native-cli'
import { join, resolve } from 'path'
import { nativeRuntimeLaunchConfig } from './prepare-native-runtime'
import { readFile } from 'fs/promises'

it('native builds reuse original Desktop OAuth defaults, overrides and dev-secrets rules unchanged', () => {
  const keys = [
    'GITOWL_NATIVE_BUILD',
    'DESKTOP_OAUTH_CLIENT_ID',
    'DESKTOP_OAUTH_CLIENT_SECRET',
  ] as const
  const saved = keys.map(key => process.env[key])
  try {
    for (const overrides of [
      {},
      { DESKTOP_OAUTH_CLIENT_ID: 'override-id' },
      { DESKTOP_OAUTH_CLIENT_SECRET: 'override-secret' },
      {
        DESKTOP_OAUTH_CLIENT_ID: 'override-id',
        DESKTOP_OAUTH_CLIENT_SECRET: 'override-secret',
      },
    ]) {
      delete process.env.DESKTOP_OAUTH_CLIENT_ID
      delete process.env.DESKTOP_OAUTH_CLIENT_SECRET
      Object.assign(process.env, overrides)
      delete process.env.GITOWL_NATIVE_BUILD
      const upstream = getReplacements()
      process.env.GITOWL_NATIVE_BUILD = '1'
      const native = getReplacements()
      assert.equal(native.__OAUTH_CLIENT_ID__, upstream.__OAUTH_CLIENT_ID__)
      assert.equal(native.__OAUTH_SECRET__, upstream.__OAUTH_SECRET__)
      assert.equal(native.__DEV_SECRETS__, upstream.__DEV_SECRETS__)
      assert.ok(JSON.parse(native.__OAUTH_CLIENT_ID__).length > 0)
      assert.ok(JSON.parse(native.__OAUTH_SECRET__).length > 0)
    }
  } finally {
    keys.forEach((key, index) => {
      if (saved[index] === undefined) {
        delete process.env[key]
      } else {
        process.env[key] = saved[index]
      }
    })
  }
})

it('stages only the original compiled callback scheme, without credentials or unconditional stable-handler registration', async () => {
  for (const [channel, secret, expected] of [
    ['research-preview', '', 'x-github-desktop-dev-auth'],
    ['production', '', 'x-github-desktop-dev-auth'],
    ['research-preview', 'configured-fixture', 'x-github-desktop-auth'],
    ['development', 'configured-fixture', 'x-github-desktop-dev-auth'],
  ]) {
    const result = await nativeRuntimeLaunchConfig({
      ...process.env,
      RELEASE_CHANNEL: channel,
      GITOWL_NATIVE_BUILD: '1',
      DESKTOP_OAUTH_CLIENT_SECRET: secret,
    })
    assert.deepEqual(result, { oauthCallbackScheme: expected })
    assert.doesNotMatch(
      JSON.stringify(result),
      /configured-fixture|client_id|client_secret|token/
    )
  }
  const config = JSON.parse(await readFile('app/native/mygo.json', 'utf8'))
  assert.deepEqual(
    config.urlSchemes,
    ['gitowl', 'gitowl-auth'],
    'only the running owner registers the selected original OAuth callback; installers must not claim both Desktop handlers'
  )
})

describe('Gitowl packaged CLI resources', () => {
  it('resolves the fixed MyGo installation, never a Squirrel bin or Desktop command', () => {
    const root = resolve('_temp/native-full-ui/installation with spaces')
    assert.deepEqual(
      nativeCLIPaths(join(root, 'desktop-kernel/electron.exe')),
      {
        bin: join(root, 'bin'),
        command: 'gitowl',
        batch: join(root, 'desktop-kernel/app/static/github.bat'),
        shell: join(root, 'desktop-kernel/app/static/github.sh'),
      }
    )
  })
  it('keeps the original cli.js parser in both flattened Windows launch resources', async () => {
    const batch = await readFile('script/native-windows-cli.bat', 'utf8')
    const shell = await readFile('script/native-windows-cli.sh', 'utf8')
    assert.match(batch, /ELECTRON_RUN_AS_NODE=1/)
    assert.match(batch, /\.\.\\cli\.js/)
    assert.match(shell, /ELECTRON_RUN_AS_NODE=1/)
    assert.match(shell, /\.\.\/cli\.js/)
    assert.doesNotMatch(batch + shell, /GitHubDesktop\.exe/)
  })
})

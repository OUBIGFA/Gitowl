import assert from 'node:assert/strict'
import { after, before, describe, it } from 'node:test'
import { EventEmitter } from 'events'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { exec } from 'dugite'
import { ipcRenderer } from 'electron'
import {
  NativeOnboardingPresenter,
  projectSignInView,
} from '../../src/native-kernel/onboarding-presenter'
import { WelcomeStep } from '../../src/ui/welcome/welcome-step'
import { SignInStep } from '../../src/lib/stores/sign-in-store'
import {
  saveConfiguredGitAuthor,
  deleteGitAuthorConfigLock,
} from '../../src/ui/lib/git-author-configuration'

// Only the Electron boundary is supplied; the original AppStore/Dispatcher,
// Git configuration helpers, Welcome marker and SignInStore are exercised.
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
let fixture: string
let globalConfig: string
const previousGlobal = process.env.GIT_CONFIG_GLOBAL
const previousNoSystem = process.env.GIT_CONFIG_NOSYSTEM
before(async () => {
  const scratch = resolve('_temp/native-onboarding-tests')
  await mkdir(scratch, { recursive: true })
  fixture = await mkdtemp(join(scratch, 'case-'))
  globalConfig = join(fixture, 'global-config')
  process.env.GIT_CONFIG_GLOBAL = globalConfig
  process.env.GIT_CONFIG_NOSYSTEM = '1'
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
})
after(async () => {
  window.close()
  if (previousGlobal === undefined) {
    delete process.env.GIT_CONFIG_GLOBAL
  } else {
    process.env.GIT_CONFIG_GLOBAL = previousGlobal
  }
  if (previousNoSystem === undefined) {
    delete process.env.GIT_CONFIG_NOSYSTEM
  } else {
    process.env.GIT_CONFIG_NOSYSTEM = previousNoSystem
  }
  await rm(fixture, { recursive: true, force: true })
})

async function config(key: string) {
  const result = await exec(
    ['config', '--file', globalConfig, '--get', key],
    fixture
  )
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout.trim()
}

describe('shared original Git author configuration', () => {
  it('uses conditional ordered writes and the real original Git lock recovery', async () => {
    await writeFile(globalConfig + '.lock', 'owned fixture lock')
    const blocked = await saveConfiguredGitAuthor(
      'Fixture Author',
      'fixture@example.invalid',
      undefined,
      undefined
    )
    assert.deepEqual(blocked, { kind: 'locked', path: globalConfig + '.lock' })
    assert.equal(
      await readFile(globalConfig + '.lock', 'utf8'),
      'owned fixture lock'
    )
    await deleteGitAuthorConfigLock(
      blocked.kind === 'locked'
        ? blocked.path
        : assert.fail('Expected an original lock prompt')
    )
    const saved = await saveConfiguredGitAuthor(
      'Fixture Author',
      'fixture@example.invalid',
      undefined,
      undefined
    )
    assert.deepEqual(saved, { kind: 'saved' })
    assert.equal(await config('user.name'), 'Fixture Author')
    assert.equal(await config('user.email'), 'fixture@example.invalid')
    const bytes = await readFile(globalConfig, 'utf8')
    await saveConfiguredGitAuthor(
      '',
      '',
      'Fixture Author',
      'fixture@example.invalid'
    )
    assert.equal(
      await readFile(globalConfig, 'utf8'),
      bytes,
      'empty inputs must keep the original skip-write semantics'
    )
  })
})

describe('native Welcome using the original kernel', () => {
  it('does not bypass a fresh profile, and completes only after the original author form save', async () => {
    const { getAppRuntime } = await import('../../src/ui/app-runtime')
    const runtime = getAppRuntime()
    const presenter = new NativeOnboardingPresenter(runtime)
    assert.equal(runtime.appStore.getState().showWelcomeFlow, true)
    await presenter.start()
    assert.equal(presenter.getState()?.step, WelcomeStep.Start)
    await assert.rejects(
      presenter.configureAuthor('BYPASS', 'wrong@example.invalid', false),
      /not the active Welcome step/
    )
    assert.equal(runtime.appStore.getState().showWelcomeFlow, true)
    await presenter.advance(WelcomeStep.ConfigureGit)
    assert.equal(presenter.getState()?.globalName, 'Fixture Author')
    await presenter.configureAuthor(
      'Native Author',
      'native@example.invalid',
      false
    )
    assert.equal(await config('user.name'), 'Native Author')
    assert.equal(await config('user.email'), 'native@example.invalid')
    assert.equal(runtime.appStore.getState().showWelcomeFlow, false)
    assert.equal(localStorage.getItem('has-shown-welcome-flow'), '1')
    assert.equal(presenter.getState(), null)
  })

  it('projects original authentication state without OAuth state, tokens or continuations', () => {
    const dto = projectSignInView({
      kind: SignInStep.Authentication,
      endpoint: 'https://api.example.invalid',
      expectedLogin: 'alice',
      loading: true,
      error: new Error('Visible error'),
      resultCallback: () => assert.fail('No callback may be reflected'),
      oauthState: {
        state: 'OAUTH-SENTINEL',
        endpoint: 'https://api.example.invalid',
        onAuthCompleted: () => assert.fail(),
        onAuthError: () => assert.fail(),
      },
    })
    assert.deepEqual(dto, {
      kind: 'Authentication',
      endpoint: 'https://api.example.invalid',
      expectedLogin: 'alice',
      loading: true,
      error: 'Visible error',
      unrecognizedEnterpriseServer: false,
    })
    assert.doesNotMatch(
      JSON.stringify(dto),
      /SENTINEL|onAuth|resultCallback|token/i
    )
  })
})

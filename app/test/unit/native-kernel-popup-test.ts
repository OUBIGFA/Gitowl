import assert from 'node:assert/strict'
import { after, afterEach, before, describe, it } from 'node:test'
import { EventEmitter } from 'events'
import { mkdir, mkdtemp, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { ipcRenderer } from 'electron'
import { exec, GitError as DugiteError } from 'dugite'
import { SignInStep } from '../../src/lib/stores/sign-in-store'
import type { AppRuntime } from '../../src/ui/app-runtime'
import { Account } from '../../src/models/account'
import { Popup, PopupType } from '../../src/models/popup'
import {
  Repository,
  RepositoryWithGitHubRepository,
  assertIsRepositoryWithGitHubRepository,
} from '../../src/models/repository'
import { GitHubRepository } from '../../src/models/github-repository'
import { Owner } from '../../src/models/owner'
import { RetryActionType } from '../../src/models/retry-actions'
import { ErrorWithMetadata } from '../../src/lib/error-with-metadata'
import { CopilotError } from '../../src/lib/copilot-error'
import { shell } from '../../src/lib/app-shell'
import { GitError } from '../../src/lib/git/core'
import { acquireAssistedCommitGitLease } from '../../src/lib/git/repository-operation'
import {
  KernelPopupError,
  closeKernelPopup,
  projectKernelPopup,
  replyToKernelPopup,
  supportedKernelPopupTypes,
  validateKernelPopupReply,
} from '../../src/native-kernel/popup-presenter'

// Complete only the Electron platform boundary, as app-runtime-test does. The
// stores, Dispatcher, trampoline helper, popup queue and Git remain original.
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})

const endpoint = 'https://api.example.test'
const alice = new Account(
  'alice',
  endpoint,
  'ACCOUNT-TOKEN-SENTINEL',
  [],
  '',
  1,
  'Alice'
)
const bob = new Account('bob', endpoint, 'BOB-TOKEN-SENTINEL', [], '', 2, 'Bob')
const other = new Account(
  'alice',
  'https://other.example.test/api/v3',
  'OTHER-TOKEN-SENTINEL',
  [],
  '',
  3,
  'Other Alice'
)
const futurePopupType = 'FutureUpstreamPopup' as PopupType
const presentation = {
  accounts: [alice, bob, other],
  askForConfirmationOnForcePush: true,
}

function repository(
  login: string | null = null
): RepositoryWithGitHubRepository {
  const repository = new Repository(
    'C:/popup-fixture/repo',
    80,
    new GitHubRepository('repo', new Owner('owner', endpoint, 1), 1),
    false,
    null,
    {},
    false,
    undefined,
    undefined,
    login
  )
  assertIsRepositoryWithGitHubRepository(repository)
  return repository
}

function gitError(
  error: DugiteError | null,
  description: string | null = 'visible message'
) {
  return new GitError(
    {
      stdout: '',
      stderr: 'not serialized raw stderr',
      path: 'C:/popup-fixture/repo',
      exitCode: 1,
      gitError: error,
      gitErrorDescription: description,
    },
    ['NOT-SERIALIZED-ARGS'],
    'visible terminal output'
  )
}

function popupFailure(reason: KernelPopupError['reason']) {
  return (error: unknown) => {
    assert.ok(error instanceof KernelPopupError)
    assert.equal(error.reason, reason)
    assert.doesNotMatch(error.message, /SENTINEL|secret-user/)
    return true
  }
}

describe('native kernel popup projection', () => {
  it('whitelists the verified fields without callbacks, tokens or stored secrets', () => {
    const repo = repository('signed-out')
    const popups: ReadonlyArray<Popup> = [
      {
        id: 1,
        type: PopupType.AddSSHHost,
        host: 'git.example.test',
        ip: '192.0.2.1',
        keyType: 'ED25519',
        fingerprint: 'SHA256:fixture',
        onSubmit: () => assert.fail('Projection must not settle a popup'),
      },
      {
        id: 2,
        type: PopupType.SSHKeyPassphrase,
        keyPath: 'fixture/key',
        onSubmit: () => assert.fail('Projection must not submit a secret'),
      },
      {
        id: 3,
        type: PopupType.SSHUserPassword,
        username: 'ssh-user',
        onSubmit: () => assert.fail('Projection must not submit a secret'),
      },
      {
        id: 4,
        type: PopupType.GenericGitAuthentication,
        remoteUrl: 'https://git.example.test/repo',
        onSubmit: () => assert.fail('Projection must not save credentials'),
        onDismiss: () => assert.fail('Projection must not dismiss'),
      },
      {
        id: 5,
        type: PopupType.HookFailed,
        hookName: 'pre-commit',
        terminalOutput: Buffer.from('失败\n'),
        resolve: () => assert.fail('Projection must not decide a hook result'),
      },
      {
        id: 6,
        type: PopupType.GenerateCommitMessageOverrideWarning,
        repository: repo,
        filesSelected: [],
      },
      {
        id: 7,
        type: PopupType.GenerateCommitMessageDisclaimer,
        repository: repo,
        filesSelected: [],
      },
      {
        id: 8,
        type: PopupType.RepositoryAccount,
        repository: repo,
        accounts: [other, bob],
        onComplete: () => assert.fail('Projection must not choose an account'),
      },
      {
        id: 9,
        type: PopupType.ConfirmForcePush,
        repository: repo,
        upstreamBranch: 'origin/main',
      },
      { id: 10, type: PopupType.PushNeedsPull, repository: repo },
      { id: 11, type: PopupType.Error, error: new Error('visible failure') },
      { id: 12, type: PopupType.SignIn },
    ]
    assert.deepEqual(
      popups.map(popup => popup.type),
      [...supportedKernelPopupTypes]
    )
    const dtos = popups.map(popup =>
      projectKernelPopup(
        Object.assign(popup, {
          secret: 'GENERIC-SECRET-SENTINEL',
          account: alice,
          extraCallback: () => assert.fail('Never reflect a callback'),
        }),
        presentation
      )
    )
    const serialized = JSON.stringify(dtos)
    assert.doesNotMatch(
      serialized,
      /SENTINEL|onSubmit|onDismiss|resolve|onComplete|extraCallback/
    )
    assert.deepEqual(JSON.parse(serialized), dtos)
    assert.deepEqual(dtos[0], {
      popupID: 1,
      type: PopupType.AddSSHHost,
      supported: true,
      host: 'git.example.test',
      ip: '192.0.2.1',
      keyType: 'ED25519',
      fingerprint: 'SHA256:fixture',
    })
    assert.deepEqual(dtos[3], {
      popupID: 4,
      type: PopupType.GenericGitAuthentication,
      supported: true,
      remoteUrl: 'https://git.example.test/repo',
      username: null,
    })
    assert.deepEqual(dtos[4], {
      popupID: 5,
      type: PopupType.HookFailed,
      supported: true,
      hookName: 'pre-commit',
      terminalOutput: '失败\n',
      choices: ['abort', 'ignore'],
    })
    assert.deepEqual(dtos[7], {
      popupID: 8,
      type: PopupType.RepositoryAccount,
      supported: true,
      repositoryName: 'repo',
      endpoint,
      assignedLogin: 'signed-out',
      requiresSignIn: true,
      accounts: [{ endpoint, login: 'bob' }],
    })
    assert.deepEqual(dtos[8], {
      popupID: 9,
      type: PopupType.ConfirmForcePush,
      supported: true,
      upstreamBranch: 'origin/main',
      askForConfirmationOnForcePush: true,
    })
    assert.deepEqual(dtos[6], {
      popupID: 7,
      type: PopupType.GenerateCommitMessageDisclaimer,
      supported: true,
      generationSource: 'copilot',
    })
    assert.deepEqual(dtos[9], {
      popupID: 10,
      type: PopupType.PushNeedsPull,
      supported: true,
    })
  })

  it('retains split UTF-8 hook output and does not silently handle unsupported popups', () => {
    const bytes = Buffer.from('检查失败\n')
    assert.deepEqual(
      projectKernelPopup(
        {
          id: 12,
          type: PopupType.HookFailed,
          hookName: 'pre-push',
          terminalOutput: [bytes.subarray(0, 1), bytes.subarray(1)],
          resolve: () => {},
        },
        presentation
      ),
      {
        popupID: 12,
        type: PopupType.HookFailed,
        supported: true,
        hookName: 'pre-push',
        terminalOutput: '检查失败\n',
        choices: ['abort', 'ignore'],
      }
    )
    assert.deepEqual(
      projectKernelPopup({ type: futurePopupType } as Popup, presentation),
      { popupID: null, type: futurePopupType, supported: false }
    )
    assert.equal(projectKernelPopup(null, presentation), null)
    assert.deepEqual(
      projectKernelPopup(
        {
          id: 13,
          type: PopupType.RepositoryAccount,
          repository: repository('ALICE'),
          onComplete: () => {},
        },
        presentation
      ),
      {
        popupID: 13,
        type: PopupType.RepositoryAccount,
        supported: true,
        repositoryName: 'repo',
        endpoint,
        assignedLogin: 'ALICE',
        requiresSignIn: false,
        accounts: [
          { endpoint, login: 'alice' },
          { endpoint, login: 'bob' },
        ],
      }
    )
  })

  it('projects Error presentation and original footer choices, not retry metadata or stacks', () => {
    const error = new ErrorWithMetadata(new Error('clone failure'), {
      retryAction: {
        type: RetryActionType.Clone,
        name: 'visible clone name',
        url: 'NOT-SERIALIZED-URL-SENTINEL',
        path: 'NOT-SERIALIZED-PATH-SENTINEL',
        options: {},
      },
    })
    error.stack = 'NOT-SERIALIZED-STACK-SENTINEL'
    const dto = projectKernelPopup(
      { id: 14, type: PopupType.Error, error },
      presentation
    )
    assert.deepEqual(dto, {
      popupID: 14,
      type: PopupType.Error,
      supported: true,
      title: 'Clone failed',
      content: { message: 'clone failure', terminal: false },
      causes: [],
      retryCloneName: 'visible clone name',
      choices: ['close', 'retry'],
    })
    assert.doesNotMatch(JSON.stringify(dto), /SENTINEL|retryAction|stack/)
    const auth = projectKernelPopup(
      {
        id: 15,
        type: PopupType.Error,
        error: gitError(DugiteError.SSHAuthenticationFailed),
      },
      presentation
    )
    assert.ok(auth?.supported && auth.type === PopupType.Error)
    assert.deepEqual(auth.choices, ['close', 'openPreferences'])
    const raw = projectKernelPopup(
      { id: 16, type: PopupType.Error, error: gitError(null, null) },
      presentation
    )
    assert.ok(raw?.supported && raw.type === PopupType.Error)
    assert.deepEqual(raw.content, {
      message: 'visible terminal output',
      terminal: true,
    })
    const copilot = new CopilotError('quota failure', 402, {
      paymentRequiredErrorCode: 'quota_exceeded',
      retryAfter: '2',
    })
    const nested = projectKernelPopup(
      {
        id: 17,
        type: PopupType.Error,
        error: new Error('outer failure', { cause: copilot }),
        showCauses: true,
      },
      presentation
    )
    assert.ok(nested?.supported && nested.type === PopupType.Error)
    assert.equal(nested.title, 'Quota exceeded')
    assert.deepEqual(nested.content, {
      message: 'quota failure',
      terminal: false,
      retryAfterMessage: 'You can try again in 2 seconds.',
    })
    assert.deepEqual(nested.causes, [
      {
        message: 'quota failure',
        terminal: false,
        retryAfterMessage: 'You can try again in 2 seconds.',
      },
    ])
  })
})

describe(
  'native popup replies through the real upstream runtime',
  // This budget covers all popup cases, including serial real-Git fetch,
  // force-push and clone. Use the runner's bounded integration budget rather
  // than cancelling the last case after earlier cases consumed 30 seconds.
  { timeout: 600000 },
  () => {
    let runtime: AppRuntime
    let trampoline: typeof import('../../src/lib/trampoline/trampoline-ui-helper').trampolineUIHelper

    before(async () => {
      localStorage.clear()
      localStorage.setItem('stats-opt-out', '1')
      localStorage.setItem('has-sent-stats-opt-in-ping', '1')
      const { getAppRuntime } = await import('../../src/ui/app-runtime')
      const { AppStore } = await import('../../src/lib/stores/app-store')
      const { Dispatcher } = await import('../../src/ui/dispatcher/dispatcher')
      runtime = getAppRuntime()
      assert.ok(runtime.appStore instanceof AppStore)
      assert.ok(runtime.dispatcher instanceof Dispatcher)
      assert.deepEqual(await runtime.accountsStore.getAll(), [])
      trampoline = (
        await import('../../src/lib/trampoline/trampoline-ui-helper')
      ).trampolineUIHelper
      trampoline.setDispatcher(runtime.dispatcher)
    })

    afterEach(() => {
      // Cleanup is test ownership, not an automatic native presentation policy.
      while (runtime.appStore.getState().currentPopup !== null) {
        const popup = runtime.appStore.getState().currentPopup!
        if (supportedKernelPopupTypes.some(type => type === popup.type)) {
          closeKernelPopup(runtime, popup.id)
        } else {
          runtime.dispatcher.closePopupById(popup.id!)
        }
      }
    })
    after(() => window.close())

    function current<T extends PopupType>(
      type: T
    ): Extract<Popup, { type: T }> & { id: number } {
      const popup = runtime.appStore.getState().currentPopup
      assert.ok(popup?.type === type && popup.id !== undefined)
      return popup as Extract<Popup, { type: T }> & { id: number }
    }

    async function show(popup: Popup) {
      await runtime.dispatcher.showPopup(popup)
      return current(popup.type).id
    }

    function invalid(value: unknown) {
      assert.throws(
        () => validateKernelPopupReply(runtime, value),
        popupFailure('invalid-reply')
      )
    }

    it('cancels the original SignInStore and resolves its caller before removing the native dialog', async () => {
      const results: Array<{ kind: string }> = []
      await runtime.dispatcher.showEnterpriseSignInDialog(undefined, result =>
        results.push(result)
      )
      const popup = current(PopupType.SignIn)
      assert.equal(runtime.signInStore.getState()?.kind, 'EndpointEntry')
      await runtime.dispatcher.setSignInEndpoint(
        'https://company.example.invalid'
      )
      assert.equal(runtime.signInStore.getState()?.kind, 'Authentication')
      closeKernelPopup(runtime, popup.id)
      assert.equal(runtime.signInStore.getState(), null)
      assert.deepEqual(results, [{ kind: 'cancelled' }])
      assert.equal(runtime.appStore.getState().currentPopup, null)
    })

    it('settles the real AddSSHHost Promise for Yes, No and window close', async () => {
      for (const addHost of [true, false]) {
        const promise = trampoline.promptAddingSSHHost(
          'git.example.test',
          '192.0.2.1',
          'ED25519',
          'SHA256:fixture'
        )
        const popup = current(PopupType.AddSSHHost)
        await replyToKernelPopup(runtime, {
          popupID: popup.id,
          type: popup.type,
          action: 'submit',
          addHost,
        })
        assert.equal(await promise, addHost)
        assert.equal(runtime.appStore.getState().currentPopup, null)
        await assert.rejects(
          replyToKernelPopup(runtime, {
            popupID: popup.id,
            type: popup.type,
            action: 'submit',
            addHost,
          }),
          popupFailure('unavailable')
        )
      }
      const cancelled = trampoline.promptAddingSSHHost(
        'git.example.test',
        '192.0.2.1',
        'RSA',
        'fixture'
      )
      closeKernelPopup(runtime, current(PopupType.AddSSHHost).id)
      assert.equal(await cancelled, false)
    })

    it('settles real SSH secret Promises without inventing a remembered secret on cancel', async () => {
      for (const remember of [true, false]) {
        const key = trampoline.promptSSHKeyPassphrase('fixture/key')
        const keyPopup = current(PopupType.SSHKeyPassphrase)
        await replyToKernelPopup(runtime, {
          popupID: keyPopup.id,
          type: keyPopup.type,
          action: 'submit',
          passphrase: '  PASSPHRASE-SENTINEL  ',
          storePassphrase: remember,
        })
        assert.deepEqual(await key, {
          secret: '  PASSPHRASE-SENTINEL  ',
          storeSecret: remember,
        })
        const user = trampoline.promptSSHUserPassword('ssh-user')
        const userPopup = current(PopupType.SSHUserPassword)
        await replyToKernelPopup(runtime, {
          popupID: userPopup.id,
          type: userPopup.type,
          action: 'submit',
          password: 'PASSWORD-SENTINEL',
          storePassword: remember,
        })
        assert.deepEqual(await user, {
          secret: 'PASSWORD-SENTINEL',
          storeSecret: remember,
        })
      }
      const keyCancelled = trampoline.promptSSHKeyPassphrase('fixture/key')
      closeKernelPopup(runtime, current(PopupType.SSHKeyPassphrase).id)
      assert.deepEqual(await keyCancelled, {
        secret: undefined,
        storeSecret: false,
      })
      const userCancelled = trampoline.promptSSHUserPassword('ssh-user')
      const popup = current(PopupType.SSHUserPassword)
      await replyToKernelPopup(runtime, {
        popupID: popup.id,
        type: popup.type,
        action: 'cancel',
      })
      assert.deepEqual(await userCancelled, {
        secret: undefined,
        storeSecret: false,
      })
    })

    it('settles the real generic authentication Promise and respects predetermined usernames', async () => {
      for (const username of [undefined, 'fixed-user']) {
        const promise = trampoline.promptForGenericGitAuthentication(
          endpoint,
          username
        )
        const popup = current(PopupType.GenericGitAuthentication)
        await replyToKernelPopup(runtime, {
          popupID: popup.id,
          type: popup.type,
          action: 'submit',
          username: username ?? 'editable-user',
          password: 'GENERIC-PASSWORD-SENTINEL',
        })
        assert.deepEqual(await promise, {
          endpoint,
          login: username ?? 'editable-user',
          token: 'GENERIC-PASSWORD-SENTINEL',
        })
        assert.equal(runtime.appStore.getState().currentPopup, null)
      }
      const cancelled = trampoline.promptForGenericGitAuthentication(endpoint)
      closeKernelPopup(runtime, current(PopupType.GenericGitAuthentication).id)
      assert.equal(await cancelled, undefined)
    })

    it('calls explicit callbacks before closing, including generic onDismiss after saving', async () => {
      const events: unknown[] = []
      const sshID = await show({
        type: PopupType.SSHKeyPassphrase,
        keyPath: 'fixture/key',
        onSubmit: (secret, remember) =>
          events.push([
            secret,
            remember,
            current(PopupType.SSHKeyPassphrase).id,
          ]),
      })
      closeKernelPopup(runtime, sshID)
      assert.deepEqual(events, [[undefined, false, sshID]])
      assert.equal(runtime.appStore.getState().currentPopup, null)
      const genericEvents: unknown[] = []
      const genericID = await show({
        type: PopupType.GenericGitAuthentication,
        remoteUrl: endpoint,
        onSubmit: (username, password) =>
          genericEvents.push([
            'submit',
            username,
            password,
            current(PopupType.GenericGitAuthentication).id,
          ]),
        onDismiss: () =>
          genericEvents.push([
            'dismiss',
            current(PopupType.GenericGitAuthentication).id,
          ]),
      })
      await replyToKernelPopup(runtime, {
        popupID: genericID,
        type: PopupType.GenericGitAuthentication,
        action: 'submit',
        username: 'user',
        password: 'password',
      })
      assert.deepEqual(genericEvents, [
        ['submit', 'user', 'password', genericID],
        ['dismiss', genericID],
      ])
      assert.equal(runtime.appStore.getState().currentPopup, null)
    })

    it('preserves the original hook failure queue, ignore choice and per-id dismissal settlement', async () => {
      const aborts: string[] = []
      // These are the actual ordinary-hook promises used by AppStore, with no Git
      // hook process or paid account needed to exercise ownership settlement.
      const first = runtime.appStore['onHookFailure'](() =>
        aborts.push('first')
      )('first-hook', 'first output')
      const second = runtime.appStore['onHookFailure'](() =>
        aborts.push('second')
      )('second-hook', 'second output')
      const third = runtime.appStore['onHookFailure'](() =>
        aborts.push('third')
      )('third-hook', 'third output')
      const firstPopup = current(PopupType.HookFailed)
      invalid({
        popupID: firstPopup.id,
        type: firstPopup.type,
        action: 'submit',
        result: 'retry',
      })
      await replyToKernelPopup(runtime, {
        popupID: firstPopup.id,
        type: firstPopup.type,
        action: 'submit',
        result: 'ignore',
      })
      assert.equal(await first, 'ignore')
      assert.deepEqual(aborts, [])
      const secondPopup = current(PopupType.HookFailed)
      assert.equal(secondPopup.hookName, 'second-hook')
      assert.notEqual(secondPopup.id, firstPopup.id)
      assert.throws(
        () => closeKernelPopup(runtime, firstPopup.id),
        popupFailure('unavailable')
      )
      assert.equal(current(PopupType.HookFailed), secondPopup)
      closeKernelPopup(runtime, secondPopup.id)
      assert.equal(await second, 'abort')
      assert.deepEqual(aborts, ['second'])
      const thirdPopup = current(PopupType.HookFailed)
      assert.equal(thirdPopup.hookName, 'third-hook')
      await replyToKernelPopup(runtime, {
        popupID: thirdPopup.id,
        type: thirdPopup.type,
        action: 'submit',
        result: 'abort',
      })
      assert.equal(await third, 'abort')
      assert.deepEqual(aborts, ['second', 'third'])
      assert.equal(runtime.appStore.getState().currentPopup, null)
    })

    it('keeps original assigned and alternate sign-in continuations bound to their popup', async () => {
      const choosing = runtime.appStore._chooseRepositoryAccount(
        repository('alice'),
        [bob]
      )
      await Promise.resolve()
      const popup = current(PopupType.RepositoryAccount)
      await replyToKernelPopup(runtime, {
        popupID: popup.id,
        type: popup.type,
        action: 'sign-in',
      })
      current(PopupType.SignIn)
      const signIn = runtime.signInStore.getState()
      assert.ok(
        signIn?.kind === SignInStep.Authentication,
        'original authentication step was not opened'
      )
      assert.equal(signIn.endpoint, endpoint)
      assert.equal(signIn.expectedLogin, 'alice')
      signIn.resultCallback({ kind: 'success', account: alice })
      assert.equal(await choosing, alice)
      assert.equal(runtime.appStore.getState().currentPopup, null)
      // A late result cannot revive a dismissed owner or close a newer popup.
      signIn.resultCallback({ kind: 'success', account: alice })
      assert.equal(runtime.appStore.getState().currentPopup, null)

      const alternate = runtime.appStore._chooseRepositoryAccount(
        repository('signed-out'),
        []
      )
      await Promise.resolve()
      const otherPopup = current(PopupType.RepositoryAccount)
      await replyToKernelPopup(runtime, {
        popupID: otherPopup.id,
        type: otherPopup.type,
        action: 'choose-another',
      })
      const choice = projectKernelPopup(otherPopup, runtime.appStore.getState())
      assert.ok(
        choice?.type === PopupType.RepositoryAccount &&
          'requiresSignIn' in choice &&
          !choice.requiresSignIn
      )
      await replyToKernelPopup(runtime, {
        popupID: otherPopup.id,
        type: otherPopup.type,
        action: 'sign-in',
      })
      const otherSignIn = runtime.signInStore.getState()
      assert.ok(
        otherSignIn?.kind === SignInStep.Authentication,
        'alternate sign-in step missing'
      )
      assert.equal(otherSignIn.expectedLogin, undefined)
      otherSignIn.resultCallback({ kind: 'success', account: bob })
      const view = projectKernelPopup(otherPopup, runtime.appStore.getState())
      assert.ok(
        view?.type === PopupType.RepositoryAccount && 'selectedAccount' in view
      )
      assert.deepEqual(view.selectedAccount, { endpoint, login: 'bob' })
      await replyToKernelPopup(runtime, {
        popupID: otherPopup.id,
        type: otherPopup.type,
        action: 'submit',
        account: { endpoint, login: 'bob' },
      })
      assert.equal(await alternate, bob)
    })

    it('resolves RepositoryAccount with the original allowed Account, or undefined on close', async () => {
      const repo = repository()
      const choosing = runtime.appStore._chooseRepositoryAccount(repo, [
        bob,
        other,
      ])
      // AppStore serializes these prompts through its original Promise queue.
      await Promise.resolve()
      const popup = current(PopupType.RepositoryAccount)
      invalid({
        popupID: popup.id,
        type: popup.type,
        action: 'submit',
        account: { endpoint, login: 'alice' },
      })
      invalid({
        popupID: popup.id,
        type: popup.type,
        action: 'submit',
        account: { endpoint: other.endpoint, login: other.login },
      })
      invalid({
        popupID: popup.id,
        type: popup.type,
        action: 'submit',
        account: { endpoint, login: 'bob', token: 'WIRE-TOKEN-SENTINEL' },
      })
      await replyToKernelPopup(runtime, {
        popupID: popup.id,
        type: popup.type,
        action: 'submit',
        account: { endpoint, login: 'BOB' },
      })
      assert.equal(await choosing, bob)
      assert.equal(
        repo.login,
        null,
        'The presenter must not persist account assignments itself'
      )
      const cancelled = runtime.appStore._chooseRepositoryAccount(repo, [
        alice,
        bob,
      ])
      await Promise.resolve()
      closeKernelPopup(runtime, current(PopupType.RepositoryAccount).id)
      assert.equal(await cancelled, undefined)
    })

    it('preserves generation consent/settings and the draft using the real no-account path', async () => {
      const repo = repository()
      const draft = {
        summary: 'keep my draft',
        description: 'keep my body',
        timestamp: 1,
      }
      await runtime.dispatcher.setCommitMessage(repo, draft)
      await runtime.dispatcher.setConfirmCommitMessageOverrideSetting(true)
      await runtime.dispatcher.promptOverrideWithGeneratedCommitMessage(
        repo,
        []
      )
      const warning = current(PopupType.GenerateCommitMessageOverrideWarning)
      invalid({
        popupID: warning.id,
        type: warning.type,
        action: 'submit',
        confirmCommitMessageOverride: 'false',
      })
      closeKernelPopup(runtime, warning.id)
      assert.equal(
        runtime.appStore.getState().askForConfirmationOnCommitMessageOverride,
        true
      )
      assert.equal(
        runtime.appStore.getState().commitMessageGenerationDisclaimerLastSeen,
        null
      )
      await runtime.dispatcher.promptOverrideWithGeneratedCommitMessage(
        repo,
        []
      )
      const accepted = current(PopupType.GenerateCommitMessageOverrideWarning)
      await replyToKernelPopup(runtime, {
        popupID: accepted.id,
        type: accepted.type,
        action: 'submit',
        confirmCommitMessageOverride: false,
      })
      assert.equal(
        runtime.appStore.getState().askForConfirmationOnCommitMessageOverride,
        false
      )
      const disclaimerID = await show({
        type: PopupType.GenerateCommitMessageDisclaimer,
        repository: repo,
        filesSelected: [],
      })
      closeKernelPopup(runtime, disclaimerID)
      assert.equal(
        runtime.appStore.getState().commitMessageGenerationDisclaimerLastSeen,
        null
      )
      const acceptID = await show({
        type: PopupType.GenerateCommitMessageDisclaimer,
        repository: repo,
        filesSelected: [],
      })
      const before = Date.now()
      await replyToKernelPopup(runtime, {
        popupID: acceptID,
        type: PopupType.GenerateCommitMessageDisclaimer,
        action: 'submit',
      })
      const seen =
        runtime.appStore.getState().commitMessageGenerationDisclaimerLastSeen
      assert.ok(seen !== null && seen >= before && seen <= Date.now())
      assert.deepEqual(
        runtime.repositoryStateManager.get(repo).changesState.commitMessage,
        draft
      )
      assert.equal(runtime.appStore.getState().currentPopup, null)
      // This verifies the real dispatch/no-account continuation, not a paid
      // Copilot request or generated content, and never commits/pushes the draft.
    })

    it('rejects malformed, extra, mismatched, stale, covered and unsupported replies without settlement', async () => {
      let settled = 0
      const id = await show({
        type: PopupType.SSHKeyPassphrase,
        keyPath: 'fixture/key',
        onSubmit: () => settled++,
      })
      const valid = {
        popupID: id,
        type: PopupType.SSHKeyPassphrase,
        action: 'submit',
        passphrase: 'SECRET-SENTINEL',
        storePassphrase: false,
      }
      assert.deepEqual(validateKernelPopupReply(runtime, valid), valid)
      for (const value of [
        null,
        [],
        'SECRET-SENTINEL',
        { ...valid, popupID: 0 },
        { ...valid, popupID: Number.MAX_SAFE_INTEGER + 1 },
        { ...valid, type: PopupType.SSHUserPassword },
        { ...valid, passphrase: '' },
        { ...valid, passphrase: 'SECRET-SENTINEL\0' },
        { ...valid, passphrase: 'SECRET-SENTINEL\n' },
        { ...valid, passphrase: undefined },
        { ...valid, storePassphrase: 'false' },
        { ...valid, secret: 'SECRET-SENTINEL' },
        { ...valid, callback: 'onSubmit' },
        { ...valid, action: 'onSubmit' },
        {
          popupID: id,
          type: PopupType.SSHKeyPassphrase,
          action: 'cancel',
          storePassphrase: true,
        },
      ]) {
        invalid(value)
        await assert.rejects(
          replyToKernelPopup(runtime, value),
          popupFailure('invalid-reply')
        )
      }
      assert.equal(settled, 0)
      const errorID = await show({
        type: PopupType.Error,
        error: new Error('covering error'),
      })
      assert.throws(
        () => validateKernelPopupReply(runtime, valid),
        popupFailure('unavailable')
      )
      assert.throws(
        () => closeKernelPopup(runtime, id),
        popupFailure('unavailable')
      )
      assert.equal(settled, 0)
      closeKernelPopup(runtime, errorID)
      closeKernelPopup(runtime, id)
      assert.equal(settled, 1)
      const unsupportedID = await show({ type: futurePopupType } as Popup)
      assert.throws(
        () => closeKernelPopup(runtime, unsupportedID),
        popupFailure('unsupported')
      )
      await assert.rejects(
        replyToKernelPopup(runtime, {
          popupID: unsupportedID,
          type: futurePopupType,
          action: 'cancel',
        }),
        popupFailure('unsupported')
      )
      assert.equal(current(futurePopupType).id, unsupportedID)
    })

    it('keeps generic fixed-user and password validation at the reply boundary', async () => {
      const promise = trampoline.promptForGenericGitAuthentication(
        endpoint,
        'fixed-user'
      )
      const popup = current(PopupType.GenericGitAuthentication)
      const valid = {
        popupID: popup.id,
        type: popup.type,
        action: 'submit',
        username: 'fixed-user',
        password: 'PASSWORD-SENTINEL',
      }
      for (const value of [
        { ...valid, username: 'secret-user' },
        { ...valid, username: '' },
        { ...valid, password: '' },
        { ...valid, password: 'PASSWORD-SENTINEL\r' },
        { ...valid, secret: 'PASSWORD-SENTINEL' },
        { ...valid, storePassword: true },
      ]) {
        invalid(value)
      }
      closeKernelPopup(runtime, popup.id)
      assert.equal(await promise, undefined)
    })

    it('opens only the current original billing recovery link without dismissing the error', async t => {
      const id = await show({
        type: PopupType.Error,
        error: new CopilotError('billing setup required', 402, {
          paymentRequiredErrorCode: 'billing_not_configured',
        }),
      })
      const opened: string[] = []
      t.mock.method(shell, 'openExternal', async (url: string) => {
        opened.push(url)
      })
      invalid({
        popupID: id,
        type: PopupType.Error,
        action: 'openAction',
        contentIndex: -1,
      })
      invalid({
        popupID: id,
        type: PopupType.Error,
        action: 'openAction',
        contentIndex: 1,
      })
      invalid({
        popupID: id,
        type: PopupType.Error,
        action: 'openAction',
        contentIndex: 0,
        url: 'https://example.invalid/',
      })
      await replyToKernelPopup(runtime, {
        popupID: id,
        type: PopupType.Error,
        action: 'openAction',
        contentIndex: 0,
      })
      assert.deepEqual(opened, ['https://github.com/settings/copilot'])
      assert.equal(runtime.appStore.getState().currentPopup?.id, id)
      await closeKernelPopup(runtime, id)
      assert.equal(runtime.appStore.getState().currentPopup, null)
    })

    it('uses only original Error actions and opens the original Preferences popup after closing', async () => {
      const id = await show({
        type: PopupType.Error,
        error: new Error('failure'),
      })
      invalid({ popupID: id, type: PopupType.Error, action: 'retry' })
      invalid({ popupID: id, type: PopupType.Error, action: 'openPreferences' })
      await replyToKernelPopup(runtime, {
        popupID: id,
        type: PopupType.Error,
        action: 'close',
      })
      assert.equal(runtime.appStore.getState().currentPopup, null)
      const authID = await show({
        type: PopupType.Error,
        error: gitError(DugiteError.HTTPSAuthenticationFailed),
      })
      invalid({
        popupID: authID,
        type: PopupType.Error,
        action: 'openPreferences',
        popup: { type: PopupType.SignIn },
      })
      await replyToKernelPopup(runtime, {
        popupID: authID,
        type: PopupType.Error,
        action: 'openPreferences',
      })
      assert.equal(current(PopupType.Preferences).type, PopupType.Preferences)
      assert.equal(
        runtime.appStore
          .getState()
          .allPopups.some(popup => popup.id === authID),
        false
      )
    })

    it('fetches, force-pushes with lease and retries clone through original Dispatcher using local Git only', async t => {
      const root = resolve('_temp/kernel-reuse')
      await mkdir(root, { recursive: true })
      const directory = await mkdtemp(join(root, 'popup-git-'))
      t.after(async () => {
        // Clone legitimately launches a fire-and-forget selection refresh.
        // Stop periodic work and let the original ownership primitive drain
        // native readers before deleting their working directories. Keep the
        // isolated IndexedDB rows until window.close so queued metrics still
        // have their original repository identities.
        await runtime.appStore._selectRepository(null)
        const repositories = runtime.appStore
          .getState()
          .repositories.filter(
            (repository): repository is Repository =>
              repository instanceof Repository &&
              repository.path.startsWith(directory)
          )
        const leases = await Promise.all(
          repositories.map(repository =>
            acquireAssistedCommitGitLease(repository.path)
          )
        )
        try {
          await rm(directory, {
            recursive: true,
            force: true,
            maxRetries: 5,
            retryDelay: 50,
          })
        } finally {
          leases.forEach(lease => lease.release())
        }
      })
      const work = join(directory, 'work')
      const remote = join(directory, 'remote.git')
      const hooks = join(directory, 'empty-hooks')
      await Promise.all([work, remote, hooks].map(path => mkdir(path)))
      async function git(path: string, args: string[]) {
        const result = await exec(args, path)
        assert.equal(result.exitCode, 0, result.stderr)
        return result.stdout.trim()
      }
      await git(remote, [
        'init',
        '--bare',
        '--template=',
        '--initial-branch=main',
      ])
      await git(work, ['init', '--template=', '--initial-branch=main'])
      for (const [key, value] of [
        ['user.name', 'Popup Test'],
        ['user.email', 'popup@example.test'],
        ['commit.gpgSign', 'false'],
        ['core.hooksPath', hooks],
      ]) {
        await git(work, ['config', key, value])
      }
      await writeFile(join(work, 'file.txt'), 'first\n')
      await git(work, ['add', 'file.txt'])
      await git(work, ['commit', '-m', 'first'])
      const first = await git(work, ['rev-parse', 'HEAD'])
      await git(work, ['remote', 'add', 'origin', remote])
      await git(work, ['push', '--set-upstream', 'origin', 'main'])
      await runtime.dispatcher.addRepositories([work])
      const repo = runtime.appStore
        .getState()
        .repositories.find(
          (candidate): candidate is Repository =>
            candidate instanceof Repository && candidate.path === work
        )
      assert.ok(repo)
      await runtime.dispatcher.refreshRepository(repo)
      await writeFile(join(work, 'file.txt'), 'second\n')
      await git(work, ['commit', '-am', 'second'])
      const second = await git(work, ['rev-parse', 'HEAD'])
      await git(work, ['push'])
      await git(work, ['reset', '--hard', first])
      await git(work, ['update-ref', 'refs/remotes/origin/main', first])
      await runtime.dispatcher.refreshRepository(repo)
      const fetchID = await show({
        type: PopupType.PushNeedsPull,
        repository: repo,
      })
      invalid({
        popupID: fetchID,
        type: PopupType.PushNeedsPull,
        action: 'submit',
        pull: true,
      })
      const fetching = replyToKernelPopup(runtime, {
        popupID: fetchID,
        type: PopupType.PushNeedsPull,
        action: 'submit',
      })
      assert.equal(
        current(PopupType.PushNeedsPull).id,
        fetchID,
        'Fetch closes only after awaiting Dispatcher'
      )
      await fetching
      assert.equal(
        await git(work, ['rev-parse', 'refs/remotes/origin/main']),
        second
      )
      assert.equal(
        await git(work, ['rev-parse', 'HEAD']),
        first,
        'The Fetch choice must not silently pull/merge'
      )
      assert.equal(runtime.appStore.getState().currentPopup, null)
      const forceID = await show({
        type: PopupType.ConfirmForcePush,
        repository: repo,
        upstreamBranch: 'origin/main',
      })
      invalid({
        popupID: forceID,
        type: PopupType.ConfirmForcePush,
        action: 'submit',
        askForConfirmationOnForcePush: 'false',
      })
      const pushing = replyToKernelPopup(runtime, {
        popupID: forceID,
        type: PopupType.ConfirmForcePush,
        action: 'submit',
        askForConfirmationOnForcePush: false,
      })
      assert.equal(
        runtime.appStore.getState().currentPopup,
        null,
        'Force push closes before invoking the operation'
      )
      assert.equal(
        runtime.appStore.getState().askForConfirmationOnForcePush,
        false
      )
      await pushing
      assert.equal(await git(remote, ['rev-parse', 'refs/heads/main']), first)
      const clonePath = join(directory, 'clone')
      const retryID = await show({
        type: PopupType.Error,
        error: new ErrorWithMetadata(new Error('clone failed'), {
          retryAction: {
            type: RetryActionType.Clone,
            name: 'fixture',
            url: remote,
            path: clonePath,
            options: {},
          },
        }),
      })
      invalid({
        popupID: retryID,
        type: PopupType.Error,
        action: 'retry',
        url: 'WIRE-URL-SENTINEL',
      })
      await replyToKernelPopup(runtime, {
        popupID: retryID,
        type: PopupType.Error,
        action: 'retry',
      })
      assert.equal(await git(clonePath, ['rev-parse', 'HEAD']), first)
      assert.equal(
        runtime.appStore
          .getState()
          .allPopups.some(popup => popup.id === retryID),
        false
      )
    })
  }
)

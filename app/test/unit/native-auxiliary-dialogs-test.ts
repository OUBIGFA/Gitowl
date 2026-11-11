import { listenOnLoopback } from '../helpers/http-server'
import assert from 'node:assert/strict'
import { after, afterEach, before, describe, it, mock } from 'node:test'
import type { TestContext } from 'node:test'
import { EventEmitter } from 'events'
import { createServer } from 'http'
import { AddressInfo } from 'net'
import { mkdir, mkdtemp, writeFile, readFile } from 'fs/promises'
import { join, resolve } from 'path'
import * as React from 'react'
import { Account } from '../../src/models/account'
import { Popup, PopupType } from '../../src/models/popup'
import { TutorialStep } from '../../src/models/tutorial-step'
import {
  Repository,
  RepositoryWithGitHubRepository,
  assertIsRepositoryWithGitHubRepository,
} from '../../src/models/repository'
import { GitHubRepository } from '../../src/models/github-repository'
import { Owner } from '../../src/models/owner'
import { PullRequest, PullRequestRef } from '../../src/models/pull-request'
import { Branch, BranchType } from '../../src/models/branch'
import { ReleaseSummary } from '../../src/models/release-notes'
import { BypassReason } from '../../src/ui/secret-scanning/bypass-reason'
import { ISecretScanResult } from '../../src/ui/secret-scanning/push-protection-error-dialog'
import { APICheckStatus, APICheckConclusion } from '../../src/lib/api'
import { IRefCheck } from '../../src/lib/ci-checks/ci-checks'
import { IAppState, SelectionType } from '../../src/lib/app-state'
import { ImageDiffType, DiffType, Image } from '../../src/models/diff'
import { ComputedAction } from '../../src/models/computed-action'
import { CommittedFileChange, AppFileStatusKind } from '../../src/models/status'
import { TerminalOutput } from '../../src/lib/git/core'
import { AppRuntime } from '../../src/ui/app-runtime'
import { exec } from 'dugite'
import { Disposable } from 'event-kit'
import { render, fireEvent, waitFor } from '../helpers/ui/render'
import labels from '../../../references/native-ui-auxiliary-labels.json'

// Only OS boundaries are substitutes: no mocked Dispatcher, Store, API, Git or popup callbacks.
const passwords = new Map<string, string>()
mock.module('keytar', {
  namedExports: {
    getPassword: async (key: string, login: string) =>
      passwords.get(`${key}/${login}`) ?? null,
    setPassword: async (key: string, login: string, token: string) => {
      passwords.set(`${key}/${login}`, token)
    },
    deletePassword: async (key: string, login: string) =>
      passwords.delete(`${key}/${login}`),
  },
})
mock.module('../../src/lib/editors/win32', {
  namedExports: {
    getAvailableEditors: async () => [
      { editor: 'Fixture Editor', path: 'fixture-editor.exe' },
    ],
  },
})
mock.module('../../src/lib/editors/linux', {
  namedExports: {
    getAvailableEditors: async () => [
      { editor: 'Fixture Editor', path: 'fixture-editor' },
    ],
  },
})
mock.module('../../src/lib/editors/darwin', {
  namedExports: {
    getAvailableEditors: async () => [
      {
        editor: 'Fixture Editor',
        path: 'fixture-editor',
        bundleID: 'test.fixture.editor',
      },
    ],
  },
})

const endpoint = 'https://fixture.example.invalid/api/v3'
const account = new Account(
  'fixture-user',
  endpoint,
  'TOKEN-SENTINEL',
  [],
  '',
  1,
  'Fixture'
)
function repo(
  apiEndpoint = endpoint,
  login: string | null = null
): RepositoryWithGitHubRepository {
  const repository = new Repository(
    'fixture-repository',
    701,
    new GitHubRepository(
      'repo',
      new Owner('owner', apiEndpoint, 1),
      701,
      false,
      apiEndpoint.replace('/api/v3', '') + '/owner/repo'
    ),
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
const repository = repo()
const branch = new Branch(
  'feature',
  'origin/feature',
  { sha: 'a'.repeat(40) },
  BranchType.Local,
  'refs/heads/feature'
)
const base = new Branch(
  'origin/main',
  null,
  { sha: 'b'.repeat(40) },
  BranchType.Remote,
  'refs/remotes/origin/main'
)
function pullRequest(repository: RepositoryWithGitHubRepository) {
  return new PullRequest(
    new Date('2026-01-01'),
    'Fixture pull request',
    7,
    new PullRequestRef('feature', 'a'.repeat(40), repository.gitHubRepository),
    new PullRequestRef('main', 'b'.repeat(40), repository.gitHubRepository),
    'fixture-user',
    false,
    'fixture body'
  )
}
const pr = pullRequest(repository)
const identity = {
  id: 1,
  login: 'fixture-user',
  avatar_url: '',
  html_url: 'https://fixture.example.invalid/fixture-user',
  type: 'User' as const,
}
const comment = {
  id: 7,
  body: 'Comment body',
  user: identity,
  html_url: 'https://fixture.example.invalid/comment/7',
  created_at: '2026-01-01',
  updated_at: '2026-01-01',
}
const review = {
  ...comment,
  state: 'APPROVED' as const,
  submitted_at: '2026-01-01',
}
const secret: ISecretScanResult = {
  id: 'fixture-secret-id',
  description: 'GitHub token',
  requiresApproval: false,
  bypassURL: 'https://fixture.example.invalid/bypass/id',
  locations: [
    { path: 'config.txt', commitSha: 'a'.repeat(40), lineNumber: 4 },
    { path: 'test.txt', commitSha: 'b'.repeat(40), lineNumber: 9 },
  ],
}
const release: ReleaseSummary = {
  latestVersion: __APP_VERSION__,
  datePublished: '2026-01-01',
  enhancements: [{ kind: 'improved', message: 'Enhancement' }],
  bugfixes: [{ kind: 'fixed', message: 'Fix' }],
  pretext: [{ kind: 'pretext', message: 'Release summary' }],
  other: [{ kind: 'other', message: 'Other' }],
  thankYous: [],
}
function check(id: number, suite = id): IRefCheck {
  return {
    id,
    checkSuiteId: suite,
    name: `Check ${id}`,
    description: 'Failed',
    status: APICheckStatus.Completed,
    conclusion: APICheckConclusion.Failure,
    appName: 'GitHub Actions',
    htmlUrl: null,
  }
}
let auxiliary: typeof import('../../src/native-kernel/auxiliary-dialogs')
let runtime: AppRuntime
const mounted: Disposable[] = []
const ipc = new EventEmitter()
const calls: Array<[string, unknown[]]> = []
let licenseRoot: string
const field = (popup: Popup, id: string, state = runtime.appStore.getState()) =>
  auxiliary.auxiliaryDialogs
    .project(popup, state)
    .fields.find(field => field.id === id)
const actions = (popup: Popup, state = runtime.appStore.getState()) =>
  auxiliary.auxiliaryDialogs
    .project(popup, state)
    .actions.map(action => action.id)
function mount(popup: Popup, changed = () => {}) {
  const handle = auxiliary.mountAuxiliaryDialog(runtime, popup, changed)
  mounted.push(handle)
  return handle
}
async function show(popup: Popup) {
  await runtime.dispatcher.showPopup(popup)
  const current = runtime.appStore.getState().currentPopup!
  assert.equal(current.type, popup.type)
  assert.ok(current.id !== undefined)
  return current
}

before(async () => {
  const { ipcRenderer } = await import('electron')
  Object.assign(ipcRenderer, {
    send: (channel: string, ...args: unknown[]) => {
      calls.push([channel, args])
    },
    sendSync: (channel: string, ...args: unknown[]) => {
      calls.push([channel, args])
    },
    invoke: async (channel: string, ...args: unknown[]) => {
      calls.push([channel, args])
      if (channel === 'get-app-path') {
        return licenseRoot
      }
      return undefined
    },
    on: ipc.on.bind(ipc),
    once: ipc.once.bind(ipc),
    removeListener: ipc.removeListener.bind(ipc),
  })
  const root = resolve('_temp/native-full-ui')
  await mkdir(root, { recursive: true })
  licenseRoot = await mkdtemp(join(root, 'licenses-'))
  await mkdir(join(licenseRoot, 'static'))
  await writeFile(
    join(licenseRoot, 'static', 'licenses.json'),
    JSON.stringify({
      [`desktop@${__APP_VERSION__}`]: {
        sourceText: 'MIT fixture desktop license',
      },
      'fixture-library@1': {
        sourceText: 'Fixture license text',
        repository: 'git+https://github.com/example/fixture',
      },
    })
  )
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  runtime = (await import('../../src/ui/app-runtime')).getAppRuntime()
  auxiliary = await import('../../src/native-kernel/auxiliary-dialogs')
  assert.deepEqual(await runtime.accountsStore.getAll(), [])
})
afterEach(() => {
  mounted.splice(0).forEach(handle => handle.dispose())
  while (runtime.appStore.getState().currentPopup) {
    const popup = runtime.appStore.getState().currentPopup!
    runtime.dispatcher.closePopupById(popup.id!)
  }
  calls.length = 0
})
after(async () => {
  await runtime.appStore._selectRepository(null)
  // Original account/repository metrics may still read Git asynchronously after
  // selection ends. Keep this ignored per-run fixture until the worker exits;
  // deleting a live repository would manufacture ENOENT unrelated to CI UI.
  window.close()
})

function fixtures(): Popup[] {
  return [
    {
      type: PopupType.ConfirmAccountSignOut,
      account,
      repositoryCount: 2,
      onComplete: () => assert.fail('project must not complete'),
    },
    { type: PopupType.InstallGit, path: 'fixture-repository' },
    { type: PopupType.About },
    { type: PopupType.Acknowledgements },
    {
      type: PopupType.UntrustedCertificate,
      url: 'https://fixture.example.invalid/path',
      certificate: {
        subjectName: 'Fixture certificate',
      } as Electron.Certificate,
    },
    { type: PopupType.TermsAndConditions },
    { type: PopupType.CLIInstalled },
    {
      type: PopupType.ExternalEditorFailed,
      message: 'Original visible diagnostic',
      openPreferences: true,
      suggestDefaultEditor: true,
    },
    { type: PopupType.CopilotAppNotFound },
    { type: PopupType.OpenWithExternalEditor },
    { type: PopupType.OpenShellFailed, message: 'Original shell diagnostic' },
    { type: PopupType.InitializeLFS, repositories: [repository] },
    { type: PopupType.LFSAttributeMismatch },
    { type: PopupType.ReleaseNotes, newReleases: [release] },
    { type: PopupType.DeletePullRequest, repository, branch, pullRequest: pr },
    {
      type: PopupType.PushRejectedDueToMissingWorkflowScope,
      repository,
      rejectedPath: '.github/workflows/ci.yml',
    },
    {
      type: PopupType.SAMLReauthRequired,
      endpoint,
      organizationName: 'Fixture org',
    },
    { type: PopupType.MoveToApplicationsFolder },
    {
      type: PopupType.ThankYou,
      friendlyName: 'Fixture',
      latestVersion: '1.0.0',
      userContributions: release.enhancements,
    },
    { type: PopupType.InvalidatedToken, account },
    {
      type: PopupType.PullRequestChecksFailed,
      repository,
      pullRequest: pr,
      checks: [check(1)],
      shouldChangeRepository: true,
    },
    {
      type: PopupType.CICheckRunRerun,
      repository: repository.gitHubRepository,
      localRepository: repository,
      checkRuns: [check(1)],
      prRef: 'refs/pull/7/head',
      failedOnly: true,
    },
    {
      type: PopupType.PullRequestReview,
      repository,
      pullRequest: pr,
      review,
      shouldCheckoutBranch: true,
      shouldChangeRepository: true,
    },
    { type: PopupType.UnreachableCommits, selectedTab: 0 },
    {
      type: PopupType.StartPullRequest,
      repository,
      prBaseBranches: [base],
      currentBranch: branch,
      defaultBranch: base,
      prRecentBaseBranches: [base],
      imageDiffType: ImageDiffType.TwoUp,
      nonLocalCommitSHA: null,
      showSideBySideDiff: false,
      currentBranchHasPullRequest: false,
    },
    { type: PopupType.InstallingUpdate },
    {
      type: PopupType.PullRequestComment,
      repository,
      pullRequest: pr,
      comment,
      shouldCheckoutBranch: true,
      shouldChangeRepository: false,
    },
    { type: PopupType.PushProtectionError, secrets: [secret] },
    {
      type: PopupType.BypassPushProtection,
      secret,
      bypassPushProtection: () => assert.fail('projection must not bypass'),
      onDismissed: () => assert.fail('projection must not dismiss'),
    },
    {
      type: PopupType.CommitProgress,
      subscribeToCommitOutput: () =>
        assert.fail('projection must not subscribe'),
    },
    {
      type: PopupType.PushBranchCommits,
      repository,
      branch,
      unPushedCommits: 2,
    },
    { type: PopupType.CreateTutorialRepository, account },
    { type: PopupType.ConfirmExitTutorial },
  ]
}

describe('auxiliary dialog projection and action boundary', () => {
  it('presents every assigned production type, safely and with a complete trilingual label catalog', () => {
    const popups = fixtures()
    assert.deepEqual(
      popups.map(popup => popup.type),
      auxiliary.auxiliaryDialogs.types.filter(type => !type.startsWith('Test'))
    )
    assert.equal(popups.length, 33)
    for (const original of popups) {
      const popup = Object.assign(original, {
        stack: 'STACK-SENTINEL',
        oauth: 'OAUTH-SENTINEL',
        actualSecret: 'SECRET-SENTINEL',
        rawAccount: account,
        callback: () => {},
      })
      const dto = auxiliary.auxiliaryDialogs.project(
        popup,
        runtime.appStore.getState()
      )
      assert.equal(dto.supported, true)
      assert.equal(dto.layout, 'form')
      assert.ok(dto.actions.length > 0)
      const keys = [
        dto.titleKey,
        dto.descriptionKey!,
        ...dto.fields.map(f => f.labelKey),
        ...dto.actions.map(a => a.labelKey),
        ...dto.fields
          .flatMap(f => f.choices ?? [])
          .map(c => c.label)
          .filter(label => label.startsWith('aux.')),
      ]
      for (const key of keys) {
        const translations = (labels as Record<string, Record<string, string>>)[
          key
        ]
        assert.ok(translations, `${popup.type}: ${key}`)
        assert.deepEqual(Object.keys(translations).sort(), [
          'en',
          'zh-Hans',
          'zh-Hant',
        ])
        assert.ok(Object.values(translations).every(text => text.length > 0))
      }
      assert.equal(
        new Set(dto.actions.map(action => action.id)).size,
        dto.actions.length
      )
      for (const action of dto.actions) {
        assert.ok(
          Array.isArray(action.fields),
          `Missing field contract for ${popup.type}/${action.id}`
        )
        assert.ok(
          action.fields.every(id =>
            dto.fields.some(field => field.id === id && !field.readOnly)
          ),
          `Unavailable field on ${popup.type}/${action.id}`
        )
        if (action.id === 'close' || action.id === 'cancel') {
          assert.deepEqual(action.fields, [])
        }
      }
      for (const field of dto.fields) {
        if (field.changeAction) {
          assert.ok(
            dto.actions
              .find(action => action.id === field.changeAction)
              ?.fields?.includes(field.id),
            `Missing change action for ${popup.type}/${field.id}`
          )
        }
      }
      assert.doesNotMatch(
        JSON.stringify(dto),
        /SENTINEL|onComplete|subscribeToCommitOutput|callback|bypassPushProtection/
      )
      assert.deepEqual(JSON.parse(JSON.stringify(dto)), dto)
    }
  })

  it('rejects unknown actions and extra fields for every production type without dispatching', async () => {
    for (const popup of fixtures()) {
      const before = runtime.appStore.getState().currentPopup
      await assert.rejects(
        auxiliary.auxiliaryDialogs.reply(runtime, popup, 'onSubmit', {}),
        /Invalid native auxiliary action/
      )
      await assert.rejects(
        auxiliary.auxiliaryDialogs.reply(runtime, popup, 'cancel', {
          accountToken: 'WIRE-SENTINEL',
        }),
        /Invalid native dialog fields/
      )
      assert.equal(runtime.appStore.getState().currentPopup, before)
    }
    assert.deepEqual(calls, [])
  })

  it('preserves EditorError footer precedence and safest destructive default', () => {
    const popup: Popup = {
      type: PopupType.ExternalEditorFailed,
      message: 'diagnostic',
      openPreferences: true,
      suggestDefaultEditor: true,
    }
    assert.deepEqual(actions(popup), ['close', 'preferences'])
    assert.deepEqual(actions({ ...popup, openPreferences: false }), [
      'close',
      'downloadEditor',
    ])
    const bypass = fixtures().find(
      p => p.type === PopupType.BypassPushProtection
    )!
    const buttons = auxiliary.auxiliaryDialogs.project(
      bypass,
      runtime.appStore.getState()
    ).actions
    assert.equal(buttons.find(button => button.id === 'cancel')?.primary, true)
    assert.equal(
      buttons.find(button => button.id === 'allowSecret')?.primary,
      false
    )
    assert.ok(buttons.find(button => button.id === 'allowSecret')?.destructive)
  })

  it('projects current PR preview files/diff/notes/options and exact original submit blocking', () => {
    const popup = fixtures().find(p => p.type === PopupType.StartPullRequest)!
    const file = new CommittedFileChange(
      'fixture.txt',
      { kind: AppFileStatusKind.Modified },
      'a'.repeat(40),
      'b'.repeat(40)
    )
    const state = runtime.repositoryStateManager.get(repository)
    const prState = {
      baseBranch: base,
      commitSHAs: ['a'.repeat(40)],
      mergeStatus: {
        kind: ComputedAction.Conflicts,
        conflictedFiles: 1,
      } as const,
      commitSelection: {
        ...state.commitSelection,
        shas: ['a'.repeat(40)],
        file,
        changesetData: { files: [file], linesAdded: 1, linesDeleted: 1 },
        diff: {
          kind: DiffType.Text,
          text: '@@ -1 +1 @@\n-old\n+new',
          hunks: [],
          maxLineNumber: 1,
          hasHiddenBidiChars: false,
        } as const,
      },
    }
    const app: IAppState = {
      ...runtime.appStore.getState(),
      selectedState: {
        type: SelectionType.Repository,
        repository,
        state: { ...state, pullRequestState: prState },
      },
    }
    assert.equal(
      field(popup, 'diff', app)?.diff?.text,
      prState.commitSelection.diff.text
    )
    assert.equal(field(popup, 'diff', app)?.kind, 'diff')
    assert.equal(field(popup, 'baseBranch', app)?.value, base.name)
    assert.equal(
      field(popup, 'comparisonNote', app)?.labelKey,
      'aux.allCommits'
    )
    assert.equal(
      field(popup, 'mergeStatus', app)?.labelKey,
      'aux.merge.conflicts'
    )
    assert.equal(
      auxiliary.auxiliaryDialogs
        .project(popup, app)
        .actions.find(a => a.id === 'createPullRequest')?.disabled,
      false,
      'Conflicts do not block browser PR creation in the original UI'
    )
    const bytes = Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jPz8AAAAASUVORK5CYII=',
      'base64'
    )
    const image = Object.assign(
      new Image(
        bytes.buffer,
        bytes.toString('base64'),
        'image/png',
        bytes.length
      ),
      { token: 'IMAGE-METADATA-SENTINEL' }
    )
    const images: IAppState = {
      ...app,
      selectedState: {
        type: SelectionType.Repository,
        repository,
        state: {
          ...state,
          pullRequestState: {
            ...prState,
            commitSelection: {
              ...prState.commitSelection,
              diff: { kind: DiffType.Image, previous: image, current: image },
            },
          },
        },
      },
    }
    const projectedImage = field(popup, 'diff', images)!
    assert.equal(projectedImage.kind, 'diff')
    assert.equal(projectedImage.readOnly, true)
    assert.deepEqual(projectedImage.diff?.current, {
      contents: image.contents,
      mediaType: image.mediaType,
      bytes: image.bytes,
    })
    assert.doesNotMatch(
      JSON.stringify(projectedImage),
      /rawContents|SENTINEL|\"buffer\"|\"type\":\"Buffer\"/
    )
    const empty: IAppState = {
      ...app,
      selectedState: {
        type: SelectionType.Repository,
        repository,
        state: { ...state, pullRequestState: { ...prState, commitSHAs: [] } },
      },
    }
    assert.equal(
      auxiliary.auxiliaryDialogs
        .project(popup, empty)
        .actions.find(a => a.id === 'createPullRequest')?.disabled,
      true
    )
  })

  it('does not offer checkout for an approved review even when switch flags are true', () => {
    const popup = fixtures().find(p => p.type === PopupType.PullRequestReview)!
    assert.deepEqual(actions(popup), [
      'close',
      'viewNotification',
      'viewAuthor',
    ])
    assert.equal(field(popup, 'body')?.text, review.body)
  })
})

describe('auxiliary dialog original runtime continuations', () => {
  it('settles the original sign-out completion before closing, with undefined on cancel', async () => {
    for (const result of [false, true, undefined]) {
      const results: Array<[boolean | undefined, PopupType | undefined]> = []
      const popup = await show({
        type: PopupType.ConfirmAccountSignOut,
        account,
        repositoryCount: 2,
        onComplete: value => {
          results.push([value, runtime.appStore.getState().currentPopup?.type])
        },
      })
      if (result === undefined) {
        await auxiliary.auxiliaryDialogs.cancel(runtime, popup)
      } else {
        for (const invalid of [
          { clearAssignments: 'false' },
          { clearAssignments: result, token: 'WIRE-SENTINEL' },
          {},
        ]) {
          await assert.rejects(
            auxiliary.auxiliaryDialogs.reply(runtime, popup, 'signOut', invalid)
          )
          assert.deepEqual(results, [])
        }
        await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'signOut', {
          clearAssignments: result,
        })
      }
      assert.deepEqual(results, [[result, PopupType.ConfirmAccountSignOut]])
      assert.equal(runtime.appStore.getState().currentPopup, null)
      assert.deepEqual(
        await runtime.accountsStore.getAll(),
        [],
        'Presenter must not invent credential business'
      )
    }
  })

  it('accepts only explicit original bypass reasons and leaves async dismissal to the real Popup callback', async () => {
    for (const reason of Object.values(BypassReason)) {
      const results: unknown[] = []
      const popup = await show({
        type: PopupType.BypassPushProtection,
        secret,
        bypassPushProtection: (original, reason) =>
          results.push([original, reason]),
        onDismissed: () => results.push('dismiss'),
      })
      for (const reason of ['automatic', '', null, 1, 'will_fix_later\0']) {
        await assert.rejects(
          auxiliary.auxiliaryDialogs.reply(runtime, popup, 'allowSecret', {
            reason,
          })
        )
      }
      await assert.rejects(
        auxiliary.auxiliaryDialogs.reply(runtime, popup, 'allowSecret', {
          reason,
          secret: { ...secret, bypassURL: 'https://attacker.invalid' },
        })
      )
      assert.deepEqual(results, [])
      await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'allowSecret', {
        reason,
      })
      assert.equal(runtime.appStore.getState().currentPopup, popup)
      assert.deepEqual(results, [[secret, reason]])
      await auxiliary.auxiliaryDialogs.cancel(runtime, popup)
      assert.equal(results.at(-1), 'dismiss')
    }
  })

  it('subscribes once, preserves split UTF-8, and unsubscribes before close without aborting a commit', async () => {
    let listener: ((chunk: TerminalOutput) => void) | undefined
    let subscribed = 0
    let unsubscribed = 0
    let changed = 0
    const popup = await show({
      type: PopupType.CommitProgress,
      subscribeToCommitOutput: cb => {
        subscribed++
        listener = cb
        return {
          unsubscribe: () => {
            unsubscribed++
            assert.equal(
              runtime.appStore.getState().currentPopup?.type,
              PopupType.CommitProgress
            )
          },
        }
      },
    })
    const handle = mount(popup, () => changed++)
    const bytes = Buffer.from('检查失败\n')
    listener!(bytes.subarray(0, 1))
    listener!([bytes.subarray(1, 4), bytes.subarray(4)])
    assert.equal(field(popup, 'terminal')?.value, '检查失败\n')
    assert.equal(changed, 2)
    auxiliary.auxiliaryDialogs.project(popup, runtime.appStore.getState())
    assert.equal(subscribed, 1)
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'close', {})
    handle.dispose()
    assert.equal(unsubscribed, 1)
    assert.equal(runtime.appStore.getState().currentPopup, null)
    listener!('after close')
    assert.equal(changed, 2)
  })

  it('loads the same original packaged licenses, and disposal blocks late view updates', async () => {
    const popup = await show({ type: PopupType.Acknowledgements })
    mount(popup)
    await waitFor(() =>
      assert.equal(
        field(popup, 'license.1')?.text,
        'fixture-library@1\nFixture license text'
      )
    )
    assert.equal(field(popup, 'license')?.text, 'MIT fixture desktop license')
    assert.ok(actions(popup).includes('licenseRepository.1'))
    await auxiliary.auxiliaryDialogs.reply(
      runtime,
      popup,
      'licenseRepository.1',
      {}
    )
    assert.ok(
      calls.some(
        ([channel, args]) =>
          channel === 'open-external' &&
          args[0] === 'https://github.com/example/fixture'
      )
    )
  })

  it('opens the original Preferences after closing the failed editor popup', async () => {
    const popup = await show({
      type: PopupType.ExternalEditorFailed,
      message: 'diagnostic',
      openPreferences: true,
      suggestDefaultEditor: true,
    })
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, popup, 'downloadEditor', {})
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'preferences', {})
    const current = runtime.appStore.getState().currentPopup
    assert.equal(current?.type, PopupType.Preferences)
    assert.ok(
      !runtime.appStore.getState().allPopups.some(p => p.id === popup.id)
    )
  })

  it('retains original commit output lifetime even when another popup covers it', async () => {
    let listener: (chunk: TerminalOutput) => void = () => {}
    let unsubscribe = 0
    const popup = await show({
      type: PopupType.CommitProgress,
      subscribeToCommitOutput: cb => {
        listener = cb
        return {
          unsubscribe: () => {
            unsubscribe++
          },
        }
      },
    })
    mount(popup)
    await show({ type: PopupType.About })
    listener('still running')
    assert.equal(unsubscribe, 0)
    assert.equal(field(popup, 'terminal')?.value, 'still running')
  })

  it('rejects uninstalled editors instead of using an arbitrary wire-selected executable', async () => {
    const popup = await show({ type: PopupType.OpenWithExternalEditor })
    mount(popup)
    await waitFor(() =>
      assert.equal(field(popup, 'editor')?.choices?.[0]?.id, 'Fixture Editor')
    )
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, popup, 'openEditor', {
        editor: 'uninstalled.exe',
      })
    )
    assert.equal(runtime.appStore.getState().currentPopup, popup)
  })

  it('shows complete original legal text and links in both React and native without an embedded web view', async () => {
    const { TermsAndConditions } = await import(
      '../../src/ui/terms-and-conditions/terms-and-conditions'
    )
    const view = render(
      React.createElement(TermsAndConditions, { onDismissed: () => {} })
    )
    await waitFor(() => assert.ok(view.container.querySelector('dialog')))
    const text = field({ type: PopupType.TermsAndConditions }, 'legalTerms')!
      .text!
    for (const element of view.container.querySelectorAll('p, h2, h3, li')) {
      assert.ok(
        text.includes(element.textContent ?? ''),
        element.textContent ?? ''
      )
    }
    assert.equal(view.container.querySelectorAll('ol > li').length, 7)
    assert.match(text, /Governing Law\. You agree/)
    assert.match(
      text,
      /YOU EXPRESSLY UNDERSTAND AND AGREE THAT GITHUB SHALL NOT BE LIABLE/
    )
    fireEvent.click(view.getByText('Close'))
  })

  it('keeps markdown link parsing and explicit link actions on the original sanitizer/filter pipeline', async () => {
    const popup = await show({
      type: PopupType.PullRequestComment,
      repository,
      pullRequest: pr,
      comment: {
        ...comment,
        body: '[Docs](../docs) [Unsafe](javascript:alert(1)) and @mona #12',
      },
      shouldCheckoutBranch: false,
      shouldChangeRepository: false,
    })
    const { dialogMarkdownLinks } = await import(
      '../../src/ui/lib/dialog-markdown'
    )
    const links = await dialogMarkdownLinks(
      '[Docs](../docs) [Unsafe](javascript:alert(1)) and @mona #12',
      {
        emoji: runtime.appStore.getState().emoji,
        repository: pr.base.gitHubRepository,
        markdownContext: 'PullRequestComment',
      },
      pr.base.gitHubRepository.htmlURL ?? undefined
    )
    assert.ok(links.length >= 3)
    mount(popup)
    await waitFor(() =>
      assert.ok((field(popup, 'contentLink')?.choices?.length ?? 0) >= 3)
    )
    assert.ok(
      !field(popup, 'contentLink')?.choices?.some(
        choice => choice.label === 'Unsafe'
      )
    )
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, popup, 'openContentLink', {
        contentLink: '0',
        url: 'https://attacker.invalid',
      })
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'openContentLink', {
      contentLink: '0',
    })
    assert.ok(
      calls.some(
        ([channel, args]) =>
          channel === 'open-external' &&
          args[0] === 'https://fixture.example.invalid/docs'
      )
    )
    assert.equal(runtime.appStore.getState().currentPopup, popup)
  })

  it('targets the exact invalidated account without starting browser OAuth or reusing another account', async () => {
    const popup = await show({ type: PopupType.InvalidatedToken, account })
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'signIn', {})
    assert.equal(
      runtime.appStore.getState().currentPopup?.type,
      PopupType.SignIn
    )
    const state = runtime.signInStore.getState()
    assert.ok(state?.kind === 'Authentication')
    assert.equal(state.expectedLogin, account.login)
    assert.equal(state.endpoint, account.endpoint)
    runtime.signInStore.reset()
  })

  it('preserves original installing-update polling, explicit quit and disposal cancellation', async () => {
    ipc.emit('auto-updater-update-available')
    const popup = await show({ type: PopupType.InstallingUpdate })
    const handle = mount(popup)
    assert.ok(!calls.some(([channel]) => channel === 'quit-app'))
    ipc.emit('auto-updater-checking-for-update')
    assert.ok(calls.some(([channel]) => channel === 'quit-app'))
    calls.length = 0
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'quitAnyway', {})
    assert.deepEqual(
      calls
        .filter(([channel]) =>
          [
            'will-quit-even-if-updating',
            'quit-app',
            'cancel-quitting',
          ].includes(channel)
        )
        .map(([channel]) => channel),
      ['will-quit-even-if-updating', 'quit-app', 'cancel-quitting']
    )
    assert.equal(runtime.appStore.getState().currentPopup, null)
    handle.dispose()
    calls.length = 0
    ipc.emit('auto-updater-checking-for-update')
    assert.ok(!calls.some(([channel]) => channel === 'quit-app'))
  })

  it('uses the original nested bypass promise and cancels it without approving or pushing a secret', async () => {
    const popup = await show({
      type: PopupType.PushProtectionError,
      secrets: [secret],
    })
    mount(popup)
    const opening = auxiliary.auxiliaryDialogs.reply(
      runtime,
      popup,
      'bypass.0',
      {}
    )
    await waitFor(() =>
      assert.equal(
        runtime.appStore.getState().currentPopup?.type,
        PopupType.BypassPushProtection
      )
    )
    const bypass = runtime.appStore.getState().currentPopup!
    await auxiliary.auxiliaryDialogs.cancel(runtime, bypass)
    await opening
    assert.equal(runtime.appStore.getState().currentPopup, popup)
    assert.equal(
      auxiliary.auxiliaryDialogs
        .project(popup, runtime.appStore.getState())
        .actions.find(action => action.id === 'bypass.0')?.disabled,
      false
    )
  })

  it('verifies original React download/reset semantics rather than guessing from the click handler', async () => {
    const { EditorError } = await import('../../src/ui/editor/editor-error')
    const { suggestedExternalEditor } = await import(
      '../../src/lib/editors/shared'
    )
    let dismissed = 0
    const { DialogStackContext } = await import('../../src/ui/dialog/dialog')
    const prototype = window.HTMLDialogElement.prototype
    Object.assign(prototype, {
      showModal(this: HTMLDialogElement) {
        this.open = true
      },
      close(this: HTMLDialogElement) {
        this.open = false
      },
    })
    const appeared = new Promise<void>(resolve =>
      document.addEventListener('dialog-appeared', () => resolve(), {
        once: true,
      })
    )
    const view = render(
      React.createElement(
        DialogStackContext.Provider,
        { value: { isTopMost: true } },
        React.createElement(EditorError, {
          message: 'diagnostic',
          suggestDefaultEditor: true,
          onDismissed: () => {
            dismissed++
          },
          showPreferencesDialog: () =>
            assert.fail('Download does not open Preferences'),
        })
      )
    )
    await appeared
    fireEvent.click(view.getByText(`Download ${suggestedExternalEditor.name}`))
    await waitFor(() => assert.equal(dismissed, 1))
    assert.ok(
      calls.some(
        ([channel, args]) =>
          channel === 'open-external' && args[0] === suggestedExternalEditor.url
      )
    )
  })

  it('reads CI eligibility and logs through the actual API/Dispatcher against loopback fixtures only', async t => {
    const requests: string[] = []
    let failRerun = true
    const server = createServer((req, res) => {
      requests.push(`${req.method} ${req.url}`)
      res.setHeader('Content-Type', 'application/json')
      res.setHeader('x-github-enterprise-version', '3.18.0')
      if (req.method === 'POST' && req.url?.endsWith('/rerequest')) {
        res.writeHead(failRerun ? 403 : 202).end('{}')
        return
      }
      const id = Number(req.url?.match(/check-suites\/(\d+)/)?.[1])
      if (id) {
        if (id === 5) {
          res.writeHead(404).end('{}')
          return
        }
        res.end(
          JSON.stringify({
            id,
            rerequestable: id !== 4,
            created_at: new Date(
              Date.now() - (id === 2 ? 31 : 1) * 86400000
            ).toISOString(),
            status: id === 3 ? 'in_progress' : 'completed',
          })
        )
        return
      }
      if (req.url?.includes('/actions/runs/91/jobs')) {
        res.end(
          JSON.stringify({
            jobs: [
              {
                id: 1,
                html_url: 'https://fixture.example.invalid/checks/1',
                steps: [
                  {
                    number: 1,
                    name: 'Build',
                    status: 'completed',
                    conclusion: 'failure',
                    started_at: '2026-01-01',
                    completed_at: '2026-01-01',
                  },
                ],
              },
            ],
          })
        )
        return
      }
      if (req.url?.includes('/actions/runs')) {
        res.end(
          JSON.stringify({
            workflow_runs: [
              {
                id: 91,
                check_suite_id: 1,
                workflow_id: 100,
                name: 'Fixture workflow',
                event: 'pull_request',
                created_at: '2026-01-01',
              },
            ],
          })
        )
        return
      }
      // Any unrelated original background account lookup stays in this local fixture.
      res.writeHead(404).end('{}')
    })
    await listenOnLoopback(server)
    t.after(async () => {
      server.closeAllConnections()
      await new Promise<void>(resolve => server.close(() => resolve()))
    })
    const endpoint = `http://127.0.0.1:${
      (server.address() as AddressInfo).port
    }/api/v3`
    const localAccount = new Account(
      'ci-fixture',
      endpoint,
      'FAKE-LOCAL-CI-TOKEN',
      [],
      '',
      801,
      'CI fixture'
    )
    const added = await runtime.accountsStore.addAccount(localAccount)
    assert.ok(added)
    const apiRepository = {
      name: 'repo',
      owner: { ...identity, login: 'owner' },
      private: false,
      fork: false,
      html_url: endpoint.replace('/api/v3', '') + '/owner/repo',
      clone_url: '',
      ssh_url: '',
      default_branch: 'main',
      pushed_at: '2026-01-01',
      has_issues: true,
      archived: false,
      parent: undefined,
    }
    const github = await runtime.repositoriesStore.upsertGitHubRepository(
      endpoint,
      apiRepository,
      added.login
    )
    const repoPath = join(licenseRoot, 'ci-repository')
    await mkdir(repoPath)
    const initialized = await exec(
      ['init', '--template=', '--initial-branch=main'],
      repoPath
    )
    assert.equal(initialized.exitCode, 0, initialized.stderr)
    const stored = await runtime.repositoriesStore.addRepository(
      repoPath,
      undefined,
      { missing: true }
    )
    const localRepository = await runtime.repositoriesStore.setGitHubRepository(
      stored,
      github,
      added.login
    )
    assertIsRepositoryWithGitHubRepository(localRepository)
    const checks = [
      check(1),
      check(2),
      check(3),
      check(4),
      check(5),
      { ...check(6), checkSuiteId: null },
    ]
    const popup = await show({
      type: PopupType.CICheckRunRerun,
      repository: localRepository.gitHubRepository,
      localRepository,
      checkRuns: checks,
      failedOnly: true,
      prRef: 'refs/pull/7/head',
    })
    mount(popup)
    await waitFor(() =>
      assert.equal(field(popup, 'rerunnable')?.text, 'Check 1\nFailed\nfailure')
    )
    assert.match(
      field(popup, 'nonRerunnable')?.text ?? '',
      /Check 2[\s\S]*Check 3[\s\S]*Check 4[\s\S]*Check 5[\s\S]*Check 6/
    )
    assert.equal(
      requests.filter(path => path.includes('/check-suites/')).length,
      5
    )
    assert.equal(
      auxiliary.auxiliaryDialogs
        .project(popup, runtime.appStore.getState())
        .actions.find(action => action.id === 'rerun')?.disabled,
      false
    )
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, popup, 'rerun', {
        checkSuiteId: 99,
      })
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'rerun', {})
    assert.equal(
      runtime.appStore.getState().currentPopup,
      popup,
      'An API failure retains the original rerun dialog'
    )
    assert.ok(
      !auxiliary.auxiliaryDialogs
        .project(popup, runtime.appStore.getState())
        .fields.some(field => field.id === 'loading')
    )
    failRerun = false
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'rerun', {})
    assert.equal(runtime.appStore.getState().currentPopup, null)
    assert.equal(
      requests.filter(
        path => path.startsWith('POST ') && path.endsWith('/rerequest')
      ).length,
      2
    )
    const failed = await show({
      type: PopupType.PullRequestChecksFailed,
      repository: localRepository,
      pullRequest: pullRequest(localRepository),
      checks,
      shouldChangeRepository: false,
    })
    mount(failed)
    await waitFor(() =>
      assert.ok(
        !auxiliary.auxiliaryDialogs
          .project(failed, runtime.appStore.getState())
          .fields.some(f => f.id === 'loading')
      )
    )
    assert.equal(field(failed, 'steps')?.text, '1. Build\nfailure')
    assert.ok(requests.some(path => path.includes('/actions/runs/91/jobs')))
    await auxiliary.auxiliaryDialogs.reply(runtime, failed, 'viewStep', {
      step: '1',
    })
    assert.ok(
      calls.some(
        ([channel, args]) =>
          channel === 'open-external' &&
          args[0] === 'https://fixture.example.invalid/checks/1/#step:1:1'
      )
    )
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, failed, 'viewStep', {
        step: '999',
      })
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, failed, 'selectCheck', {
      check: '3',
    })
    assert.equal(field(failed, 'check')?.value, '3')
    assert.ok(requests.some(path => path.includes('/actions/runs')))
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, failed, 'selectCheck', {
        check: '999',
      })
    )
    await runtime.accountsStore.removeAccount(added)
  })
})

function enableDevelopment(t: TestContext) {
  const previous = __DEV__
  Object.assign(globalThis, { __DEV__: true })
  t.after(() => Object.assign(globalThis, { __DEV__: previous }))
}
async function fixtureGit(path: string, args: ReadonlyArray<string>) {
  assert.ok(
    path.startsWith(licenseRoot),
    'Never run Git outside the isolated fixture'
  )
  const result = await exec([...args], path)
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout.trim()
}
async function localBranchFixture() {
  const path = await mkdtemp(join(licenseRoot, 'push-'))
  const bare = await mkdtemp(join(licenseRoot, 'push-remote-'))
  const hooks = join(path, 'empty-hooks')
  await mkdir(hooks)
  await fixtureGit(path, ['init', '--template=', '--initial-branch=main'])
  await fixtureGit(bare, [
    'init',
    '--bare',
    '--template=',
    '--initial-branch=main',
  ])
  for (const [key, value] of [
    ['user.name', 'Native dialog fixture'],
    ['user.email', 'dialog@example.test'],
    ['commit.gpgSign', 'false'],
    ['push.gpgSign', 'false'],
    ['core.hooksPath', hooks],
  ]) {
    await fixtureGit(path, ['config', key, value])
  }
  await writeFile(join(path, 'file.txt'), 'first\n')
  await fixtureGit(path, ['add', 'file.txt'])
  await fixtureGit(path, ['commit', '-m', 'fixture initial'])
  await fixtureGit(path, ['remote', 'add', 'origin', bare])
  const [repository] = await runtime.dispatcher.addRepositories([path])
  assert.ok(repository)
  await runtime.dispatcher.selectRepository(repository)
  await runtime.dispatcher.refreshRepository(repository)
  const tip = runtime.repositoryStateManager.get(repository).branchesState.tip
  assert.ok(tip.kind === 'Valid')
  return { path, bare, repository, branch: tip.branch }
}

function assertFieldContracts(popup: Popup) {
  const dto = auxiliary.auxiliaryDialogs.project(
    popup,
    runtime.appStore.getState()
  )
  for (const action of dto.actions) {
    assert.ok(Array.isArray(action.fields), `${popup.type}/${action.id}`)
    assert.ok(
      action.fields.every(id =>
        dto.fields.some(field => field.id === id && !field.readOnly)
      ),
      `${popup.type}/${action.id}`
    )
    if (['close', 'cancel', 'done'].includes(action.id)) {
      assert.deepEqual(action.fields, [])
    }
  }
  for (const field of dto.fields) {
    if (field.changeAction) {
      assert.ok(
        dto.actions
          .find(action => action.id === field.changeAction)
          ?.fields?.includes(field.id),
        `${popup.type}/${field.id}`
      )
    }
  }
  for (const key of [
    dto.titleKey,
    dto.descriptionKey,
    ...dto.fields.map(f => f.labelKey),
    ...dto.actions.map(a => a.labelKey),
    ...dto.fields.flatMap(
      f =>
        f.choices
          ?.map(c => c.label)
          .filter(label => label.startsWith('aux.')) ?? []
    ),
  ].filter((key): key is string => key !== undefined)) {
    assert.ok((labels as Record<string, unknown>)[key], key)
  }
  assert.deepEqual(JSON.parse(JSON.stringify(dto)), dto)
  return dto
}

describe('previously missing production popup continuations', () => {
  it('keeps cancel distinct from create-without-push and pushes actual commits before the original PR continuation', async t => {
    const { path, bare, repository: local, branch } = await localBranchFixture()
    const server = createServer((_request, response) =>
      response.writeHead(404).end('{}')
    )
    await listenOnLoopback(server)
    t.after(() => {
      server.closeAllConnections()
      server.close()
    })
    const host = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
    const github = new GitHubRepository(
      'repo',
      new Owner('owner', `${host}/api/v3`, 1),
      900,
      false,
      `${host}/owner/repo`
    )
    const repository = new Repository(local.path, local.id, github, false)
    const publish = await show({
      type: PopupType.PushBranchCommits,
      repository,
      branch,
    })
    mount(publish)
    assert.deepEqual(actions(publish), ['cancel', 'publishBranch'])
    await auxiliary.auxiliaryDialogs.cancel(runtime, publish)
    assert.equal(
      (await exec(['show-ref', '--verify', 'refs/heads/main'], bare)).exitCode,
      128
    )
    assert.ok(!calls.some(([channel]) => channel === 'open-external'))
    const without = await show({
      type: PopupType.PushBranchCommits,
      repository,
      branch,
      unPushedCommits: 1,
    })
    mount(without)
    assertFieldContracts(without)
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, without, 'withoutPush', {
        branch: 'forged',
      })
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, without, 'withoutPush', {})
    assert.equal(
      (await exec(['show-ref', '--verify', 'refs/heads/main'], bare)).exitCode,
      128
    )
    assert.ok(
      calls.some(
        ([channel, args]) =>
          channel === 'open-external' &&
          args[0] === `${host}/owner/repo/pull/new/main`
      )
    )
    calls.length = 0
    const pushing = await show({
      type: PopupType.PushBranchCommits,
      repository,
      branch,
      unPushedCommits: 1,
    })
    let updates = 0
    mount(pushing, () => updates++)
    const pending = auxiliary.auxiliaryDialogs.reply(
      runtime,
      pushing,
      'pushCommits',
      {}
    )
    assert.ok(
      auxiliary.auxiliaryDialogs
        .project(pushing, runtime.appStore.getState())
        .actions.every(action => action.disabled)
    )
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, pushing, 'pushCommits', {})
    )
    await pending
    assert.equal(
      await fixtureGit(bare, ['rev-parse', 'main']),
      await fixtureGit(path, ['rev-parse', 'HEAD'])
    )
    assert.ok(updates >= 2)
    assert.ok(
      calls.some(
        ([channel, args]) =>
          channel === 'open-external' &&
          args[0] === `${host}/owner/repo/pull/new/main`
      )
    )
    assert.equal(runtime.appStore.getState().currentPopup, null)
  })

  it('creates a real private tutorial through the original API/Git workflow, blocks dismissal during progress and only pauses on explicit exit', async t => {
    const root = await mkdtemp(join(licenseRoot, 'tutorial-'))
    const bare = await mkdtemp(join(licenseRoot, 'tutorial-remote-'))
    await fixtureGit(bare, [
      'init',
      '--bare',
      '--template=',
      '--initial-branch=main',
    ])
    const config = join(root, 'global-config')
    const hooks = join(root, 'empty-hooks')
    await mkdir(hooks)
    await writeFile(
      config,
      `[user]\n name = Tutorial Fixture\n email = tutorial@example.test\n[commit]\n gpgSign = false\n[core]\n hooksPath = ${hooks.replace(
        /\\/g,
        '/'
      )}\n`
    )
    const prior = {
      global: process.env.GIT_CONFIG_GLOBAL,
      system: process.env.GIT_CONFIG_NOSYSTEM,
      location: localStorage.getItem('last-clone-location'),
    }
    process.env.GIT_CONFIG_GLOBAL = config
    process.env.GIT_CONFIG_NOSYSTEM = '1'
    localStorage.setItem('last-clone-location', root)
    t.after(() => {
      if (prior.global === undefined) {
        delete process.env.GIT_CONFIG_GLOBAL
      } else {
        process.env.GIT_CONFIG_GLOBAL = prior.global
      }
      if (prior.system === undefined) {
        delete process.env.GIT_CONFIG_NOSYSTEM
      } else {
        process.env.GIT_CONFIG_NOSYSTEM = prior.system
      }
      if (prior.location === null) {
        localStorage.removeItem('last-clone-location')
      } else {
        localStorage.setItem('last-clone-location', prior.location)
      }
    })
    const requests: Array<{
      method: string | undefined
      url: string | undefined
      body: Record<string, unknown>
    }> = []
    let response: import('http').ServerResponse | undefined
    let entered!: () => void
    const started = new Promise<void>(resolve => {
      entered = resolve
    })
    const server = createServer(async (request, res) => {
      const chunks: Buffer[] = []
      for await (const chunk of request) {
        chunks.push(Buffer.from(chunk))
      }
      const body =
        chunks.length === 0 ? {} : JSON.parse(Buffer.concat(chunks).toString())
      requests.push({ method: request.method, url: request.url, body })
      res.setHeader('Content-Type', 'application/json')
      if (request.method === 'POST' && request.url === '/api/v3/user/repos') {
        response = res
        entered()
        return
      }
      res.writeHead(404).end('{}')
    })
    await listenOnLoopback(server)
    t.after(() => {
      response?.destroy()
      server.closeAllConnections()
      server.close()
    })
    const host = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
    const account = new Account(
      'tutorial-fixture',
      `${host}/api/v3`,
      'FAKE-TUTORIAL-TOKEN',
      [],
      '',
      950,
      'Tutorial fixture'
    )
    const added = await runtime.accountsStore.addAccount(account)
    assert.ok(added)
    t.after(() => runtime.accountsStore.removeAccount(added))
    const cancelled = await show({
      type: PopupType.CreateTutorialRepository,
      account: added,
    })
    await auxiliary.auxiliaryDialogs.cancel(runtime, cancelled)
    assert.equal(
      requests.filter(request => request.method === 'POST').length,
      0
    )
    const failures: string[] = []
    const errorSubscription = runtime.appStore.onDidError(error => {
      failures.push(error.message)
    })
    t.after(() => errorSubscription.dispose())
    const popup = await show({
      type: PopupType.CreateTutorialRepository,
      account: added,
    })
    mount(popup)
    const pending = auxiliary.auxiliaryDialogs.reply(
      runtime,
      popup,
      'createTutorial',
      {}
    )
    await started
    const progress = runtime.appStore.getState().currentPopup!
    assert.ok(
      progress.type === PopupType.CreateTutorialRepository &&
        progress.progress !== undefined
    )
    assert.doesNotMatch(
      JSON.stringify(assertFieldContracts(progress)),
      /FAKE-TUTORIAL-TOKEN|onComplete/
    )
    await assert.rejects(
      async () => auxiliary.auxiliaryDialogs.cancel(runtime, progress),
      /progress/
    )
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, progress, 'createTutorial', {})
    )
    assert.equal(
      requests.find(request => request.method === 'POST')?.body.private,
      true
    )
    response!.end(
      JSON.stringify({
        id: 950,
        name: 'desktop-tutorial',
        owner: { ...identity, login: added.login },
        private: true,
        fork: false,
        html_url: `${host}/tutorial-fixture/desktop-tutorial`,
        clone_url: bare,
        ssh_url: bare,
        default_branch: 'main',
        pushed_at: '2026-01-01',
        has_issues: true,
        archived: false,
      })
    )
    await pending
    await waitFor(() =>
      assert.ok(
        runtime.appStore
          .getState()
          .repositories.some(
            repository =>
              repository instanceof Repository &&
              repository.path === join(root, 'desktop-tutorial')
          ),
        failures.join('\n')
      )
    )
    const created = runtime.appStore
      .getState()
      .repositories.find(
        (repository): repository is Repository =>
          repository instanceof Repository &&
          repository.path === join(root, 'desktop-tutorial')
      )
    assert.ok(
      created?.isTutorialRepository,
      `Only the original tutorial workflow may set the tutorial flag. ${failures.join(
        '\n'
      )}`
    )
    assert.equal(
      await fixtureGit(bare, ['log', '-1', '--format=%s']),
      'Initial commit'
    )
    assert.match(
      await readFile(join(created.path, 'README.md'), 'utf8'),
      /Welcome to GitHub Desktop/
    )
    await runtime.dispatcher.selectRepository(created)
    const before = runtime.appStore.getState().currentOnboardingTutorialStep
    const exitCancel = await show({ type: PopupType.ConfirmExitTutorial })
    await auxiliary.auxiliaryDialogs.cancel(runtime, exitCancel)
    assert.equal(
      runtime.appStore.getState().currentOnboardingTutorialStep,
      before
    )
    const exit = await show({ type: PopupType.ConfirmExitTutorial })
    await auxiliary.auxiliaryDialogs.reply(runtime, exit, 'exitTutorial', {})
    assert.equal(
      runtime.appStore.getState().currentOnboardingTutorialStep,
      TutorialStep.Paused
    )
    assert.equal(runtime.appStore.getState().currentPopup, null)
    await runtime.appStore._selectRepository(null)
    const missing = await show({ type: PopupType.ConfirmExitTutorial })
    await auxiliary.auxiliaryDialogs.reply(runtime, missing, 'exitTutorial', {})
    assert.equal(
      runtime.appStore.getState().currentPopup,
      missing,
      'Original onContinue=false must not close an unrelated prompt'
    )
    await auxiliary.auxiliaryDialogs.cancel(runtime, missing)
    await runtime.accountsStore.removeAccount(added)
  })
})

describe('original development popup tools', () => {
  const popups = (): Popup[] => [
    { type: PopupType.TestNotifications, repository },
    { type: PopupType.TestIcons },
    { type: PopupType.TestAbout },
    { type: PopupType.TestCLIAction },
    { type: PopupType.TestCopilotSnapshotCard },
    { type: PopupType.TestTokenExpiration },
  ]

  it('keeps all six tools explicitly unavailable by default instead of forwarding debug commands', async t => {
    const previous = process.env.GITHUB_DESKTOP_PREVIEW_FEATURES
    delete process.env.GITHUB_DESKTOP_PREVIEW_FEATURES
    t.after(() => {
      if (previous === undefined) {
        delete process.env.GITHUB_DESKTOP_PREVIEW_FEATURES
      } else {
        process.env.GITHUB_DESKTOP_PREVIEW_FEATURES = previous
      }
    })
    assert.equal(__DEV__, false)
    for (const detail of popups()) {
      const popup = await show(detail)
      mount(popup)
      const view = assertFieldContracts(popup)
      assert.deepEqual(
        view.actions.map(a => a.id),
        ['close']
      )
      assert.equal(view.descriptionKey, 'aux.developmentOnly')
      await assert.rejects(
        auxiliary.auxiliaryDialogs.reply(runtime, popup, 'dispatchCLI', {})
      )
      await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'close', {})
    }
  })

  it('honors the original explicitly enabled preview test-menu capability without installing an update', async t => {
    const previous = process.env.GITHUB_DESKTOP_PREVIEW_FEATURES
    process.env.GITHUB_DESKTOP_PREVIEW_FEATURES = '1'
    t.after(() => {
      if (previous === undefined) {
        delete process.env.GITHUB_DESKTOP_PREVIEW_FEATURES
      } else {
        process.env.GITHUB_DESKTOP_PREVIEW_FEATURES = previous
      }
    })
    assert.equal(__DEV__, false)
    const popup = await show({ type: PopupType.TestIcons })
    mount(popup)
    assert.equal(
      assertFieldContracts(popup).fields.filter(field => field.kind === 'icons')
        .length,
      5
    )
    await auxiliary.auxiliaryDialogs.cancel(runtime, popup)
  })

  it('projects the original developer controls, all field/choice contracts and native mature glyph IDs', async t => {
    enableDevelopment(t)
    for (const detail of popups()) {
      const popup = await show(detail)
      mount(popup)
      const dto = assertFieldContracts(popup)
      assert.doesNotMatch(
        JSON.stringify(dto),
        /TOKEN-SENTINEL|fake-token|refreshToken|accessToken|<svg|rawAccount/
      )
      if (popup.type === PopupType.TestIcons) {
        const icons = dto.fields.filter(field => field.kind === 'icons')
        assert.deepEqual(
          icons.map(field => field.value),
          [12, 16, 24, 48, 96]
        )
        assert.equal(icons.flatMap(field => field.choices ?? []).length, 733)
        assert.ok(
          icons.every(
            field => field.readOnly && field.iconLibrary === 'octicons'
          )
        )
      }
      await auxiliary.auxiliaryDialogs.cancel(runtime, popup)
    }
  })

  it('preserves actual CLI dispatch and original notification Back rather than a generic stack removal', async t => {
    enableDevelopment(t)
    const { repository: local } = await localBranchFixture()
    const cli = await show({ type: PopupType.TestCLIAction })
    mount(cli)
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, cli, 'cliChange', {
        cliKind: 'arbitrary',
      })
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, cli, 'cliChange', {
      cliPath: local.path,
    })
    await auxiliary.auxiliaryDialogs.reply(runtime, cli, 'dispatchCLI', {
      cliKind: 'open-repository',
      cliPath: local.path,
    })
    const selected = runtime.appStore.getState().selectedState
    assert.ok(
      selected?.type === SelectionType.Repository &&
        selected.repository.id === local.id
    )
    assert.equal(runtime.appStore.getState().currentPopup, null)
    const notifications = await show({
      type: PopupType.TestNotifications,
      repository,
    })
    mount(notifications)
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(
        runtime,
        notifications,
        'chooseNotificationType',
        {}
      )
    )
    await auxiliary.auxiliaryDialogs.reply(
      runtime,
      notifications,
      'chooseNotificationType',
      { notificationType: '0' }
    )
    assert.ok(actions(notifications).includes('backNotification'))
    await auxiliary.auxiliaryDialogs.reply(
      runtime,
      notifications,
      'backNotification',
      {}
    )
    assert.equal(runtime.appStore.getState().currentPopup, notifications)
    assert.ok(actions(notifications).includes('chooseNotificationType'))
    await auxiliary.auxiliaryDialogs.cancel(runtime, notifications)
  })

  it('uses the real snapshot presets/visibility and never modifies live account or quota stores', async t => {
    enableDevelopment(t)
    const accounts = runtime.appStore.getState().accounts
    const quotas = runtime.appStore.getState().copilotQuotaSnapshotsByAccount
    const popup = await show({ type: PopupType.TestCopilotSnapshotCard })
    mount(popup)
    await auxiliary.auxiliaryDialogs.reply(
      runtime,
      popup,
      'aiCreditsPreset',
      {}
    )
    assert.equal(field(popup, 'preview.premium_interactions')?.text, '2.5/12.5')
    assert.equal(field(popup, 'preview.chat'), undefined)
    await auxiliary.auxiliaryDialogs.reply(
      runtime,
      popup,
      'rateLimitPreset',
      {}
    )
    assert.equal(field(popup, 'preview.session')?.value, 0.65)
    assert.equal(field(popup, 'preview.weekly')?.value, 0.08)
    await assert.rejects(
      auxiliary.auxiliaryDialogs.reply(runtime, popup, 'snapshotChange', {
        'chat:enabled': 'true',
      })
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'snapshotChange', {
      'chat:enabled': false,
    })
    assert.equal(field(popup, 'preview.chat'), undefined)
    assert.equal(runtime.appStore.getState().accounts, accounts)
    assert.equal(
      runtime.appStore.getState().copilotQuotaSnapshotsByAccount,
      quotas
    )
    assertFieldContracts(popup)
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'done', {})
  })

  it('edits original session-only token expiry without refreshing, saving or projecting either credential', async t => {
    enableDevelopment(t)
    const server = createServer((_request, response) =>
      response.writeHead(404).end('{}')
    )
    await listenOnLoopback(server)
    t.after(() => {
      server.closeAllConnections()
      server.close()
    })
    const endpoint = `http://127.0.0.1:${
      (server.address() as AddressInfo).port
    }/api/v3`
    const account = new Account(
      'expiry-fixture',
      endpoint,
      'EXPIRY-ACCESS-SENTINEL',
      [],
      '',
      980,
      'Expiry fixture'
    )
    const expiresAt = Date.now() + 8 * 60 * 60_000
    const added = await runtime.accountsStore.addAccount(account, {
      accessToken: account.token,
      refreshToken: 'EXPIRY-REFRESH-SENTINEL',
      expiresAt,
    })
    assert.ok(added)
    t.after(() => runtime.accountsStore.removeAccount(added))
    const saved = [...passwords.entries()]
    const popup = await show({ type: PopupType.TestTokenExpiration })
    mount(popup)
    await waitFor(() =>
      assert.equal(
        auxiliary.auxiliaryDialogs
          .project(popup, runtime.appStore.getState())
          .actions.find(action => action.id === 'applyExpiration')?.disabled,
        false
      )
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'expire62min', {})
    const dateTime = field(popup, 'testDateTime')!.value
    assert.equal(
      (await runtime.dispatcher.getAccountTokenExpirationForTesting(added))
        .isOverridden,
      false
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'applyExpiration', {
      testDateTime: dateTime,
    })
    const expiration =
      await runtime.dispatcher.getAccountTokenExpirationForTesting(added)
    assert.equal(expiration.isOverridden, true)
    assert.equal(expiration.originalExpiresAt, expiresAt)
    assert.ok(expiration.expiresAt! < expiresAt)
    assert.doesNotMatch(
      JSON.stringify(assertFieldContracts(popup)),
      /SENTINEL|refreshToken|accessToken/
    )
    assert.deepEqual(
      [...passwords.entries()],
      saved,
      'Local expiry overrides must not write secure storage'
    )
    await auxiliary.auxiliaryDialogs.reply(
      runtime,
      popup,
      'resetExpiration',
      {}
    )
    assert.equal(
      (await runtime.dispatcher.getAccountTokenExpirationForTesting(added))
        .expiresAt,
      expiresAt
    )
    await auxiliary.auxiliaryDialogs.reply(runtime, popup, 'close', {})
    await runtime.accountsStore.removeAccount(added)
  })
})

import { listenOnLoopback } from '../helpers/http-server'
import assert from 'node:assert/strict'
import { after, before, describe, it, mock } from 'node:test'
import { EventEmitter } from 'events'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { exec } from 'dugite'
import { createServer, ServerResponse } from 'http'
import { AddressInfo } from 'net'
import { ipcRenderer } from 'electron'
import labels from '../../../references/native-ui-settings-labels.json'
import { Account } from '../../src/models/account'
import type { IBYOKModel } from '../../src/lib/copilot/byok'
import type { AppRuntime } from '../../src/ui/app-runtime'
import type { NativeDialogModule } from '../../src/native-kernel/dialog-module'
import { Popup, PopupType } from '../../src/models/popup'
import { PreferencesTab } from '../../src/models/preferences'
import { ApplicationTheme } from '../../src/ui/lib/application-theme'

// Only platform/credential boundaries are mocked. The original stores,
// Dispatcher, Git writes/diffs and locked/busy/popup behaviours run unchanged.
const secrets = new Map<string, string>()
let secretWriteGate: (() => Promise<void>) | undefined
mock.module('keytar', {
  namedExports: {
    setPassword: async (service: string, login: string, value: string) => {
      await secretWriteGate?.()
      secrets.set(`${service}:${login}`, value)
    },
    getPassword: async (service: string, login: string) =>
      secrets.get(`${service}:${login}`) ?? null,
    deletePassword: async (service: string, login: string) =>
      secrets.delete(`${service}:${login}`),
  },
})
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
let ProviderStorageKey: string
const priorElectron = Object.getOwnPropertyDescriptor(
  process.versions,
  'electron'
)
let runtime: AppRuntime
let dialogs: NativeDialogModule
let mountSettings: typeof import('../../src/native-kernel/settings-dialogs').mountSettingsDialog
let dir: string
let globalConfig: string
const prior = {
  global: process.env.GIT_CONFIG_GLOBAL,
  noSystem: process.env.GIT_CONFIG_NOSYSTEM,
}
before(async () => {
  await mkdir(resolve('_temp/native-full-ui'), { recursive: true })
  dir = await mkdtemp(join(resolve('_temp/native-full-ui'), 'settings-'))
  globalConfig = join(dir, 'global-config')
  process.env.GIT_CONFIG_GLOBAL = globalConfig
  process.env.GIT_CONFIG_NOSYSTEM = '1'
  await writeFile(
    globalConfig,
    '[user]\n\tname = Fixture Author\n\temail = fixture@example.invalid\n'
  )
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  Object.defineProperty(process.versions, 'electron', {
    configurable: true,
    value: 'fixture-electron',
  })
  ProviderStorageKey = (
    await import('../../src/lib/commit-generation/provider-store')
  ).ProviderStorageKey
  const partition = await import('../../src/native-kernel/settings-dialogs')
  dialogs = partition.settingsDialogs
  mountSettings = partition.mountSettingsDialog
  runtime = (await import('../../src/ui/app-runtime')).getAppRuntime()
})
after(async () => {
  window.close()
  if (prior.global === undefined) {
    delete process.env.GIT_CONFIG_GLOBAL
  } else {
    process.env.GIT_CONFIG_GLOBAL = prior.global
  }
  if (prior.noSystem === undefined) {
    delete process.env.GIT_CONFIG_NOSYSTEM
  } else {
    process.env.GIT_CONFIG_NOSYSTEM = prior.noSystem
  }
  if (priorElectron) {
    Object.defineProperty(process.versions, 'electron', priorElectron)
  } else {
    Reflect.deleteProperty(process.versions, 'electron')
  }
  await rm(dir, {
    recursive: true,
    force: true,
    maxRetries: 20,
    retryDelay: 100,
  })
})
async function popup(detail: Popup) {
  await runtime.dispatcher.showPopup(detail)
  const p = runtime.appStore.getState().currentPopup
  assert.ok(p)
  return p
}
const view = (p: Popup) => dialogs.project(p, runtime.appStore.getState())
const reply = (p: Popup, action: string, values: unknown = {}) =>
  dialogs.reply(runtime, p, action, values)
const field = (p: Popup, id: string) => {
  const f = view(p).fields.find(f => f.id === id)
  assert.ok(f, id)
  return f
}
async function git(path: string, args: string[]) {
  const r = await exec(args, path)
  assert.equal(r.exitCode, 0, r.stderr)
  return r.stdout.trim()
}

// This test checks the exported behaviour, not source-text matching or a replacement UI controller.
describe('native settings partition with original core', () => {
  it('keeps preference edits disabled until the original initial load completes', async () => {
    const p = await popup({ type: PopupType.Preferences })
    assert.ok(view(p).fields.every(f => f.disabled), 'initial inputs must wait for loading')
    assert.equal(view(p).actions.find(a => a.id === 'change')?.disabled, true)
    await reply(p, 'load')
    assert.equal(field(p, 'tab').disabled, undefined)
    await reply(p, 'change', { tab: 'integrations' })
    assert.equal(field(p, 'tab').value, 'integrations')
    await reply(p, 'cancel')
  })
  it('shows denied system notification permission and opens the original settings URL', async t => {
    const invoke = ipcRenderer.invoke
    t.mock.method(
      ipcRenderer,
      'invoke',
      (channel: string, ...args: unknown[]) =>
        channel === 'get-notifications-permission'
          ? Promise.resolve('denied')
          : invoke(channel, ...args)
    )
    const opened: string[] = []
    t.mock.method(runtime.dispatcher, 'openInBrowser', async (url: string) => {
      opened.push(url)
    })
    const p = await popup({ type: PopupType.Preferences })
    await reply(p, 'load')
    await reply(p, 'change', { tab: 'notifications' })
    await reply(p, 'change', { notificationsEnabled: true })
    assert.equal(
      field(p, 'notificationPermission').labelKey,
      'settings.notificationsDenied'
    )
    await reply(p, 'notificationSettings')
    assert.deepEqual(opened, ['ms-settings:notifications'])
    await reply(p, 'change', { notificationsEnabled: false })
    assert.equal(
      view(p).fields.some(f => f.id === 'notificationPermission'),
      false
    )
    await reply(p, 'cancel')
  })

  it('declares all seven settings popup types and presents every original preference area', async () => {
    assert.equal(dialogs.types.length, 7)
    const p = await popup({ type: PopupType.Preferences })
    assert.equal(view(p).actions.find(a => a.id === 'save')?.disabled, true)
    await reply(p, 'load')
    assert.equal(field(p, 'tab').choices?.length, 9)
    const layoutPages = view(p).layoutPages
    assert.equal(layoutPages?.length, 9)
    assert.equal(field(p, 'tab').value, 'accounts')
    assert.ok(
      layoutPages
        ?.find(page => page.id === 'integrations')
        ?.fields.some(field => field.id === 'customShellArguments')
    )
    assert.ok(
      layoutPages
        ?.find(page => page.id === 'prompts')
        ?.fields.some(field => field.id === 'confirmForcePush')
    )
    assert.equal(layoutPages?.find(page => page.id === 'git')?.fields.find(field => field.id === 'committerName')?.value, 'Fixture Author')
    assert.ok(layoutPages?.find(page => page.id === 'appearance')?.fields.find(field => field.id === 'theme')?.changeAction)
    const expected: Record<string, string[]> = {
      accounts: ['account'],
      integrations: [
        'selectedExternalEditor',
        'selectedShell',
        'useCustomEditor',
        'customEditorPath',
        'customEditorArguments',
        'useCustomShell',
      ],
      copilot: ['generationSource', 'generationModel', 'generationLanguage'],
      git: ['committerName', 'committerEmail', 'defaultBranch'],
      appearance: ['theme', 'tabSize', 'alwaysShowWorktreeList'],
      notifications: ['notificationsEnabled'],
      prompts: [
        'confirmDiscardChanges',
        'confirmForcePush',
        'confirmWorktreeRemoval',
        'uncommittedChangesStrategy',
      ],
      advanced: [
        'useWindowsOpenSSH',
        'useExternalCredentialHelper',
        'optOutOfUsageTracking',
        'repositoryIndicatorsEnabled',
      ],
      accessibility: ['underlineLinks', 'showDiffCheckMarks'],
    }
    for (const [tab, ids] of Object.entries(expected)) {
      await reply(p, 'change', { tab })
      for (const id of ids) {
        field(p, id)
      }
      if (tab === 'appearance') {
        // Keep storage keys stable while exposing the same readable examples
        // as the original Appearance settings.
        const choices = field(p, 'numberFormat').choices!
        assert.ok(choices.some(c => c.label === '1,234,567.89'))
        assert.ok(choices.some(c => c.label === '1.234.567,89'))
        assert.ok(choices.every(c => c.label !== c.id))
      }
    }
    await reply(p, 'cancel')
  })

  it('navigates without checking other inputs and retains hidden-page draft edits', async () => {
    const p = await popup({ type: PopupType.Preferences })
    await reply(p, 'load')
    await reply(p, 'change', { committerName: '<>' })
    const error = view(p).error
    assert.ok(error)
    await reply(p, 'navigate', { tab: 'appearance' })
    assert.equal(field(p, 'tab').value, 'appearance')
    assert.equal(view(p).error, error)
    assert.equal(view(p).layoutPages?.find(page => page.id === 'git')?.fields.find(f => f.id === 'committerName')?.value, '<>')
    await assert.rejects(reply(p, 'navigate', { tab: 'git', committerName: 'Unexpected' }))
    await assert.rejects(reply(p, 'change', { notificationsEnabled: 'true' }))
    await reply(p, 'change', { committerName: 'Restored author' })
    await reply(p, 'navigate', { tab: 'git' })
    assert.equal(field(p, 'committerName').value, 'Restored author')
    await reply(p, 'cancel')
  })

  it('rejects non-whitelisted values, typed option/checkbox mismatches and unknown actions before writes', async () => {
    const p = await popup({
      type: PopupType.Preferences,
      initialSelectedTab: PreferencesTab.Appearance,
    })
    await reply(p, 'load')
    const before = await readFile(globalConfig, 'utf8')
    await assert.rejects(reply(p, 'arbitraryMethod'), /Invalid/)
    await assert.rejects(reply(p, 'change', { theme: 'not-a-theme' }), /option/)
    await assert.rejects(
      reply(p, 'change', { alwaysShowWorktreeList: 'true' }),
      /choice/
    )
    await assert.rejects(
      reply(p, 'save', { callback: () => {}, secret: 'SECRET-SENTINEL' }),
      /fields/
    )
    assert.equal(await readFile(globalConfig, 'utf8'), before)
    await reply(p, 'cancel')
  })

  it('restores original theme/tab previews on cancel and discards unsaved Git author/language drafts', async () => {
    const initial = runtime.appStore.getState()
    const bytes = await readFile(globalConfig, 'utf8')
    const p = await popup({
      type: PopupType.Preferences,
      initialSelectedTab: PreferencesTab.Appearance,
    })
    await reply(p, 'load')
    await reply(p, 'change', {
      theme: ApplicationTheme.Dark,
      tabSize: '8',
      tab: 'git',
    })
    await reply(p, 'change', {
      committerName: 'Unsaved author',
      tab: 'copilot',
    })
    await reply(p, 'change', { generationLanguage: 'zh-Hans' })
    assert.equal(
      runtime.appStore.getState().commitGeneration?.language,
      initial.commitGeneration?.language
    )
    await reply(p, 'cancel')
    assert.equal(
      runtime.appStore.getState().selectedTheme,
      initial.selectedTheme
    )
    assert.equal(
      runtime.appStore.getState().selectedTabSize,
      initial.selectedTabSize
    )
    assert.equal(await readFile(globalConfig, 'utf8'), bytes)
  })

  it('retains the popup on real original config-lock failure, performs explicit recovery and then saves', async () => {
    const p = await popup({
      type: PopupType.Preferences,
      initialSelectedTab: PreferencesTab.Git,
    })
    await reply(p, 'load')
    await writeFile(globalConfig + '.lock', 'fixture-owned')
    await reply(p, 'save', {
      committerName: 'Saved Native Author',
      committerEmail: 'native@example.invalid',
    })
    assert.equal(runtime.appStore.getState().currentPopup?.id, p.id)
    assert.ok(view(p).actions.some(a => a.id === 'unlockGit'))
    await assert.rejects(
      reply(p, 'unlockGit', { path: 'outside-fixture' }),
      /fields/
    )
    assert.equal(
      await readFile(globalConfig + '.lock', 'utf8'),
      'fixture-owned'
    )
    await reply(p, 'unlockGit')
    await reply(p, 'save')
    assert.equal(runtime.appStore.getState().currentPopup, null)
    assert.match(await readFile(globalConfig, 'utf8'), /Saved Native Author/)
  })

  it('keeps model callbacks in the original popup, validates exact IDs/reasoning and discards on cancel', async () => {
    const saved: IBYOKModel[] = []
    let p = await popup({
      type: PopupType.EditCopilotBYOKModel,
      model: null,
      otherModelIds: ['duplicate'],
      onSave: m => {
        saved.push(m)
      },
    })
    const json = JSON.stringify(view(p))
    assert.doesNotMatch(json, /onSave|otherModelIds/)
    await reply(p, 'save', {
      modelID: 'duplicate',
      modelName: '',
      reasoningEffort: '',
    })
    assert.equal(saved.length, 0)
    assert.equal(runtime.appStore.getState().currentPopup?.id, p.id)
    await assert.rejects(
      reply(p, 'save', { modelID: 'm', reasoningEffort: 'arbitrary' }),
      /option/
    )
    await reply(p, 'save', {
      modelID: ' model:exact ',
      modelName: '',
      reasoningEffort: 'high',
    })
    assert.deepEqual(saved, [
      { id: 'model:exact', name: 'model:exact', reasoningEffort: 'high' },
    ])
    p = await popup({
      type: PopupType.EditCopilotBYOKModel,
      model: null,
      otherModelIds: [],
      onSave: m => {
        saved.push(m)
      },
    })
    await reply(p, 'cancel')
    assert.equal(saved.length, 1)
  })

  it('projects provider credentials as write-only and retains failed saves for retry', async () => {
    const p = await popup({
      type: PopupType.EditCopilotBYOKProvider,
      provider: null,
    })
    const secret = field(p, 'secret')
    assert.equal(secret.sensitive, true)
    assert.equal(secret.value, undefined)
    await reply(p, 'save', {
      name: 'Fixture',
      baseUrl: 'https://provider.invalid/v1',
      type: 'openai',
      authKind: 'apiKey',
      wireApi: 'responses',
      secret: 'SECRET-SENTINEL',
    })
    assert.ok(view(p).error)
    assert.equal(runtime.appStore.getState().currentPopup?.id, p.id)
    assert.doesNotMatch(JSON.stringify(view(p)), /SECRET-SENTINEL/)
    await reply(p, 'cancel')
  })

  it('keeps all labels/choices translated and never projects account tokens or missing model fallbacks', async () => {
    const checkLabels = (p: Popup) => {
      const dto = view(p)
      const fields = [...dto.fields, ...(dto.layoutPages?.flatMap(page => page.fields) ?? [])]
      for (const action of dto.actions) {
        assert.ok(
          Array.isArray(action.fields),
          `Missing field contract for ${p.type}/${action.id}`
        )
        assert.ok(
          action.fields.every(id =>
            fields.some(field => field.id === id && !field.readOnly)
          )
        )
        if (
          action.id === 'cancel' ||
          action.id === 'done' ||
          action.id === 'load'
        ) {
          assert.deepEqual(action.fields, [])
        }
      }
      for (const field of dto.fields) {
        if (field.changeAction) {
          assert.ok(
            dto.actions
              .find(action => action.id === field.changeAction)
              ?.fields?.includes(field.id),
            `${p.type}/${field.id}`
          )
        }
      }
      for (const label of [
        dto.titleKey,
        ...dto.fields.map(f => f.labelKey),
        ...dto.actions.map(a => a.labelKey),
        ...dto.fields.flatMap(
          f =>
            f.choices
              ?.map(c => c.label)
              .filter(l => l.startsWith('settings.')) ?? []
        ),
      ]) {
        const entry = (labels as Record<string, Record<string, string>>)[label]
        assert.ok(entry, label)
        assert.deepEqual(Object.keys(entry).sort(), [
          'en',
          'zh-Hans',
          'zh-Hant',
        ])
        assert.ok(Object.values(entry).every(text => text.trim().length > 0))
      }
    }
    const p = await popup({ type: PopupType.Preferences })
    await reply(p, 'load')
    for (const tab of field(p, 'tab').choices ?? []) {
      await reply(p, 'change', { tab: tab.id })
      checkLabels(p)
    }
    await reply(p, 'cancel')
    const account = new Account(
      'fixture',
      'https://api.example.invalid',
      'TOKEN-SENTINEL',
      [],
      '',
      700,
      'Fixture'
    )
    const q = await popup({ type: PopupType.CopilotUserSettings, account })
    assert.equal(field(q, 'commit-message-generation').value, '')
    assert.doesNotMatch(JSON.stringify(view(q)), /TOKEN-SENTINEL/)
    checkLabels(q)
    await reply(q, 'done')
  })

  it('loads original preference data on mount and notifies final local drafts without leaking a subscription', async () => {
    const p = await popup({
      type: PopupType.Preferences,
      initialSelectedTab: PreferencesTab.Git,
    })
    let updates = 0
    const handle = mountSettings(runtime, p, () => updates++)
    try {
      for (
        let i = 0;
        i < 100 && view(p).actions.find(a => a.id === 'save')?.disabled;
        i++
      ) {
        await new Promise(resolve => setTimeout(resolve, 10))
      }
      assert.equal(field(p, 'committerName').value, 'Saved Native Author')
      assert.ok(
        updates >= 2,
        'Mount/load completion must reproject without a store write'
      )
      await reply(p, 'change', { committerName: 'Unsaved local input' })
      assert.equal(field(p, 'committerName').value, 'Unsaved local input')
      handle.dispose()
      const before = updates
      // Disposing destroys the draft. A newly projected form must load again
      // before accepting edits, even when no lifecycle observer is attached.
      await reply(p, 'load')
      await reply(p, 'change', { committerName: 'Detached observer' })
      assert.equal(
        updates,
        before,
        'Disposed modal observer received another update'
      )
    } finally {
      handle.dispose()
    }
    await reply(p, 'cancel')
    assert.match(await readFile(globalConfig, 'utf8'), /Saved Native Author/)
  })

  it('retains SDK editor name/protocol/timeout drafts across reprojects and saves the original provider identity', async () => {
    const providers = await popup({ type: PopupType.CopilotCustomProviders })
    await reply(providers, 'addSDKProvider')
    let p = runtime.appStore.getState().currentPopup!
    await reply(p, 'save', {
      name: 'Original SDK',
      format: 'openai-chat',
      baseUrl: 'https://fixture.invalid/v1',
      authKind: 'none',
      requestTimeoutSeconds: '12',
    })
    const provider = runtime.appStore.getState().commitGeneration!.providers[0]
    await reply(providers, 'editProvider', { provider: `sdk:${provider.id}` })
    p = runtime.appStore.getState().currentPopup!
    let updates = 0
    const handle = mountSettings(runtime, p, () => updates++)
    try {
      await reply(p, 'change', {
        name: 'Retained SDK name',
        format: 'anthropic-messages',
        requestTimeoutSeconds: '42',
      })
      assert.equal(field(p, 'name').value, 'Retained SDK name')
      assert.equal(field(p, 'format').value, 'anthropic-messages')
      assert.equal(field(p, 'requestTimeoutSeconds').value, '42')
      assert.ok(
        field(p, 'format').choices?.every(choice =>
          choice.label.startsWith('settings.')
        )
      )
      assert.equal(field(p, 'format').changeAction, 'change')
      assert.ok(updates >= 2)
      await reply(p, 'save')
      const saved = runtime.appStore
        .getState()
        .commitGeneration!.providers.find(
          current => current.id === provider.id
        )!
      assert.equal(saved.id, provider.id)
      assert.equal(saved.name, 'Retained SDK name')
      assert.equal(saved.format, 'anthropic-messages')
      assert.equal(saved.requestTimeoutSeconds, 42)
    } finally {
      handle.dispose()
    }
    await reply(providers, 'deleteProvider', { provider: `sdk:${provider.id}` })
    await reply(runtime.appStore.getState().currentPopup!, 'delete')
    await reply(providers, 'done')
  })

  it('creates an SDK provider on the original stack and deletes it only after explicit confirmation', async () => {
    let p = await popup({ type: PopupType.CopilotCustomProviders })
    await reply(p, 'addSDKProvider')
    p = runtime.appStore.getState().currentPopup!
    assert.equal(p.type, PopupType.EditCopilotBYOKProvider)
    field(p, 'format')
    await reply(p, 'save', {
      name: 'Native SDK Draft',
      format: 'openai-chat',
      baseUrl: 'https://fixture.invalid/v1',
      authKind: 'none',
      requestTimeoutSeconds: '12',
    })
    assert.equal(
      runtime.appStore.getState().currentPopup?.type,
      PopupType.CopilotCustomProviders
    )
    const provider = runtime.appStore.getState().commitGeneration!.providers[0]
    assert.equal(provider.name, 'Native SDK Draft')
    assert.equal(provider.models.length, 0)
    p = runtime.appStore.getState().currentPopup!
    await reply(p, 'deleteProvider', { provider: `sdk:${provider.id}` })
    let confirm = runtime.appStore.getState().currentPopup!
    assert.equal(confirm.type, PopupType.ConfirmDeleteCopilotBYOKProvider)
    await reply(confirm, 'cancel')
    assert.equal(
      runtime.appStore.getState().commitGeneration!.providers.length,
      1
    )
    await reply(p, 'deleteProvider', { provider: `sdk:${provider.id}` })
    confirm = runtime.appStore.getState().currentPopup!
    await reply(confirm, 'delete')
    assert.equal(
      runtime.appStore.getState().commitGeneration!.providers.length,
      0
    )
    await reply(p, 'done')
  })

  it('does not close or submit a second time while the original OS credential write is busy', async t => {
    let p = await popup({ type: PopupType.CopilotCustomProviders })
    await reply(p, 'addSDKProvider')
    p = runtime.appStore.getState().currentPopup!
    let release: () => void = () => {}
    let entered: () => void = () => {}
    const pending = new Promise<void>(resolve => {
      release = resolve
    })
    const started = new Promise<void>(resolve => {
      entered = resolve
    })
    secretWriteGate = () => {
      entered()
      return pending
    }
    t.after(() => {
      release()
      secretWriteGate = undefined
    })
    const saving = reply(p, 'save', {
      name: 'Busy Fixture',
      format: 'openai-chat',
      baseUrl: 'https://fixture.invalid/v1',
      authKind: 'apiKey',
      secret: 'BUSY-SECRET-SENTINEL',
    })
    await started
    assert.ok(view(p).actions.every(a => a.disabled))
    assert.doesNotMatch(JSON.stringify(view(p)), /BUSY-SECRET-SENTINEL/)
    await assert.rejects(reply(p, 'save', {}), /busy/)
    await assert.rejects(reply(p, 'cancel'), /busy/)
    release()
    secretWriteGate = undefined
    await saving
    assert.equal(
      runtime.appStore.getState().currentPopup?.type,
      PopupType.CopilotCustomProviders
    )
    const provider = runtime.appStore.getState().commitGeneration!.providers[0]
    await runtime.dispatcher.deleteCommitGenerationProvider(provider.id)
    await reply(runtime.appStore.getState().currentPopup!, 'done')
  })

  it('edits actual repository remote/alias/gitignore/local Git identity via original helpers', async () => {
    const path = join(dir, 'repo-settings')
    await mkdir(path)
    await git(path, ['init', '--initial-branch=main'])
    await git(path, ['remote', 'add', 'origin', join(dir, 'local-bare.git')])
    const [repository] = await runtime.dispatcher.addRepositories([path])
    assert.ok(repository)
    await runtime.dispatcher.selectRepository(repository)
    const p = await popup({ type: PopupType.RepositorySettings, repository })
    await reply(p, 'load')
    field(p, 'remoteURL')
    field(p, 'alias')
    await reply(p, 'change', {
      remoteURL: join(dir, 'changed-local-bare.git'),
      alias: 'Native Alias',
      tab: 'ignoredFiles',
    })
    await reply(p, 'change', {
      ignoreText: '# fixture\nignored.tmp\n',
      tab: 'gitConfig',
    })
    assert.equal(field(p, 'committerName').readOnly, true)
    await reply(p, 'change', { gitConfigLocation: 'Local' })
    await reply(p, 'save', {
      committerName: 'Local Author',
      committerEmail: 'local@example.invalid',
    })
    assert.equal(runtime.appStore.getState().currentPopup, null)
    assert.equal(
      await git(path, ['remote', 'get-url', 'origin']),
      join(dir, 'changed-local-bare.git')
    )
    assert.equal(
      await git(path, ['config', '--local', 'user.name']),
      'Local Author'
    )
    assert.equal(
      await readFile(join(path, '.gitignore'), 'utf8'),
      '# fixture\nignored.tmp\n'
    )
    assert.ok(
      runtime.appStore
        .getState()
        .repositories.some(r => 'alias' in r && r.alias === 'Native Alias')
    )
  })
})

describe('native provider extensions through real core and official OpenAI SDK', () => {
  it('discovers/enables/renames without fallback, generates only reviewed draft from real Git diff, and cancels original busy state', async t => {
    const requests: Array<{
      url: string
      body: Record<string, any>
      authorization?: string
    }> = []
    let catalog = ['fixture-model']
    let hold = false
    let entered: () => void = () => {}
    let pending: ServerResponse | undefined
    const server = createServer(async (req, res) => {
      const chunks: Buffer[] = []
      for await (const chunk of req) {
        chunks.push(Buffer.from(chunk))
      }
      const body = chunks.length
        ? JSON.parse(Buffer.concat(chunks).toString())
        : {}
      requests.push({
        url: req.url ?? '',
        body,
        authorization: req.headers.authorization,
      })
      res.setHeader('Content-Type', 'application/json')
      if (req.url === '/v1/models') {
        res.end(
          JSON.stringify({
            object: 'list',
            data: catalog.map(id => ({
              id,
              object: 'model',
              created: 0,
              owned_by: 'fixture',
            })),
          })
        )
        return
      }
      if (hold) {
        pending = res
        entered()
        return
      }
      res.end(
        JSON.stringify({
          id: 'fixture-response',
          object: 'response',
          status: 'completed',
          error: null,
          incomplete_details: null,
          output: [
            {
              type: 'message',
              role: 'assistant',
              status: 'completed',
              content: [
                {
                  type: 'output_text',
                  text: '{"title":"本地生成说明","description":"仅填充待审阅草稿"}',
                  annotations: [],
                },
              ],
            },
          ],
        })
      )
    })
    await listenOnLoopback(server)
    t.after(() => {
      pending?.destroy()
      server.closeAllConnections()
      server.close()
    })
    const baseUrl = `http://127.0.0.1:${
      (server.address() as AddressInfo).port
    }/v1`
    const provider = await runtime.dispatcher.saveCommitGenerationProvider(
      {
        name: 'SDK Fixture',
        format: 'openai-responses',
        baseUrl,
        authKind: 'apiKey',
      },
      'fake-local-key'
    )
    await runtime.dispatcher.refreshCommitGenerationModels(provider.id)
    assert.equal(
      runtime.appStore.getState().commitGeneration?.enabledModels.length,
      0
    )
    await runtime.dispatcher.setCommitGenerationModelEnabled(
      provider.id,
      'fixture-model',
      true
    )
    await runtime.dispatcher.selectCommitGenerationModel('custom', {
      providerId: provider.id,
      modelId: 'fixture-model',
    })
    const renamed = await runtime.dispatcher.saveCommitGenerationProvider({
      ...provider,
      name: 'Renamed Fixture',
    })
    assert.equal(renamed.id, provider.id)
    assert.deepEqual(runtime.appStore.getState().commitGeneration?.selection, {
      providerId: provider.id,
      modelId: 'fixture-model',
    })
    catalog.push('new-model')
    await runtime.dispatcher.refreshCommitGenerationModels(provider.id)
    assert.equal(
      runtime.appStore
        .getState()
        .commitGeneration?.providers[0].models.find(m => m.id === 'new-model')
        ?.enabled,
      false
    )
    assert.equal(
      runtime.appStore.getState().commitGeneration?.selection?.modelId,
      'fixture-model'
    )
    assert.doesNotMatch(
      localStorage.getItem(ProviderStorageKey) ?? '',
      /fake-local-key/
    )
    assert.ok(
      [...secrets.values()].some(value => value.endsWith('fake-local-key')),
      'credential writes must stay in the mocked OS store'
    )
    const path = join(dir, 'generation')
    await mkdir(path)
    await git(path, ['init', '--initial-branch=main'])
    await writeFile(join(path, 'source.txt'), 'base\n')
    await git(path, ['add', '.'])
    await git(path, ['commit', '-m', 'Fixture baseline'])
    await writeFile(join(path, 'source.txt'), 'base\n+ real local change\n')
    const [repository] = await runtime.dispatcher.addRepositories([path])
    await runtime.dispatcher.selectRepository(repository)
    await runtime.appStore._loadStatus(repository)
    const files =
      runtime.repositoryStateManager.get(repository).changesState
        .workingDirectory.files
    assert.ok(files.length > 0)
    const head = await git(path, ['rev-parse', 'HEAD'])
    await runtime.dispatcher.setCommitMessage(repository, {
      summary: 'Human draft',
      description: '',
      timestamp: Date.now(),
    })
    runtime.appStore._updateCommitMessageGenerationDisclaimerLastSeen()
    runtime.dispatcher.setCommitGenerationLanguage('zh-Hans')
    assert.equal(
      await runtime.appStore._generateCommitMessage(repository, files),
      true
    )
    const draft =
      runtime.repositoryStateManager.get(repository).changesState.commitMessage
    assert.equal(draft.summary, '本地生成说明')
    assert.equal(
      await git(path, ['rev-parse', 'HEAD']),
      head,
      'generation must not create a commit'
    )
    const wire = requests.find(r => r.url === '/v1/responses')!
    assert.equal(wire.authorization, 'Bearer fake-local-key')
    assert.equal(wire.body.model, 'fixture-model')
    assert.equal(wire.body.store, false)
    assert.match(wire.body.instructions, /zh-Hans/)
    assert.match(wire.body.input, /real local change/)
    hold = true
    const started = new Promise<void>(resolve => {
      entered = resolve
    })
    const generating = runtime.appStore._generateCommitMessage(
      repository,
      files
    )
    await started
    assert.equal(
      runtime.repositoryStateManager.get(repository).isGeneratingCommitMessage,
      true
    )
    await runtime.appStore._cancelGenerateCommitMessage(repository)
    assert.equal(await generating, false)
    assert.equal(
      runtime.repositoryStateManager.get(repository).isGeneratingCommitMessage,
      false
    )
    assert.equal(
      runtime.repositoryStateManager.get(repository)
        .commitMessageGenerationAbortController,
      null
    )
    assert.equal(
      runtime.repositoryStateManager.get(repository).changesState.commitMessage
        .summary,
      draft.summary
    )
    catalog = ['new-model']
    await runtime.dispatcher.refreshCommitGenerationModels(provider.id)
    assert.equal(runtime.appStore.getState().commitGeneration?.selection, null)
    const count = requests.length
    assert.equal(
      await runtime.appStore._generateCommitMessage(repository, files),
      false
    )
    assert.equal(
      requests.length,
      count,
      'expired selection must not silently pick new model/Copilot'
    )
    await runtime.dispatcher.deleteCommitGenerationProvider(provider.id)
    assert.equal(
      runtime.appStore.getState().commitGeneration?.providers.length,
      0
    )
  })
})

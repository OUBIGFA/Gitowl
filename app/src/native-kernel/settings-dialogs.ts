import { IAppState, SelectionType } from '../lib/app-state'
import {
  supportsNotifications,
  supportsNotificationsPermissionRequest,
  getNotificationSettingsUrl,
} from 'desktop-notifications'
import {
  getNotificationsPermission,
  requestNotificationsPermission,
} from '../ui/main-process-proxy'
import { Disposable } from 'event-kit'
import { displayDialogURL as safeURL } from '../ui/lib/dialog-url'
import type { AppRuntime } from '../ui/app-runtime'
import { Popup, PopupType } from '../models/popup'
import { PreferencesTab } from '../models/preferences'
import type {
  IPreferencesProps,
  IPreferencesState,
} from '../ui/preferences/preferences'
import {
  loadPreferences,
  savePreferences,
} from '../ui/preferences/preferences-controller'
import type { IRepositorySettingsState } from '../ui/repository-settings/repository-settings'
import {
  GitConfigLocation,
  RepositorySettingsTab,
  loadRepositorySettings,
  saveRepositorySettings,
} from '../ui/repository-settings/settings-controller'
import { ApplicationTheme } from '../ui/lib/application-theme'
import { formatNumber } from '../lib/format-number'
import type { IRemote } from '../models/remote'
import {
  Repository,
  getForkContributionTarget,
  isRepositoryWithForkedGitHubRepository,
} from '../models/repository'
import { ForkContributionTarget } from '../models/workflow-preferences'
import { UncommittedChangesStrategy } from '../models/uncommitted-changes-strategy'
import {
  getCacheHooksEnv,
  getGitHookEnvShell,
  getHooksEnvEnabled,
  shellFriendlyNames,
} from '../lib/hooks/config'
import { TargetPathArgument } from '../lib/custom-integration'
import {
  dateFormats,
  timeFormats,
  numberFormats,
  numberFormatToKey,
  getDateFormatPreference,
  getTimeFormatPreference,
  getNumberFormatPreference,
} from '../models/formatting-preferences'
import {
  gitAuthorNameIsValid,
  InvalidGitAuthorNameMessage,
} from '../ui/lib/identifier-rules'
import { deleteGitAuthorConfigLock } from '../ui/lib/git-author-configuration'
import {
  IBYOKModel,
  IBYOKProvider,
  encodeModelKey,
  parseModelKey,
  requiresNewBYOKSecret,
} from '../lib/copilot/byok'
import {
  getCopilotAccountCacheKey,
  ReasoningEffortOrder,
  CopilotFeature,
} from '../lib/stores/copilot-store'
import {
  ConfiguredProvider,
  ProviderFormats,
  ProviderError,
  validateProviderSettings,
  isValidProviderBaseUrl,
  getSupportedProviderAuthKinds,
} from '../lib/commit-generation/provider-types'
import {
  enableCopilotAppHandoff,
  enableFormattingPreferences,
  enableHooksEnvironment,
  enableCopilotConflictResolution,
} from '../lib/feature-flag'
import {
  NativeDialogModule,
  NativeDialogPresentation,
  NativeDialogField,
  NativeDialogAction,
  dialogValues,
  dialogString,
  dialogBoolean,
} from './dialog-module'

const key = (name: string) => `settings.${name}`
const text = (
  id: string,
  value = '',
  extra: Partial<NativeDialogField> = {}
): NativeDialogField => ({
  id,
  labelKey: key(id),
  kind: 'text',
  value,
  ...extra,
})
const check = (id: string, value: boolean): NativeDialogField => ({
  id,
  labelKey: key(id),
  kind: 'checkbox',
  value,
})
const select = (
  id: string,
  value: string,
  choices: ReadonlyArray<{ id: string; label: string; disabled?: boolean }>
): NativeDialogField => ({
  id,
  labelKey: key(id),
  kind: 'select',
  value,
  choices,
})
// Choice labels prefixed settings.* are catalog keys, technical/account/model labels are verbatim.
const options = (ids: ReadonlyArray<string>) =>
  ids.map(id => ({ id, label: key(`option.${id}`) }))
const action = (
  id: string,
  extra: Partial<NativeDialogAction> = {}
): NativeDialogAction => ({ id, labelKey: key(id), ...extra })

const booleanPrefs = [
  'useWindowsOpenSSH',
  'showCommitLengthWarning',
  'notificationsEnabled',
  'optOutOfUsageTracking',
  'useExternalCredentialHelper',
  'confirmRepositoryRemoval',
  'confirmDiscardChanges',
  'confirmDiscardChangesPermanently',
  'confirmDiscardStash',
  'confirmCheckoutCommit',
  'confirmForcePush',
  'confirmUndoCommit',
  'askForConfirmationOnCommitFilteredChanges',
  'confirmCommitMessageOverride',
  'confirmWorktreeRemoval',
  'repositoryIndicatorsEnabled',
  'alwaysShowWorktreeList',
  'underlineLinks',
  'showDiffCheckMarks',
  'alwaysUseCopilotForConflictResolution',
  'useCustomEditor',
  'useCustomShell',
  'preferAbsoluteDates',
] as const
const promptPrefs = [
  'confirmRepositoryRemoval',
  'confirmDiscardChanges',
  'confirmDiscardChangesPermanently',
  'confirmDiscardStash',
  'confirmCheckoutCommit',
  'confirmForcePush',
  'confirmUndoCommit',
  'askForConfirmationOnCommitFilteredChanges',
  'confirmCommitMessageOverride',
  'confirmWorktreeRemoval',
  'showCommitLengthWarning',
] as const
const tabs = [
  'accounts',
  'integrations',
  'copilot',
  'git',
  'appearance',
  'notifications',
  'prompts',
  'advanced',
  'accessibility',
] as const

interface ProviderDraft {
  name: string
  type: IBYOKProvider['type']
  baseUrl: string
  wireApi: 'completions' | 'responses'
  azureApiVersion: string
  authKind: IBYOKProvider['authKind']
  requestTimeoutSeconds: string
  models: ReadonlyArray<IBYOKModel>
  format: ConfiguredProvider['format']
}
interface Frame {
  notificationPermission?: NotificationPermission
  busy: boolean
  loaded: boolean
  error: string | null
  prefs?: IPreferencesState
  repo?: IRepositorySettingsState
  initial?: IAppState
  originalRemote?: IRemote | null
  account: string
  alias?: string
  generation?: IAppState['commitGeneration']
  provider?: ProviderDraft
  model?: IBYOKModel
  selectedProvider: string
  selectedProviderModel: string
  sdk?: ConfiguredProvider | null
}
const frames = new WeakMap<Popup, Frame>()
// This metadata only identifies a native SDK edit on the ORIGINAL popup stack. No callback/state coordinator.
const sdkPopups = new Map<number, ConfiguredProvider | null>()
const observers = new WeakMap<Popup, () => void>()

/** Observe only the modal draft. Core models/accounts continue to emit their
 * original AppStore updates; the parent owns the allPopups mount lifetime. */
export function mountSettingsDialog(
  runtime: AppRuntime,
  popup: Popup,
  onChange: () => void
): Disposable {
  if (!settingsDialogs.types.includes(popup.type) || observers.has(popup)) {
    throw new Error('Invalid settings dialog lifecycle')
  }
  const initial = frame(popup, runtime.appStore.getState())
  observers.set(popup, onChange)
  const handle = new Disposable(() => {
    observers.delete(popup)
    frames.delete(popup)
    if (popup.id !== undefined) {
      sdkPopups.delete(popup.id)
    }
  })
  // React loads these controls on mount. Defer until the parent has recorded
  // the handle, otherwise a synchronous update could recursively mount it.
  if (!initial.loaded) {
    queueMicrotask(() => {
      if (observers.get(popup) === onChange) {
        void reply(runtime, popup, 'load', {}).catch(error =>
          runtime.dispatcher.postError(error)
        )
      }
    })
  }
  return handle
}

function changed(popup: Popup) {
  observers.get(popup)?.()
}

function prefsInitial(
  s: IAppState,
  popup: Extract<Popup, { type: PopupType.Preferences }>
): IPreferencesState {
  return {
    selectedIndex: popup.initialSelectedTab ?? PreferencesTab.Accounts,
    committerName: '',
    committerEmail: '',
    defaultBranch: '',
    initialCommitterName: null,
    initialCommitterEmail: null,
    initialDefaultBranch: null,
    disallowedCharactersMessage: null,
    isLoadingGitConfig: true,
    availableEditors: [],
    availableShells: [],
    useWindowsOpenSSH: s.useWindowsOpenSSH,
    showCommitLengthWarning: s.showCommitLengthWarning,
    notificationsEnabled: s.notificationsEnabled,
    optOutOfUsageTracking: s.optOutOfUsageTracking,
    useExternalCredentialHelper: s.useExternalCredentialHelper,
    confirmRepositoryRemoval: s.askForConfirmationOnRepositoryRemoval,
    confirmDiscardChanges: s.askForConfirmationOnDiscardChanges,
    confirmDiscardChangesPermanently:
      s.askForConfirmationOnDiscardChangesPermanently,
    confirmDiscardStash: s.askForConfirmationOnDiscardStash,
    confirmCheckoutCommit: s.askForConfirmationOnCheckoutCommit,
    confirmForcePush: s.askForConfirmationOnForcePush,
    confirmUndoCommit: s.askForConfirmationOnUndoCommit,
    askForConfirmationOnCommitFilteredChanges:
      s.askForConfirmationOnCommitFilteredChanges,
    confirmCommitMessageOverride: s.askForConfirmationOnCommitMessageOverride,
    confirmWorktreeRemoval: s.askForConfirmationOnWorktreeRemoval,
    uncommittedChangesStrategy: s.uncommittedChangesStrategy,
    selectedExternalEditor: s.selectedExternalEditor,
    selectedShell: s.selectedShell,
    initiallySelectedTheme: s.selectedTheme,
    initiallySelectedTabSize: s.selectedTabSize,
    useCustomEditor: s.useCustomEditor,
    customEditor: s.customEditor ?? { path: '', arguments: TargetPathArgument },
    useCustomShell: s.useCustomShell,
    customShell: s.customShell ?? { path: '', arguments: TargetPathArgument },
    copilotAppPath: s.copilotAppPath ?? '',
    repositoryIndicatorsEnabled: s.repositoryIndicatorsEnabled,
    alwaysShowWorktreeList: s.alwaysShowWorktreeList,
    underlineLinks: s.underlineLinks,
    showDiffCheckMarks: s.showDiffCheckMarks,
    selectedCopilotModelsByAccount: new Map(s.selectedCopilotModelsByAccount),
    alwaysUseCopilotForConflictResolution:
      s.alwaysUseCopilotForConflictResolution,
    enableGitHookEnv: getHooksEnvEnabled(),
    cacheGitHookEnv: getCacheHooksEnv(),
    selectedGitHookEnvShell: getGitHookEnvShell(),
    hooksPreferencesDirty: false,
    selectedDateFormat: getDateFormatPreference(),
    selectedTimeFormat: getTimeFormatPreference(),
    selectedNumberFormat: getNumberFormatPreference(),
    preferAbsoluteDates: s.preferAbsoluteDates,
  }
}
function frame(p: Popup, s: IAppState): Frame {
  let f = frames.get(p)
  if (f !== undefined) {
    return f
  }
  f = {
    busy: false,
    loaded: true,
    error: null,
    account: '',
    selectedProvider: '',
    selectedProviderModel: '',
    initial: s,
  }
  if (p.type === PopupType.Preferences) {
    f.prefs = prefsInitial(s, p)
    f.generation = s.commitGeneration
    f.loaded = false
  }
  if (p.type === PopupType.RepositorySettings) {
    f.loaded = false
    f.alias = p.repository.alias ?? ''
    f.repo = {
      selectedTab: p.initialSelectedTab ?? RepositorySettingsTab.Remote,
      remote:
        s.selectedState?.type === SelectionType.Repository &&
        s.selectedState.repository.id === p.repository.id
          ? s.selectedState.state.remote
          : null,
      ignoreText: null,
      ignoreTextHasChanged: false,
      disabled: false,
      saveDisabled: false,
      gitConfigLocation: GitConfigLocation.Global,
      initialGitConfigLocation: GitConfigLocation.Global,
      committerName: '',
      committerEmail: '',
      globalCommitterName: '',
      globalCommitterEmail: '',
      initialCommitterName: null,
      initialCommitterEmail: null,
      forkContributionTarget: getForkContributionTarget(p.repository),
      isLoadingGitConfig: true,
    }
  }
  if (p.type === PopupType.EditCopilotBYOKProvider) {
    f.sdk = p.id === undefined ? undefined : sdkPopups.get(p.id)
    const provider = p.provider
    f.provider = {
      name: f.sdk?.name ?? provider?.name ?? '',
      type: provider?.type ?? 'openai',
      baseUrl: safeURL(f.sdk?.baseUrl ?? provider?.baseUrl ?? ''),
      wireApi: provider?.wireApi ?? 'completions',
      azureApiVersion: provider?.azureApiVersion ?? '',
      authKind: f.sdk?.authKind ?? provider?.authKind ?? 'apiKey',
      requestTimeoutSeconds:
        (
          f.sdk?.requestTimeoutSeconds ?? provider?.requestTimeoutSeconds
        )?.toString() ?? '',
      models: provider?.models ?? [],
      format: f.sdk?.format ?? 'openai-responses',
    }
  }
  if (p.type === PopupType.EditCopilotBYOKModel) {
    f.model = p.model ?? { id: '', name: '' }
  }
  frames.set(p, f)
  return f
}
function close(r: AppRuntime, p: Popup) {
  if (p.id === undefined) {
    r.dispatcher.closePopup(p.type)
  } else {
    r.dispatcher.closePopupById(p.id)
    sdkPopups.delete(p.id)
  }
  frames.delete(p)
}
function accountChoices(s: IAppState) {
  return s.accounts.map(a => ({
    id: getCopilotAccountCacheKey(a),
    label: `${a.login} · ${safeURL(a.endpoint)}`,
  }))
}
function copilotFields(
  p: Extract<Popup, { type: PopupType.CopilotUserSettings }>,
  s: IAppState
): NativeDialogField[] {
  const id = getCopilotAccountCacheKey(p.account)
  const models = s.copilotModelsByAccount.get(id) ?? []
  const choices = [
    { id: '', label: key('option.chooseModel') },
    ...models.map(m => ({
      id: encodeModelKey({ kind: 'copilot', modelId: m.id }),
      label: m.name,
    })),
    ...s.byokProviders.flatMap(provider =>
      provider.models.map(m => ({
        id: encodeModelKey({
          kind: 'byok',
          providerId: provider.id,
          modelId: m.id,
        }),
        label: `${provider.name} / ${m.name}`,
      }))
    ),
  ]
  const selections = s.selectedCopilotModelsByAccount.get(id) ?? {}
  const pick = (feature: CopilotFeature) => {
    const raw = selections[feature]
    const value = raw === undefined ? '' : encodeModelKey(parseModelKey(raw))
    return select(
      feature,
      choices.some(c => c.id === value) ? value : '',
      choices
    )
  }
  const quota = s.copilotQuotaSnapshotsByAccount.get(id)
  // Whitelist numeric quota counters, not raw Copilot/account snapshots.
  const usage =
    quota === undefined || quota === null
      ? ''
      : [...quota.entries()]
          .map(
            ([name, q]) => `${name}: ${q.usedRequests}/${q.entitlementRequests}`
          )
          .join('\n')
  return [
    text('account', `${p.account.login} · ${safeURL(p.account.endpoint)}`, {
      kind: 'readonly',
    }),
    text('usage', usage, { kind: 'readonly' }),
    pick('commit-message-generation'),
    ...(enableCopilotConflictResolution()
      ? [
          pick('conflict-resolution'),
          check(
            'alwaysUseCopilotForConflictResolution',
            s.alwaysUseCopilotForConflictResolution
          ),
        ]
      : []),
  ]
}

function settingsActionFields(
  popup: Popup,
  actionID: string,
  fields: ReadonlyArray<NativeDialogField>
): ReadonlyArray<string> {
  const editable = fields.filter(
    f =>
      !f.readOnly &&
      !f.disabled &&
      !['readonly', 'progress'].includes(f.kind) &&
      (!f.sensitive || actionID === 'save')
  )
  if (['cancel', 'done', 'load', 'delete'].includes(actionID)) {
    return []
  }
  if (popup.type === PopupType.Preferences) {
    if (actionID === 'navigate') return ['tab']
    if (['signOut', 'copilotSettings'].includes(actionID)) {
      return ['account']
    }
    return ['save', 'change'].includes(actionID) ? editable.map(f => f.id) : []
  }
  if (popup.type === PopupType.RepositorySettings) {
    return ['save', 'change'].includes(actionID) ? editable.map(f => f.id) : []
  }
  if (popup.type === PopupType.CopilotUserSettings) {
    return actionID === 'change' ? editable.map(f => f.id) : []
  }
  if (popup.type === PopupType.CopilotCustomProviders) {
    if (
      ['editProvider', 'deleteProvider', 'refreshModels'].includes(actionID)
    ) {
      return ['provider']
    }
    if (actionID.startsWith('saveDeployments:')) {
      return [actionID.replace('saveDeployments:', 'deployments:')]
    }
    return actionID === 'change'
      ? editable
          .filter(f => f.id === 'provider' || f.id.startsWith('enabled:'))
          .map(f => f.id)
      : []
  }
  return editable.map(f => f.id)
}

// Project any original preference page without changing the active tab. The
// sizing pass uses an isolated Frame copy and never sends values to native UI.
function projectPreferencePage(s: IAppState, ps: IPreferencesState, f: Frame) {
  const fields: NativeDialogField[] = []
  const actions: NativeDialogAction[] = []
  switch (ps.selectedIndex) {
    case PreferencesTab.Accounts:
      fields.push(
        select('account', f.account, [
          { id: '', label: key('option.chooseAccount') },
          ...accountChoices(s),
        ])
      )
      actions.push(
        action('signIn'),
        action('enterpriseSignIn'),
        action('signOut', { destructive: true, disabled: f.account === '' })
      )
      break
    case PreferencesTab.Integrations:
      fields.push(
        {
          ...select('selectedExternalEditor', ps.selectedExternalEditor ?? '', [
            { id: '', label: key('noExternalEditor') },
            ...ps.availableEditors.map(id => ({ id, label: id })),
          ]),
          disabled: ps.useCustomEditor,
        },
        select(
          'selectedShell',
          ps.selectedShell,
          ps.availableShells.map(id => ({ id, label: id }))
        )
      )
      for (const kind of ['Editor', 'Shell'] as const) {
        const integration = kind === 'Editor' ? ps.customEditor : ps.customShell
        fields.push(
          check(
            `useCustom${kind}`,
            kind === 'Editor' ? ps.useCustomEditor : ps.useCustomShell
          ),
          text(`custom${kind}Path`, integration.path),
          text(`custom${kind}Arguments`, integration.arguments ?? ''),
          ...(__DARWIN__
            ? [text(`custom${kind}BundleID`, integration.bundleID ?? '')]
            : [])
        )
      }
      if (enableCopilotAppHandoff()) {
        fields.push(text('copilotAppPath', ps.copilotAppPath))
      }
      break
    case PreferencesTab.Copilot:
      fields.push(
        select('account', f.account, [
          { id: '', label: key('option.chooseAccount') },
          ...accountChoices(s),
        ])
      )
      if (s.commitGeneration) {
        const g = f.generation ?? s.commitGeneration
        const valid =
          g.selection !== null &&
          s.commitGeneration.enabledModels.some(
            m =>
              m.providerId === g.selection!.providerId &&
              m.modelId === g.selection!.modelId
          )
        if (g.selection !== null && !valid) {
          f.generation = { ...g, selection: null }
        }
        fields.push(
          select('generationSource', g.source, options(['copilot', 'custom'])),
          select(
            'generationModel',
            valid ? `${g.selection!.providerId}:${g.selection!.modelId}` : '',
            [
              { id: '', label: key('option.chooseModel') },
              ...s.commitGeneration.enabledModels.map(m => ({
                id: `${m.providerId}:${m.modelId}`,
                label: `${m.providerName} / ${m.modelName}`,
              })),
            ]
          ),
          select('generationLanguage', g.language, [
            ...options(['auto', 'en', 'zh-Hans', 'zh-Hant']),
            ...(['auto', 'en', 'zh-Hans', 'zh-Hant'].includes(g.language)
              ? []
              : [{ id: g.language, label: g.language }]),
          ])
        )
      }
      actions.push(
        action('copilotSettings', { disabled: f.account === '' }),
        action('customProviders'),
        action('copilotPlans'),
        action('copilotFeatures')
      )
      break
    case PreferencesTab.Git:
      fields.push(
        text('committerName', ps.committerName),
        text('committerEmail', ps.committerEmail),
        text('defaultBranch', ps.defaultBranch)
      )
      if (enableHooksEnvironment()) {
        fields.push(
          check('enableGitHookEnv', ps.enableGitHookEnv ?? false),
          check('cacheGitHookEnv', ps.cacheGitHookEnv ?? true),
          select(
            'selectedGitHookEnvShell',
            ps.selectedGitHookEnvShell ?? 'git-bash',
            Object.entries(shellFriendlyNames).map(([id, label]) => ({
              id,
              label,
            }))
          )
        )
      }
      actions.push(action('editGlobalGitConfig'))
      if (ps.existingLockFilePath) {
        actions.push(action('unlockGit', { destructive: true }))
      }
      break
    case PreferencesTab.Appearance:
      fields.push(
        select(
          'theme',
          s.selectedTheme,
          options(Object.values(ApplicationTheme))
        ),
        select(
          'tabSize',
          String(s.selectedTabSize),
          [1, 2, 3, 4, 5, 6, 7, 8].map(n => ({
            id: String(n),
            label: String(n),
          }))
        ),
        check('alwaysShowWorktreeList', ps.alwaysShowWorktreeList)
      )
      if (enableFormattingPreferences()) {
        fields.push(
          select(
            'dateFormat',
            ps.selectedDateFormat ?? '',
            dateFormats.map(d => ({ id: d.pattern, label: d.example }))
          ),
          select(
            'timeFormat',
            ps.selectedTimeFormat ?? '',
            timeFormats.map(d => ({ id: d.pattern, label: d.example }))
          ),
          select(
            'numberFormat',
            numberFormatToKey(
              ps.selectedNumberFormat ?? getNumberFormatPreference()
            ),
            numberFormats.map(n => ({
              id: numberFormatToKey(n),
              label: formatNumber(1234567.89, n),
            }))
          ),
          check('preferAbsoluteDates', ps.preferAbsoluteDates ?? false)
        )
      }
      break
    case PreferencesTab.Notifications:
      fields.push(check('notificationsEnabled', ps.notificationsEnabled))
      if (supportsNotifications() && ps.notificationsEnabled) {
        fields.push({
          id: 'notificationPermission',
          kind: 'readonly',
          labelKey: key(
            f.notificationPermission === 'denied'
              ? 'notificationsDenied'
              : 'notificationsConfigure'
          ),
        })
        if (
          supportsNotificationsPermissionRequest() &&
          f.notificationPermission === 'default'
        ) {
          actions.push(action('notificationGrant'))
        }
        if (getNotificationSettingsUrl() !== null)
          actions.push(action('notificationSettings'))
      }
      break
    case PreferencesTab.Prompts:
      fields.push(
        ...promptPrefs.map(id => check(id, ps[id])),
        select(
          'uncommittedChangesStrategy',
          ps.uncommittedChangesStrategy,
          options(Object.values(UncommittedChangesStrategy))
        )
      )
      break
    case PreferencesTab.Advanced:
      fields.push(
        ...(
          [
            'useWindowsOpenSSH',
            'useExternalCredentialHelper',
            'optOutOfUsageTracking',
            'repositoryIndicatorsEnabled',
          ] as const
        ).map(id => check(id, ps[id]))
      )
      break
    case PreferencesTab.Accessibility:
      fields.push(
        check('underlineLinks', ps.underlineLinks),
        check('showDiffCheckMarks', ps.showDiffCheckMarks)
      )
      break
  }
  return { fields, actions }
}

function project(p: Popup, s: IAppState): NativeDialogPresentation {
  const f = frame(p, s)
  let fields: NativeDialogField[] = []
  let actions: NativeDialogAction[] = []
  let ps = f.prefs
  if (
    ps &&
    f.initial?.selectedCopilotModelsByAccount !==
      s.selectedCopilotModelsByAccount
  ) {
    f.prefs = {
      ...ps,
      selectedCopilotModelsByAccount: new Map(s.selectedCopilotModelsByAccount),
      alwaysUseCopilotForConflictResolution:
        s.alwaysUseCopilotForConflictResolution,
    }
    f.initial = {
      ...f.initial!,
      selectedCopilotModelsByAccount: s.selectedCopilotModelsByAccount,
    }
    ps = f.prefs
  }
  if (p.type === PopupType.Preferences && ps) {
    fields.push(select('tab', tabs[ps.selectedIndex], options(tabs)))
    const page = projectPreferencePage(s, ps, f)
    fields.push(...page.fields)
    actions.push(...page.actions)
    actions.push(
      action('navigate', { labelKey: key('tab') }),
      action('change'),
      action('save', { primary: true, disabled: !f.loaded }),
      action('cancel')
    )
    if (!f.loaded) {
      actions.unshift(action('load'))
    }
  } else if (p.type === PopupType.RepositorySettings && f.repo) {
    const rs = f.repo
    const repoTabs = [
      'remote',
      'ignoredFiles',
      'gitConfig',
      ...(isRepositoryWithForkedGitHubRepository(p.repository)
        ? ['forkSettings']
        : []),
    ]
    fields.push(select('tab', repoTabs[rs.selectedTab], options(repoTabs)))
    switch (rs.selectedTab) {
      case RepositorySettingsTab.Remote:
        if (rs.remote) {
          fields.push(text('remoteURL', safeURL(rs.remote.url)))
        } else {
          actions.push(action('publish'))
        }
        fields.push(text('alias', f.alias ?? ''))
        if (p.repository.gitHubRepository) {
          const login =
            rs.selectedLogin === undefined
              ? p.repository.login
              : rs.selectedLogin
          const accounts = s.accounts.filter(
            a => a.endpoint === p.repository.gitHubRepository!.endpoint
          )
          const signedOut =
            login !== null &&
            !accounts.some(a => a.login.toLowerCase() === login.toLowerCase())
          fields.push(
            select('repositoryLogin', login ?? '', [
              { id: '', label: key('option.unassigned') },
              ...(signedOut
                ? [{ id: login, label: login, disabled: true }]
                : []),
              ...accounts.map(a => ({ id: a.login, label: a.login })),
            ])
          )
          if (signedOut) {
            fields.push(text('signedOutAccount', login, { kind: 'readonly' }))
          }
        }
        break
      case RepositorySettingsTab.IgnoredFiles:
        fields.push(
          text('ignoreText', rs.ignoreText ?? '', {
            kind: 'multiline',
            height: 240,
          })
        )
        actions.push(action('ignoreExamples'))
        break
      case RepositorySettingsTab.GitConfig:
        fields.push(
          select(
            'gitConfigLocation',
            rs.gitConfigLocation,
            options(Object.values(GitConfigLocation))
          ),
          text(
            'committerName',
            rs.gitConfigLocation === GitConfigLocation.Global
              ? rs.globalCommitterName
              : rs.committerName,
            { readOnly: rs.gitConfigLocation === GitConfigLocation.Global }
          ),
          text(
            'committerEmail',
            rs.gitConfigLocation === GitConfigLocation.Global
              ? rs.globalCommitterEmail
              : rs.committerEmail,
            { readOnly: rs.gitConfigLocation === GitConfigLocation.Global }
          )
        )
        break
      case RepositorySettingsTab.ForkSettings:
        fields.push(
          select(
            'forkContributionTarget',
            rs.forkContributionTarget,
            options(Object.values(ForkContributionTarget))
          )
        )
        break
    }
    actions.push(
      action('change'),
      action('save', { primary: true, disabled: !f.loaded }),
      action('cancel')
    )
    if (!f.loaded) {
      actions.unshift(action('load'))
    }
  } else if (p.type === PopupType.EditCopilotBYOKProvider && f.provider) {
    const d = f.provider
    const sdk = f.sdk
    if (sdk !== undefined) {
      fields.push(
        text('name', d.name),
        select('format', d.format, options(ProviderFormats)),
        text('baseUrl', safeURL(d.baseUrl)),
        select(
          'authKind',
          getSupportedProviderAuthKinds(d.format).includes(d.authKind)
            ? d.authKind
            : '',
          [
            {
              id: '',
              label: key('option.chooseAuthentication'),
              disabled: true,
            },
            ...options(getSupportedProviderAuthKinds(d.format)),
          ]
        ),
        text('requestTimeoutSeconds', d.requestTimeoutSeconds)
      )
    } else {
      fields.push(
        text('name', d.name),
        select('type', d.type, options(['openai', 'azure', 'anthropic'])),
        text('baseUrl', safeURL(d.baseUrl)),
        select('wireApi', d.wireApi, options(['completions', 'responses'])),
        text('azureApiVersion', d.azureApiVersion),
        select('authKind', d.authKind, options(['apiKey', 'bearer', 'none'])),
        text('requestTimeoutSeconds', d.requestTimeoutSeconds)
      )
      fields.push(
        select('model', f.selectedProviderModel, [
          { id: '', label: key('option.chooseModel') },
          ...d.models.map((m, index) => ({
            id: String(index),
            label: `${m.name} / ${m.id}`,
          })),
        ])
      )
      actions.push(
        action('addModel'),
        action('editModel'),
        action('removeModel')
      )
    }
    // Write-only secret. NEVER read keytar for a dialog, never retain it in Frame or reflect its reply.
    fields.push({
      id: 'secret',
      labelKey: key('secret'),
      kind: 'text',
      sensitive: true,
    })
    actions.push(
      action('change'),
      action('save', { primary: true }),
      action('cancel')
    )
  } else if (p.type === PopupType.EditCopilotBYOKModel) {
    fields.push(
      text('modelID', f.model!.id, { required: true }),
      text('modelName', f.model!.name),
      select('reasoningEffort', f.model!.reasoningEffort ?? '', [
        { id: '', label: key('option.defaultReasoning') },
        ...options(ReasoningEffortOrder),
      ])
    )
    actions.push(action('save', { primary: true }), action('cancel'))
  } else if (p.type === PopupType.CopilotUserSettings) {
    fields = copilotFields(p, s)
    actions.push(
      action('change'),
      action('refreshCopilot'),
      action('customProviders'),
      action('customInstructions'),
      action('generationHelp'),
      action('done', { primary: true })
    )
  } else if (p.type === PopupType.CopilotCustomProviders) {
    const selectedSDK =
      s.commitGeneration?.providers.some(
        provider => `sdk:${provider.id}` === f.selectedProvider
      ) ?? false
    const selectedBYOK = s.byokProviders.some(
      provider => `byok:${provider.id}` === f.selectedProvider
    )
    if (!selectedSDK && !selectedBYOK) {
      f.selectedProvider = ''
    }
    fields.push(
      select('provider', f.selectedProvider, [
        { id: '', label: key('option.chooseProvider') },
        ...s.byokProviders.map(b => ({ id: `byok:${b.id}`, label: b.name })),
        ...(s.commitGeneration?.providers ?? []).map(b => ({
          id: `sdk:${b.id}`,
          label: b.name,
        })),
      ])
    )
    actions.push(
      action('addProvider'),
      ...(s.commitGeneration ? [action('addSDKProvider')] : []),
      action('editProvider'),
      action('deleteProvider', { destructive: true }),
      action('refreshModels'),
      action('done', { primary: true })
    )
    // Each model is individually editable; disabled models remain visible here, never on the home picker.
    for (const provider of s.commitGeneration?.providers ?? []) {
      for (const m of provider.models) {
        fields.push(
          {
            ...check(`enabled:${provider.id}:${m.id}`, m.enabled),
            labelKey: key('modelEnabled'),
            disabled: !m.available,
            ...(m.available ? { changeAction: 'change' } : {}),
          },
          text(
            `available:${provider.id}:${m.id}`,
            `${provider.name} / ${m.name}`,
            {
              labelKey: key(
                m.available ? 'modelAvailable' : 'modelUnavailable'
              ),
              kind: 'readonly',
              disabled: !m.available || !m.enabled,
            }
          )
        )
      }
      if (provider.format === 'azure-openai-v1') {
        fields.push(
          text(
            `deployments:${provider.id}`,
            provider.models
              .filter(m => m.available)
              .map(m => m.id)
              .join('\n'),
            { labelKey: key('deployments'), kind: 'multiline' }
          )
        )
        actions.push(
          action(`saveDeployments:${provider.id}`, {
            labelKey: key('saveDeployments'),
          })
        )
      }
    }
    actions.push(action('change'))
  } else if (p.type === PopupType.ConfirmDeleteCopilotBYOKProvider) {
    fields.push(text('name', p.provider.name, { kind: 'readonly' }))
    actions.push(
      action('delete', { primary: true, destructive: true }),
      action('cancel')
    )
  } else {
    throw new Error('Unsupported settings dialog')
  }
  if (f.busy) {
    fields.push({ id: 'busy', labelKey: key('busy'), kind: 'progress' })
  }
  const presentField = (field: NativeDialogField): NativeDialogField => ({
    ...field,
    ...(!field.readOnly &&
    !field.sensitive &&
    ['text', 'multiline', 'checkbox', 'select'].includes(field.kind) &&
    actions.some(a => a.id === 'change')
      ? {
          changeAction:
            p.type === PopupType.Preferences && field.id === 'tab'
              ? 'navigate'
              : 'change',
        }
      : {}),
    ...(f.busy || !f.loaded ? { disabled: true } : {}),
  })
  const presentAction = (
    a: NativeDialogAction,
    pageFields: ReadonlyArray<NativeDialogField>
  ): NativeDialogAction => ({
    ...a,
    fields: settingsActionFields(p, a.id, pageFields),
    ...(f.busy || (!f.loaded && a.id !== 'load' && a.id !== 'cancel')
      ? { disabled: true }
      : {}),
  })
  const layoutPages =
    p.type === PopupType.Preferences && ps
      ? tabs.map((id, selectedIndex) => {
          const page = projectPreferencePage(s, { ...ps, selectedIndex }, f)
          return {
            id,
            fields: page.fields.map(presentField),
            actions: page.actions.map(a => presentAction(a, page.fields)),
          }
        })
      : undefined
  const allFields = [
    ...new Map(
      [...fields, ...(layoutPages?.flatMap(page => page.fields) ?? [])].map(
        field => [field.id, field]
      )
    ).values(),
  ]
  return {
    popupID: p.id ?? null,
    type: p.type,
    supported: true,
    layout: 'form',
    titleKey: key(`title.${p.type}`),
    fields: fields.map(presentField),
    actions: actions.map(a =>
      presentAction(
        a,
        p.type === PopupType.Preferences && ['change', 'save'].includes(a.id)
          ? allFields
          : fields
      )
    ),
    error:
      f.error ??
      (ps?.copilotAppPathError
        ? key('error.invalidCopilotPath')
        : ps?.disallowedCharactersMessage
        ? key('error.invalidAuthor')
        : f.repo?.errors?.length
        ? key('error.repositorySaveFailed')
        : null),
    ...(layoutPages ? { layoutPages } : { width: 760, height: 640 }),
  }
}

function choice<T extends string>(
  value: unknown,
  allowed: ReadonlyArray<T>
): T {
  const v = dialogString(value)
  const match = allowed.find(x => x === v)
  if (match === undefined) {
    throw new Error('Invalid settings option')
  }
  return match
}
function applyPreferences(
  r: AppRuntime,
  f: Frame,
  v: Readonly<Record<string, unknown>>
) {
  let ps = f.prefs!
  for (const id of booleanPrefs) {
    if (v[id] !== undefined) {
      ps = { ...ps, [id]: dialogBoolean(v[id]) }
    }
  }
  for (const id of [
    'committerName',
    'committerEmail',
    'defaultBranch',
    'copilotAppPath',
  ] as const) {
    if (v[id] !== undefined) {
      ps = { ...ps, [id]: dialogString(v[id]) }
    }
  }
  if (v.tab !== undefined) {
    ps = { ...ps, selectedIndex: tabs.indexOf(choice(v.tab, tabs)) }
  }
  if (v.account !== undefined) {
    f.account = choice(v.account, [
      '',
      ...accountChoices(r.appStore.getState()).map(a => a.id),
    ])
  }
  if (v.selectedExternalEditor !== undefined) {
    ps = {
      ...ps,
      selectedExternalEditor:
        v.selectedExternalEditor === ''
          ? null
          : choice(v.selectedExternalEditor, ps.availableEditors),
    }
  }
  if (v.selectedShell !== undefined) {
    ps = { ...ps, selectedShell: choice(v.selectedShell, ps.availableShells) }
  }
  if (v.uncommittedChangesStrategy !== undefined) {
    ps = {
      ...ps,
      uncommittedChangesStrategy: choice(
        v.uncommittedChangesStrategy,
        Object.values(UncommittedChangesStrategy)
      ),
    }
  }
  if (v.dateFormat !== undefined) {
    ps = {
      ...ps,
      selectedDateFormat: choice(
        v.dateFormat,
        dateFormats.map(d => d.pattern)
      ),
    }
  }
  if (v.timeFormat !== undefined) {
    ps = {
      ...ps,
      selectedTimeFormat: choice(
        v.timeFormat,
        timeFormats.map(d => d.pattern)
      ),
    }
  }
  if (v.numberFormat !== undefined) {
    const id = choice(v.numberFormat, numberFormats.map(numberFormatToKey))
    ps = {
      ...ps,
      selectedNumberFormat: numberFormats.find(
        n => numberFormatToKey(n) === id
      )!,
    }
  }
  for (const kind of ['Editor', 'Shell'] as const) {
    let integration = kind === 'Editor' ? ps.customEditor : ps.customShell
    if (v[`custom${kind}Path`] !== undefined) {
      integration = {
        ...integration,
        path: dialogString(v[`custom${kind}Path`]),
      }
    }
    if (v[`custom${kind}Arguments`] !== undefined) {
      integration = {
        ...integration,
        arguments: dialogString(v[`custom${kind}Arguments`]),
      }
    }
    if (v[`custom${kind}BundleID`] !== undefined) {
      integration = {
        ...integration,
        bundleID: dialogString(v[`custom${kind}BundleID`]) || undefined,
      }
    }
    ps =
      kind === 'Editor'
        ? { ...ps, customEditor: integration }
        : { ...ps, customShell: integration }
  }
  for (const id of ['enableGitHookEnv', 'cacheGitHookEnv'] as const) {
    if (v[id] !== undefined) {
      ps = { ...ps, [id]: dialogBoolean(v[id]), hooksPreferencesDirty: true }
    }
  }
  if (v.selectedGitHookEnvShell !== undefined) {
    ps = {
      ...ps,
      selectedGitHookEnvShell: choice(
        v.selectedGitHookEnvShell,
        Object.keys(shellFriendlyNames)
      ),
      hooksPreferencesDirty: true,
    }
  }
  ps = {
    ...ps,
    disallowedCharactersMessage: gitAuthorNameIsValid(ps.committerName)
      ? null
      : InvalidGitAuthorNameMessage,
  }
  f.prefs = ps
  // Upstream theme/tab-width preview writes immediately and is rolled back on cancel.
  if (v.theme !== undefined) {
    r.dispatcher.setSelectedTheme(
      choice(v.theme, Object.values(ApplicationTheme))
    )
  }
  if (v.tabSize !== undefined) {
    r.dispatcher.setSelectedTabSize(
      Number(choice(v.tabSize, ['1', '2', '3', '4', '5', '6', '7', '8']))
    )
  }
}
function prefsProps(r: AppRuntime, p: Popup, f: Frame): IPreferencesProps {
  const s = r.appStore.getState()
  const initial = f.initial!
  return {
    ...initial,
    dispatcher: r.dispatcher,
    repository:
      s.selectedState?.type === SelectionType.Repository
        ? s.selectedState.repository
        : null,
    onDismissed: () => close(r, p),
    onEditGlobalGitConfig: () => r.dispatcher.editGlobalGitConfig(),
    confirmRepositoryRemoval: initial.askForConfirmationOnRepositoryRemoval,
    confirmDiscardChanges: initial.askForConfirmationOnDiscardChanges,
    confirmDiscardChangesPermanently:
      initial.askForConfirmationOnDiscardChangesPermanently,
    confirmDiscardStash: initial.askForConfirmationOnDiscardStash,
    confirmCheckoutCommit: initial.askForConfirmationOnCheckoutCommit,
    confirmForcePush: initial.askForConfirmationOnForcePush,
    confirmUndoCommit: initial.askForConfirmationOnUndoCommit,
    askForConfirmationOnCommitFilteredChanges:
      initial.askForConfirmationOnCommitFilteredChanges,
    confirmCommitMessageOverride:
      initial.askForConfirmationOnCommitMessageOverride,
    confirmWorktreeRemoval: initial.askForConfirmationOnWorktreeRemoval,
  }
}
async function showSDK(
  r: AppRuntime,
  p: Popup,
  provider: ConfiguredProvider | null,
  type:
    | PopupType.EditCopilotBYOKProvider
    | PopupType.ConfirmDeleteCopilotBYOKProvider
) {
  const shell: IBYOKProvider | null =
    provider === null
      ? null
      : {
          id: provider.id,
          name: provider.name,
          type: 'openai',
          baseUrl: provider.baseUrl,
          authKind: provider.authKind,
          models: [],
        }
  await r.dispatcher.showPopup(
    type === PopupType.EditCopilotBYOKProvider
      ? { type, provider: shell }
      : { type, provider: shell! }
  )
  const current = r.appStore.getState().currentPopup
  if (current?.type !== type || current.id === undefined || current === p) {
    throw new Error('Could not open settings dialog')
  }
  sdkPopups.set(current.id, provider)
  frames.delete(current)
  changed(current)
}

async function reply(
  r: AppRuntime,
  p: Popup,
  name: string,
  input: unknown
): Promise<void> {
  const s = r.appStore.getState()
  const f = frame(p, s)
  const view = project(p, s)
  const declared = view.actions.find(a => a.id === name)
  if (declared === undefined || declared.disabled || f.busy) {
    throw new Error('Invalid or busy settings action')
  }
  const v = dialogValues(input ?? {}, declared.fields ?? [])
  const schemaFields =
    p.type === PopupType.Preferences && ['change', 'save'].includes(name)
      ? [
          ...new Map(
            [
              ...view.fields,
              ...(view.layoutPages?.flatMap(page => page.fields) ?? []),
            ].map(field => [field.id, field])
          ).values(),
        ]
      : view.fields
  // Validate displayed option IDs at the wire boundary before ANY writes or callbacks.
  for (const field of schemaFields) {
    if (v[field.id] === undefined) {
      continue
    }
    if (field.kind === 'checkbox') {
      dialogBoolean(v[field.id])
    } else if (field.kind === 'select') {
      choice(
        v[field.id],
        field.choices
          ?.filter(c => !c.disabled || c.id === field.value)
          .map(c => c.id) ?? []
      )
    } else {
      dialogString(v[field.id])
    }
  }
  if (name === 'cancel' || name === 'done') {
    await cancel(r, p)
    return
  }
  // Navigation only selects an original draft page. It neither validates nor
  // rewrites unrelated inputs, clears errors, or owns the save/load busy flag.
  if (p.type === PopupType.Preferences && name === 'navigate') {
    const tab = choice(v.tab, tabs)
    f.prefs = { ...f.prefs!, selectedIndex: tabs.indexOf(tab) }
    changed(p)
    if (tab === 'notifications' && supportsNotifications()) {
      f.notificationPermission = await getNotificationsPermission()
      if (frames.has(p)) changed(p)
    }
    return
  }
  // Draft validation does not lock editing or cancellation. Only explicit
  // load/save/network operations own the original modal busy state.
  f.busy = name !== 'change'
  f.error = null
  changed(p)
  try {
    if (name === 'load') {
      if (p.type === PopupType.Preferences) {
        f.prefs = {
          ...f.prefs!,
          ...(await loadPreferences({ accounts: s.accounts })),
        }
        r.dispatcher.fetchCopilotModels()
        r.dispatcher.fetchCopilotQuotaSnapshots()
        if (supportsNotifications())
          f.notificationPermission = await getNotificationsPermission()
      } else if (p.type === PopupType.RepositorySettings) {
        const remote = r.repositoryStateManager.get(p.repository).remote
        f.originalRemote = remote
        f.repo = {
          ...f.repo!,
          ...(await loadRepositorySettings(p.repository)),
          remote,
        }
      }
      f.loaded = true
      return
    }
    if (p.type === PopupType.Preferences) {
      applyPreferences(r, f, v)
      const account = s.accounts.find(
        a => getCopilotAccountCacheKey(a) === f.account
      )
      if (f.generation) {
        let g = f.generation
        if (v.generationLanguage !== undefined) {
          g = {
            ...g,
            language: choice(
              v.generationLanguage,
              schemaFields
                .find(field => field.id === 'generationLanguage')
                ?.choices?.map(c => c.id) ?? []
            ),
          }
        }
        if (v.generationSource !== undefined) {
          g = {
            ...g,
            source: choice(v.generationSource, ['copilot', 'custom']),
          }
        }
        if (v.generationModel !== undefined) {
          const id = dialogString(v.generationModel)
          const model = s.commitGeneration?.enabledModels.find(
            m => `${m.providerId}:${m.modelId}` === id
          )
          g = {
            ...g,
            selection: model
              ? { providerId: model.providerId, modelId: model.modelId }
              : null,
          }
        }
        f.generation = g
      }
      switch (name) {
        case 'change':
          if (v.tab === 'notifications' && supportsNotifications())
            f.notificationPermission = await getNotificationsPermission()
          return
        case 'notificationGrant':
          await requestNotificationsPermission()
          f.notificationPermission = await getNotificationsPermission()
          return
        case 'notificationSettings': {
          const url = getNotificationSettingsUrl()
          if (url !== null) await r.dispatcher.openInBrowser(url)
          return
        }
        case 'save':
          if (!f.loaded || f.prefs!.disallowedCharactersMessage) {
            throw new Error('Invalid settings form')
          }
          await savePreferences(
            prefsProps(r, p, f),
            f.prefs!,
            change => {
              f.prefs = { ...f.prefs!, ...change }
            },
            async () => {
              const g = f.generation
              if (g) {
                const current = r.appStore.getState().commitGeneration!
                if (
                  g.source !== current.source ||
                  g.selection?.providerId !== current.selection?.providerId ||
                  g.selection?.modelId !== current.selection?.modelId
                ) {
                  await r.dispatcher.selectCommitGenerationModel(
                    g.source,
                    g.source === 'copilot' ? null : g.selection
                  )
                }
                if (g.language !== current.language) {
                  r.dispatcher.setCommitGenerationLanguage(g.language)
                }
              }
            }
          )
          return
        case 'signIn':
          close(r, p)
          await r.dispatcher.showDotComSignInDialog()
          return
        case 'enterpriseSignIn':
          close(r, p)
          await r.dispatcher.showEnterpriseSignInDialog()
          return
        case 'signOut':
          if (!account) {
            throw new Error('Invalid settings account')
          }
          await r.dispatcher.removeAccount(account)
          return
        case 'copilotSettings':
          if (!account) {
            throw new Error('Invalid settings account')
          }
          await r.dispatcher.showPopup({
            type: PopupType.CopilotUserSettings,
            account,
          })
          return
        case 'customProviders':
          await r.dispatcher.showPopup({
            type: PopupType.CopilotCustomProviders,
          })
          return
        case 'copilotPlans':
          await r.dispatcher.openInBrowser(
            'https://github.com/features/copilot/plans'
          )
          return
        case 'copilotFeatures':
          await r.dispatcher.openInBrowser(
            'https://github.com/settings/copilot/features'
          )
          return
        case 'editGlobalGitConfig':
          await r.dispatcher.editGlobalGitConfig()
          return
        case 'unlockGit':
          if (!f.prefs!.existingLockFilePath) {
            throw new Error('No settings lock')
          }
          await deleteGitAuthorConfigLock(f.prefs!.existingLockFilePath)
          f.prefs = { ...f.prefs!, existingLockFilePath: undefined }
          return
      }
    }
    if (p.type === PopupType.RepositorySettings) {
      let rs = f.repo!
      if (v.tab !== undefined) {
        rs = {
          ...rs,
          selectedTab: [
            'remote',
            'ignoredFiles',
            'gitConfig',
            'forkSettings',
          ].indexOf(dialogString(v.tab)),
        }
      }
      if (v.alias !== undefined) {
        f.alias = dialogString(v.alias).trim()
      }
      if (v.remoteURL !== undefined && rs.remote) {
        const url = dialogString(v.remoteURL)
        if (/\r|\n/.test(url)) {
          throw new Error('Invalid remote URL')
        }
        if (url !== safeURL(rs.remote.url)) {
          rs = { ...rs, remote: { ...rs.remote, url } }
        }
      }
      if (v.ignoreText !== undefined) {
        const ignoreText = dialogString(v.ignoreText)
        rs = {
          ...rs,
          ignoreText,
          ignoreTextHasChanged:
            rs.ignoreTextHasChanged || ignoreText !== rs.ignoreText,
        }
      }
      if (v.repositoryLogin !== undefined) {
        rs = { ...rs, selectedLogin: dialogString(v.repositoryLogin) || null }
      }
      if (v.gitConfigLocation !== undefined) {
        rs = {
          ...rs,
          gitConfigLocation: choice(
            v.gitConfigLocation,
            Object.values(GitConfigLocation)
          ),
        }
      }
      if (v.forkContributionTarget !== undefined) {
        rs = {
          ...rs,
          forkContributionTarget: choice(
            v.forkContributionTarget,
            Object.values(ForkContributionTarget)
          ),
        }
      }
      for (const id of ['committerName', 'committerEmail'] as const) {
        if (v[id] !== undefined) {
          rs = { ...rs, [id]: dialogString(v[id]) }
        }
      }
      f.repo = rs
      if (name === 'change') {
        return
      }
      if (name === 'publish') {
        await r.dispatcher.showPopup({
          type: PopupType.PublishRepository,
          repository: p.repository,
        })
        return
      }
      if (name === 'ignoreExamples') {
        await r.dispatcher.openInBrowser('https://git-scm.com/docs/gitignore')
        return
      }
      if (name === 'save') {
        if (!f.loaded || !gitAuthorNameIsValid(rs.committerName)) {
          throw new Error('Invalid settings form')
        }
        await saveRepositorySettings(
          {
            dispatcher: r.dispatcher,
            repository: p.repository,
            remote: f.originalRemote ?? null,
            accounts: s.accounts,
            repositoryAccount: null,
            onDismissed: () => close(r, p),
          },
          rs,
          change => {
            f.repo = { ...f.repo!, ...change }
          },
          async () => {
            // Account replacement precedes remote refresh; alias then edits that current original repository.
            if (f.alias !== (p.repository.alias ?? '')) {
              const current = r.appStore
                .getState()
                .repositories.find(
                  candidate =>
                    candidate instanceof Repository &&
                    candidate.id === p.repository.id &&
                    candidate.path === p.repository.path
                )
              if (!(current instanceof Repository)) {
                throw new Error('Repository is no longer available')
              }
              await r.dispatcher.changeRepositoryAlias(current, f.alias || null)
            }
          }
        )
        return
      }
    }
    if (p.type === PopupType.EditCopilotBYOKModel && name === 'save') {
      const id = dialogString(v.modelID ?? f.model!.id).trim()
      const modelName = dialogString(v.modelName ?? f.model!.name).trim()
      const effort = choice(
        v.reasoningEffort ?? f.model!.reasoningEffort ?? '',
        ['', ...ReasoningEffortOrder]
      )
      f.model = {
        id,
        name: modelName,
        ...(effort === '' ? {} : { reasoningEffort: effort }),
      }
      if (id === '' || p.otherModelIds.includes(id)) {
        throw new Error('Invalid or duplicate model identifier')
      }
      p.onSave({
        id,
        name: modelName || id,
        ...(effort === '' ? {} : { reasoningEffort: effort }),
      })
      close(r, p)
      return
    }
    if (p.type === PopupType.EditCopilotBYOKProvider) {
      const d = f.provider!
      for (const id of [
        'name',
        'baseUrl',
        'azureApiVersion',
        'requestTimeoutSeconds',
      ] as const) {
        if (v[id] !== undefined) {
          d[id] = dialogString(v[id])
        }
      }
      if (v.type !== undefined) {
        d.type = choice(v.type, ['openai', 'azure', 'anthropic'])
      }
      if (v.wireApi !== undefined) {
        d.wireApi = choice(v.wireApi, ['completions', 'responses'])
      }
      if (v.authKind !== undefined) {
        d.authKind = choice(v.authKind, ['apiKey', 'bearer', 'none'])
      }
      if (v.format !== undefined) {
        d.format = choice(v.format, ProviderFormats)
      }
      if (v.model !== undefined) {
        f.selectedProviderModel = dialogString(v.model)
      }
      if (name === 'change') {
        return
      }
      if (name === 'addModel' || name === 'editModel') {
        const index =
          name === 'addModel'
            ? null
            : Number(
                choice(
                  v.model,
                  d.models.map((_, i) => String(i))
                )
              )
        await r.dispatcher.showPopup({
          type: PopupType.EditCopilotBYOKModel,
          model: index === null ? null : d.models[index],
          otherModelIds: d.models.filter((_, i) => i !== index).map(m => m.id),
          onSave: model => {
            d.models =
              index === null
                ? [...d.models, model]
                : d.models.map((m, i) => (i === index ? model : m))
            changed(p)
          },
        })
        return
      }
      if (name === 'removeModel') {
        const index = Number(
          choice(
            v.model,
            d.models.map((_, i) => String(i))
          )
        )
        d.models = d.models.filter((_, i) => i !== index)
        f.selectedProviderModel = ''
        return
      }
      if (name === 'save') {
        const secret =
          v.secret === undefined || v.secret === ''
            ? undefined
            : dialogString(v.secret)
        const timeout =
          d.requestTimeoutSeconds.trim() === ''
            ? undefined
            : Number(d.requestTimeoutSeconds)
        if (
          timeout !== undefined &&
          (!Number.isFinite(timeout) || timeout <= 0)
        ) {
          throw new ProviderError('invalid-configuration')
        }
        if (f.sdk !== undefined) {
          const settings = {
            ...(f.sdk === null ? {} : { id: f.sdk.id }),
            name: d.name.trim(),
            baseUrl: d.baseUrl.trim(),
            format: d.format,
            authKind: d.authKind,
            ...(timeout === undefined
              ? {}
              : { requestTimeoutSeconds: timeout }),
          }
          validateProviderSettings(settings)
          await r.dispatcher.saveCommitGenerationProvider(settings, secret)
          close(r, p)
          return
        }
        if (
          d.name.trim() === '' ||
          !isValidProviderBaseUrl(d.baseUrl.trim()) ||
          d.models.length === 0 ||
          new Set(d.models.map(m => m.id)).size !== d.models.length
        ) {
          throw new ProviderError('invalid-configuration')
        }
        if (
          secret === undefined &&
          requiresNewBYOKSecret(d.authKind, p.provider)
        ) {
          throw new ProviderError('secret-required')
        }
        const provider: IBYOKProvider = {
          id: p.provider?.id ?? crypto.randomUUID(),
          name: d.name.trim(),
          type: d.type,
          baseUrl: d.baseUrl.trim(),
          authKind: d.authKind,
          models: d.models,
          ...(d.type === 'openai' ? { wireApi: d.wireApi } : {}),
          ...(d.type === 'azure' && d.azureApiVersion.trim()
            ? { azureApiVersion: d.azureApiVersion.trim() }
            : {}),
          ...(timeout === undefined ? {} : { requestTimeoutSeconds: timeout }),
        }
        // Original throwing control point prevents Dispatcher legacy catch/log wrappers from closing a failed save.
        if (p.provider === null) {
          await r.appStore._addCopilotBYOKProvider(provider, secret ?? null)
        } else {
          await r.appStore._updateCopilotBYOKProvider(
            provider,
            d.authKind === 'none' ? null : secret
          )
        }
        close(r, p)
        return
      }
    }
    if (p.type === PopupType.CopilotUserSettings) {
      if (name === 'change') {
        const choices = copilotFields(p, s)
        for (const feature of [
          'commit-message-generation',
          'conflict-resolution',
        ] as const) {
          if (v[feature] !== undefined) {
            const value = choice(
              v[feature],
              choices.find(c => c.id === feature)?.choices?.map(c => c.id) ?? []
            )
            r.dispatcher.setSelectedCopilotModel(
              p.account,
              feature,
              value || null
            )
          }
        }
        if (v.alwaysUseCopilotForConflictResolution !== undefined) {
          r.dispatcher.setAlwaysUseCopilotForConflictResolution(
            dialogBoolean(v.alwaysUseCopilotForConflictResolution)
          )
        }
        return
      }
      if (name === 'refreshCopilot') {
        await r.dispatcher.fetchCopilotModels()
        await r.dispatcher.fetchCopilotQuotaSnapshots()
        return
      }
      if (name === 'customProviders') {
        await r.dispatcher.showPopup({ type: PopupType.CopilotCustomProviders })
        return
      }
      const url =
        name === 'customInstructions'
          ? 'https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-custom-instructions'
          : 'https://docs.github.com/en/desktop/making-changes-in-a-branch/committing-and-reviewing-changes-to-your-project-in-github-desktop#write-a-commit-message-and-push-your-changes'
      await r.dispatcher.openInBrowser(url)
      return
    }
    if (p.type === PopupType.CopilotCustomProviders) {
      const id =
        v.provider === undefined ? f.selectedProvider : dialogString(v.provider)
      f.selectedProvider = id
      const sdk = s.commitGeneration?.providers.find(b => `sdk:${b.id}` === id)
      const byok = s.byokProviders.find(b => `byok:${b.id}` === id)
      if (name === 'addProvider') {
        await r.dispatcher.showPopup({
          type: PopupType.EditCopilotBYOKProvider,
          provider: null,
        })
        return
      }
      if (name === 'addSDKProvider') {
        await showSDK(r, p, null, PopupType.EditCopilotBYOKProvider)
        return
      }
      if (name === 'editProvider' || name === 'deleteProvider') {
        const type =
          name === 'editProvider'
            ? PopupType.EditCopilotBYOKProvider
            : PopupType.ConfirmDeleteCopilotBYOKProvider
        if (sdk) {
          await showSDK(r, p, sdk, type)
          return
        }
        if (!byok) {
          throw new Error('Invalid settings provider')
        }
        await r.dispatcher.showPopup({ type, provider: byok })
        return
      }
      if (name === 'refreshModels') {
        if (!sdk) {
          throw new Error('Select an SDK provider')
        }
        await r.dispatcher.refreshCommitGenerationModels(sdk.id)
        return
      }
      for (const provider of s.commitGeneration?.providers ?? []) {
        for (const model of provider.models) {
          const field = `enabled:${provider.id}:${model.id}`
          if (v[field] !== undefined) {
            await r.dispatcher.setCommitGenerationModelEnabled(
              provider.id,
              model.id,
              dialogBoolean(v[field])
            )
          }
        }
        if (name === `saveDeployments:${provider.id}`) {
          const ids = dialogString(v[`deployments:${provider.id}`])
            .split('\n')
            .map(id => id.trim())
            .filter(Boolean)
          await r.dispatcher.setCommitGenerationDeployments(
            provider.id,
            ids.map(id => ({ id, name: id }))
          )
          return
        }
      }
      if (name === 'change') {
        return
      }
    }
    if (
      p.type === PopupType.ConfirmDeleteCopilotBYOKProvider &&
      name === 'delete'
    ) {
      if (p.id !== undefined && sdkPopups.has(p.id)) {
        await r.dispatcher.deleteCommitGenerationProvider(p.provider.id)
      } else {
        await r.appStore._deleteCopilotBYOKProvider(p.provider.id)
      }
      close(r, p)
      return
    }
    throw new Error('Invalid settings action')
  } catch (e) {
    f.error =
      e instanceof ProviderError ? key(`error.${e.code}`) : key('error.failed')
    // Reject validation before entry; operation failures remain on the same original popup for retry/discard.
  } finally {
    f.busy = false
    changed(p)
  }
}
async function cancel(r: AppRuntime, p: Popup) {
  const f = frames.get(p)
  if (f?.busy) {
    throw new Error('Settings operation is busy')
  }
  if (p.type === PopupType.Preferences && f?.prefs) {
    const current = r.appStore.getState()
    if (current.selectedTheme !== f.prefs.initiallySelectedTheme) {
      r.dispatcher.setSelectedTheme(f.prefs.initiallySelectedTheme)
    }
    if (current.selectedTabSize !== f.prefs.initiallySelectedTabSize) {
      r.dispatcher.setSelectedTabSize(f.prefs.initiallySelectedTabSize)
    }
  }
  close(r, p)
}

/** Native feature partition; callbacks and secrets never cross its JSON projection. Initial prefs/repo load is an explicit typed action. */
export const settingsDialogs: NativeDialogModule = {
  types: [
    PopupType.Preferences,
    PopupType.RepositorySettings,
    PopupType.EditCopilotBYOKProvider,
    PopupType.EditCopilotBYOKModel,
    PopupType.CopilotUserSettings,
    PopupType.CopilotCustomProviders,
    PopupType.ConfirmDeleteCopilotBYOKProvider,
  ],
  project,
  reply,
  cancel,
}

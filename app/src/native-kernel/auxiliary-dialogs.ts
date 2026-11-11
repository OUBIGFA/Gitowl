import { projectKernelDiff } from './diff-projection'
import * as Path from 'path'
import { parse as parseURL } from 'url'
import { StringDecoder } from 'string_decoder'
import { Disposable } from 'event-kit'
import { IAppState, IPullRequestState, SelectionType } from '../lib/app-state'
import { getAccountForRepository } from '../lib/get-account-for-repository'
import { getHTMLURL, IAPIComment } from '../lib/api'
import { Account, getAccountKey } from '../models/account'
import type { PullRequest } from '../models/pull-request'
import type { ValidNotificationPullRequestReview } from '../lib/valid-notification-pull-request-review'
import type { IAccountTokenExpiration } from '../lib/credential-sessions'
import {
  getNotificationsPermission,
  requestNotificationsPermission,
  supportsNotificationsPermissionRequest,
  getNotificationSettingsUrl,
} from 'desktop-notifications'
import {
  TestNotificationFlow,
  TestNotificationFlows,
  TestNotificationType,
  TestNotificationStepKind,
  TestNotificationStepResultMap,
  loadNotificationTestStep,
  simulateTestNotification,
  backNotificationTest,
  getTestCLIAction,
  firstTestAccountKey,
  testTokenLocalDateTime,
  chosenTestTokenExpiration,
  initialAboutTestState,
  checkAboutTestUpdates,
} from '../ui/lib/dialog-development-actions'
import {
  SnapshotTestDraft,
  IEditableSnapshot,
  snapshotDefinitions,
  getInitialAccount,
  getDefaultSnapshots,
  getTestSnapshots,
  tokenBasedBillingTestPreset,
  rateLimitTestPreset,
} from '../ui/preferences/test-snapshot-controller'
import {
  getVisibleQuotaSnapshots,
  getVisibleRateLimitSnapshots,
  getUsedPercentage,
  hasTokenBasedBilling,
} from '../ui/preferences/snapshot-card-data'
import { getIconPreviewCatalog } from '../ui/octicons/icon-preview-catalog'
import { displayDialogURL, displayDialogDiagnostic } from '../ui/lib/dialog-url'
import {
  IRefCheck,
  isFailure,
  getCheckRunStepURL,
} from '../lib/ci-checks/ci-checks'
import { getAvailableEditors } from '../lib/editors/lookup'
import { suggestedExternalEditor } from '../lib/editors/shared'
import {
  enableCustomIntegration,
  enableTestMenuItems,
} from '../lib/feature-flag'
import { TargetPathArgument } from '../lib/custom-integration'
import { shell } from '../lib/app-shell'
import { isOSNoLongerSupportedByElectron } from '../lib/get-os'
import { Popup, PopupType } from '../models/popup'
import { PreferencesTab } from '../models/preferences'
import { Repository, getNonForkGitHubRepository } from '../models/repository'
import { ImageDiffType } from '../models/diff'
import { ComputedAction } from '../models/computed-action'
import { AppRuntime } from '../ui/app-runtime'
import { getName, getVersion } from '../ui/lib/app-proxy'
import { updateStore, UpdateStatus, IUpdateState } from '../ui/lib/update-store'
import {
  showCertificateTrustDialog,
  writeClipboardText,
} from '../ui/main-process-proxy'
import { copilotAppMarketingUrl } from '../lib/copilot-app'
import { ReleaseNotesUri } from '../ui/lib/releases'
import {
  BypassReason,
  BypassReasonType,
} from '../ui/secret-scanning/bypass-reason'
import { dialogMarkdownLinks } from '../ui/lib/dialog-markdown'
import { Tokenizer, TokenType } from '../lib/text-token-parser'
import { DesktopFakeRepository } from '../lib/desktop-fake-repository'
import {
  DialogLicenses,
  loadDialogLicenses,
  normalizedLicenseURL,
} from '../ui/lib/dialog-licenses'
import { applicationTermsText } from '../ui/terms-and-conditions/terms-content'
import {
  determineCheckRerunnability,
  rerunDialogChecks,
  loadDialogCheckLogs,
  reauthorizeDialog,
  submitPullRequestPreview,
  submitNotificationDialog,
  openSecretBypassDialog,
  displayDialogRelease,
  submitPushBranchDialog,
  createPullRequestWithoutPush,
} from '../ui/lib/auxiliary-dialog-actions'
import { supportsRerunningIndividualOrFailedChecks } from '../lib/endpoint-capabilities'
import { getPullRequestCommitRef } from '../models/pull-request'
import {
  NativeDialogModule,
  NativeDialogPresentation,
  NativeDialogField,
  NativeDialogAction,
  dialogValues,
  dialogString,
  dialogBoolean,
} from './dialog-module'

const developmentTypes = [
  PopupType.TestNotifications,
  PopupType.TestIcons,
  PopupType.TestAbout,
  PopupType.TestCLIAction,
  PopupType.TestCopilotSnapshotCard,
  PopupType.TestTokenExpiration,
] as const

const types = [
  PopupType.ConfirmAccountSignOut,
  PopupType.InstallGit,
  PopupType.About,
  PopupType.Acknowledgements,
  PopupType.UntrustedCertificate,
  PopupType.TermsAndConditions,
  PopupType.CLIInstalled,
  PopupType.ExternalEditorFailed,
  PopupType.CopilotAppNotFound,
  PopupType.OpenWithExternalEditor,
  PopupType.OpenShellFailed,
  PopupType.InitializeLFS,
  PopupType.LFSAttributeMismatch,
  PopupType.ReleaseNotes,
  PopupType.DeletePullRequest,
  PopupType.PushRejectedDueToMissingWorkflowScope,
  PopupType.SAMLReauthRequired,
  PopupType.MoveToApplicationsFolder,
  PopupType.ThankYou,
  PopupType.InvalidatedToken,
  PopupType.PullRequestChecksFailed,
  PopupType.CICheckRunRerun,
  PopupType.PullRequestReview,
  PopupType.UnreachableCommits,
  PopupType.StartPullRequest,
  PopupType.InstallingUpdate,
  PopupType.PullRequestComment,
  PopupType.PushProtectionError,
  PopupType.BypassPushProtection,
  PopupType.CommitProgress,
  PopupType.PushBranchCommits,
  PopupType.CreateTutorialRepository,
  PopupType.ConfirmExitTutorial,
  ...developmentTypes,
] as const

interface DevelopmentSession {
  values: Record<string, string>
  about: IUpdateState
  timer?: ReturnType<typeof setTimeout>
  settleTimer?: () => void
  flow: TestNotificationFlow | null
  stepResults: TestNotificationStepResultMap
  pullRequests: ReadonlyArray<PullRequest>
  reviews: ReadonlyArray<ValidNotificationPullRequestReview>
  comments: ReadonlyArray<IAPIComment>
  notificationRequest: number
  permission: 'default' | 'granted' | 'denied'
  accountKey: string
  expiration: IAccountTokenExpiration | null
  dateTime: string
  tokenBusy: boolean
  tokenRequest: number
  snapshot: SnapshotTestDraft
  error: string | null
  messageKey: string | null
}

function developmentSession(
  accounts: ReadonlyArray<Account>
): DevelopmentSession {
  return {
    values: {
      cliKind: 'open-repository',
      cliPath: '',
      cliURL: '',
      cliBranch: '',
    },
    about: initialAboutTestState(),
    flow: null,
    stepResults: new Map(),
    pullRequests: [],
    reviews: [],
    comments: [],
    notificationRequest: 0,
    permission: 'default',
    accountKey: firstTestAccountKey(accounts),
    expiration: null,
    dateTime: '',
    tokenBusy: false,
    tokenRequest: 0,
    snapshot: {
      ...getInitialAccount(accounts),
      snapshots: getDefaultSnapshots(),
    },
    error: null,
    messageKey: null,
  }
}

function isDevelopmentPopup(popup: Popup) {
  return developmentTypes.some(type => type === popup.type)
}

/** UI-only state previously held in React, never a second account, Git or PR store. */
interface Session {
  changed: () => void
  disposed: boolean
  dispose?: () => void
  loading: boolean
  switching: boolean
  pushing: boolean
  editors: ReadonlyArray<string>
  licenses: DialogLicenses | null
  checks: ReadonlyArray<IRefCheck>
  rerunnable: ReadonlyArray<IRefCheck>
  nonRerunnable: ReadonlyArray<IRefCheck>
  terminal: string
  tab: 'unreachable' | 'reachable'
  selectedCheckID: number | null
  sideBySide: boolean
  bypassed: Set<string>
  expandedSecrets: Set<string>
  links: ReadonlyArray<{ label: string; url: string }>
  development?: DevelopmentSession
}
const sessions = new WeakMap<Popup, Session>()

function initialSession(popup: Popup, state?: IAppState): Session {
  const checks =
    popup.type === PopupType.PullRequestChecksFailed ? popup.checks : []
  return {
    changed: () => {},
    disposed: false,
    loading: false,
    switching: false,
    pushing: false,
    editors: [],
    licenses: null,
    checks,
    rerunnable: [],
    nonRerunnable: [],
    terminal: '',
    selectedCheckID: (checks.find(isFailure) ?? checks[0])?.id ?? null,
    tab:
      popup.type === PopupType.UnreachableCommits && popup.selectedTab === 1
        ? 'reachable'
        : 'unreachable',
    sideBySide:
      popup.type === PopupType.StartPullRequest && popup.showSideBySideDiff,
    bypassed: new Set(),
    expandedSecrets: new Set(),
    links: [],
    ...(isDevelopmentPopup(popup)
      ? { development: developmentSession(state?.accounts ?? []) }
      : {}),
  }
}

/**
 * Parent mounts each original allPopups entry and disposes when it is removed,
 * not on every reproject/when covered. This preserves original React subscription
 * lifetime. Notifications carry no data; reproject the whitelisted presentation.
 */
export function mountAuxiliaryDialog(
  runtime: AppRuntime,
  popup: Popup,
  onChange: () => void
): Disposable {
  if (!types.some(type => type === popup.type) || sessions.has(popup)) {
    throw new Error('Invalid auxiliary dialog lifecycle')
  }
  const session = initialSession(popup, runtime.appStore.getState())
  session.changed = () => {
    if (!session.disposed) {
      onChange()
    }
  }
  sessions.set(popup, session)
  const disposables: Disposable[] = []
  const report = (error: unknown) => {
    if (!session.disposed) {
      runtime.dispatcher.postError(
        error instanceof Error
          ? error
          : new Error('Auxiliary dialog loading failed', { cause: error })
      )
    }
  }

  if (enableTestMenuItems() && popup.type === PopupType.TestNotifications) {
    void updateTestNotificationPermission(session).catch(report)
  }
  if (enableTestMenuItems() && popup.type === PopupType.TestTokenExpiration) {
    void loadTestTokenDetails(runtime, session)
    let accounts = runtime.appStore.getState().accounts
    disposables.push(
      runtime.appStore.onDidUpdate(state => {
        const d = session.development!
        const before = accounts.find(a => getAccountKey(a) === d.accountKey)
        const next = state.accounts.find(a => getAccountKey(a) === d.accountKey)
        accounts = state.accounts
        if (
          next === undefined &&
          (d.accountKey !== '' || accounts.length > 0)
        ) {
          d.accountKey = firstTestAccountKey(accounts)
          void loadTestTokenDetails(runtime, session)
        } else if (next !== before) {
          void loadTestTokenDetails(runtime, session)
        }
      })
    )
  }

  switch (popup.type) {
    case PopupType.CommitProgress: {
      // xterm accepts byte chunks; decode incrementally to retain split UTF-8.
      const decoder = new StringDecoder('utf8')
      const subscription = popup.subscribeToCommitOutput(chunk => {
        if (session.disposed) {
          return
        }
        for (const part of Array.isArray(chunk) ? chunk : [chunk]) {
          session.terminal +=
            typeof part === 'string' ? part : decoder.write(part)
        }
        session.changed()
      })
      disposables.push(new Disposable(subscription.unsubscribe))
      break
    }
    case PopupType.InstallingUpdate: {
      const update = () => {
        if (updateStore.state.status !== UpdateStatus.UpdateAvailable) {
          runtime.dispatcher.quitApp(false)
        }
        session.changed()
      }
      disposables.push(updateStore.onDidChange(update))
      update()
      break
    }
    case PopupType.About:
    case PopupType.ReleaseNotes:
      disposables.push(updateStore.onDidChange(session.changed))
      break
    case PopupType.PullRequestReview:
    case PopupType.PullRequestComment: {
      let generation = 0
      let emoji = runtime.appStore.getState().emoji
      const load = () => {
        const id = ++generation
        const body =
          popup.type === PopupType.PullRequestReview
            ? popup.review.body
            : popup.comment.body
        dialogMarkdownLinks(
          body,
          {
            emoji,
            repository: popup.pullRequest.base.gitHubRepository,
            markdownContext: 'PullRequestComment',
          },
          popup.pullRequest.base.gitHubRepository.htmlURL ?? undefined,
          () => session.disposed || generation !== id
        )
          .then(links => {
            if (!session.disposed && generation === id) {
              session.links = links
              session.changed()
            }
          })
          .catch(report)
      }
      load()
      disposables.push(
        runtime.appStore.onDidUpdate(state => {
          if (state.emoji !== emoji) {
            emoji = state.emoji
            load()
          }
        })
      )
      break
    }
    case PopupType.Acknowledgements:
      session.loading = true
      loadDialogLicenses(licenses => {
        if (session.disposed) {
          return
        }
        session.licenses = licenses
        session.loading = false
        session.changed()
      }).catch(report)
      break
    case PopupType.OpenWithExternalEditor:
      session.loading = true
      getAvailableEditors()
        .then(editors => {
          if (session.disposed) {
            return
          }
          session.editors = editors.map(editor => editor.editor)
          session.loading = false
          session.changed()
        })
        .catch(report)
      break
    case PopupType.CICheckRunRerun:
      session.loading = true
      determineCheckRerunnability(
        runtime.dispatcher,
        popup.repository,
        popup.localRepository,
        popup.checkRuns,
        popup.failedOnly
      )
        .then(result => {
          if (session.disposed) {
            return
          }
          session.rerunnable = result.rerunnable
          session.nonRerunnable = result.nonRerunnable
          session.loading = false
          session.changed()
        })
        .catch(report)
      break
    case PopupType.PullRequestChecksFailed: {
      let generation = 0
      let account = getAccountForRepository(
        runtime.appStore.getState().accounts,
        popup.repository
      )
      const load = () => {
        const id = ++generation
        loadDialogCheckLogs(
          runtime.appStore.getState().accounts,
          popup.repository,
          popup.pullRequest,
          popup.checks,
          () => session.disposed || generation !== id,
          (checks, loading) => {
            session.checks = checks
            session.loading = loading
            session.changed()
          }
        ).catch(report)
      }
      load()
      disposables.push(
        runtime.appStore.onDidUpdate(state => {
          const next = getAccountForRepository(state.accounts, popup.repository)
          if (next !== account) {
            account = next
            load()
          }
        })
      )
      break
    }
  }
  if (
    popup.type === PopupType.ReleaseNotes ||
    popup.type === PopupType.ThankYou
  ) {
    const state = runtime.appStore.getState()
    const release =
      popup.type === PopupType.ReleaseNotes
        ? displayDialogRelease(popup.newReleases)
        : undefined
    const notes =
      popup.type === PopupType.ThankYou
        ? popup.userContributions
        : [
            ...(release?.enhancements ?? []),
            ...(release?.bugfixes ?? []),
            ...(release?.other ?? []),
          ]
    const tokenizer = new Tokenizer(state.emoji, DesktopFakeRepository)
    const links = notes
      .flatMap(note => tokenizer.tokenize(note.message))
      .filter(token => token.kind === TokenType.Link)
      .map(token => ({ label: token.text, url: token.url }))
    session.links = links
    if (release?.pretext[0]) {
      dialogMarkdownLinks(
        release.pretext[0].message,
        { emoji: state.emoji },
        undefined,
        () => session.disposed
      )
        .then(markdownLinks => {
          if (!session.disposed) {
            session.links = [...markdownLinks, ...links]
            session.changed()
          }
        })
        .catch(report)
    }
  }
  const handle = new Disposable(() => {
    session.disposed = true
    if (session.development) {
      clearTimeout(session.development.timer)
      session.development.settleTimer?.()
      session.development.tokenRequest++
      session.development.notificationRequest++
    }
    disposables.forEach(disposable => disposable.dispose())
    sessions.delete(popup)
    if (popup.type === PopupType.InstallingUpdate) {
      runtime.dispatcher.cancelQuittingApp()
    }
  })
  session.dispose = () => handle.dispose()
  return handle
}

const key = (id: string) => `aux.${id.replace(/\.[0-9]+(?:\.[0-9]+)?$/, '')}`
function readonly(id: string, text: string): NativeDialogField {
  return { id, labelKey: key(id), kind: 'readonly', text, readOnly: true }
}
function select(
  id: string,
  value: string,
  choices: ReadonlyArray<{ id: string; label: string }>
): NativeDialogField {
  return { id, labelKey: key(id), kind: 'select', value, choices }
}
function checkbox(id: string, value: boolean): NativeDialogField {
  return { id, labelKey: key(id), kind: 'checkbox', value }
}
function button(
  id: string,
  primary = false,
  destructive = false,
  disabled = false
): NativeDialogAction {
  return { id, labelKey: key(id), primary, destructive, disabled }
}
function selectedRepository(state: IAppState) {
  return state.selectedState?.type === SelectionType.Repository
    ? state.selectedState
    : null
}
function previewState(
  state: IAppState,
  popup: Extract<Popup, { type: PopupType.StartPullRequest }>
): IPullRequestState | null {
  const selected = selectedRepository(state)
  return selected?.repository.id === popup.repository.id
    ? selected.state.pullRequestState
    : null
}
function checksText(checks: ReadonlyArray<IRefCheck>) {
  return checks
    .map(
      check =>
        `${check.name}\n${check.description}\n${
          check.conclusion ?? check.status
        }`
    )
    .join('\n\n')
}
function notificationSwitch(
  popup: Extract<
    Popup,
    { type: PopupType.PullRequestReview | PopupType.PullRequestComment }
  >
) {
  return (
    !(
      popup.type === PopupType.PullRequestReview &&
      popup.review.state === 'APPROVED'
    ) &&
    (popup.shouldChangeRepository || popup.shouldCheckoutBranch)
  )
}

function auxiliaryActionFields(
  popup: Popup,
  actionID: string,
  fields: ReadonlyArray<NativeDialogField>
): ReadonlyArray<string> {
  if (['cancel', 'close'].includes(actionID)) {
    return []
  }
  if (actionID === 'openContentLink') {
    return ['contentLink']
  }
  switch (popup.type) {
    case PopupType.ConfirmAccountSignOut:
      return ['clearAssignments']
    case PopupType.OpenWithExternalEditor:
      return fields
        .filter(
          f =>
            !f.readOnly &&
            ['editor', 'customPath', 'customArguments', 'bundleID'].includes(
              f.id
            )
        )
        .map(f => f.id)
    case PopupType.MoveToApplicationsFolder:
      return ['doNotAskAgain']
    case PopupType.PullRequestChecksFailed:
      return actionID === 'selectCheck'
        ? ['check']
        : actionID === 'viewStep'
        ? ['step']
        : []
    case PopupType.UnreachableCommits:
      return actionID === 'selectTab' ? ['tab'] : []
    case PopupType.StartPullRequest:
      return actionID === 'changeBase'
        ? ['baseBranch']
        : actionID === 'selectFile'
        ? ['file']
        : actionID === 'diffOptions'
        ? ['hideWhitespace', 'sideBySide', 'imageDiffType']
        : []
    case PopupType.BypassPushProtection:
      return ['reason']
    case PopupType.TestCLIAction:
      return ['cliChange', 'dispatchCLI'].includes(actionID)
        ? fields.filter(f => !f.readOnly).map(f => f.id)
        : []
    case PopupType.TestTokenExpiration:
      return actionID === 'chooseTestAccount'
        ? ['testAccount']
        : ['editExpiration', 'applyExpiration'].includes(actionID)
        ? ['testDateTime']
        : []
    case PopupType.TestCopilotSnapshotCard:
      return actionID === 'snapshotChange'
        ? fields
            .filter(
              f => !f.readOnly && f.kind !== 'readonly' && f.kind !== 'progress'
            )
            .map(f => f.id)
        : []
    case PopupType.TestNotifications:
      return actionID === 'chooseNotificationType'
        ? ['notificationType']
        : ['chooseNotification', 'viewNotificationItem'].includes(actionID)
        ? ['notificationItem']
        : []
    default:
      return []
  }
}

/** All production types have an explicit field whitelist and original footer precedence. */
function project(popup: Popup, state: IAppState): NativeDialogPresentation {
  const session = sessions.get(popup) ?? initialSession(popup, state)
  if (isDevelopmentPopup(popup)) {
    return projectDevelopmentDialog(popup, state, session)
  }
  const fields: NativeDialogField[] = []
  let actions: NativeDialogAction[] = [button('close', true)]
  let titleKey = key(`${popup.type}.title`)
  let descriptionKey = key(`${popup.type}.description`)
  const confirm = (id: string, destructive = false, disabled = false) => {
    actions = [
      button('cancel', destructive),
      button(id, !destructive, destructive, disabled),
    ]
  }
  switch (popup.type) {
    case PopupType.ConfirmAccountSignOut:
      fields.push(
        readonly('login', popup.account.login),
        readonly('endpoint', popup.account.friendlyEndpoint),
        readonly('repositoryCount', String(popup.repositoryCount)),
        checkbox('clearAssignments', false)
      )
      confirm('signOut')
      break
    case PopupType.InstallGit:
      fields.push(readonly('path', popup.path))
      actions = [button('installGit'), button('openWithoutGit', true)]
      break
    case PopupType.About: {
      fields.push(
        readonly('application', getName()),
        readonly('version', __DEV__ ? __SHA__.substring(0, 10) : getVersion()),
        readonly('architecture', process.arch)
      )
      const updates = state.updateState
      const canCheck = __RELEASE_CHANNEL__ !== 'development'
      const updateLabel = canCheck
        ? ['checking', 'downloading', 'latest', 'ready', 'notChecked'][
            updates.status
          ]
        : 'development'
      fields.push({
        id: 'updateStatus',
        labelKey: key(`update.${updateLabel}`),
        kind: 'readonly',
        readOnly: true,
      })
      if (!__LINUX__ && canCheck) {
        if (isOSNoLongerSupportedByElectron()) {
          fields.push({
            id: 'updateError',
            labelKey: key('update.unsupportedOS'),
            kind: 'readonly',
            readOnly: true,
          })
          actions.push(button('supportedOS'))
        } else if (!updates.lastSuccessfulCheck) {
          fields.push({
            id: 'updateError',
            labelKey: key('update.missingLastChecked'),
            kind: 'readonly',
            readOnly: true,
          })
        }
      }
      if (updates.lastSuccessfulCheck !== null) {
        fields.push(
          readonly('lastChecked', updates.lastSuccessfulCheck.toISOString())
        )
      }
      actions.push(
        button('releaseNotes'),
        button('terms'),
        button('acknowledgements'),
        button('copilotTransparency')
      )
      if (__RELEASE_CHANNEL__ !== 'beta') {
        actions.push(button('betaChannel'))
      }
      if (canCheck) {
        if (updates.status === UpdateStatus.UpdateReady) {
          actions.push(button('quitAndInstall'))
        } else {
          actions.push(
            button(
              'checkUpdates',
              false,
              false,
              ![
                UpdateStatus.UpdateNotChecked,
                UpdateStatus.UpdateNotAvailable,
              ].includes(updates.status) || isOSNoLongerSupportedByElectron()
            )
          )
        }
      }
      break
    }
    case PopupType.Acknowledgements: {
      const licenses = session.licenses
      if (licenses === null) {
        fields.push({
          id: 'loading',
          labelKey: key('loading'),
          kind: 'progress',
        })
      } else {
        const desktop = licenses[`desktop@${getVersion()}`]
        if (desktop?.sourceText) {
          fields.push(readonly('license', desktop.sourceText))
        }
        for (const [index, name] of Object.keys(licenses).sort().entries()) {
          if (index === 0) {
            continue
          }
          const license = licenses[name]
          fields.push(
            readonly(
              `license.${index}`,
              `${name}\n${
                license.sourceText ??
                (license.license
                  ? `License: ${license.license}`
                  : 'Unknown license')
              }`
            )
          )
          if (license.repository) {
            actions.push(button(`licenseRepository.${index}`))
          }
        }
      }
      actions.push(button('website'), button('source'))
      break
    }
    case PopupType.UntrustedCertificate:
      fields.push(
        readonly('host', parseURL(popup.url).hostname ?? ''),
        readonly('certificate', popup.certificate.subjectName)
      )
      confirm('viewCertificate', true)
      if (!__DARWIN__) {
        actions[1] = { ...actions[1], labelKey: key('addCertificate') }
      }
      break
    case PopupType.TermsAndConditions:
      fields.push(readonly('legalTerms', applicationTermsText()))
      actions.push(
        button('contact'),
        button('logos'),
        button('privacy'),
        button('creativeCommons')
      )
      break
    case PopupType.CLIInstalled:
      fields.push(readonly('path', '/usr/local/bin/github'))
      actions[0] = { ...actions[0], labelKey: key('ok') }
      break
    case PopupType.ExternalEditorFailed:
      fields.push(
        readonly(
          'diagnostic',
          displayDialogDiagnostic(popup.message, state.accounts)
        )
      )
      // App passes openPreferences as viewPreferences. It wins over download.
      if (popup.openPreferences) {
        actions.push(button('preferences'))
      } else if (popup.suggestDefaultEditor) {
        fields.push(readonly('editorName', suggestedExternalEditor.name))
        actions.push(button('downloadEditor'))
      }
      break
    case PopupType.CopilotAppNotFound:
      actions.push(button('downloadCopilot'), button('preferences'))
      break
    case PopupType.OpenWithExternalEditor:
      fields.push(
        select(
          'editor',
          session.editors[0] ?? (enableCustomIntegration() ? 'other' : ''),
          [
            ...session.editors.map(editor => ({ id: editor, label: editor })),
            ...(enableCustomIntegration()
              ? [{ id: 'other', label: key('customEditor') }]
              : []),
          ]
        )
      )
      if (enableCustomIntegration()) {
        fields.push(
          {
            id: 'customPath',
            labelKey: key('customPath'),
            kind: 'text',
            value: '',
          },
          {
            id: 'customArguments',
            labelKey: key('customArguments'),
            kind: 'text',
            value: TargetPathArgument,
          }
        )
      }
      confirm('openEditor', false, session.loading)
      if (__DARWIN__ && enableCustomIntegration()) {
        fields.push({
          id: 'bundleID',
          labelKey: key('bundleID'),
          kind: 'text',
          value: '',
        })
      }
      break
    case PopupType.OpenShellFailed:
      fields.push(
        readonly(
          'diagnostic',
          displayDialogDiagnostic(popup.message, state.accounts)
        )
      )
      actions.push(button('preferences'))
      break
    case PopupType.InitializeLFS:
      fields.push(
        readonly('repositoryCount', String(popup.repositories.length))
      )
      if (popup.repositories.length <= 10) {
        fields.push(
          readonly(
            'repositories',
            popup.repositories.map(repository => repository.path).join('\n')
          )
        )
      }
      confirm('initializeLFS')
      actions[0] = { ...actions[0], labelKey: key('notNow') }
      actions.push(button('lfsWebsite'))
      break
    case PopupType.LFSAttributeMismatch:
      confirm('updateFilters')
      actions[0] = { ...actions[0], labelKey: key('notNow') }
      actions.push(button('editGlobalGitConfig'))
      break
    case PopupType.ReleaseNotes: {
      const release = displayDialogRelease(popup.newReleases)
      if (release) {
        fields.push(
          readonly('version', release.latestVersion),
          readonly('published', release.datePublished),
          readonly('pretext', release.pretext[0]?.message ?? ''),
          readonly(
            'enhancements',
            release.enhancements.map(note => note.message).join('\n')
          ),
          readonly(
            'bugfixes',
            release.bugfixes.map(note => note.message).join('\n')
          ),
          readonly('other', release.other.map(note => note.message).join('\n'))
        )
      }
      if (
        popup.newReleases[0]?.latestVersion !== undefined &&
        popup.newReleases[0].latestVersion !== __APP_VERSION__
      ) {
        actions.push(button('installAndRestart', false, true))
      }
      actions.push(button('releaseNotes'))
      break
    }
    case PopupType.PushBranchCommits: {
      const publish = popup.unPushedCommits === undefined
      titleKey = key(publish ? 'publishBranch.title' : 'pushCommits.title')
      descriptionKey = key(
        publish ? 'publishBranch.description' : 'pushCommits.description'
      )
      fields.push(readonly('branch', popup.branch.name))
      if (popup.unPushedCommits !== undefined) {
        fields.push(readonly('unpushedCommits', String(popup.unPushedCommits)))
      }
      actions = publish
        ? [button('cancel'), button('publishBranch', true)]
        : [button('withoutPush'), button('pushCommits', true)]
      if (session.pushing) {
        actions = actions.map(a => ({ ...a, disabled: true }))
        fields.push({
          id: 'loading',
          labelKey: key('loading'),
          kind: 'progress',
        })
      }
      break
    }
    case PopupType.CreateTutorialRepository: {
      fields.push(
        readonly('login', popup.account.login),
        readonly(
          'endpoint',
          displayDialogURL(getHTMLURL(popup.account.endpoint))
        )
      )
      if (popup.progress !== undefined) {
        fields.push({
          id: 'tutorialProgress',
          labelKey: key('tutorialProgress'),
          kind: 'progress',
          value: popup.progress.value,
          text: displayDialogDiagnostic(
            popup.progress.title ?? '',
            state.accounts
          ),
        })
        if (popup.progress.description) {
          fields.push(
            readonly(
              'diagnostic',
              displayDialogDiagnostic(
                popup.progress.description,
                state.accounts
              )
            )
          )
        }
      }
      confirm('createTutorial', false, popup.progress !== undefined)
      if (popup.progress !== undefined) {
        actions = actions.map(a => ({ ...a, disabled: true }))
      }
      actions.push(button('tutorialAccount'))
      break
    }
    case PopupType.ConfirmExitTutorial: {
      const selected = selectedRepository(state)?.repository
      if (selected?.isTutorialRepository) {
        fields.push(readonly('path', selected.path))
      }
      confirm('exitTutorial')
      break
    }
    case PopupType.DeletePullRequest:
      fields.push(
        readonly('branch', popup.branch.name),
        readonly('pullRequest', `#${popup.pullRequest.pullRequestNumber}`)
      )
      confirm('deleteBranch', true)
      actions.push(button('viewPullRequest'))
      break
    case PopupType.PushRejectedDueToMissingWorkflowScope:
      fields.push(readonly('rejectedPath', popup.rejectedPath))
      confirm('reauthorize')
      break
    case PopupType.SAMLReauthRequired:
      fields.push(readonly('organization', popup.organizationName))
      confirm('reauthorize')
      break
    case PopupType.MoveToApplicationsFolder:
      fields.push(checkbox('doNotAskAgain', false))
      actions = [button('notNow'), button('moveAndRestart', true)]
      break
    case PopupType.ThankYou:
      fields.push(
        readonly('friendlyName', popup.friendlyName),
        readonly('version', popup.latestVersion ?? ''),
        readonly(
          'contributions',
          popup.userContributions.map(note => note.message).join('\n')
        )
      )
      break
    case PopupType.InvalidatedToken:
      fields.push(
        readonly('login', popup.account.login),
        readonly('endpoint', popup.account.friendlyEndpoint)
      )
      confirm('signIn')
      actions[0] = { ...actions[0], labelKey: key('no') }
      actions[1] = { ...actions[1], labelKey: key('yes') }
      break
    case PopupType.PullRequestChecksFailed: {
      fields.push(
        readonly(
          'pullRequest',
          `${popup.pullRequest.title} #${popup.pullRequest.pullRequestNumber}`
        ),
        select(
          'check',
          String(session.selectedCheckID ?? ''),
          session.checks.map(check => ({
            id: String(check.id),
            label: check.name,
          }))
        )
      )
      const selected = session.checks.find(
        check => check.id === session.selectedCheckID
      )
      if (selected) {
        fields.push(readonly('diagnostic', selected.description))
        fields.push(
          readonly(
            'steps',
            selected.actionJobSteps
              ?.map(
                step =>
                  `${step.number}. ${step.name}\n${
                    step.conclusion ?? step.status
                  }`
              )
              .join('\n') ?? ''
          )
        )
        if (selected.actionJobSteps) {
          fields.push(
            select(
              'step',
              '',
              selected.actionJobSteps.map(step => ({
                id: String(step.number),
                label: step.name,
              }))
            )
          )
        }
      }
      confirm('switchPullRequest', false, session.switching)
      actions[0] = { ...actions[0], labelKey: key('dismiss') }
      actions[1] = {
        ...actions[1],
        labelKey: key(
          popup.shouldChangeRepository
            ? 'switchRepositoryPullRequest'
            : 'switchPullRequest'
        ),
      }
      actions.push(
        button('selectCheck'),
        button('viewCheck'),
        button('rerunAll', false, false, session.checks.length === 0)
      )
      if (
        supportsRerunningIndividualOrFailedChecks(
          popup.repository.gitHubRepository.endpoint
        )
      ) {
        actions.push(
          button('rerunFailed', false, false, !session.checks.some(isFailure)),
          button('rerunSelected', false, false, selected === undefined)
        )
      }
      if (selected?.actionJobSteps) {
        actions.push(button('viewStep'))
      }
      break
    }
    case PopupType.CICheckRunRerun:
      fields.push(
        readonly('rerunnable', checksText(session.rerunnable)),
        readonly('nonRerunnable', checksText(session.nonRerunnable))
      )
      confirm(
        'rerun',
        false,
        session.loading || session.rerunnable.length === 0
      )
      break
    case PopupType.PullRequestReview:
    case PopupType.PullRequestComment: {
      const item =
        popup.type === PopupType.PullRequestReview
          ? popup.review
          : popup.comment
      fields.push(
        readonly(
          'pullRequest',
          `${popup.pullRequest.title} #${popup.pullRequest.pullRequestNumber}`
        ),
        readonly('author', item.user.login),
        readonly('body', item.body),
        readonly(
          'eventDate',
          popup.type === PopupType.PullRequestReview
            ? popup.review.submitted_at
            : popup.comment.created_at
        )
      )
      if (popup.type === PopupType.PullRequestReview) {
        fields.push(readonly('reviewState', popup.review.state))
      }
      if (notificationSwitch(popup)) {
        confirm('switchPullRequest', false, session.switching)
        actions[0] = { ...actions[0], labelKey: key('dismiss') }
        actions[1] = {
          ...actions[1],
          labelKey: key(
            popup.shouldChangeRepository
              ? 'switchRepositoryPullRequest'
              : 'switchPullRequest'
          ),
        }
      }
      actions.push(button('viewNotification'), button('viewAuthor'))
      break
    }
    case PopupType.UnreachableCommits: {
      const selected = selectedRepository(state)
      fields.push(
        select('tab', session.tab, [
          { id: 'unreachable', label: key('unreachable') },
          { id: 'reachable', label: key('reachable') },
        ])
      )
      if (selected) {
        const selection = selected.state.commitSelection
        const shas =
          session.tab === 'reachable'
            ? selection.shasInDiff
            : selection.shas.filter(sha => !selection.shasInDiff.includes(sha))
        fields.push(
          readonly(
            'commits',
            shas
              .map(sha => {
                const commit = selected.state.commitLookup.get(sha)
                return commit
                  ? `${commit.sha}\n${commit.summary}\n${commit.body}`
                  : sha
              })
              .join('\n\n')
          )
        )
      }
      actions.push(button('selectTab'), button('unreachableHelp'))
      break
    }
    case PopupType.StartPullRequest: {
      const pr = previewState(state, popup)
      const selection = pr?.commitSelection
      fields.push(
        readonly('branch', popup.currentBranch.name),
        select(
          'baseBranch',
          pr?.baseBranch?.name ?? '',
          popup.prBaseBranches.map(branch => ({
            id: branch.name,
            label: branch.name,
          }))
        ),
        readonly(
          'recentBases',
          popup.prRecentBaseBranches.map(branch => branch.name).join('\n')
        ),
        readonly('defaultBranch', popup.defaultBranch?.name ?? ''),
        readonly('commitCount', String(pr?.commitSHAs?.length ?? 0)),
        readonly(
          'linesAdded',
          String(selection?.changesetData.linesAdded ?? 0)
        ),
        readonly(
          'linesDeleted',
          String(selection?.changesetData.linesDeleted ?? 0)
        )
      )
      const merge = pr?.mergeStatus?.kind
      const note =
        pr?.baseBranch == null
          ? 'noDefaultBranch'
          : selection?.shas.length === 0
          ? merge === ComputedAction.Invalid
            ? 'unrelatedHistories'
            : 'noChanges'
          : 'allCommits'
      fields.push(
        {
          id: 'comparisonNote',
          labelKey: key(note),
          kind: 'readonly',
          readOnly: true,
        },
        {
          id: 'mergeStatus',
          labelKey: key(`merge.${merge ?? 'none'}`),
          kind: 'readonly',
          readOnly: true,
        }
      )
      const files = selection?.changesetData.files ?? []
      fields.push(
        select(
          'file',
          selection?.file?.id ?? '',
          files.map(file => ({ id: file.id, label: file.path }))
        ),
        checkbox('hideWhitespace', state.hideWhitespaceInPullRequestDiff),
        checkbox('sideBySide', session.sideBySide),
        select(
          'imageDiffType',
          String(state.imageDiffType),
          Object.values(ImageDiffType)
            .filter((value): value is number => typeof value === 'number')
            .map(value => ({
              id: String(value),
              label: key(`imageMode.${ImageDiffType[value]}`),
            }))
        )
      )
      const diff = projectKernelDiff(selection?.diff ?? null, popup)
      if (diff !== null) {
        fields.push({
          id: 'diff',
          labelKey: key('diff'),
          kind: 'diff',
          readOnly: true,
          diff,
          diffPath: selection?.file?.path ?? '',
          sideBySide: session.sideBySide,
          imageDiffType: state.imageDiffType,
        })
      }
      confirm(
        popup.currentBranchHasPullRequest
          ? 'viewPullRequest'
          : 'createPullRequest',
        false,
        !pr?.commitSHAs?.length
      )
      actions.push(
        button('changeBase'),
        button('selectFile', false, false, files.length === 0),
        button('diffOptions')
      )
      if (selection?.file) {
        actions.push(button('openFileInEditor'))
      }
      break
    }
    case PopupType.InstallingUpdate:
      actions = [button('cancel', true), button('quitAnyway', false, true)]
      break
    case PopupType.PushProtectionError:
      for (const [index, secret] of popup.secrets.entries()) {
        fields.push(
          readonly(
            `secret.${index}`,
            `${secret.description}${
              popup.secrets.filter(s => s.description === secret.description)
                .length > 1
                ? ` (${secret.id})`
                : ''
            }`
          )
        )
        const locations = session.expandedSecrets.has(secret.id)
          ? secret.locations
          : secret.locations.slice(0, 1)
        fields.push(
          readonly(
            `locations.${index}`,
            locations
              .map(
                location =>
                  `${location.commitSha}\n${location.path}:${location.lineNumber}`
              )
              .join('\n\n')
          )
        )
        actions.push(
          button(
            `bypass.${index}`,
            false,
            false,
            !secret.requiresApproval && session.bypassed.has(secret.id)
          )
        )
        if (secret.locations.length > 1) {
          actions.push(button(`locations.${index}`))
        }
        secret.locations.forEach((_, location) =>
          actions.push(button(`copySecretSHA.${index}.${location}`))
        )
      }
      actions.push(button('secretScanningHelp'), button('remediation'))
      break
    case PopupType.BypassPushProtection:
      fields.push(
        readonly('secretDescription', popup.secret.description),
        select('reason', BypassReason.FalsePositive, [
          { id: BypassReason.UsedInTests, label: key('reason.used_in_tests') },
          {
            id: BypassReason.FalsePositive,
            label: key('reason.false_positive'),
          },
          {
            id: BypassReason.WillFixLater,
            label: key('reason.will_fix_later'),
          },
        ])
      )
      confirm('allowSecret', true)
      break
    case PopupType.CommitProgress:
      fields.push({
        id: 'terminal',
        labelKey: key('terminal'),
        kind: 'multiline',
        value: session.terminal,
        readOnly: true,
        height: 360,
      })
      break
    default:
      throw new Error('Popup is not owned by auxiliary dialogs')
  }
  if (session.links.length > 0) {
    fields.push(
      select(
        'contentLink',
        '',
        session.links.map((link, index) => ({
          id: String(index),
          label: link.label,
        }))
      )
    )
    actions.push(button('openContentLink'))
  }
  if (
    session.loading ||
    session.switching ||
    popup.type === PopupType.InstallingUpdate
  ) {
    fields.push({ id: 'loading', labelKey: key('loading'), kind: 'progress' })
  }
  return {
    popupID: popup.id ?? null,
    type: popup.type,
    supported: true,
    layout: 'form',
    titleKey,
    descriptionKey,
    fields: fields.map(field => {
      const changeAction =
        popup.type === PopupType.PullRequestChecksFailed && field.id === 'check'
          ? 'selectCheck'
          : popup.type === PopupType.UnreachableCommits && field.id === 'tab'
          ? 'selectTab'
          : popup.type === PopupType.StartPullRequest &&
            field.id === 'baseBranch'
          ? 'changeBase'
          : popup.type === PopupType.StartPullRequest && field.id === 'file'
          ? 'selectFile'
          : undefined
      return changeAction === undefined ? field : { ...field, changeAction }
    }),
    actions: actions.map(action => ({
      ...action,
      fields: auxiliaryActionFields(popup, action.id, fields),
    })),
    width: popup.type === PopupType.StartPullRequest ? 900 : 600,
  }
}

async function updateTestNotificationPermission(session: Session) {
  const permission = await getNotificationsPermission()
  if (!session.disposed) {
    session.development!.permission = permission
    session.changed()
  }
}

async function loadTestTokenDetails(
  runtime: AppRuntime,
  session: Session,
  change?: { readonly expiresAt: number | undefined }
) {
  const d = session.development!
  const request = ++d.tokenRequest
  const account = runtime.appStore
    .getState()
    .accounts.find(a => getAccountKey(a) === d.accountKey)
  d.tokenBusy = account !== undefined
  d.expiration = change === undefined ? null : d.expiration
  d.error = null
  d.messageKey = null
  session.changed()
  if (account === undefined) {
    return
  }
  try {
    const expiration =
      change === undefined
        ? await runtime.dispatcher.getAccountTokenExpirationForTesting(account)
        : await runtime.dispatcher.setAccountTokenExpirationForTesting(
            account,
            change.expiresAt
          )
    if (!session.disposed && request === d.tokenRequest) {
      d.expiration = expiration
      d.dateTime = testTokenLocalDateTime(expiration.expiresAt)
      d.messageKey =
        change === undefined
          ? null
          : key(
              change.expiresAt === undefined ? 'tokenRestored' : 'tokenApplied'
            )
    }
  } catch {
    // Session/keychain diagnostics may contain credentials; only fixed UI prose leaves this view.
    if (!session.disposed && request === d.tokenRequest) {
      d.error = key('debug.tokenFailed')
    }
  } finally {
    if (!session.disposed && request === d.tokenRequest) {
      d.tokenBusy = false
      session.changed()
    }
  }
}

function notificationTestItems(
  popup: Extract<Popup, { type: PopupType.TestNotifications }>,
  d: DevelopmentSession
) {
  const step = d.flow?.steps[d.stepResults.size]
  if (step === TestNotificationStepKind.SelectPullRequest) {
    const github = popup.repository.gitHubRepository
    return d.pullRequests.map(pr => ({
      id: String(pr.pullRequestNumber),
      label: `#${pr.pullRequestNumber} ${pr.title}\n${pr.author}`,
      url: `${getHTMLURL(github.endpoint)}/${github.owner.login}/${
        github.name
      }/pull/${pr.pullRequestNumber}`,
    }))
  }
  if (step === TestNotificationStepKind.SelectPullRequestReview) {
    return d.reviews.map(review => ({
      id: String(review.id),
      label: `${review.body}\n${review.user.login} (${review.state})`,
      url: review.html_url,
    }))
  }
  return d.comments.map(comment => ({
    id: String(comment.id),
    label: `${comment.body}\n${comment.user.login}`,
    url: comment.html_url,
  }))
}

async function prepareNotificationTest(
  runtime: AppRuntime,
  popup: Extract<Popup, { type: PopupType.TestNotifications }>,
  session: Session
): Promise<void> {
  const d = session.development!
  const request = ++d.notificationRequest
  if (d.flow === null) {
    session.loading = false
    session.changed()
    return
  }
  if (d.flow.steps[d.stepResults.size] === undefined) {
    simulateTestNotification(
      runtime.notificationsDebugStore,
      runtime.dispatcher,
      popup.repository,
      d.flow,
      d.stepResults
    )
    const back = backNotificationTest(d.flow, d.stepResults)!
    d.flow = back.selectedFlow
    d.stepResults = back.stepResults
    return prepareNotificationTest(runtime, popup, session)
  }
  session.loading = true
  d.error = null
  session.changed()
  try {
    const data = await loadNotificationTestStep(
      runtime.notificationsDebugStore,
      popup.repository,
      d.flow,
      d.stepResults
    )
    if (
      !session.disposed &&
      request === d.notificationRequest &&
      data !== null
    ) {
      if (data.pullRequests !== undefined) {
        d.pullRequests = data.pullRequests
      }
      if (data.reviews !== undefined) {
        d.reviews = data.reviews
      }
      if (data.comments !== undefined) {
        d.comments = data.comments
      }
    }
  } catch {
    if (!session.disposed && request === d.notificationRequest) {
      d.error = key('debug.notificationFailed')
    }
  } finally {
    if (!session.disposed && request === d.notificationRequest) {
      session.loading = false
      session.changed()
    }
  }
}

/** Development views preserve their original tools, but cannot be used as a
 * production escape hatch. They project only UI primitives and library IDs. */
function projectDevelopmentDialog(
  popup: Popup,
  state: IAppState,
  session: Session
): NativeDialogPresentation {
  const fields: NativeDialogField[] = []
  let actions = [button('close', true)]
  const base = {
    popupID: popup.id ?? null,
    type: popup.type,
    supported: true as const,
    layout: 'form' as const,
    titleKey: key(`${popup.type}.title`),
    width: 760,
    height: 640,
  }
  if (!enableTestMenuItems()) {
    return {
      ...base,
      descriptionKey: key('developmentOnly'),
      fields,
      actions: actions.map(a => ({ ...a, fields: [] })),
    }
  }
  const d = session.development!
  switch (popup.type) {
    case PopupType.TestIcons: {
      const catalog = getIconPreviewCatalog()
      for (const height of [...new Set(catalog.map(icon => icon.height))].sort(
        (a, b) => a - b
      )) {
        fields.push({
          id: `icons.${height}`,
          labelKey: key('icons'),
          kind: 'icons',
          iconLibrary: 'octicons',
          value: height,
          readOnly: true,
          choices: catalog
            .filter(icon => icon.height === height)
            .map(icon => ({
              id: icon.id,
              label: `${icon.name} (${icon.height}x${icon.width})`,
            })),
        })
      }
      break
    }
    case PopupType.TestAbout: {
      fields.push(
        readonly('application', getName()),
        readonly('version', __DEV__ ? __SHA__.substring(0, 10) : getVersion()),
        readonly('architecture', process.arch),
        {
          id: 'updateStatus',
          labelKey: key(
            `update.${
              ['checking', 'downloading', 'latest', 'ready', 'notChecked'][
                d.about.status
              ]
            }`
          ),
          kind: 'readonly',
          readOnly: true,
        }
      )
      if (d.about.lastSuccessfulCheck) {
        fields.push(
          readonly('lastChecked', d.about.lastSuccessfulCheck.toISOString())
        )
      }
      actions.push(
        button('terms'),
        button('acknowledgements'),
        button('releaseNotes'),
        button('copilotTransparency'),
        button('betaChannel')
      )
      actions.push(
        d.about.status === UpdateStatus.UpdateReady
          ? button('quitAndInstall')
          : button(
              'checkUpdates',
              false,
              false,
              ![
                UpdateStatus.UpdateNotAvailable,
                UpdateStatus.UpdateNotChecked,
              ].includes(d.about.status)
            )
      )
      break
    }
    case PopupType.TestCLIAction: {
      const v = d.values
      fields.push({
        ...select('cliKind', v.cliKind, [
          { id: 'open-repository', label: key('cliOpenRepository') },
          { id: 'clone-url', label: key('cliCloneURL') },
        ]),
        changeAction: 'cliChange',
      })
      for (const id of v.cliKind === 'open-repository'
        ? ['cliPath']
        : ['cliURL', 'cliBranch']) {
        fields.push({
          id,
          labelKey: key(id),
          kind: 'text',
          value: id === 'cliURL' ? displayDialogURL(v[id]) : v[id],
          changeAction: 'cliChange',
        })
      }
      actions = [
        button('cliChange'),
        button(
          'dispatchCLI',
          true,
          false,
          getTestCLIAction(
            v.cliKind as 'open-repository' | 'clone-url',
            v.cliPath,
            v.cliURL,
            v.cliBranch
          ) === null
        ),
        button('cancel'),
      ]
      break
    }
    case PopupType.TestTokenExpiration: {
      const canApply = !d.tokenBusy && d.expiration?.isRefreshable === true
      fields.push({
        ...select(
          'testAccount',
          d.accountKey,
          state.accounts.map(account => ({
            id: getAccountKey(account),
            label: `${account.login} (${account.friendlyEndpoint})`,
          }))
        ),
        changeAction: 'chooseTestAccount',
        disabled: d.tokenBusy,
      })
      if (d.expiration) {
        fields.push(
          readonly(
            'tokenExpires',
            testTokenLocalDateTime(d.expiration.expiresAt)
          ),
          {
            ...checkbox('tokenOverridden', d.expiration.isOverridden),
            readOnly: true,
          }
        )
        if (d.expiration.isOverridden) {
          fields.push(
            readonly(
              'tokenOriginalExpires',
              testTokenLocalDateTime(d.expiration.originalExpiresAt)
            )
          )
        }
        if (!d.expiration.isRefreshable) {
          fields.push({
            id: 'notRefreshable',
            labelKey: key('notRefreshable'),
            kind: 'readonly',
            readOnly: true,
          })
        }
      }
      fields.push({
        id: 'testDateTime',
        labelKey: key('testDateTime'),
        kind: 'text',
        value: d.dateTime,
        disabled: !canApply,
        changeAction: 'editExpiration',
      })
      actions = [
        button(
          'chooseTestAccount',
          false,
          false,
          d.tokenBusy || state.accounts.length === 0
        ),
        button(
          'applyExpiration',
          true,
          false,
          !canApply || chosenTestTokenExpiration(d.dateTime) === undefined
        ),
        button('editExpiration', false, false, !canApply),
        ...[
          'expireNow',
          'expire10min',
          'expire1h',
          'expire62min',
          'expire8h',
        ].map(id => button(id, false, false, !canApply)),
        button(
          'resetExpiration',
          false,
          false,
          d.tokenBusy || d.expiration?.isOverridden !== true
        ),
        button('reloadExpiration', false, false, d.tokenBusy),
        button('close'),
      ]
      break
    }
    case PopupType.TestNotifications: {
      fields.push({
        id: 'notificationPermission',
        labelKey: key(`permission.${d.permission}`),
        kind: 'readonly',
        readOnly: true,
      })
      if (
        supportsNotificationsPermissionRequest() &&
        d.permission === 'default'
      ) {
        actions.push(button('grantNotifications'))
      }
      if (getNotificationSettingsUrl() !== null) {
        actions.push(button('notificationSettings'))
      }
      if (d.flow === null) {
        fields.push({
          ...select(
            'notificationType',
            '',
            TestNotificationFlows.map(flow => ({
              id: String(flow.type),
              label: key(`notificationType.${TestNotificationType[flow.type]}`),
            }))
          ),
          changeAction: 'chooseNotificationType',
        })
        actions.push(button('chooseNotificationType'))
      } else {
        fields.push({
          ...select('notificationItem', '', notificationTestItems(popup, d)),
          disabled: session.loading,
        })
        actions.push(
          button('chooseNotification', false, false, session.loading),
          button('viewNotificationItem', false, false, session.loading),
          button('backNotification')
        )
      }
      break
    }
    case PopupType.TestCopilotSnapshotCard: {
      const snapshot = d.snapshot
      for (const [id, value] of [
        ['testLogin', snapshot.login],
        ['testName', snapshot.name],
        ['testEndpoint', displayDialogURL(snapshot.endpoint)],
        ['testAvatarURL', displayDialogURL(snapshot.avatarURL)],
      ]) {
        fields.push({ id, labelKey: key(id), kind: 'text', value })
      }
      const snapshots = getTestSnapshots(snapshot.snapshots)
      const tokenBased = hasTokenBasedBilling(snapshots)
      const visible = [
        ...getVisibleRateLimitSnapshots(snapshots, tokenBased),
        ...getVisibleQuotaSnapshots(snapshots, tokenBased),
      ]
      for (const item of visible) {
        const name = item.displayName === 'AI credits' ? 'aiCredits' : item.key
        fields.push({
          id: `preview.${item.key}`,
          labelKey: key(`snapshot.${name}`),
          kind: 'progress',
          value: getUsedPercentage(item.snapshot) / 100,
          readOnly: true,
          ...(item.snapshot.isUnlimitedEntitlement
            ? { disabled: true }
            : {
                text: `${item.snapshot.usedRequests}/${item.snapshot.entitlementRequests}`,
              }),
        })
        if (item.snapshot.isUnlimitedEntitlement) {
          fields.push({
            id: `unlimited.${item.key}`,
            labelKey: key('snapshot.unlimited'),
            kind: 'readonly',
            readOnly: true,
          })
        }
        if (item.snapshot.resetDate) {
          fields.push({
            ...readonly(`reset.${item.key}`, item.snapshot.resetDate),
            labelKey: key('snapshot.resetDate'),
          })
        }
      }
      for (const definition of snapshotDefinitions) {
        fields.push({
          id: `snapshot.${definition.key}`,
          labelKey: key(`snapshot.${definition.key}`),
          kind: 'readonly',
          readOnly: true,
        })
        for (const [property, value] of Object.entries(
          snapshot.snapshots[definition.key]
        )) {
          fields.push({
            id: `${definition.key}:${property}`,
            labelKey: key(`snapshot.field.${property}`),
            kind: typeof value === 'boolean' ? 'checkbox' : 'text',
            value,
            ...(typeof value === 'boolean'
              ? { changeAction: 'snapshotChange' }
              : {}),
          })
        }
      }
      actions = [
        button('snapshotChange'),
        button('aiCreditsPreset'),
        button('rateLimitPreset'),
        button('done', true),
      ]
      break
    }
  }
  if (session.loading || d.tokenBusy) {
    fields.push({ id: 'loading', labelKey: key('loading'), kind: 'progress' })
  }
  if (d.messageKey) {
    fields.push({
      id: 'message',
      labelKey: d.messageKey,
      kind: 'readonly',
      readOnly: true,
    })
  }
  return {
    ...base,
    descriptionKey: key(`${popup.type}.description`),
    fields,
    actions: actions.map(a => ({
      ...a,
      fields: auxiliaryActionFields(popup, a.id, fields),
    })),
    error: d.error,
  }
}

async function replyDevelopmentDialog(
  runtime: AppRuntime,
  popup: Popup,
  action: string,
  input: unknown,
  session: Session
) {
  const d = session.development!
  const view = projectDevelopmentDialog(
    popup,
    runtime.appStore.getState(),
    session
  )
  const v = dialogValues(
    input ?? {},
    view.actions.find(a => a.id === action)?.fields ?? []
  )
  // All displayed fields are checked before changing even ephemeral UI state.
  for (const field of view.fields) {
    if (v[field.id] === undefined) {
      continue
    }
    if (field.kind === 'checkbox') {
      dialogBoolean(v[field.id])
    } else if (field.kind === 'select') {
      choice(
        v[field.id],
        field.choices?.filter(c => !c.disabled).map(c => c.id) ?? []
      )
    } else {
      dialogString(v[field.id])
    }
  }
  if (action === 'done') {
    await cancel(runtime, popup)
    return
  }
  switch (popup.type) {
    case PopupType.TestAbout:
      if (action === 'checkUpdates') {
        await checkAboutTestUpdates(
          change => {
            if (!session.disposed) {
              d.about = { ...d.about, ...change }
              session.changed()
            }
          },
          milliseconds =>
            new Promise<void>(resolve => {
              if (session.disposed) {
                resolve()
                return
              }
              clearTimeout(d.timer)
              d.settleTimer = resolve
              d.timer = setTimeout(() => {
                d.timer = undefined
                d.settleTimer = undefined
                resolve()
              }, milliseconds)
            })
        )
      } else if (action === 'quitAndInstall') {
        await cancel(runtime, popup)
      } else if (action === 'terms' || action === 'acknowledgements') {
        await runtime.dispatcher.showPopup({
          type:
            action === 'terms'
              ? PopupType.TermsAndConditions
              : PopupType.Acknowledgements,
        })
      } else {
        await shell.openExternal(
          action === 'releaseNotes'
            ? ReleaseNotesUri
            : action === 'betaChannel'
            ? 'https://desktop.github.com/beta'
            : 'https://gh.io/copilot-for-desktop-transparency'
        )
      }
      return
    case PopupType.TestCLIAction: {
      const next = { ...d.values }
      for (const [id, value] of Object.entries(v)) {
        const text = dialogString(value)
        next[id] =
          id === 'cliURL' && text === displayDialogURL(next[id])
            ? next[id]
            : text
      }
      d.values = next
      if (action === 'dispatchCLI') {
        const cli = getTestCLIAction(
          next.cliKind as 'open-repository' | 'clone-url',
          next.cliPath,
          next.cliURL,
          next.cliBranch
        )
        if (cli === null) {
          throw new Error('Invalid native CLI action')
        }
        await runtime.dispatcher.dispatchCLIAction(cli)
        await close(runtime, popup)
      } else {
        session.changed()
      }
      return
    }
    case PopupType.TestTokenExpiration: {
      if (action === 'chooseTestAccount') {
        d.accountKey = dialogString(v.testAccount)
        await loadTestTokenDetails(runtime, session)
        return
      }
      if (action === 'editExpiration' || action === 'applyExpiration') {
        d.dateTime = dialogString(v.testDateTime)
        d.messageKey = null
        if (action === 'applyExpiration') {
          const expiresAt = chosenTestTokenExpiration(d.dateTime)
          if (expiresAt === undefined) {
            throw new Error('Invalid token expiration')
          }
          await loadTestTokenDetails(runtime, session, { expiresAt })
        } else {
          session.changed()
        }
      } else if (action === 'resetExpiration') {
        await loadTestTokenDetails(runtime, session, { expiresAt: undefined })
      } else if (action === 'reloadExpiration') {
        await loadTestTokenDetails(runtime, session)
      } else {
        const presets: Readonly<Record<string, number>> = {
          expireNow: -1,
          expire10min: 10,
          expire1h: 60,
          expire62min: 62,
          expire8h: 480,
        }
        const minutes = presets[action]
        if (minutes === undefined) {
          throw new Error('Invalid token expiration preset')
        }
        d.dateTime = testTokenLocalDateTime(Date.now() + minutes * 60_000)
        d.messageKey = null
        session.changed()
      }
      return
    }
    case PopupType.TestNotifications: {
      if (action === 'grantNotifications') {
        await requestNotificationsPermission()
        await updateTestNotificationPermission(session)
        return
      }
      if (action === 'notificationSettings') {
        const url = getNotificationSettingsUrl()
        if (url !== null) {
          await shell.openExternal(url)
        }
        return
      }
      if (action === 'chooseNotificationType') {
        const type = choice(
          v.notificationType,
          TestNotificationFlows.map(flow => String(flow.type))
        )
        d.flow = TestNotificationFlows.find(flow => String(flow.type) === type)!
        d.stepResults = new Map()
      } else if (action === 'backNotification') {
        const next = backNotificationTest(d.flow, d.stepResults)
        if (next === null) {
          return
        }
        d.flow = next.selectedFlow
        d.stepResults = next.stepResults
      } else {
        const item = notificationTestItems(popup, d).find(
          item => item.id === v.notificationItem
        )
        if (item === undefined) {
          throw new Error('Invalid test notification item')
        }
        if (action === 'viewNotificationItem') {
          await shell.openExternal(item.url)
          return
        }
        const step = d.flow!.steps[d.stepResults.size]
        if (step === TestNotificationStepKind.SelectPullRequest) {
          d.stepResults.set(step, {
            kind: step,
            pullRequest: d.pullRequests.find(
              pr => String(pr.pullRequestNumber) === item.id
            )!,
          })
        } else if (step === TestNotificationStepKind.SelectPullRequestReview) {
          d.stepResults.set(step, {
            kind: step,
            review: d.reviews.find(review => String(review.id) === item.id)!,
          })
        } else {
          const comment = d.comments.find(
            comment => String(comment.id) === item.id
          )!
          d.stepResults.set(TestNotificationStepKind.SelectPullRequestComment, {
            kind: TestNotificationStepKind.SelectPullRequestComment,
            comment,
            isIssueComment: comment.html_url.includes('#issuecomment-'),
          })
        }
      }
      await prepareNotificationTest(runtime, popup, session)
      return
    }
    case PopupType.TestCopilotSnapshotCard: {
      if (action === 'aiCreditsPreset' || action === 'rateLimitPreset') {
        d.snapshot = {
          ...d.snapshot,
          snapshots:
            action === 'aiCreditsPreset'
              ? tokenBasedBillingTestPreset()
              : rateLimitTestPreset(),
        }
      } else {
        let next = d.snapshot
        for (const [id, property] of [
          ['testLogin', 'login'],
          ['testName', 'name'],
          ['testEndpoint', 'endpoint'],
          ['testAvatarURL', 'avatarURL'],
        ] as const) {
          if (v[id] !== undefined) {
            next = { ...next, [property]: dialogString(v[id]) }
          }
        }
        for (const definition of snapshotDefinitions) {
          let snapshot: IEditableSnapshot = next.snapshots[definition.key]
          for (const property of Object.keys(snapshot) as Array<
            keyof IEditableSnapshot
          >) {
            const value = v[`${definition.key}:${property}`]
            if (value !== undefined) {
              snapshot = {
                ...snapshot,
                [property]:
                  typeof snapshot[property] === 'boolean'
                    ? dialogBoolean(value)
                    : dialogString(value),
              }
            }
          }
          next = {
            ...next,
            snapshots: { ...next.snapshots, [definition.key]: snapshot },
          }
        }
        d.snapshot = next
      }
      session.changed()
      return
    }
  }
  throw new Error('Invalid development dialog action')
}

function noValues(value: unknown) {
  return dialogValues(value ?? {}, [])
}
function values(value: unknown, allowed: ReadonlyArray<string>) {
  return dialogValues(value, allowed)
}
function choice(value: unknown, allowed: ReadonlyArray<string>) {
  const text = dialogString(value)
  if (!allowed.includes(text)) {
    throw new Error('Invalid native auxiliary selection')
  }
  return text
}
function requireSession(popup: Popup) {
  const session = sessions.get(popup)
  if (!session || session.disposed) {
    throw new Error('Auxiliary dialog is not mounted')
  }
  return session
}
function close(runtime: AppRuntime, popup: Popup) {
  if (popup.id === undefined) {
    throw new Error('Auxiliary dialog has no popup ID')
  }
  return runtime.dispatcher.closePopupById(popup.id)
}

/** Close is not sign-out, token renewal, PR checkout, Git cancellation or update installation. */
function cancel(runtime: AppRuntime, popup: Popup) {
  if (!types.some(type => type === popup.type)) {
    throw new Error('Popup is not owned by auxiliary dialogs')
  }
  if (
    popup.type === PopupType.CreateTutorialRepository &&
    popup.progress !== undefined
  ) {
    throw new Error('Tutorial creation cannot be dismissed during progress')
  }
  if (popup.type === PopupType.ConfirmAccountSignOut) {
    popup.onComplete(undefined)
  }
  if (popup.type === PopupType.BypassPushProtection) {
    popup.onDismissed()
  }
  if (
    popup.type === PopupType.CommitProgress ||
    popup.type === PopupType.InstallingUpdate ||
    isDevelopmentPopup(popup)
  ) {
    // Parent disposal handles unmount too. Dispose synchronously for the original close ordering.
    const session = sessions.get(popup)
    session?.dispose?.()
  }
  return close(runtime, popup)
}

/** Validate current explicit actions/fields before any callback or core/platform effect. */
async function reply(
  runtime: AppRuntime,
  popup: Popup,
  action: string,
  value: unknown
) {
  const state = runtime.appStore.getState()
  if (action === 'cancel') {
    noValues(value)
    await cancel(runtime, popup)
    return
  }
  const shown = project(popup, state).actions.find(
    button => button.id === action
  )
  if (!shown || shown.disabled) {
    throw new Error('Invalid native auxiliary action')
  }
  dialogValues(value ?? {}, shown.fields ?? [])
  if (action === 'close') {
    noValues(value)
    await cancel(runtime, popup)
    return
  }
  const dispatcher = runtime.dispatcher
  if (isDevelopmentPopup(popup)) {
    if (!enableTestMenuItems()) {
      throw new Error('The original test menu capability is not enabled')
    }
    await replyDevelopmentDialog(
      runtime,
      popup,
      action,
      value,
      requireSession(popup)
    )
    return
  }
  if (action === 'openContentLink') {
    const fields = values(value, ['contentLink'])
    const session = requireSession(popup)
    const id = choice(
      fields.contentLink,
      session.links.map((_, index) => String(index))
    )
    const url = session.links[Number(id)].url
    if (
      popup.type === PopupType.PullRequestReview ||
      popup.type === PopupType.PullRequestComment
    ) {
      await dispatcher.openInBrowser(url)
    } else {
      await shell.openExternal(url)
    }
    return
  }
  switch (popup.type) {
    case PopupType.ConfirmAccountSignOut: {
      const fields = values(value, ['clearAssignments'])
      const clear = dialogBoolean(fields.clearAssignments)
      popup.onComplete(clear)
      await close(runtime, popup)
      return
    }
    case PopupType.InstallGit:
      noValues(value)
      if (action === 'installGit') {
        shell.openExternal(
          'https://help.github.com/articles/set-up-git/#setting-up-git'
        )
        await close(runtime, popup)
      } else {
        dispatcher.openShell(popup.path, true)
        await close(runtime, popup)
      }
      return
    case PopupType.About:
      noValues(value)
      switch (action) {
        case 'checkUpdates':
          if (
            !__LINUX__ &&
            __RELEASE_CHANNEL__ !== 'development' &&
            !isOSNoLongerSupportedByElectron()
          ) {
            await updateStore.checkForUpdates(false, true)
          }
          return
        case 'quitAndInstall':
          updateStore.quitAndInstallUpdate()
          return
        case 'terms':
          await dispatcher.showPopup({ type: PopupType.TermsAndConditions })
          return
        case 'acknowledgements':
          await dispatcher.showPopup({ type: PopupType.Acknowledgements })
          return
        case 'betaChannel':
          await shell.openExternal('https://desktop.github.com/beta')
          return
        case 'copilotTransparency':
          await shell.openExternal(
            'https://gh.io/copilot-for-desktop-transparency'
          )
          return
        case 'supportedOS':
          await shell.openExternal(
            'https://docs.github.com/en/desktop/installing-and-configuring-github-desktop/overview/supported-operating-systems'
          )
          return
        case 'releaseNotes':
          await shell.openExternal(ReleaseNotesUri)
          return
      }
      break
    case PopupType.Acknowledgements: {
      noValues(value)
      if (action === 'website' || action === 'source') {
        await shell.openExternal(
          action === 'website'
            ? 'https://desktop.github.com'
            : 'https://github.com/desktop/desktop'
        )
        return
      }
      const index = Number(action.split('.')[1])
      const licenses = requireSession(popup).licenses!
      const entry = licenses[Object.keys(licenses).sort()[index]]
      await shell.openExternal(normalizedLicenseURL(entry.repository!))
      return
    }
    case PopupType.UntrustedCertificate:
      noValues(value)
      await close(runtime, popup)
      await showCertificateTrustDialog(
        popup.certificate,
        'Could not securely connect to the server, because its certificate is not trusted. Attackers might be trying to steal your information.\n\nTo connect unsafely, which may put your data at risk, you can “Always trust” the certificate and try again.'
      )
      return
    case PopupType.TermsAndConditions: {
      noValues(value)
      const urls = {
        contact: 'https://github.com/contact',
        logos: 'https://github.com/logos',
        privacy: 'https://help.github.com/articles/github-privacy-statement/',
        creativeCommons: 'https://creativecommons.org/licenses/by/4.0/',
      }
      await shell.openExternal(urls[action as keyof typeof urls])
      return
    }
    case PopupType.ExternalEditorFailed:
    case PopupType.OpenShellFailed:
    case PopupType.CopilotAppNotFound:
      noValues(value)
      if (action === 'preferences') {
        await close(runtime, popup)
        await dispatcher.showPopup({
          type: PopupType.Preferences,
          initialSelectedTab: PreferencesTab.Integrations,
        })
      } else {
        shell.openExternal(
          action === 'downloadEditor'
            ? suggestedExternalEditor.url
            : copilotAppMarketingUrl
        )
        // Original download button is the non-prevented reset button; inline Copilot links are not.
        if (action === 'downloadEditor') {
          await close(runtime, popup)
        }
      }
      return
    case PopupType.OpenWithExternalEditor: {
      const session = requireSession(popup)
      const fields = values(
        value,
        enableCustomIntegration()
          ? ['editor', 'customPath', 'customArguments', 'bundleID']
          : ['editor']
      )
      const editor = choice(fields.editor, [
        ...session.editors,
        ...(enableCustomIntegration() ? ['other'] : []),
      ])
      for (const field of ['customPath', 'customArguments', 'bundleID']) {
        if (fields[field] !== undefined) {
          dialogString(fields[field])
        }
      }
      const custom =
        editor === 'other'
          ? {
              path: dialogString(fields.customPath),
              arguments: dialogString(fields.customArguments),
              ...(fields.bundleID === undefined
                ? {}
                : { bundleID: dialogString(fields.bundleID) }),
            }
          : null
      if (custom && custom.path.length === 0) {
        throw new Error('Invalid native custom editor path')
      }
      const repository = selectedRepository(state)?.repository
      if (repository instanceof Repository) {
        await dispatcher.openInSelectedExternalEditor(
          repository.path,
          editor === 'other' ? null : editor,
          custom
        )
      }
      await close(runtime, popup)
      return
    }
    case PopupType.InitializeLFS:
      noValues(value)
      if (action === 'lfsWebsite') {
        await shell.openExternal('https://git-lfs.github.com/')
        return
      }
      dispatcher.installLFSHooks(popup.repositories)
      await close(runtime, popup)
      return
    case PopupType.LFSAttributeMismatch:
      noValues(value)
      if (action === 'editGlobalGitConfig') {
        await dispatcher.editGlobalGitConfig()
        return
      }
      dispatcher.installGlobalLFSFilters(true)
      await close(runtime, popup)
      return
    case PopupType.ReleaseNotes:
      noValues(value)
      if (action === 'releaseNotes') {
        await shell.openExternal(ReleaseNotesUri)
      } else {
        updateStore.quitAndInstallUpdate()
      }
      return
    case PopupType.PushBranchCommits: {
      noValues(value)
      const props = {
        dispatcher,
        repository: popup.repository,
        branch: popup.branch,
        onConfirm: () => {
          dispatcher.openCreatePullRequestInBrowser(
            popup.repository,
            popup.branch
          )
        },
        onDismissed: () => {
          close(runtime, popup)
        },
      }
      if (action === 'withoutPush') {
        createPullRequestWithoutPush(props)
      } else {
        const session = requireSession(popup)
        await submitPushBranchDialog(props, pushing => {
          session.pushing = pushing
          session.changed()
        })
      }
      return
    }
    case PopupType.CreateTutorialRepository:
      noValues(value)
      if (action === 'tutorialAccount') {
        await shell.openExternal(getHTMLURL(popup.account.endpoint))
      } else {
        // The original AppStore updates progress and owns closing on all outcomes.
        await dispatcher.createTutorialRepository(popup.account)
      }
      return
    case PopupType.ConfirmExitTutorial: {
      noValues(value)
      const selected = selectedRepository(state)?.repository
      if (selected?.isTutorialRepository) {
        const pausing = dispatcher.pauseTutorial(selected)
        await close(runtime, popup)
        await pausing
      }
      // Original onContinue=false leaves the popup open if the tutorial is gone.
      return
    }
    case PopupType.DeletePullRequest:
      noValues(value)
      if (action === 'viewPullRequest') {
        await dispatcher.showPullRequest(popup.repository)
      } else {
        dispatcher.deleteLocalBranch(popup.repository, popup.branch)
        await close(runtime, popup)
      }
      return
    case PopupType.PushRejectedDueToMissingWorkflowScope:
    case PopupType.SAMLReauthRequired: {
      noValues(value)
      const session = requireSession(popup)
      session.loading = true
      session.changed()
      await reauthorizeDialog(
        dispatcher,
        popup.type === PopupType.SAMLReauthRequired
          ? popup.endpoint
          : popup.repository.gitHubRepository.endpoint,
        () => {
          if (popup.type === PopupType.PushRejectedDueToMissingWorkflowScope) {
            dispatcher.push(popup.repository)
          } else if (popup.retryAction) {
            dispatcher.performRetry(popup.retryAction)
          }
        },
        () => {
          close(runtime, popup)
        }
      )
      return
    }
    case PopupType.MoveToApplicationsFolder:
      if (action === 'notNow') {
        const fields = values(value, ['doNotAskAgain'])
        const ask = !dialogBoolean(fields.doNotAskAgain)
        await close(runtime, popup)
        await dispatcher.setAskToMoveToApplicationsFolderSetting(ask)
      } else {
        // Submit does not persist the opt-out checkbox, matching the original.
        const fields = values(value, ['doNotAskAgain'])
        dialogBoolean(fields.doNotAskAgain)
        await close(runtime, popup)
        try {
          await dispatcher.moveToApplicationsFolder()
        } catch (error) {
          dispatcher.postError(error)
        }
      }
      return
    case PopupType.InvalidatedToken:
      noValues(value)
      await close(runtime, popup)
      await dispatcher.showAccountSignInDialog(
        popup.account.endpoint,
        popup.account.login
      )
      return
    case PopupType.PullRequestChecksFailed: {
      const session = requireSession(popup)
      if (action === 'selectCheck') {
        const fields = values(value, ['check'])
        const selected = choice(
          fields.check,
          session.checks.map(check => String(check.id))
        )
        session.selectedCheckID = Number(selected)
        session.changed()
        return
      }
      if (action === 'viewStep') {
        const fields = values(value, ['step'])
        const check = session.checks.find(
          check => check.id === session.selectedCheckID
        )!
        const number = choice(
          fields.step,
          (check.actionJobSteps ?? []).map(step => String(step.number))
        )
        const step = check.actionJobSteps!.find(
          step => String(step.number) === number
        )!
        const url = getCheckRunStepURL(
          check,
          step,
          getNonForkGitHubRepository(popup.repository),
          popup.pullRequest.pullRequestNumber
        )
        if (url !== null) {
          await dispatcher.openInBrowser(url)
        }
        return
      }
      noValues(value)
      if (action === 'viewCheck') {
        const check = session.checks.find(
          check => check.id === session.selectedCheckID
        )
        const url =
          check?.htmlUrl ??
          `${getNonForkGitHubRepository(popup.repository).htmlURL}/pull/${
            popup.pullRequest.pullRequestNumber
          }`
        await dispatcher.openInBrowser(url)
        return
      }
      if (['rerunAll', 'rerunFailed', 'rerunSelected'].includes(action)) {
        dispatcher.incrementMetric('checksFailedDialogRerunChecksCount')
        await dispatcher.showPopup({
          type: PopupType.CICheckRunRerun,
          checkRuns:
            action === 'rerunSelected'
              ? session.checks.filter(
                  check => check.id === session.selectedCheckID
                )
              : session.checks,
          repository: getNonForkGitHubRepository(popup.repository),
          localRepository: popup.repository,
          prRef: getPullRequestCommitRef(popup.pullRequest.pullRequestNumber),
          failedOnly: action === 'rerunFailed',
        })
        return
      }
      dispatcher.incrementMetric('checksFailedDialogSwitchToPullRequestCount')
      session.switching = true
      session.changed()
      await dispatcher.selectRepository(popup.repository)
      await dispatcher.checkoutPullRequest(popup.repository, popup.pullRequest)
      session.switching = false
      session.changed()
      await close(runtime, popup)
      return
    }
    case PopupType.CICheckRunRerun: {
      noValues(value)
      const session = requireSession(popup)
      session.loading = true
      session.changed()
      const succeeded = await rerunDialogChecks(
        dispatcher,
        popup.repository,
        popup.localRepository,
        popup.prRef,
        session.rerunnable,
        popup.failedOnly
      )
      if (!succeeded) {
        session.loading = false
        session.changed()
        return
      }
      await close(runtime, popup)
      return
    }
    case PopupType.PullRequestReview:
    case PopupType.PullRequestComment: {
      noValues(value)
      const item =
        popup.type === PopupType.PullRequestReview
          ? popup.review
          : popup.comment
      if (action === 'viewNotification' || action === 'viewAuthor') {
        await shell.openExternal(
          action === 'viewAuthor' ? item.user.html_url : item.html_url
        )
        return
      }
      const session = requireSession(popup)
      await submitNotificationDialog(
        dispatcher,
        popup.repository,
        popup.pullRequest,
        notificationSwitch(popup),
        switching => {
          session.switching = switching
          session.changed()
        },
        () => {
          if (popup.type === PopupType.PullRequestReview) {
            dispatcher.recordPullRequestReviewDialogSwitchToPullRequest(
              popup.review.state
            )
          } else {
            dispatcher.incrementMetric(
              'pullRequestCommentDialogSwitchToPullRequestCount'
            )
          }
        },
        () => {
          close(runtime, popup)
        }
      )
      return
    }
    case PopupType.UnreachableCommits:
      if (action === 'selectTab') {
        const fields = values(value, ['tab'])
        requireSession(popup).tab = choice(fields.tab, [
          'unreachable',
          'reachable',
        ]) as 'unreachable' | 'reachable'
        requireSession(popup).changed()
      } else {
        noValues(value)
        await shell.openExternal(
          'https://github.com/desktop/desktop/blob/development/docs/learn-more/unreachable-commits.md'
        )
      }
      return
    case PopupType.StartPullRequest: {
      const pr = previewState(state, popup)
      if (pr === null) {
        throw new Error('Pull request preview is unavailable')
      }
      if (action === 'changeBase') {
        const fields = values(value, ['baseBranch'])
        const name = choice(
          fields.baseBranch,
          popup.prBaseBranches.map(branch => branch.name)
        )
        await dispatcher.updatePullRequestBaseBranch(
          popup.repository,
          popup.prBaseBranches.find(branch => branch.name === name)!
        )
        return
      }
      if (action === 'selectFile') {
        const fields = values(value, ['file'])
        const files = pr.commitSelection?.changesetData.files ?? []
        const id = choice(
          fields.file,
          files.map(file => file.id)
        )
        await dispatcher.changePullRequestFileSelection(
          popup.repository,
          files.find(file => file.id === id)!
        )
        return
      }
      if (action === 'diffOptions') {
        const fields = values(value, [
          'hideWhitespace',
          'sideBySide',
          'imageDiffType',
        ])
        const hide = dialogBoolean(fields.hideWhitespace)
        const side = dialogBoolean(fields.sideBySide)
        const image = choice(
          fields.imageDiffType,
          Object.values(ImageDiffType)
            .filter(value => typeof value === 'number')
            .map(String)
        )
        const session = requireSession(popup)
        dispatcher.incrementMetric('diffOptionsViewedCount')
        session.sideBySide = side
        await dispatcher.onHideWhitespaceInPullRequestDiffChanged(
          hide,
          popup.repository,
          pr.commitSelection?.file ?? null
        )
        await dispatcher.changeImageDiffType(Number(image) as ImageDiffType)
        session.changed()
        return
      }
      noValues(value)
      if (action === 'openFileInEditor') {
        const file = pr.commitSelection?.file
        if (file) {
          await dispatcher.openInExternalEditor(
            Path.join(popup.repository.path, file.path)
          )
        }
        return
      }
      submitPullRequestPreview(
        dispatcher,
        popup.repository,
        pr,
        popup.currentBranchHasPullRequest,
        () => {
          close(runtime, popup)
        }
      )
      return
    }
    case PopupType.InstallingUpdate:
      noValues(value)
      dispatcher.quitApp(true)
      await cancel(runtime, popup)
      return
    case PopupType.PushProtectionError: {
      noValues(value)
      const session = requireSession(popup)
      if (action === 'secretScanningHelp' || action === 'remediation') {
        if (action === 'remediation') {
          dispatcher.incrementMetric(
            'secretRemediationInstructionsLinkClickedCount'
          )
        }
        await shell.openExternal(
          action === 'secretScanningHelp'
            ? 'https://docs.github.com/code-security/secret-scanning/protecting-pushes-with-secret-scanning'
            : 'https://docs.github.com/code-security/secret-scanning/working-with-secret-scanning-and-push-protection/working-with-push-protection-in-the-github-ui#resolving-a-blocked-commit'
        )
        return
      }
      const [, index, location] = action.split('.')
      const secret = popup.secrets[Number(index)]
      if (action.startsWith('copySecretSHA.')) {
        writeClipboardText(secret.locations[Number(location)].commitSha)
        return
      }
      if (action.startsWith('locations.')) {
        if (session.expandedSecrets.has(secret.id)) {
          session.expandedSecrets.delete(secret.id)
        } else {
          session.expandedSecrets.add(secret.id)
        }
        session.changed()
        return
      }
      if (secret.requiresApproval) {
        dispatcher.incrementMetric(
          'secretsDetectedOnPushDelegatedBypassLinkClickedCount'
        )
        await shell.openExternal(secret.bypassURL)
        return
      }
      const result = await openSecretBypassDialog(dispatcher, secret)
      if (result && !session.disposed) {
        session.bypassed.add(secret.id)
        session.changed()
      }
      return
    }
    case PopupType.BypassPushProtection: {
      const fields = values(value, ['reason'])
      const reason = choice(
        fields.reason,
        Object.values(BypassReason)
      ) as BypassReasonType
      popup.bypassPushProtection(popup.secret, reason)
      // Original callback owns asynchronous close, errors and success settlement.
      return
    }
  }
  throw new Error('Invalid native auxiliary action')
}

export const auxiliaryDialogs: NativeDialogModule = {
  types,
  project,
  reply,
  cancel,
}

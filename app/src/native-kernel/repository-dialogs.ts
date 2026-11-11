import * as Path from 'path'
import { Disposable } from 'event-kit'
import { displayDialogURL, displayDialogDiagnostic } from '../ui/lib/dialog-url'
import untildify from 'untildify'
import { API, IAPIOrganization } from '../lib/api'
import {
  IAppState,
  SelectionType,
  FoldoutType,
  HistoryTabMode,
} from '../lib/app-state'
import { Popup, PopupType } from '../models/popup'
import { getAccountKey, isSameAccount } from '../models/account'
import { BranchType, StartPoint } from '../models/branch'
import { TipState } from '../models/tip'
import { CloneRepositoryTab } from '../models/clone-repository-tab'
import { ForkContributionTarget } from '../models/workflow-preferences'
import {
  getForkContributionTarget,
  nameOf,
  isRepositoryWithForkedGitHubRepository,
} from '../models/repository'
import { getStartPoint } from '../lib/create-branch'
import { sanitizedRefName } from '../lib/sanitize-ref-name'
import { sanitizeCloneName, IRepositoryIdentifier } from '../lib/remote-parsing'
import {
  addSafeDirectory,
  getRepositoryType,
  getGitDescription,
} from '../lib/git'
import { pathExists } from '../lib/path-exists'
import { directoryExists } from '../lib/directory-exists'
import {
  canonicalMutationPath,
  isRepositoryAffectedByAssistedCommit,
  withoutRepositoryGitAccess,
} from '../lib/git/repository-operation'
import { getUnderlyingError } from '../lib/underlying-error'
import type { AppRuntime } from '../ui/app-runtime'
import { showOpenDialog, showSaveDialog } from '../ui/main-process-proxy'
import { getDefaultDir, setDefaultDir } from '../ui/lib/default-dir'
import { repositoryFullPath } from '../ui/lib/repository-path-policy'
import { getGitIgnoreNames } from '../ui/add-repository/gitignores'
import { getLicenses, ILicense } from '../ui/add-repository/licenses'
import { sanitizedRepositoryName } from '../ui/add-repository/sanitized-repository-name'
import { createRepositoryCore } from '../ui/lib/repository-creation'
import {
  validateRepositoryClonePath,
  resolveRepositoryCloneInfo,
  updatedRepositoryCloneURL,
  parseRepositoryCloneIdentifier,
} from '../ui/lib/repository-clone'
import { createRepositoryFork } from '../ui/lib/repository-fork'
import { checkBranchNameRules } from '../ui/lib/branch-name-rule-validation'
import { startTimer } from '../ui/lib/timing'
import { RepositorySettingsTab } from '../ui/repository-settings/settings-controller'
import {
  NativeDialogModule,
  NativeDialogField,
  NativeDialogAction,
  NativeDialogPresentation,
  dialogValues,
  dialogString,
  dialogBoolean,
} from './dialog-module'

const types = [
  PopupType.RenameBranch,
  PopupType.DeleteBranch,
  PopupType.DeleteRemoteBranch,
  PopupType.CreateBranch,
  PopupType.AddRepository,
  PopupType.CreateRepository,
  PopupType.CloneRepository,
  PopupType.PublishRepository,
  PopupType.RemoveRepository,
  PopupType.CreateTag,
  PopupType.DeleteTag,
  PopupType.ChangeRepositoryAlias,
  PopupType.AddWorktree,
  PopupType.RenameWorktree,
  PopupType.DeleteWorktree,
  PopupType.DeleteWorktreeFailed,
  PopupType.ChooseForkSettings,
  PopupType.CreateFork,
  PopupType.UpstreamAlreadyExists,
] as const

type RepositoryPopup = Extract<Popup, { type: typeof types[number] }>
type Draft = {
  values: Record<string, string | boolean>
  error: string | null
  busy: boolean
  loaded: boolean
  gitIgnores: ReadonlyArray<string>
  licenses: ReadonlyArray<ILicense>
  orgs: ReadonlyArray<IAPIOrganization>
  orgAccount: string | null
  forkFailed: boolean
  readmeExists: boolean
  pathIsSubfolder: boolean
  cloneTabs: Map<string, Record<string, string | boolean>>
  cloneIdentifiers: Map<string, IRepositoryIdentifier | null>
}
// Only presentation state. Objects, mutation guards, credentials and repository
// state remain in the original Popup/Stores. Weak keys die with the popup.
const drafts = new WeakMap<Popup, Draft>()
const observers = new WeakMap<Popup, () => void>()

/** Same lifetime as the original modal, including while SignIn/Error covers it. */
export function mountRepositoryDialog(
  runtime: AppRuntime,
  popupInput: Popup,
  onChange: () => void
): Disposable {
  const popup = owned(popupInput)
  if (observers.has(popup)) {
    throw new Error('Invalid repository dialog lifecycle')
  }
  const draft = draftFor(popup, runtime.appStore.getState())
  observers.set(popup, onChange)
  const handle = new Disposable(() => {
    observers.delete(popup)
    drafts.delete(popup)
  })
  if (
    !draft.loaded &&
    [
      PopupType.CreateRepository,
      PopupType.CloneRepository,
      PopupType.AddWorktree,
      PopupType.PublishRepository,
    ].includes(popup.type)
  ) {
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

const key = (name: string) => `repositoryDialog.${name}`
// Local form prose is translated; upstream Git/API/rules diagnostics remain
// original technical text after credential redaction.
const localErrorKeys: Readonly<Record<string, string>> = {
  'A branch with this name already exists': 'duplicateBranch',
  'A name is required': 'nameRequired',
  'A local path is required': 'pathRequired',
  'The repository does not require a safe-directory exception':
    'trustNotRequired',
  'This directory does not appear to be a Git repository': 'notRepository',
  'Bare repositories are not currently supported': 'bareRepository',
  'The repository is owned by another user; explicitly trust it to continue':
    'unsafeRepository',
  'A repository name and local path are required': 'nameAndPath',
  'Unknown gitignore': 'unknownGitignore',
  'Unknown license': 'unknownLicense',
  'Finish or cancel the assisted commit run before creating a repository in this directory':
    'assistedBusy',
  'Finish or cancel the assisted commit run before cloning into this directory.':
    'assistedBusy',
  'This directory already contains a Git repository; add it instead':
    'existingRepository',
  'Directory could not be created at this path': 'directoryFailed',
  'Select a signed-in account': 'accountRequired',
  'Select a repository for the current signed-in account': 'repositoryRequired',
  'A repository URL and local path are required': 'urlAndPath',
  'Repository not found; check account, network and URL': 'repositoryNotFound',
  'A repository name is required': 'nameRequired',
  'Organization is no longer available for the selected account':
    'organizationUnavailable',
  'The tag name cannot be longer than 245 characters': 'tagTooLong',
  'A tag with this name already exists': 'duplicateTag',
  'An alias is required': 'aliasRequired',
  'A worktree name, path and branch are required': 'worktreeRequired',
  'Choose a different worktree name': 'worktreeUnchanged',
  'Clone repository selection is no longer available': 'repositoryUnavailable',
  'Unable to read path on disk. Please check the path and try again.':
    'pathUnreadable',
  'The local path cannot end in .app on macOS. Choose a different folder name to avoid creating an application bundle.':
    'appPath',
  'This folder contains files. Git can only clone to empty folders.':
    'folderNotEmpty',
  'There is already a file with this name. Git can only clone to a folder.':
    'fileExists',
}
const text = (id: string, value: string, label = id): NativeDialogField => ({
  id,
  labelKey: key(label),
  kind: 'text',
  value: id === 'url' ? displayDialogURL(value) : value,
})
const readonly = (
  id: string,
  value: string,
  label = id
): NativeDialogField => ({
  id,
  labelKey: key(label),
  kind: 'readonly',
  value,
  readOnly: true,
})
const check = (id: string, value: boolean): NativeDialogField => ({
  id,
  labelKey: key(id),
  kind: 'checkbox',
  value,
})
const choice = (
  id: string,
  value: string,
  choices: ReadonlyArray<{ id: string; label: string }>
): NativeDialogField => ({
  id,
  labelKey: key(id),
  kind: 'select',
  value,
  choices,
})
const action = (
  id: string,
  label = id,
  primary = false,
  destructive = false
): NativeDialogAction => ({ id, labelKey: key(label), primary, destructive })

function owned(popup: Popup): RepositoryPopup {
  if (!types.includes(popup.type as typeof types[number])) {
    throw new Error('Not a repository dialog')
  }
  return popup as RepositoryPopup
}

function repositoryState(popup: RepositoryPopup, state: IAppState) {
  return 'repository' in popup &&
    state.selectedState?.type === SelectionType.Repository &&
    state.selectedState.repository.id === popup.repository.id
    ? state.selectedState.state
    : null
}

function initialDraft(popup: RepositoryPopup, state: IAppState): Draft {
  const values: Draft['values'] = {}
  switch (popup.type) {
    case PopupType.RenameBranch:
      values.name = popup.branch.name
      break
    case PopupType.CreateBranch: {
      values.name = popup.initialName ?? ''
      const branches = repositoryState(popup, state)?.branchesState
      values.startPoint = branches
        ? getStartPoint(branches, StartPoint.UpstreamDefaultBranch)
        : StartPoint.Head
      break
    }
    case PopupType.DeleteBranch:
      values.includeRemote = false
      break
    case PopupType.AddRepository:
      values.path = popup.path ?? ''
      break
    case PopupType.CreateRepository:
      values.name = popup.path ? Path.basename(popup.path) : ''
      values.path = popup.path ? Path.dirname(popup.path) : ''
      values.description = ''
      values.readme = false
      values.gitIgnore = 'None'
      values.license = 'None'
      break
    case PopupType.CloneRepository:
      values.url = popup.initialURL ?? ''
      values.path = ''
      values.tab = String(state.selectedCloneRepositoryTab)
      values.account = state.selectedCloneAccount
        ? getAccountKey(state.selectedCloneAccount)
        : state.accounts[0]
        ? getAccountKey(state.accounts[0])
        : ''
      values.repository = ''
      values.filter = ''
      break
    case PopupType.PublishRepository: {
      const repository = popup.repository
      const assigned =
        repository.login !== null && repository.gitHubRepository !== null
          ? state.accounts.find(a =>
              isSameAccount(a, {
                login: repository.login!,
                endpoint: repository.gitHubRepository!.endpoint,
              })
            )
          : undefined
      values.account = assigned
        ? getAccountKey(assigned)
        : state.accounts.length === 1
        ? getAccountKey(state.accounts[0])
        : ''
      values.name = repository.name
      values.description = ''
      values.private = true
      values.organization = ''
      break
    }
    case PopupType.RemoveRepository:
      values.deleteFromDisk = false
      break
    case PopupType.CreateTag:
      values.name = popup.initialName ?? ''
      break
    case PopupType.ChangeRepositoryAlias:
      values.alias = popup.repository.alias ?? popup.repository.name
      break
    case PopupType.AddWorktree:
      values.name = popup.initialWorktreeName ?? popup.initialBranchName ?? ''
      values.branch = popup.initialBranchName ?? ''
      values.path = ''
      break
    case PopupType.RenameWorktree:
      values.name = Path.basename(popup.worktreePath)
      break
    case PopupType.DeleteWorktree:
      values.doNotAskAgain = !state.askForConfirmationOnWorktreeRemoval
      break
    case PopupType.ChooseForkSettings:
      values.target = String(getForkContributionTarget(popup.repository))
      break
  }
  return {
    values,
    error: null,
    busy: false,
    loaded: false,
    gitIgnores: [],
    licenses: [],
    orgs: [],
    orgAccount: null,
    forkFailed: false,
    readmeExists: false,
    pathIsSubfolder: false,
    cloneTabs: new Map([
      [String(CloneRepositoryTab.GitHub), { ...values }],
      [String(CloneRepositoryTab.Generic), { ...values }],
    ]),
    cloneIdentifiers: new Map(),
  }
}

function draftFor(popup: RepositoryPopup, state: IAppState) {
  let draft = drafts.get(popup)
  if (!draft) {
    draft = initialDraft(popup, state)
    drafts.set(popup, draft)
  }
  return draft
}

function project(
  popupInput: Popup,
  state: IAppState
): NativeDialogPresentation {
  const popup = owned(popupInput)
  const draft = draftFor(popup, state)
  const v = draft.values
  const fields: NativeDialogField[] = []
  let actions: NativeDialogAction[] = []
  const repoState = repositoryState(popup, state)
  if ('repository' in popup) {
    fields.push(
      readonly('repositoryName', nameOf(popup.repository)),
      readonly('repositoryPath', popup.repository.path)
    )
  }
  const submit = (label: string, destructive = false) => {
    actions.push(action('confirm', label, true, destructive))
  }
  switch (popup.type) {
    case PopupType.RenameBranch:
      fields.push(
        readonly('branchName', popup.branch.name),
        text('name', String(v.name))
      )
      if (popup.branch.upstream) {
        fields.push(readonly('remoteBranch', popup.branch.upstream), {
          id: 'renameRemoteWarning',
          labelKey: key('renameRemoteWarning'),
          kind: 'readonly',
          readOnly: true,
        })
      }
      submit('rename')
      break
    case PopupType.DeleteBranch:
      fields.push(readonly('branchName', popup.branch.name))
      if (popup.existsOnRemote && popup.branch.upstreamRemoteName) {
        fields.push(check('includeRemote', Boolean(v.includeRemote)))
      }
      submit('delete', true)
      break
    case PopupType.DeleteRemoteBranch:
      fields.push(readonly('branchName', popup.branch.name))
      submit('delete', true)
      break
    case PopupType.CreateBranch: {
      const branches = repoState?.branchesState
      fields.push(text('name', String(v.name)))
      if (popup.targetCommit) {
        fields.push(
          readonly(
            'targetCommit',
            `${popup.targetCommit.sha}\n${popup.targetCommit.summary}`
          )
        )
      } else if (branches?.tip.kind === TipState.Valid) {
        const choices = [
          { id: StartPoint.CurrentBranch, label: branches.tip.branch.name },
        ]
        if (branches.upstreamDefaultBranch) {
          choices.unshift({
            id: StartPoint.UpstreamDefaultBranch,
            label: branches.upstreamDefaultBranch.name,
          })
        } else if (
          branches.defaultBranch &&
          branches.defaultBranch.name !== branches.tip.branch.name
        ) {
          choices.unshift({
            id: StartPoint.DefaultBranch,
            label: branches.defaultBranch.name,
          })
        }
        if (!choices.some(c => c.id === v.startPoint)) {
          v.startPoint = choices[0].id
        }
        fields.push(choice('startPoint', String(v.startPoint), choices))
      } else {
        fields.push(
          readonly(
            'targetCommit',
            branches?.tip.kind === TipState.Detached
              ? branches.tip.currentSha
              : 'HEAD'
          )
        )
      }
      if (
        branches?.allBranches.some(
          b =>
            b.type === BranchType.Remote &&
            b.nameWithoutRemote === sanitizedRefName(String(v.name))
        )
      ) {
        fields.push({
          id: 'branchRemoteWarning',
          labelKey: key('branchRemoteWarning'),
          kind: 'readonly',
          readOnly: true,
        })
      }
      submit('createBranch')
      if (isRepositoryWithForkedGitHubRepository(popup.repository)) {
        actions.push(action('forkSettings'))
      }
      break
    }
    case PopupType.AddRepository:
      fields.push(text('path', String(v.path)))
      submit('addRepository')
      actions.push(action('trust'), action('createHere'))
      break
    case PopupType.CreateRepository:
      fields.push(
        text('name', String(v.name)),
        text('path', String(v.path)),
        text('description', String(v.description)),
        check('readme', Boolean(v.readme)),
        choice(
          'gitIgnore',
          String(v.gitIgnore),
          ['None', ...draft.gitIgnores].map(id => ({
            id,
            label: id === 'None' ? '—' : id,
          }))
        ),
        choice(
          'license',
          String(v.license),
          ['None', ...draft.licenses.map(l => l.name)].map(id => ({
            id,
            label: id === 'None' ? '—' : id,
          }))
        )
      )
      if (draft.readmeExists && v.readme) {
        fields.push({
          id: 'readmeWarning',
          labelKey: key('readmeWarning'),
          kind: 'readonly',
          readOnly: true,
        })
      }
      if (draft.pathIsSubfolder) {
        fields.push({
          id: 'subfolderWarning',
          labelKey: key('subfolderWarning'),
          kind: 'readonly',
          readOnly: true,
        })
      }
      submit('createRepository')
      actions.push(action('load'), action('addInstead'))
      break
    case PopupType.CloneRepository: {
      fields.push(
        choice('tab', String(v.tab), [
          { id: String(CloneRepositoryTab.GitHub), label: 'GitHub' },
          { id: String(CloneRepositoryTab.Generic), label: 'URL' },
        ])
      )
      if (v.tab === String(CloneRepositoryTab.GitHub)) {
        fields.push(
          choice(
            'account',
            String(v.account),
            state.accounts.map(a => ({
              id: getAccountKey(a),
              label: `${a.login} (${a.friendlyEndpoint})`,
            }))
          ),
          text('filter', String(v.filter))
        )
        const account = state.accounts.find(a => getAccountKey(a) === v.account)
        const repositories = account
          ? state.apiRepositories.get(account)?.repositories ?? []
          : []
        fields.push(
          choice(
            'repository',
            String(v.repository),
            repositories
              .filter(r =>
                `${r.owner.login}/${r.name}`
                  .toLowerCase()
                  .includes(String(v.filter).toLowerCase())
              )
              .map(r => ({
                id: `${r.owner.login}/${r.name}`,
                label: `${r.owner.login}/${r.name}`,
              }))
          )
        )
        actions.push(action('refresh'))
      } else {
        fields.push(text('url', String(v.url)))
      }
      fields.push(text('path', String(v.path)))
      submit('clone')
      actions.push(action('load'), action('signIn'), action('signInEnterprise'))
      break
    }
    case PopupType.PublishRepository:
      fields.push(
        choice(
          'account',
          String(v.account),
          state.accounts.map(a => ({
            id: getAccountKey(a),
            label: `${a.login} (${a.friendlyEndpoint})`,
          }))
        ),
        text('name', String(v.name)),
        text('description', String(v.description)),
        check('private', Boolean(v.private)),
        choice('organization', String(v.organization), [
          { id: '', label: '—' },
          ...draft.orgs.map(o => ({ id: String(o.id), label: o.login })),
        ])
      )
      submit('publish')
      actions.push(action('load'), action('signIn'), action('signInEnterprise'))
      break
    case PopupType.RemoveRepository:
      fields.push(check('deleteFromDisk', Boolean(v.deleteFromDisk)))
      submit('remove', true)
      break
    case PopupType.CreateTag:
      fields.push(
        text('name', String(v.name)),
        readonly('targetCommit', popup.targetCommitSha)
      )
      if (popup.localTags) {
        fields.push({
          id: 'previousTags',
          labelKey: key('previousTags'),
          kind: 'list',
          choices: Array.from(popup.localTags.keys())
            .filter(t => t.includes(String(v.name)))
            .slice(-3)
            .map(id => ({ id, label: id })),
        })
      }
      submit('createTag')
      break
    case PopupType.DeleteTag:
      fields.push(readonly('tagName', popup.tagName))
      submit('delete', true)
      break
    case PopupType.ChangeRepositoryAlias:
      fields.push(text('alias', String(v.alias)))
      if (popup.repository.gitHubRepository) {
        fields.push({
          id: 'aliasWarning',
          labelKey: key('aliasWarning'),
          kind: 'readonly',
          readOnly: true,
        })
      }
      submit('saveAlias')
      break
    case PopupType.AddWorktree:
      fields.push(
        text('name', String(v.name), 'worktreeName'),
        text('path', String(v.path)),
        text('branch', String(v.branch))
      )
      if (repoState) {
        fields.push({
          id: 'branches',
          labelKey: key('branches'),
          kind: 'list',
          choices: repoState.branchesState.allBranches.map(b => ({
            id: b.ref,
            label: b.name,
          })),
        })
      }
      submit('createWorktree')
      actions.push(action('load'))
      break
    case PopupType.RenameWorktree:
      fields.push(
        readonly('worktreePath', popup.worktreePath),
        text('name', String(v.name))
      )
      submit('rename')
      break
    case PopupType.DeleteWorktree:
    case PopupType.DeleteWorktreeFailed: {
      fields.push(readonly('worktreePath', popup.worktreePath))
      const worktree = repoState?.worktrees.find(
        w => w.path === popup.worktreePath
      )
      if (worktree) {
        fields.push(
          {
            ...check('locked', worktree.isLocked),
            readOnly: true,
            disabled: true,
          },
          {
            ...check('missing', worktree.isPrunable),
            readOnly: true,
            disabled: true,
          }
        )
      }
      if (popup.type === PopupType.DeleteWorktree) {
        if (!popup.options.isMissing) {
          fields.push(check('doNotAskAgain', Boolean(v.doNotAskAgain)))
        }
        submit(popup.options.isMissing ? 'remove' : 'delete', true)
      } else {
        // Only the original visible message is transported, never Error metadata.
        fields.push({
          id: 'failure',
          labelKey: key('failure'),
          kind: 'multiline',
          value: displayDialogDiagnostic(
            getUnderlyingError(popup.error).toString(),
            state.accounts
          ),
          readOnly: true,
          height: 8,
        })
        submit('forceDelete', true)
      }
      break
    }
    case PopupType.ChooseForkSettings:
      fields.push(
        choice('target', String(v.target), [
          {
            id: String(ForkContributionTarget.Parent),
            label: popup.repository.gitHubRepository.parent.fullName,
          },
          {
            id: String(ForkContributionTarget.Self),
            label: popup.repository.gitHubRepository.fullName,
          },
        ])
      )
      submit('continue')
      break
    case PopupType.CreateFork:
      fields.push(
        readonly('parent', popup.repository.gitHubRepository.fullName),
        readonly(
          'fork',
          `${popup.account.login}/${popup.repository.gitHubRepository.name}`
        )
      )
      if (!draft.forkFailed) {
        submit('createFork')
      } else if (popup.repository.gitHubRepository.htmlURL) {
        actions.push(action('manualFork'))
      }
      break
    case PopupType.UpstreamAlreadyExists:
      fields.push(
        readonly('currentRemote', displayDialogURL(popup.existingRemote.url)),
        readonly(
          'expectedRemote',
          displayDialogURL(
            popup.repository.gitHubRepository?.parent?.cloneURL ?? ''
          )
        )
      )
      actions.push(action('update', 'update', true, true), action('ignore'))
      break
  }
  if (
    [
      PopupType.RenameBranch,
      PopupType.CreateBranch,
      PopupType.CreateTag,
    ].includes(popup.type)
  ) {
    const effectiveName = sanitizedRefName(String(v.name))
    if (effectiveName !== v.name) {
      fields.push(readonly('effectiveName', effectiveName))
    }
  }
  if (
    fields.some(
      f => !f.readOnly && ['text', 'checkbox', 'select'].includes(f.kind)
    )
  ) {
    actions.push(action('edit', 'checkInputs'))
  }
  actions.push(action('cancel'))
  if (
    [
      PopupType.AddRepository,
      PopupType.CreateRepository,
      PopupType.CloneRepository,
      PopupType.AddWorktree,
    ].includes(popup.type)
  ) {
    actions.push(action('choose'))
  }
  const requiredIDs = ['name', 'path', 'alias', 'url']
  const missingRequired = fields.some(
    f =>
      !f.readOnly &&
      requiredIDs.includes(f.id) &&
      (typeof f.value !== 'string' || f.value.trim().length === 0)
  )
  const invalidSelection =
    (popup.type === PopupType.PublishRepository ||
      (popup.type === PopupType.CloneRepository &&
        v.tab === String(CloneRepositoryTab.GitHub))) &&
    !state.accounts.some(a => getAccountKey(a) === v.account)
  const invalidTag =
    popup.type === PopupType.CreateTag &&
    (sanitizedRefName(String(v.name)).length > 245 ||
      popup.localTags?.has(sanitizedRefName(String(v.name))))
  const unchangedWorktree =
    popup.type === PopupType.RenameWorktree &&
    v.name === Path.basename(popup.worktreePath)
  // Mount owns initial loading. Retain the continuation only while incomplete
  // so a failed load can be retried without presenting a redundant setup step.
  if (draft.loaded) {
    actions = actions.filter(a => a.id !== 'load')
  }
  actions = actions.map(a =>
    a.id === 'confirm'
      ? {
          ...a,
          disabled:
            missingRequired ||
            invalidSelection ||
            !!invalidTag ||
            unchangedWorktree,
        }
      : a
  )
  return {
    popupID: popup.id ?? null,
    type: popup.type,
    supported: true,
    layout: 'form',
    titleKey: key(
      popup.type === PopupType.DeleteWorktree && popup.options.isMissing
        ? 'DeleteMissingWorktree'
        : popup.type
    ),
    descriptionKey:
      popup.type === PopupType.CreateBranch
        ? key(
            popup.targetCommit
              ? 'CreateBranchCommitDescription'
              : repoState?.branchesState.tip.kind === TipState.Unborn
              ? 'CreateBranchUnbornDescription'
              : repoState?.branchesState.tip.kind === TipState.Detached
              ? 'CreateBranchDetachedDescription'
              : 'CreateBranchDescription'
          )
        : [
            PopupType.DeleteBranch,
            PopupType.DeleteRemoteBranch,
            PopupType.DeleteTag,
            PopupType.RemoveRepository,
            PopupType.DeleteWorktree,
            PopupType.DeleteWorktreeFailed,
            PopupType.UpstreamAlreadyExists,
            PopupType.CreateFork,
            PopupType.ChooseForkSettings,
          ].includes(popup.type)
        ? key(`${popup.type}Description`)
        : undefined,
    fields: fields.map(f => ({
      ...f,
      required: (!f.readOnly && requiredIDs.includes(f.id)) || undefined,
      disabled: draft.busy || f.disabled,
      ...(!f.readOnly && ['text', 'checkbox', 'select'].includes(f.kind)
        ? { changeAction: 'edit' }
        : {}),
    })),
    actions: actions.map(a => ({
      ...a,
      fields: [
        'cancel',
        'manualFork',
        'forkSettings',
        'update',
        'ignore',
        'load',
      ].includes(a.id)
        ? []
        : fields
            .filter(
              f =>
                !f.readOnly &&
                !f.disabled &&
                ['text', 'checkbox', 'select'].includes(f.kind)
            )
            .map(f => f.id),
      ...(draft.busy ? { disabled: true } : {}),
    })),
    error:
      draft.error === null
        ? null
        : localErrorKeys[draft.error]
        ? key(`error.${localErrorKeys[draft.error]}`)
        : displayDialogDiagnostic(draft.error, state.accounts),
  }
}

function close(runtime: AppRuntime, popup: Popup) {
  if (popup.id !== undefined) {
    runtime.dispatcher.closePopupById(popup.id)
  } else {
    runtime.dispatcher.closePopup(popup.type)
  }
}

function unquote(path: string) {
  return path.startsWith('"') && path.endsWith('"') ? path.slice(1, -1) : path
}
function resolvedLocalPath(path: string) {
  return Path.resolve('/', untildify(unquote(path)))
}

async function cancel(runtime: AppRuntime, popupInput: Popup) {
  const popup = owned(popupInput)
  if (drafts.get(popup)?.busy) {
    throw new Error('Repository dialog is busy')
  }
  if (
    popup.type === PopupType.DeleteWorktreeFailed &&
    popup.originalWorktree !== null
  ) {
    // Upstream starts restoration before dismissal; the original object, not a
    // path supplied by the frontend, is passed to switchWorktree.
    const restoring = runtime.dispatcher.switchWorktree(
      popup.repository,
      popup.originalWorktree
    )
    close(runtime, popup)
    await restoring
    return
  }
  close(runtime, popup)
}

async function loadRepositoryDialogDefaults(
  popup: RepositoryPopup,
  draft: Draft,
  state: IAppState
) {
  const next = draft.values
  if (
    [
      PopupType.CreateRepository,
      PopupType.AddWorktree,
      PopupType.CloneRepository,
    ].includes(popup.type) &&
    !next.path
  ) {
    next.path = await getDefaultDir()
    if (popup.type === PopupType.CloneRepository) {
      const parsed = parseRepositoryCloneIdentifier(dialogString(next.url))
      const cloneName = parsed ? sanitizeCloneName(parsed.name) : null
      if (cloneName) {
        next.path = Path.join(dialogString(next.path), cloneName)
        draft.cloneIdentifiers.set(dialogString(next.tab), parsed)
      }
      const otherTab =
        next.tab === String(CloneRepositoryTab.GitHub)
          ? String(CloneRepositoryTab.Generic)
          : String(CloneRepositoryTab.GitHub)
      draft.cloneTabs.get(otherTab)!.path = await getDefaultDir()
    }
  }
  if (popup.type === PopupType.CreateRepository) {
    draft.gitIgnores = await getGitIgnoreNames()
    draft.licenses = await getLicenses()
  } else if (popup.type === PopupType.PublishRepository) {
    if (!draft.loaded) {
      next.description = (await getGitDescription(popup.repository.path)) ?? ''
    }
    const account = state.accounts.find(a => getAccountKey(a) === next.account)
    if (account) {
      const orgs = await API.fromAccount(account).fetchOrgs()
      // A later account edit must not inherit an earlier account's organizations.
      if (draft.values.account === getAccountKey(account)) {
        draft.orgs = [...orgs].sort((a, b) =>
          a.login.toLowerCase().localeCompare(b.login.toLowerCase())
        )
        draft.orgAccount = getAccountKey(account)
      }
    }
  }
  draft.loaded = true
}

/** Reject unknown actions/fields before any state mutation or Dispatcher call. */
async function reply(
  runtime: AppRuntime,
  popupInput: Popup,
  actionID: string,
  input: unknown
): Promise<void> {
  const popup = owned(popupInput)
  const state = runtime.appStore.getState()
  const presentation = project(popup, state)
  const draft = draftFor(popup, state)
  if (draft.busy) {
    throw new Error('Repository dialog is busy')
  }
  const editable = presentation.fields.filter(
    f =>
      !f.readOnly &&
      !f.disabled &&
      ['text', 'checkbox', 'select'].includes(f.kind)
  )
  const declared = presentation.actions.find(a => a.id === actionID)
  if (!declared) {
    throw new Error('Invalid repository dialog action')
  }
  const values = dialogValues(
    input === undefined ? {} : input,
    declared.fields ?? []
  )
  const next = { ...draft.values }
  for (const [id, value] of Object.entries(values)) {
    const field = editable.find(f => f.id === id)!
    const parsed =
      field.kind === 'checkbox' ? dialogBoolean(value) : dialogString(value)
    if (
      field.kind === 'select' &&
      !field.choices?.some(c => c.id === parsed && !c.disabled)
    ) {
      // Empty optional selections are valid; arbitrary IDs never are.
      if (parsed !== '' || !['account', 'repository'].includes(id)) {
        throw new Error('Invalid repository dialog selection')
      }
    }
    next[id] =
      id === 'url' && parsed === displayDialogURL(String(draft.values.url))
        ? draft.values.url
        : parsed
  }
  if (actionID === 'cancel') {
    await cancel(runtime, popup)
    return
  }
  const accountChanged = next.account !== draft.values.account
  const previousValues = draft.values
  if (
    popup.type === PopupType.CloneRepository &&
    next.tab !== previousValues.tab
  ) {
    draft.cloneTabs.set(String(previousValues.tab), {
      ...next,
      tab: previousValues.tab,
    })
    const saved = draft.cloneTabs.get(String(next.tab))!
    for (const id of ['url', 'path', 'repository', 'filter']) {
      next[id] = saved[id]
    }
  }
  draft.values = next
  draft.error = null
  if (accountChanged) {
    draft.orgs = []
    draft.orgAccount = null
    next.organization = ''
    if (popup.type === PopupType.CloneRepository) {
      const saved = draft.cloneTabs.get(String(CloneRepositoryTab.GitHub))!
      saved.repository = ''
      saved.filter = ''
      saved.url = ''
      if (next.tab === String(CloneRepositoryTab.GitHub)) {
        next.repository = ''
        next.filter = ''
        next.url = ''
      }
      const account = state.accounts.find(
        a => getAccountKey(a) === next.account
      )
      if (account) {
        runtime.dispatcher.changeCloneRepositoriesAccount(account)
      }
    }
  }
  if (popup.type === PopupType.CloneRepository) {
    await runtime.dispatcher.changeCloneRepositoriesTab(
      Number(next.tab) as CloneRepositoryTab
    )
    if (next.repository && !accountChanged) {
      const account = state.accounts.find(
        a => getAccountKey(a) === next.account
      )
      const repository =
        account &&
        state.apiRepositories
          .get(account)
          ?.repositories.find(
            r => `${r.owner.login}/${r.name}` === next.repository
          )
      if (!repository) {
        throw new Error('Clone repository selection is no longer available')
      }
      next.url = repository.clone_url
    }
    if (next.url !== previousValues.url && next.tab === previousValues.tab) {
      const identifier = parseRepositoryCloneIdentifier(dialogString(next.url))
      if (next.path === previousValues.path) {
        const update = updatedRepositoryCloneURL(
          dialogString(next.url),
          next.path ? dialogString(next.path) : null,
          draft.cloneIdentifiers.get(String(next.tab)) ?? null
        )
        next.path = update.path ?? ''
      }
      draft.cloneIdentifiers.set(String(next.tab), identifier)
    }
  }
  if (actionID === 'edit') {
    if (popup.type === PopupType.CreateRepository) {
      const fullPath = repositoryFullPath(
        dialogString(next.name),
        dialogString(next.path)
      )
      if (fullPath) {
        draft.readmeExists = await pathExists(Path.join(fullPath, 'README.md'))
        const type = await getRepositoryType(fullPath)
        draft.pathIsSubfolder =
          type.kind === 'regular' && type.topLevelWorkingDirectory !== fullPath
      }
    }
    if (
      popup.type === PopupType.RenameBranch ||
      popup.type === PopupType.CreateBranch
    ) {
      const branchName = sanitizedRefName(dialogString(next.name))
      const rule = await checkBranchNameRules(
        branchName,
        state.accounts,
        popup.repository,
        state.cachedRepoRulesets
      )
      draft.error = rule?.error.message ?? null
      if (
        popup.type === PopupType.CreateBranch &&
        runtime.repositoryStateManager
          .get(popup.repository)
          .branchesState.allBranches.some(branch => branch.name === branchName)
      ) {
        draft.error = 'A branch with this name already exists'
      }
    }
    if (accountChanged && popup.type === PopupType.PublishRepository) {
      await loadRepositoryDialogDefaults(
        popup,
        draft,
        runtime.appStore.getState()
      )
    }
    changed(popup)
    return
  }
  draft.busy = true
  changed(popup)
  const dispatcher = runtime.dispatcher
  const s = (id: string) => dialogString(next[id])
  const b = (id: string) => dialogBoolean(next[id])
  const name = () => {
    const result = sanitizedRefName(s('name'))
    if (result.length === 0) {
      throw new Error('A name is required')
    }
    return result
  }
  try {
    if (actionID === 'choose') {
      const selected =
        popup.type === PopupType.CloneRepository && __DARWIN__
          ? await showSaveDialog({
              buttonLabel: 'Select',
              nameFieldLabel: 'Clone As:',
              showsTagField: false,
              defaultPath: s('path'),
              properties: ['createDirectory'],
            })
          : await showOpenDialog({
              properties: ['createDirectory', 'openDirectory'],
            })
      if (selected !== null) {
        const parsed =
          popup.type === PopupType.CloneRepository
            ? parseRepositoryCloneIdentifier(s('url'))
            : null
        const safeName = parsed ? sanitizeCloneName(parsed.name) : null
        next.path =
          popup.type === PopupType.CloneRepository && !__DARWIN__ && safeName
            ? Path.join(selected, safeName)
            : selected
        if (popup.type === PopupType.CloneRepository) {
          draft.cloneIdentifiers.set(s('tab'), parsed)
        }
      }
      return
    }
    if (actionID === 'load') {
      await loadRepositoryDialogDefaults(popup, draft, state)
      return
    }
    if (actionID === 'signIn' || actionID === 'signInEnterprise') {
      const onResult: Parameters<
        typeof dispatcher.showDotComSignInDialog
      >[0] = result => {
        if (result.kind === 'success' && drafts.get(popup) === draft) {
          draft.values.account = getAccountKey(result.account)
          draft.values.organization = ''
          draft.orgs = []
          draft.orgAccount = null
          if (popup.type === PopupType.CloneRepository) {
            draft.values.repository = ''
            draft.values.filter = ''
            draft.values.url = ''
            dispatcher.changeCloneRepositoriesAccount(result.account)
          }
          changed(popup)
          if (popup.type === PopupType.PublishRepository) {
            void loadRepositoryDialogDefaults(
              popup,
              draft,
              runtime.appStore.getState()
            )
              .catch(error => {
                draft.error =
                  error instanceof Error ? error.message : String(error)
              })
              .finally(() => changed(popup))
          }
        }
      }
      if (actionID === 'signIn') {
        dispatcher.showDotComSignInDialog(onResult)
      } else {
        dispatcher.showEnterpriseSignInDialog(undefined, onResult)
      }
      return
    }
    switch (popup.type) {
      case PopupType.RenameBranch: {
        const newName = name()
        const rule = await checkBranchNameRules(
          newName,
          state.accounts,
          popup.repository,
          state.cachedRepoRulesets
        )
        if (rule && !rule.isWarning) {
          throw rule.error
        }
        const pending = dispatcher.renameBranch(
          popup.repository,
          popup.branch,
          newName
        )
        close(runtime, popup)
        await pending
        break
      }
      case PopupType.DeleteBranch:
        await dispatcher.deleteLocalBranch(
          popup.repository,
          popup.branch,
          popup.existsOnRemote && popup.branch.upstreamRemoteName !== null
            ? b('includeRemote')
            : false
        )
        dispatcher.executeCompare(popup.repository, {
          kind: HistoryTabMode.History,
        })
        close(runtime, popup)
        break
      case PopupType.DeleteRemoteBranch:
        await dispatcher.deleteRemoteBranch(popup.repository, popup.branch)
        dispatcher.executeCompare(popup.repository, {
          kind: HistoryTabMode.History,
        })
        close(runtime, popup)
        break
      case PopupType.CreateBranch: {
        if (actionID === 'forkSettings') {
          dispatcher.showPopup({
            type: PopupType.RepositorySettings,
            repository: popup.repository,
            initialSelectedTab: RepositorySettingsTab.ForkSettings,
          })
          break
        }
        const branchName = name()
        const branches = runtime.repositoryStateManager.get(
          popup.repository
        ).branchesState
        if (branches.tip.kind === TipState.Unknown) {
          close(runtime, popup)
          return
        }
        if (branches.allBranches.some(branch => branch.name === branchName)) {
          throw new Error('A branch with this name already exists')
        }
        const rule = await checkBranchNameRules(
          branchName,
          state.accounts,
          popup.repository,
          state.cachedRepoRulesets
        )
        if (rule && !rule.isWarning) {
          throw rule.error
        }
        const start = getStartPoint(branches, s('startPoint') as StartPoint)
        const startPoint =
          popup.targetCommit?.sha ??
          (start === StartPoint.DefaultBranch
            ? branches.defaultBranch?.name
            : start === StartPoint.UpstreamDefaultBranch
            ? branches.upstreamDefaultBranch?.name
            : null) ??
          null
        const timer = startTimer('create branch', popup.repository)
        await dispatcher.createBranch(
          popup.repository,
          branchName,
          startPoint,
          popup.targetCommit === undefined &&
            start === StartPoint.UpstreamDefaultBranch
        )
        timer.done()
        close(runtime, popup)
        // React's onBranchCreatedFromCommit only scrolls its compare list. The
        // native list is newly projected from the original branch/history state.
        break
      }
      case PopupType.AddRepository: {
        const path = s('path')
        if (!path) {
          throw new Error('A local path is required')
        }
        if (actionID === 'createHere') {
          close(runtime, popup)
          dispatcher.showPopup({
            type: PopupType.CreateRepository,
            path: resolvedLocalPath(path),
          })
          break
        }
        const type = await getRepositoryType(unquote(path))
        if (actionID === 'trust') {
          if (type.kind !== 'unsafe') {
            throw new Error(
              'The repository does not require a safe-directory exception'
            )
          }
          await addSafeDirectory(type.path)
          return
        }
        if (type.kind === 'missing') {
          throw new Error(
            'This directory does not appear to be a Git repository'
          )
        }
        if (type.kind === 'bare') {
          throw new Error('Bare repositories are not currently supported')
        }
        if (type.kind === 'unsafe') {
          throw new Error(
            'The repository is owned by another user; explicitly trust it to continue'
          )
        }
        close(runtime, popup)
        const repositories = await dispatcher.addRepositories([
          resolvedLocalPath(path),
        ])
        if (repositories.length) {
          dispatcher.closeFoldout(FoldoutType.Repository)
          await dispatcher.selectRepository(repositories[0])
          dispatcher.recordAddExistingRepository()
        }
        break
      }
      case PopupType.CreateRepository: {
        const fullPath = repositoryFullPath(s('name'), s('path'))
        if (!fullPath) {
          throw new Error('A repository name and local path are required')
        }
        if (actionID === 'addInstead') {
          close(runtime, popup)
          dispatcher.showPopup({
            type: PopupType.AddRepository,
            path: fullPath,
          })
          break
        }
        if (
          s('gitIgnore') !== 'None' &&
          !draft.gitIgnores.includes(s('gitIgnore'))
        ) {
          throw new Error('Unknown gitignore')
        }
        if (
          s('license') !== 'None' &&
          !draft.licenses.some(l => l.name === s('license'))
        ) {
          throw new Error('Unknown license')
        }
        const canonical = await canonicalMutationPath(fullPath)
        if (isRepositoryAffectedByAssistedCommit(canonical)) {
          throw new Error(
            'Finish or cancel the assisted commit run before creating a repository in this directory'
          )
        }
        const type = await getRepositoryType(fullPath)
        const isRepository =
          type.kind === 'unsafe'
            ? await directoryExists(Path.join(fullPath, '.git'))
            : type.kind === 'regular'
            ? type.topLevelWorkingDirectory === fullPath
            : type.kind !== 'missing'
        if (isRepository) {
          throw new Error(
            'This directory already contains a Git repository; add it instead'
          )
        }
        const options = {
          name: s('name'),
          description: s('description'),
          createWithReadme: b('readme'),
          gitIgnore: s('gitIgnore'),
          license: s('license'),
          licenses: draft.licenses,
        }
        const postError = (error: Error) =>
          withoutRepositoryGitAccess(() => dispatcher.postError(error))
        const repository = await dispatcher.createRepository(fullPath, () =>
          createRepositoryCore(
            dispatcher,
            fullPath,
            options,
            postError,
            valid => {
              if (!valid) {
                draft.error = 'Directory could not be created at this path'
              }
            }
          )
        )
        if (repository) {
          if (!popup.path) {
            setDefaultDir(s('path'))
          }
          dispatcher.closeFoldout(FoldoutType.Repository)
          await dispatcher.selectRepository(repository)
          dispatcher.recordCreateRepository()
          close(runtime, popup)
        }
        break
      }
      case PopupType.CloneRepository: {
        const account =
          state.accounts.find(a => getAccountKey(a) === next.account) ?? null
        if (actionID === 'refresh') {
          if (!account) {
            throw new Error('Select a signed-in account')
          }
          await dispatcher.refreshApiRepositories(account)
          break
        }
        const tab = Number(s('tab')) as CloneRepositoryTab
        if (
          tab === CloneRepositoryTab.GitHub &&
          (!account || !next.repository)
        ) {
          throw new Error(
            'Select a repository for the current signed-in account'
          )
        }
        if (!s('url') || !s('path')) {
          throw new Error('A repository URL and local path are required')
        }
        const request = {
          tab,
          url: s('url'),
          path: s('path'),
          lastParsedIdentifier: parseRepositoryCloneIdentifier(s('url')),
          account: tab === CloneRepositoryTab.GitHub ? account : null,
        }
        const pathError = await validateRepositoryClonePath(request.path)
        if (pathError) {
          throw pathError
        }
        const info = await resolveRepositoryCloneInfo(request, state.accounts)
        if (!info) {
          throw new Error(
            'Repository not found; check account, network and URL'
          )
        }
        const currentPathError = await validateRepositoryClonePath(request.path)
        if (currentPathError) {
          throw currentPathError
        }
        dispatcher.closeFoldout(FoldoutType.Repository)
        dispatcher.clone(
          info.url.trim(),
          request.path,
          request.account === null
            ? { defaultBranch: info.defaultBranch }
            : {
                defaultBranch: info.defaultBranch,
                account: {
                  endpoint: request.account.endpoint,
                  login: request.account.login,
                },
              }
        )
        close(runtime, popup)
        setDefaultDir(Path.resolve(request.path, '..'))
        break
      }
      case PopupType.PublishRepository: {
        const account = state.accounts.find(
          a => getAccountKey(a) === next.account
        )
        if (!account) {
          throw new Error('Select a signed-in account')
        }
        const repositoryName = sanitizedRepositoryName(s('name'))
        if (!repositoryName) {
          throw new Error('A repository name is required')
        }
        const org = s('organization')
          ? draft.orgs.find(o => String(o.id) === s('organization'))
          : null
        if (
          s('organization') &&
          (!org || draft.orgAccount !== getAccountKey(account))
        ) {
          throw new Error(
            'Organization is no longer available for the selected account'
          )
        }
        await dispatcher.publishRepository(
          popup.repository,
          repositoryName,
          s('description'),
          b('private'),
          account,
          org ?? null
        )
        close(runtime, popup)
        break
      }
      case PopupType.RemoveRepository:
        await dispatcher.removeRepository(popup.repository, b('deleteFromDisk'))
        close(runtime, popup)
        break
      case PopupType.CreateTag: {
        const tagName = name()
        if (tagName.length > 245) {
          throw new Error('The tag name cannot be longer than 245 characters')
        }
        if (popup.localTags?.has(tagName)) {
          throw new Error('A tag with this name already exists')
        }
        const timer = startTimer('create tag', popup.repository)
        await dispatcher.createTag(
          popup.repository,
          tagName,
          popup.targetCommitSha
        )
        timer.done()
        close(runtime, popup)
        break
      }
      case PopupType.DeleteTag:
        await dispatcher.deleteTag(popup.repository, popup.tagName)
        close(runtime, popup)
        break
      case PopupType.ChangeRepositoryAlias: {
        if (!s('alias')) {
          throw new Error('An alias is required')
        }
        const pending = dispatcher.changeRepositoryAlias(
          popup.repository,
          s('alias')
        )
        close(runtime, popup)
        await pending
        break
      }
      case PopupType.AddWorktree: {
        const fullPath = repositoryFullPath(s('name'), s('path'))
        const branchName = sanitizedRefName(s('branch') || s('name'))
        if (!fullPath || !branchName) {
          throw new Error('A worktree name, path and branch are required')
        }
        const branch = runtime.repositoryStateManager
          .get(popup.repository)
          .branchesState.allBranches.find(b => b.name === branchName)
        const options =
          branch?.type === BranchType.Remote
            ? { createBranch: branch.nameWithoutRemote, commitish: branch.ref }
            : branch
            ? { commitish: branch.name }
            : { createBranch: branchName }
        if (
          await dispatcher.addWorktreeAndSwitch(
            popup.repository,
            fullPath,
            options
          )
        ) {
          close(runtime, popup)
        }
        break
      }
      case PopupType.RenameWorktree: {
        const newName = s('name')
        if (!newName || newName === Path.basename(popup.worktreePath)) {
          throw new Error('Choose a different worktree name')
        }
        if (
          await dispatcher.moveWorktree(
            popup.repository,
            popup.worktreePath,
            Path.join(Path.dirname(popup.worktreePath), newName)
          )
        ) {
          close(runtime, popup)
        }
        break
      }
      case PopupType.DeleteWorktree:
        dispatcher.setConfirmWorktreeRemovalSetting(!b('doNotAskAgain'))
        await dispatcher.deleteWorktree(
          popup.repository,
          popup.worktreePath,
          popup.options
        )
        close(runtime, popup)
        break
      case PopupType.DeleteWorktreeFailed:
        await dispatcher.deleteWorktree(popup.repository, popup.worktreePath, {
          ...popup.options,
          force: true,
        })
        close(runtime, popup)
        break
      case PopupType.ChooseForkSettings: {
        const pending = dispatcher.updateRepositoryWorkflowPreferences(
          popup.repository,
          {
            forkContributionTarget:
              s('target') === String(ForkContributionTarget.Parent)
                ? ForkContributionTarget.Parent
                : ForkContributionTarget.Self,
          }
        )
        close(runtime, popup)
        await pending
        break
      }
      case PopupType.CreateFork:
        if (actionID === 'manualFork') {
          const url = popup.repository.gitHubRepository.htmlURL
          if (url) {
            await dispatcher.openInBrowser(url)
          }
          break
        }
        await createRepositoryFork(
          dispatcher,
          popup.repository,
          popup.account,
          () => close(runtime, popup),
          error => {
            draft.error = error.message
            draft.forkFailed = true
          }
        )
        break
      case PopupType.UpstreamAlreadyExists: {
        const pending =
          actionID === 'update'
            ? dispatcher.updateExistingUpstreamRemote(popup.repository)
            : dispatcher.ignoreExistingUpstreamRemote(popup.repository)
        close(runtime, popup)
        await pending
        break
      }
    }
  } catch (error) {
    draft.error = error instanceof Error ? error.message : String(error)
    // Form errors stay local just as in original Publish/Clone/Create UI. Core
    // Dispatcher errors still run its full registered recovery-handler chain.
    throw error
  } finally {
    draft.busy = false
    changed(popup)
  }
}

/** Repository partition of the parent's single MyGo native dialog renderer. */
export const repositoryDialogs: NativeDialogModule = {
  types,
  project,
  reply,
  cancel,
}

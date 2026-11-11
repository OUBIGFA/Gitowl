import assert from 'node:assert/strict'
import { after, afterEach, before, describe, it } from 'node:test'
import { EventEmitter } from 'events'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { ipcRenderer } from 'electron'
import { exec } from 'dugite'
import type { AppRuntime } from '../../src/ui/app-runtime'
import { Repository } from '../../src/models/repository'
import { Popup, PopupType } from '../../src/models/popup'
import { RetryActionType } from '../../src/models/retry-actions'
import { TipState } from '../../src/models/tip'
import { MultiCommitOperationStepKind as Step } from '../../src/models/multi-commit-operation'
import {
  DiffSelection,
  DiffSelectionType,
  DiffType,
} from '../../src/models/diff'
import { getCommit, getWorkingDirectoryDiff } from '../../src/lib/git'
import { acquireAssistedCommitGitLease } from '../../src/lib/git/repository-operation'
import {
  commitDialogContext,
  operationDialogs,
  mountOperationDialog,
} from '../../src/native-kernel/operation-dialogs'

// Only Electron's platform boundary is simulated. All stores, popup management,
// original error handlers, Dispatcher and Git run against isolated local repos.
const ipc = new EventEmitter()
let trashFails = false
const root = resolve('_temp/native-full-ui')
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
  invoke: async (channel: string, path?: string) => {
    if (channel === 'move-to-trash') {
      assert.ok(path && path.startsWith(root), 'Never trash a real user path')
      if (trashFails) {
        throw new Error('Fixture trash unavailable')
      }
      await rm(path, { force: true })
    }
  },
})

async function git(path: string, args: ReadonlyArray<string>) {
  const result = await exec([...args], path)
  assert.equal(result.exitCode, 0, result.stderr)
  return result.stdout.trim()
}

const labelsPromise = readFile(
  resolve('references/native-ui-operation-labels.json'),
  'utf8'
).then(text => JSON.parse(text) as Record<string, Record<string, string>>)

describe(
  'native operation dialogs through the original runtime and local Git',
  // This deadline covers the whole suite of real repositories, not one dialog.
  // On Windows the 17 workflows exceed two minutes even without parallel files.
  { timeout: 600000 },
  () => {
    let runtime: AppRuntime
    const directories: string[] = []

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
      // Same original error subscription as native-kernel/renderer.ts. Without
      // it, AppStore failures have no presentation host to request recovery.
      runtime.appStore.onDidError(error => {
        void runtime.dispatcher.postError(error)
      })
      await mkdir(root, { recursive: true })
    })

    afterEach(() => {
      trashFails = false
      // Test ownership cleanup is not a native automatic abort/retry policy.
      while (runtime.appStore.getState().currentPopup !== null) {
        runtime.dispatcher.closePopupById(
          runtime.appStore.getState().currentPopup!.id!
        )
      }
    })

    after(async () => {
      await runtime.appStore._selectRepository(null)
      const repositories = runtime.appStore
        .getState()
        .repositories.filter(
          (r): r is Repository =>
            r instanceof Repository && directories.includes(r.path)
        )
      const leases = await Promise.all(
        repositories.map(r => acquireAssistedCommitGitLease(r.path))
      )
      try {
        await Promise.all(
          directories.map(path =>
            rm(path, {
              recursive: true,
              force: true,
              maxRetries: 5,
              retryDelay: 50,
            })
          )
        )
      } finally {
        leases.forEach(lease => lease.release())
        window.close()
      }
    })

    async function fixture() {
      const path = await mkdtemp(join(root, 'operation-git-'))
      directories.push(path)
      const hooks = join(path, 'empty-hooks')
      await mkdir(hooks)
      await git(path, ['init', '--template=', '--initial-branch=main'])
      for (const [key, value] of [
        ['user.name', 'Operation Fixture'],
        ['user.email', 'operation@example.test'],
        ['commit.gpgSign', 'false'],
        ['core.hooksPath', hooks],
        ['core.autocrlf', 'false'],
        ['core.eol', 'lf'],
      ]) {
        await git(path, ['config', key, value])
      }
      await writeFile(join(path, 'file.txt'), 'one\ntwo\nthree\n')
      await git(path, ['add', 'file.txt'])
      await git(path, ['commit', '-m', 'initial'])
      await git(path, ['branch', 'feature'])
      await runtime.dispatcher.addRepositories([path])
      const repository = runtime.appStore
        .getState()
        .repositories.find(
          (r): r is Repository => r instanceof Repository && r.path === path
        )
      assert.ok(repository)
      await runtime.appStore._selectRepository(repository)
      await runtime.dispatcher.refreshRepository(repository)
      const initial = await getCommit(repository, 'HEAD')
      assert.ok(initial)
      return { repository, path, initial }
    }

    function current<T extends PopupType>(
      type: T
    ): Extract<Popup, { type: T }> {
      const popup = runtime.appStore.getState().currentPopup
      assert.ok(
        popup?.type === type,
        `Expected ${type}, got ${popup?.type ?? 'no popup'}`
      )
      return popup as Extract<Popup, { type: T }>
    }
    async function show<T extends PopupType>(
      popup: Extract<Popup, { type: T }>
    ) {
      await runtime.dispatcher.showPopup(popup)
      return current(popup.type)
    }
    async function respond(popup: Popup, action: string, values?: unknown) {
      await operationDialogs.reply(runtime, popup, action, values)
    }
    function view(popup: Popup) {
      return operationDialogs.project(popup, runtime.appStore.getState())
    }
    function state(repository: Repository) {
      return runtime.repositoryStateManager.get(repository)
    }
    async function changed(repository: Repository, content: string) {
      await writeFile(join(repository.path, 'file.txt'), content)
      await runtime.dispatcher.refreshRepository(repository)
      const file = state(repository).changesState.workingDirectory.files.find(
        f => f.path === 'file.txt'
      )
      assert.ok(file)
      return file
    }

    it('projects the owning popup repository after another repository is selected', async () => {
      const owner = await fixture()
      const other = await fixture()
      await git(other.path, ['checkout', 'feature'])
      await runtime.dispatcher.refreshRepository(other.repository)
      const branch = state(owner.repository).branchesState.allBranches.find(
        b => b.name === 'feature'
      )!
      const popup = await show({
        type: PopupType.StashAndSwitchBranch,
        repository: owner.repository,
        branchToCheckout: branch,
      })
      const mounted = mountOperationDialog(runtime, popup, () => {})
      try {
        const result = view(popup)
        assert.equal(result.fields.find(f => f.id === 'current')?.text, 'main')
        assert.equal(
          runtime.appStore.getState().selectedState?.repository.path,
          other.path
        )
      } finally {
        mounted.dispose()
      }
      const unavailable = view(popup)
      assert.equal(
        unavailable.descriptionKey,
        'operation.repositoryUnavailable'
      )
      assert.deepEqual(
        unavailable.actions.map(a => a.id),
        ['cancel']
      )
      await operationDialogs.cancel(runtime, popup)
      assert.equal(await git(owner.path, ['branch', '--show-current']), 'main')
      assert.equal(
        await git(other.path, ['branch', '--show-current']),
        'feature'
      )
    })

    it('projects all 20 assigned types as explicit fields/actions with complete parameter-free catalog keys', async () => {
      const { repository, initial } = await fixture()
      const file = await changed(repository, 'changed\ntwo\nthree\n')
      const diff = await getWorkingDirectoryDiff(repository, file)
      assert.equal(diff.kind, DiffType.Text)
      assert.ok(diff.kind === DiffType.Text)
      const branch = state(repository).branchesState.allBranches.find(
        b => b.name === 'feature'
      )!
      await runtime.dispatcher.createStashForCurrentBranch(repository, false)
      const stash = state(repository).changesState.stashEntry
      assert.ok(stash)
      await runtime.dispatcher.startMergeBranchOperation(
        repository,
        false,
        null
      )
      const context = {
        summary: 'fixture',
        description: '',
        trailers: [],
        amend: false,
      }
      const callback = () => assert.fail('Projection cannot settle callbacks')
      const popups: Popup[] = [
        { type: PopupType.ConfirmDiscardChanges, repository, files: [file] },
        {
          type: PopupType.ConfirmDiscardSelection,
          repository,
          file,
          diff,
          selection: file.selection,
        },
        {
          type: PopupType.OversizedFiles,
          repository,
          context,
          oversizedFiles: ['large.bin'],
        },
        {
          type: PopupType.CommitConflictsWarning,
          repository,
          context,
          files: [file],
        },
        {
          type: PopupType.StashAndSwitchBranch,
          repository,
          branchToCheckout: branch,
        },
        {
          type: PopupType.ConfirmOverwriteStash,
          repository,
          branchToCheckout: null,
        },
        { type: PopupType.ConfirmDiscardStash, repository, stash },
        { type: PopupType.ConfirmCheckoutCommit, repository, commit: initial },
        {
          type: PopupType.LocalChangesOverwritten,
          repository,
          files: ['file.txt'],
          retryAction: { type: RetryActionType.Checkout, repository, branch },
        },
        {
          type: PopupType.WarnLocalChangesBeforeUndo,
          repository,
          commit: initial,
          isWorkingDirectoryClean: true,
        },
        { type: PopupType.WarningBeforeReset, repository, commit: initial },
        {
          type: PopupType.WarnForcePush,
          operation: 'Amend',
          onBegin: callback,
        },
        {
          type: PopupType.DiscardChangesRetry,
          retryAction: {
            type: RetryActionType.DiscardChanges,
            repository,
            files: [file],
          },
        },
        {
          type: PopupType.UnknownAuthors,
          authors: [
            { kind: 'unknown', username: 'unresolved', state: 'error' },
          ],
          onCommit: callback,
        },
        {
          type: PopupType.ConfirmCommitFilteredChanges,
          onCommitAnyway: callback,
          showFilesToBeCommitted: callback,
        },
        {
          type: PopupType.CommitMessage,
          repository,
          commitMessage: null,
          coAuthors: [],
          showCoAuthoredBy: false,
          dialogTitle: 'Original context',
          dialogButtonText: 'Squash',
          prepopulateCommitSummary: false,
          onSubmitCommitMessage: async () => {
            callback()
            return false
          },
        },
        { type: PopupType.MultiCommitOperation, repository },
        {
          type: PopupType.AssistedCommitDisclaimer,
          repository,
          onAccepted: callback,
        },
        { type: PopupType.CopilotConflictResolutionDisclaimer, repository },
        { type: PopupType.CopilotConflictResolutionAlwaysNudge, repository },
      ]
      assert.deepEqual(
        popups.map(p => p.type),
        operationDialogs.types
      )
      const labels = await labelsPromise
      for (const popup of popups) {
        const dto = view(
          Object.assign(popup, { id: 100, secret: 'TOKEN-SENTINEL', callback })
        )
        assert.equal(dto.supported, true)
        assert.equal(dto.layout, 'form')
        const serialized = JSON.stringify(dto)
        assert.doesNotMatch(
          serialized,
          /TOKEN-SENTINEL|onAccepted|onBegin|onCommit|onSubmitCommitMessage|retryAction|repository|selection|hunks|manualResolutions/
        )
        assert.equal(new Set(dto.fields.map(f => f.id)).size, dto.fields.length)
        assert.equal(
          new Set(dto.actions.map(a => a.id)).size,
          dto.actions.length
        )
        for (const action of dto.actions) {
          assert.ok(
            Array.isArray(action.fields),
            `No field contract for ${popup.type}/${action.id}`
          )
          assert.ok(
            action.fields.every(id =>
              dto.fields.some(field => field.id === id && !field.readOnly)
            )
          )
          if (action.id === 'cancel') {
            assert.deepEqual(action.fields, [])
          }
        }
        for (const key of [
          dto.titleKey,
          dto.descriptionKey,
          ...dto.fields.map(f => f.labelKey),
          ...dto.actions.map(a => a.labelKey),
        ].filter((k): k is string => k !== undefined)) {
          assert.ok(labels[key], `Missing catalog key ${key}`)
          assert.deepEqual(Object.keys(labels[key]).sort(), [
            'en',
            'zh-Hans',
            'zh-Hant',
          ])
          assert.ok(
            Object.values(labels[key]).every(
              text => text.length > 0 && !/\{\w+\}/.test(text)
            )
          )
        }
      }
      await operationDialogs.cancel(
        runtime,
        current(PopupType.MultiCommitOperation)
      )
      assert.equal(state(repository).multiCommitOperationState, null)
    })

    it('keeps original empty-summary prepopulation and notifies the parent on local callback completion', async () => {
      const { repository, path, initial } = await fixture()
      const contexts: unknown[] = []
      const popup = await show({
        type: PopupType.CommitMessage,
        repository,
        coAuthors: [],
        showCoAuthoredBy: false,
        commitMessage: null,
        dialogTitle: 'Original empty summary',
        dialogButtonText: 'Squash',
        prepopulateCommitSummary: true,
        onSubmitCommitMessage: async context => {
          contexts.push(context)
          return false
        },
      })
      let updates = 0
      const handle = mountOperationDialog(runtime, popup, () => updates++)
      try {
        await respond(popup, 'submit', {
          summary: '',
          description: 'retain',
          showCoAuthors: false,
          skipCommitHooks: false,
          signOffCommits: false,
        })
        assert.equal((contexts[0] as { summary: string }).summary, '')
        assert.ok(
          updates >= 2,
          'Busy start and completion must reproject without a Git state update'
        )
        assert.equal(
          view(popup).fields.find(f => f.id === 'description')?.value,
          'retain'
        )
        assert.equal(await git(path, ['rev-parse', 'HEAD']), initial.sha)
        handle.dispose()
        const count = updates
        await respond(popup, 'submit', {
          summary: 'explicit',
          description: '',
          showCoAuthors: false,
          skipCommitHooks: false,
          signOffCommits: false,
        })
        assert.equal(
          updates,
          count,
          'A disposed parent observer received a late notification'
        )
      } finally {
        handle.dispose()
      }
      await operationDialogs.cancel(runtime, popup)
    })

    it('rejects extra fields, invalid actions and non-boolean choices before callback/settings changes', async () => {
      let calls = 0
      const popup = await show({
        type: PopupType.WarnForcePush,
        operation: 'Amend',
        onBegin: () => calls++,
      })
      await runtime.dispatcher.setConfirmForcePushSetting(true)
      for (const [action, values] of [
        ['onBegin', {}],
        ['submit', { confirm: 'false' }],
        ['submit', { confirm: false, sha: 'WIRE-SHA' }],
        ['submit', { confirm: false, repository: {} }],
        ['submit', []],
        ['cancel', { confirm: false }],
        ['submit', null],
      ] as const) {
        await assert.rejects(respond(popup, action, values))
        assert.equal(calls, 0)
        assert.equal(
          runtime.appStore.getState().askForConfirmationOnForcePush,
          true
        )
        assert.equal(current(PopupType.WarnForcePush), popup)
      }
      const events: boolean[] = []
      const settled = await show({
        type: PopupType.WarnForcePush,
        operation: 'Amend',
        onBegin: () => {
          events.push(
            runtime.appStore.getState().currentPopup?.type ===
              PopupType.WarnForcePush
          )
        },
      })
      // Duplicate original popup types are deliberately not replaced by AppStore.
      assert.equal(settled, popup)
      await respond(popup, 'submit', { confirm: false })
      assert.equal(calls, 1)
      assert.deepEqual(events, [])
      assert.equal(runtime.appStore.getState().currentPopup, null)
      assert.equal(
        runtime.appStore.getState().askForConfirmationOnForcePush,
        false
      )
    })

    it('settles original callback confirmations before dismissal; cancellation never accepts', async () => {
      const { repository } = await fixture()
      for (const type of [
        PopupType.UnknownAuthors,
        PopupType.ConfirmCommitFilteredChanges,
        PopupType.AssistedCommitDisclaimer,
      ] as const) {
        const events: string[] = []
        const accept = () => events.push(current(type).type)
        const input: Popup =
          type === PopupType.UnknownAuthors
            ? { type, authors: [], onCommit: accept }
            : type === PopupType.ConfirmCommitFilteredChanges
            ? {
                type,
                onCommitAnyway: accept,
                showFilesToBeCommitted: () => events.push('showFiles'),
              }
            : { type, repository, onAccepted: accept }
        const cancelled = await show(input)
        await operationDialogs.cancel(runtime, cancelled)
        assert.deepEqual(events, [])
        const confirmed = await show(input)
        await respond(
          confirmed,
          'submit',
          type === PopupType.ConfirmCommitFilteredChanges
            ? { confirm: false }
            : {}
        )
        assert.deepEqual(events, [type])
        assert.equal(runtime.appStore.getState().currentPopup, null)
        if (type === PopupType.ConfirmCommitFilteredChanges) {
          const showFiles = await show(input)
          await respond(showFiles, 'showFiles')
          assert.deepEqual(events, [type, 'showFiles'])
        }
      }
    })

    it('Copilot consent cancellation preserves original preferences and accepted choices use the real no-account continuation', async () => {
      const { repository } = await fixture()
      const before =
        runtime.appStore.getState().copilotConflictResolutionDisclaimerLastSeen
      const cancelled = await show({
        type: PopupType.CopilotConflictResolutionDisclaimer,
        repository,
      })
      await operationDialogs.cancel(runtime, cancelled)
      assert.equal(
        runtime.appStore.getState().copilotConflictResolutionDisclaimerLastSeen,
        before
      )
      const accepted = await show({
        type: PopupType.CopilotConflictResolutionDisclaimer,
        repository,
      })
      const time = Date.now()
      await respond(accepted, 'submit')
      assert.ok(
        (runtime.appStore.getState()
          .copilotConflictResolutionDisclaimerLastSeen ?? 0) >= time
      )
      await runtime.dispatcher.setAlwaysUseCopilotForConflictResolution(false)
      const no = await show({
        type: PopupType.CopilotConflictResolutionAlwaysNudge,
        repository,
      })
      await respond(no, 'no')
      assert.equal(
        runtime.appStore.getState().alwaysUseCopilotForConflictResolution,
        false
      )
      const close = await show({
        type: PopupType.CopilotConflictResolutionAlwaysNudge,
        repository,
      })
      await operationDialogs.cancel(runtime, close)
      assert.equal(
        runtime.appStore.getState().alwaysUseCopilotForConflictResolution,
        false
      )
      const yes = await show({
        type: PopupType.CopilotConflictResolutionAlwaysNudge,
        repository,
      })
      await respond(yes, 'yes')
      assert.equal(
        runtime.appStore.getState().alwaysUseCopilotForConflictResolution,
        true
      )
      // No paid or authenticated path was run; the original preflight returns
      // without inventing a resolution when no operation/account is available.
      assert.equal(state(repository).multiCommitOperationState, null)
      assert.equal(runtime.appStore.getState().currentPopup, null)
      await runtime.dispatcher.setAlwaysUseCopilotForConflictResolution(false)
    })

    it('LocalChangesOverwritten cancellation closes a rebase outer popup without aborting Git or ending its state', async () => {
      const { repository, path } = await fixture()
      const branch = state(repository).branchesState.allBranches.find(
        b => b.name === 'feature'
      )!
      const tip = state(repository).branchesState.tip
      assert.ok(tip.kind === TipState.Valid)
      const input: Popup = {
        type: PopupType.LocalChangesOverwritten,
        repository,
        files: [],
        retryAction: {
          type: RetryActionType.Rebase,
          repository,
          baseBranch: branch,
          targetBranch: tip.branch,
        },
      }
      await runtime.dispatcher.showRebaseDialog(repository, branch)
      const operation = state(repository).multiCommitOperationState
      assert.ok(operation)
      const head = await git(path, ['rev-parse', 'HEAD'])
      await operationDialogs.cancel(runtime, await show(input))
      assert.equal(runtime.appStore.getState().currentPopup, null)
      assert.equal(state(repository).multiCommitOperationState, operation)
      assert.equal(await git(path, ['rev-parse', 'HEAD']), head)
      await changed(repository, 'stash for close\n')
      await runtime.dispatcher.createStashForCurrentBranch(repository, false)
      await runtime.dispatcher.showPopup({
        type: PopupType.MultiCommitOperation,
        repository,
      })
      const outer = current(PopupType.MultiCommitOperation)
      const close = await show(input)
      assert.deepEqual(
        view(close).actions.map(a => a.id),
        ['close']
      )
      await respond(close, 'close')
      assert.equal(current(PopupType.MultiCommitOperation), outer)
      assert.equal(state(repository).multiCommitOperationState, operation)
      // Test cleanup explicitly ends the untouched UI flow.
      runtime.dispatcher.endMultiCommitOperation(repository)
    })

    it('CommitMessage unknown-author confirmation settles its original callback once and holds busy until completion', async () => {
      const { repository } = await fixture()
      let calls = 0
      let finish!: (value: boolean) => void
      const pending = new Promise<boolean>(resolve => {
        finish = resolve
      })
      const popup = await show({
        type: PopupType.CommitMessage,
        repository,
        coAuthors: [
          { kind: 'unknown', username: 'unresolved', state: 'error' },
        ],
        showCoAuthoredBy: true,
        commitMessage: null,
        dialogTitle: 'Original callback',
        dialogButtonText: 'Continue',
        prepopulateCommitSummary: false,
        onSubmitCommitMessage: async () => {
          calls++
          return pending
        },
      })
      const values = {
        summary: 'retain after cancel',
        description: 'body',
        showCoAuthors: true,
        skipCommitHooks: false,
        signOffCommits: false,
      }
      await respond(popup, 'submit', values)
      assert.equal(calls, 0)
      await operationDialogs.cancel(runtime, current(PopupType.UnknownAuthors))
      assert.equal(calls, 0)
      assert.equal(
        view(popup).fields.find(f => f.id === 'summary')?.value,
        values.summary
      )
      await respond(popup, 'submit', values)
      await respond(current(PopupType.UnknownAuthors), 'submit')
      assert.equal(calls, 1)
      assert.ok(view(popup).actions.every(a => a.disabled))
      await assert.rejects(respond(popup, 'submit', values))
      finish(false)
      const deadline = Date.now() + 5000
      while (
        view(popup).actions.some(a => a.disabled) &&
        Date.now() < deadline
      ) {
        await new Promise(resolve => setTimeout(resolve, 10))
      }
      assert.ok(view(popup).actions.every(a => !a.disabled))
      assert.equal(
        view(popup).fields.find(f => f.id === 'summary')?.value,
        values.summary
      )
      await operationDialogs.cancel(runtime, popup)
    })

    it('discard cancellation retains files, confirmation uses original file objects and trash-failure retry', async () => {
      const { repository, path, initial } = await fixture()
      const file = await changed(repository, 'dirty\ntwo\nthree\n')
      await runtime.dispatcher.setConfirmDiscardChangesSetting(true)
      const cancelled = await show({
        type: PopupType.ConfirmDiscardChanges,
        repository,
        files: [file],
      })
      await operationDialogs.cancel(runtime, cancelled)
      assert.match(await readFile(join(path, 'file.txt'), 'utf8'), /^dirty/)
      assert.equal(
        runtime.appStore.getState().askForConfirmationOnDiscardChanges,
        true
      )
      const confirm = await show({
        type: PopupType.ConfirmDiscardChanges,
        repository,
        files: [file],
      })
      await respond(confirm, 'submit', { confirm: false })
      assert.equal(
        await readFile(join(path, 'file.txt'), 'utf8'),
        'one\ntwo\nthree\n'
      )
      assert.equal(await git(path, ['rev-parse', 'HEAD']), initial.sha)
      assert.equal(
        runtime.appStore.getState().askForConfirmationOnDiscardChanges,
        false
      )
      const retryFile = await changed(repository, 'retry\ntwo\nthree\n')
      trashFails = true
      await runtime.dispatcher.setConfirmDiscardChangesPermanentlySetting(true)
      await runtime.dispatcher.discardChanges(repository, [retryFile])
      const retry = current(PopupType.DiscardChangesRetry)
      await operationDialogs.cancel(runtime, retry)
      assert.match(await readFile(join(path, 'file.txt'), 'utf8'), /^retry/)
      await runtime.dispatcher.discardChanges(repository, [retryFile])
      const accepted = current(PopupType.DiscardChangesRetry)
      await assert.rejects(
        respond(accepted, 'submit', { confirm: false, files: ['other'] })
      )
      await respond(accepted, 'submit', { confirm: false })
      assert.equal(
        await readFile(join(path, 'file.txt'), 'utf8'),
        'one\ntwo\nthree\n'
      )
      assert.equal(
        runtime.appStore.getState()
          .askForConfirmationOnDiscardChangesPermanently,
        false
      )
    })

    it('partial-line discard uses only the original diff/selection and leaves unselected hunks', async () => {
      const { repository, path } = await fixture()
      const before =
        Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join('\n') + '\n'
      await writeFile(join(path, 'file.txt'), before)
      await git(path, ['commit', '-am', 'line basis'])
      const content = before
        .replace('line 2\n', 'selected change\n')
        .replace('line 29\n', 'unselected change\n')
      const file = await changed(repository, content)
      const diff = await getWorkingDirectoryDiff(repository, file)
      assert.ok(diff.kind === DiffType.Text && diff.hunks.length === 2)
      const firstHunkLines = diff.hunks[0].lines.flatMap((line, i) =>
        line.isIncludeableLine() ? [diff.hunks[0].unifiedDiffStart + i] : []
      )
      const selectable = diff.hunks.flatMap(h =>
        h.lines.flatMap((line, i) =>
          line.isIncludeableLine() ? [h.unifiedDiffStart + i] : []
        )
      )
      let selection = DiffSelection.fromInitialSelection(
        DiffSelectionType.None
      ).withSelectableLines(new Set(selectable))
      for (const line of firstHunkLines) {
        selection = selection.withLineSelection(line, true)
      }
      const popup = await show({
        type: PopupType.ConfirmDiscardSelection,
        repository,
        file,
        diff,
        selection,
      })
      await assert.rejects(
        respond(popup, 'submit', { confirm: false, selection: [999] })
      )
      await respond(popup, 'submit', { confirm: false })
      const actual = await readFile(join(path, 'file.txt'), 'utf8')
      assert.match(actual, /line 2\n/)
      assert.match(actual, /unselected change/)
      assert.doesNotMatch(actual, /^selected change$/m)
    })

    it('stash-and-switch cancellation and nested overwrite keep the original current branch/stash objects', async () => {
      const { repository, path } = await fixture()
      const branch = state(repository).branchesState.allBranches.find(
        b => b.name === 'feature'
      )!
      await changed(repository, 'stash one\ntwo\nthree\n')
      await runtime.dispatcher.checkoutBranch(repository, branch)
      const cancelled = current(PopupType.StashAndSwitchBranch)
      await operationDialogs.cancel(runtime, cancelled)
      assert.equal(await git(path, ['branch', '--show-current']), 'main')
      assert.match(await readFile(join(path, 'file.txt'), 'utf8'), /^stash one/)
      await runtime.dispatcher.createStashForCurrentBranch(repository, false)
      const oldStash = state(repository).changesState.stashEntry
      assert.ok(oldStash)
      await changed(repository, 'stash two\ntwo\nthree\n')
      const switchPopup = await show({
        type: PopupType.StashAndSwitchBranch,
        repository,
        branchToCheckout: branch,
      })
      await respond(switchPopup, 'submit', { move: false })
      const overwrite = current(PopupType.ConfirmOverwriteStash)
      assert.equal(overwrite.branchToCheckout, branch)
      assert.ok(
        runtime.appStore.getState().allPopups.some(p => p.id === switchPopup.id)
      )
      await operationDialogs.cancel(runtime, overwrite)
      assert.equal(current(PopupType.StashAndSwitchBranch), switchPopup)
      assert.equal(
        state(repository).changesState.stashEntry?.stashSha,
        oldStash.stashSha
      )
      assert.equal(await git(path, ['branch', '--show-current']), 'main')
      await respond(switchPopup, 'submit', { move: false })
      await respond(current(PopupType.ConfirmOverwriteStash), 'submit')
      assert.equal(await git(path, ['branch', '--show-current']), 'feature')
      assert.match(await git(path, ['stash', 'show', '-p']), /stash two/)
      assert.doesNotMatch(await git(path, ['stash', 'show', '-p']), /stash one/)
    })

    it('explicit stash overwrite and discard retain data on cancel then invoke real original stash operations', async () => {
      const { repository, path } = await fixture()
      await changed(repository, 'first stash\ntwo\nthree\n')
      await runtime.dispatcher.createStashForCurrentBranch(repository, false)
      const original = state(repository).changesState.stashEntry!
      await changed(repository, 'replacement stash\ntwo\nthree\n')
      await runtime.dispatcher.createStashForCurrentBranch(repository, true)
      await operationDialogs.cancel(
        runtime,
        current(PopupType.ConfirmOverwriteStash)
      )
      assert.equal(
        state(repository).changesState.stashEntry?.stashSha,
        original.stashSha
      )
      await runtime.dispatcher.createStashForCurrentBranch(repository, true)
      await respond(current(PopupType.ConfirmOverwriteStash), 'submit')
      const replacement = state(repository).changesState.stashEntry!
      assert.notEqual(replacement.stashSha, original.stashSha)
      const cancelled = await show({
        type: PopupType.ConfirmDiscardStash,
        repository,
        stash: replacement,
      })
      await operationDialogs.cancel(runtime, cancelled)
      assert.notEqual(await git(path, ['stash', 'list']), '')
      const discard = await show({
        type: PopupType.ConfirmDiscardStash,
        repository,
        stash: replacement,
      })
      await respond(discard, 'submit', { confirm: false })
      assert.equal(await git(path, ['stash', 'list']), '')
      assert.equal(
        runtime.appStore.getState().askForConfirmationOnDiscardStash,
        false
      )
    })

    it('local changes confirmation creates a real stash before retrying the original checkout; PopStash cannot stash again', async () => {
      const { repository, path } = await fixture()
      await git(path, ['checkout', 'feature'])
      await writeFile(join(path, 'file.txt'), 'feature version\n')
      await git(path, ['commit', '-am', 'feature version'])
      await git(path, ['checkout', 'main'])
      await runtime.dispatcher.refreshRepository(repository)
      const branch = state(repository).branchesState.allBranches.find(
        b => b.name === 'feature'
      )!
      await changed(repository, 'working version\n')
      const input: Popup = {
        type: PopupType.LocalChangesOverwritten,
        repository,
        files: ['file.txt'],
        retryAction: { type: RetryActionType.Checkout, repository, branch },
      }
      await operationDialogs.cancel(runtime, await show(input))
      assert.equal(await git(path, ['branch', '--show-current']), 'main')
      await respond(await show(input), 'submit')
      assert.equal(await git(path, ['branch', '--show-current']), 'feature')
      assert.match(await git(path, ['stash', 'show', '-p']), /working version/)
      const stash = await import('../../src/lib/git/stash').then(m =>
        m.getStashes(repository).then(result => result.desktopEntries[0])
      )
      assert.ok(stash)
      const pop = await show({
        type: PopupType.LocalChangesOverwritten,
        repository,
        files: ['file.txt'],
        retryAction: {
          type: RetryActionType.PopStash,
          repository,
          stashEntry: stash,
        },
      })
      assert.deepEqual(
        view(pop).actions.map(a => a.id),
        ['close']
      )
      await assert.rejects(respond(pop, 'submit'))
      await respond(pop, 'close')
    })

    it('undo, reset and detached checkout cancellation/confirmation use the original Commit rather than wire SHA', async () => {
      const { repository, path, initial } = await fixture()
      await changed(repository, 'second\n')
      await git(path, ['commit', '-am', 'second'])
      await runtime.dispatcher.refreshRepository(repository)
      const second = await getCommit(repository, 'HEAD')
      assert.ok(second)
      const undoInput: Popup = {
        type: PopupType.WarnLocalChangesBeforeUndo,
        repository,
        commit: second,
        isWorkingDirectoryClean: true,
      }
      await operationDialogs.cancel(runtime, await show(undoInput))
      assert.equal(await git(path, ['rev-parse', 'HEAD']), second.sha)
      const undo = await show(undoInput)
      await assert.rejects(
        respond(undo, 'submit', { confirm: false, sha: initial.sha })
      )
      await respond(undo, 'submit', { confirm: false })
      assert.equal(await git(path, ['rev-parse', 'HEAD']), initial.sha)
      assert.equal(await readFile(join(path, 'file.txt'), 'utf8'), 'second\n')
      await git(path, ['commit', '-am', 'second again'])
      const recreated = await getCommit(repository, 'HEAD')
      assert.ok(recreated)
      await changed(repository, 'uncommitted\n')
      const resetInput: Popup = {
        type: PopupType.WarningBeforeReset,
        repository,
        commit: initial,
      }
      await operationDialogs.cancel(runtime, await show(resetInput))
      assert.equal(await git(path, ['rev-parse', 'HEAD']), recreated.sha)
      await respond(await show(resetInput), 'submit')
      assert.equal(await git(path, ['rev-parse', 'HEAD']), initial.sha)
      const checkoutInput: Popup = {
        type: PopupType.ConfirmCheckoutCommit,
        repository,
        commit: recreated,
      }
      await operationDialogs.cancel(runtime, await show(checkoutInput))
      assert.equal(await git(path, ['branch', '--show-current']), 'main')
      await git(path, ['reset', '--hard', 'HEAD'])
      await runtime.dispatcher.refreshRepository(repository)
      await respond(await show(checkoutInput), 'submit', { confirm: false })
      assert.equal(await git(path, ['branch', '--show-current']), '')
      assert.equal(state(repository).branchesState.tip.kind, TipState.Detached)
    })

    it('CommitMessage preserves original context/co-author trailer construction, busy state and unsuccessful draft', async () => {
      const { repository, initial } = await fixture()
      const draft = {
        summary: 'keep original',
        description: 'body',
        timestamp: 10,
        generatedByCopilot: true,
      }
      await runtime.dispatcher.setCommitMessage(repository, draft)
      const contexts: unknown[] = []
      let finish!: (value: boolean) => void
      const pending = new Promise<boolean>(resolve => {
        finish = resolve
      })
      const popup = await show({
        type: PopupType.CommitMessage,
        repository,
        commitMessage: draft,
        coAuthors: [
          {
            kind: 'known',
            name: 'Known',
            email: 'known@example.test',
            username: 'known',
          },
        ],
        showCoAuthoredBy: false,
        dialogTitle: 'Squash selected range',
        dialogButtonText: 'Squash',
        prepopulateCommitSummary: false,
        onSubmitCommitMessage: async context => {
          contexts.push(context)
          return pending
        },
      })
      const values = {
        summary: 'reviewed',
        description: 'reviewed body',
        showCoAuthors: false,
        skipCommitHooks: false,
        signOffCommits: false,
      }
      const replying = respond(popup, 'submit', values)
      assert.ok(view(popup).actions.every(a => a.disabled))
      await assert.rejects(respond(popup, 'submit', values))
      await assert.rejects(async () => operationDialogs.cancel(runtime, popup))
      finish(false)
      await replying
      assert.deepEqual(contexts, [
        {
          summary: 'reviewed',
          description: 'reviewed body',
          trailers: [],
          amend: false,
          messageGeneratedByCopilot: false,
        },
      ])
      assert.equal(
        view(popup).fields.find(f => f.id === 'summary')?.value,
        'reviewed'
      )
      assert.deepEqual(state(repository).changesState.commitMessage, draft)
      const tip = state(repository).branchesState.tip
      assert.ok(tip.kind === TipState.Valid)
      assert.equal(tip.branch.tip.sha, initial.sha)
      const known = popup.coAuthors[0]
      assert.deepEqual(commitDialogContext(draft, [known], true, true), {
        summary: draft.summary,
        description: draft.description,
        trailers: [
          { token: 'Co-Authored-By', value: 'Known <known@example.test>' },
        ],
        amend: true,
        messageGeneratedByCopilot: true,
      })
      await operationDialogs.cancel(runtime, popup)
      assert.deepEqual(state(repository).changesState.commitMessage, draft)
    })

    it('CommitMessage failure through real Dispatcher keeps the edited draft and HEAD; success uses original callback', async () => {
      const { repository, path, initial } = await fixture()
      await changed(repository, 'commit operation\n')
      const hooks = join(path, 'reject-hooks')
      await mkdir(hooks)
      await writeFile(
        join(hooks, 'pre-commit'),
        '#!/bin/sh\necho fixture rejection >&2\nexit 1\n'
      )
      await git(path, ['config', 'core.hooksPath', hooks])
      const popup = await show({
        type: PopupType.CommitMessage,
        repository,
        commitMessage: null,
        coAuthors: [],
        showCoAuthoredBy: false,
        dialogTitle: 'Local fixture',
        dialogButtonText: 'Commit',
        prepopulateCommitSummary: false,
        onSubmitCommitMessage: context =>
          runtime.dispatcher.commitIncludedChanges(repository, context),
      })
      const values = {
        summary: 'failed draft',
        description: 'retain me',
        showCoAuthors: false,
        skipCommitHooks: false,
        signOffCommits: false,
      }
      await respond(popup, 'submit', values)
      const deadline = Date.now() + 5000
      while (
        runtime.appStore.getState().currentPopup?.type ===
          PopupType.CommitMessage &&
        Date.now() < deadline
      ) {
        await new Promise(resolve => setTimeout(resolve, 10))
      }
      const error = current(PopupType.Error)
      runtime.dispatcher.closePopupById(error.id!)
      assert.equal(current(PopupType.CommitMessage), popup)
      assert.equal(
        view(popup).fields.find(f => f.id === 'summary')?.value,
        'failed draft'
      )
      assert.equal(await git(path, ['rev-parse', 'HEAD']), initial.sha)
      await git(path, ['config', 'core.hooksPath', join(path, 'empty-hooks')])
      await respond(popup, 'submit', { ...values, summary: 'accepted locally' })
      assert.equal(
        await git(path, ['log', '-1', '--format=%s']),
        'accepted locally'
      )
      assert.equal(view(popup).fields.find(f => f.id === 'summary')?.value, '')
      assert.notEqual(await git(path, ['rev-parse', 'HEAD']), initial.sha)
    })

    it('branch rebase omits an empty commit list before its preview is available', async () => {
      const { repository } = await fixture()
      await runtime.dispatcher.showRebaseDialog(repository)
      const popup = current(PopupType.MultiCommitOperation)
      assert.ok(view(popup).fields.some(field => field.id === 'branch'))
      assert.ok(!view(popup).fields.some(field => field.id === 'commits'))
      await operationDialogs.cancel(runtime, popup)
      assert.equal(state(repository).multiCommitOperationState, null)
    })

    it('multi-commit selection uses real preview and cancel/hide/abort-return semantics, never an all-purpose abort', async () => {
      const { repository, path } = await fixture()
      await git(path, ['checkout', 'feature'])
      await writeFile(join(path, 'file.txt'), 'feature conflict\n')
      await git(path, ['commit', '-am', 'feature conflict'])
      await git(path, ['checkout', 'main'])
      await writeFile(join(path, 'file.txt'), 'main conflict\n')
      await git(path, ['commit', '-am', 'main conflict'])
      await runtime.dispatcher.refreshRepository(repository)
      const head = await git(path, ['rev-parse', 'HEAD'])
      await runtime.dispatcher.startMergeBranchOperation(
        repository,
        false,
        null
      )
      await operationDialogs.cancel(
        runtime,
        current(PopupType.MultiCommitOperation)
      )
      assert.equal(state(repository).multiCommitOperationState, null)
      assert.equal(await git(path, ['rev-parse', 'HEAD']), head)
      await runtime.dispatcher.startMergeBranchOperation(
        repository,
        false,
        null
      )
      const choose = current(PopupType.MultiCommitOperation)
      await assert.rejects(respond(choose, 'submit', { branch: 'WIRE-BRANCH' }))
      await assert.rejects(respond(choose, 'submit', { branch: 'main' }))
      await assert.rejects(
        respond(choose, 'submit', { branch: 'feature', commits: [head] })
      )
      await respond(choose, 'submit', { branch: 'feature' })
      const conflicts = current(PopupType.MultiCommitOperation)
      assert.equal(
        state(repository).multiCommitOperationState?.step.kind,
        Step.ShowConflicts
      )
      assert.ok(
        view(conflicts).actions.find(a => a.id === 'continue')?.disabled
      )
      await assert.rejects(respond(conflicts, 'continue'))
      const checks = view(conflicts).fields.filter(f => f.kind === 'checkbox')
      const resolution = Object.fromEntries(
        checks.map(f => [f.id, f.id.startsWith('ours:')])
      )
      await respond(conflicts, 'resolve', resolution)
      await operationDialogs.cancel(runtime, conflicts)
      assert.equal(
        state(repository).multiCommitOperationState?.step.kind,
        Step.HideConflicts
      )
      assert.equal(
        await git(path, ['rev-parse', 'MERGE_HEAD']),
        await git(path, ['rev-parse', 'feature'])
      )
      await runtime.dispatcher.showPopup({
        type: PopupType.MultiCommitOperation,
        repository,
      })
      await respond(current(PopupType.MultiCommitOperation), 'showConflicts')
      await respond(current(PopupType.MultiCommitOperation), 'abort')
      assert.equal(
        state(repository).multiCommitOperationState?.step.kind,
        Step.ConfirmAbort
      )
      await operationDialogs.cancel(
        runtime,
        current(PopupType.MultiCommitOperation)
      )
      assert.equal(
        state(repository).multiCommitOperationState?.step.kind,
        Step.ShowConflicts
      )
      await respond(current(PopupType.MultiCommitOperation), 'abort')
      await respond(current(PopupType.MultiCommitOperation), 'abort')
      assert.equal(state(repository).multiCommitOperationState, null)
      assert.equal(await git(path, ['rev-parse', 'HEAD']), head)
      assert.equal(
        (await exec(['rev-parse', '--verify', 'MERGE_HEAD'], path)).exitCode,
        128
      )
    })
  }
)

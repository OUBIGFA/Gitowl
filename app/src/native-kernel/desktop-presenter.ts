import type { AppRuntime } from '../ui/app-runtime'
import { projectRepositoryMenus } from './repository-menu'
import { Repository } from '../models/repository'
import { getChangedFiles } from '../lib/git/log'
import { withRepositoryGitOperation } from '../lib/git/repository-operation'
import { CloningRepository } from '../models/cloning-repository'
import { ICommitContext } from '../models/commit'
import { ICommitMessage } from '../models/commit-message'
import { FetchType } from '../models/fetch'
import {
  ForcePushBranchState,
  getCurrentBranchForcePushState,
} from '../lib/rebase'
import { WorkingDirectoryFileChange } from '../models/status'
import { DiffSelection } from '../models/diff'
import { IAppState, RepositorySectionTab, FoldoutType } from '../lib/app-state'
import { CLIAction } from '../lib/cli-action'
import { URLActionType, parseAppURL } from '../lib/parse-app-url'
import { PopupType } from '../models/popup'
import { closeKernelPopup, replyToKernelPopup } from './popup-presenter'
import { CompositeDisposable, Emitter } from 'event-kit'
import { NativeDialogLifecycle } from './dialog-presenter'
import {
  NativeOnboardingPresenter,
  projectSignInView,
} from './onboarding-presenter'
import {
  executeImagePresentationCommand,
  NativeImagePresentationCommand,
} from './image-presentation'
import {
  executeGenerationCommand,
  NativeGenerationCommand,
} from './generation-presenter'
import { includeDiffLines } from './line-selection'
import {
  NativeCommitPresenter,
  KernelAssistedReceipt,
  projectAssistedReceipt,
} from './commit-presenter'
import type { NativeCommitCommand } from './commit-protocol'
import { NativeMenuPresenter } from './menu-presenter'
import type { MenuEvent } from '../main-process/menu/menu-event'
import type { NativeWorkspaceCommand } from './workspace-protocol'
import { executeNativeWorkspaceCommand } from './workspace-presenter'
import { visibleWorkingDirectoryFiles } from './workspace-projection'
import { WelcomeStep } from '../ui/welcome/welcome-step'
import {
  NativeMissingRepositoryPresenter,
  MissingRepositoryAction,
} from './missing-repository-presenter'

/** Worktrees share IDs, so the canonical path is part of every frontend reference. */
export interface NativeRepositoryReference {
  readonly id: number
  readonly path: string
}

/**
 * Frontend-only commands for the original Desktop kernel.
 *
 * This adapter holds no repositories, accounts, commit drafts or Git state of
 * its own. The trusted host validates wire inputs, then resolves current kernel
 * objects and calls Dispatcher exactly as the upstream presenter does. Popups,
 * progress and recovery remain in AppStore and must be presented by MyGo, not
 * replaced with a simplified RPC failure or automatic decision.
 */
export class DesktopPresenter {
  private readonly onboarding: NativeOnboardingPresenter
  private readonly dialogEvents = new Emitter()
  private readonly dialogs: NativeDialogLifecycle
  private readonly commit: NativeCommitPresenter
  private readonly menu: NativeMenuPresenter
  private readonly missing: NativeMissingRepositoryPresenter

  public constructor(private readonly runtime: AppRuntime) {
    this.onboarding = new NativeOnboardingPresenter(runtime)
    this.commit = new NativeCommitPresenter(runtime, state =>
      visibleWorkingDirectoryFiles(
        state,
        runtime.appStore.getState().showChangesFilter
      )
    )
    this.menu = new NativeMenuPresenter(runtime)
    this.missing = new NativeMissingRepositoryPresenter(runtime, () => {
      this.dialogEvents.emit('update')
    })
    this.dialogs = new NativeDialogLifecycle(runtime, () => {
      this.dialogEvents.emit('update')
    })
  }

  public getState(): IAppState {
    return this.runtime.appStore.getState()
  }

  public onDidUpdate(listener: (state: IAppState) => void) {
    return new CompositeDisposable(
      this.runtime.appStore.onDidUpdate(state => {
        this.onboarding.observeSignIn()
        this.dialogs.sync(state)
        this.commit.observe(state)
        listener(state)
      }),
      this.dialogEvents.on('update', () => listener(this.getState())),
      this.commit.onDidUpdate(() => listener(this.getState())),
      this.onboarding.onDidUpdate(() => listener(this.getState()))
    )
  }

  public acknowledgeRepositoryListFocus() {
    this.runtime.dispatcher.closeFoldout(FoldoutType.Repository)
  }

  public filterRepositories(text: string) {
    return this.runtime.dispatcher.setRepositoryFilterText(text)
  }

  public reportResizableFocus(active: boolean) {
    this.runtime.dispatcher.appFocusedElementChanged(active)
  }

  public getMissingRepositoryState() {
    return this.missing.project(this.getState())
  }

  public recoverMissingRepository(
    reference: NativeRepositoryReference,
    action: MissingRepositoryAction
  ) {
    return this.missing.execute(reference, action)
  }

  public getCommitState() {
    return this.commit.getState()
  }
  public getMenuState() {
    return this.menu.getState()
  }
  public getRepositoryMenus() {
    return projectRepositoryMenus(this.getState())
  }
  public executeRepositoryMenu(
    reference: NativeRepositoryReference,
    lease: string,
    path: ReadonlyArray<number>
  ) {
    return this.menu.executeRepositoryMenu(reference, lease, path)
  }
  public onMenuPresentationAction(
    listener: Parameters<NativeMenuPresenter['onDidPerformAction']>[0]
  ) {
    return this.menu.onDidPerformAction(listener)
  }
  public executeMenu(id: string) {
    return this.menu.execute(id)
  }
  public handleMenuEvent(name: MenuEvent) {
    return this.menu.handleEvent(name)
  }

  public getOnboardingState() {
    return this.onboarding.getState()
  }
  public getSignInState() {
    return projectSignInView(this.runtime.signInStore.getState())
  }
  public advanceWelcome(step: WelcomeStep) {
    return this.onboarding.advance(step)
  }
  public configureWelcomeAuthor(
    name: string,
    email: string,
    useAccount: boolean
  ) {
    return this.onboarding.configureAuthor(name, email, useAccount)
  }
  public deleteWelcomeConfigLock() {
    return this.onboarding.deleteConfigLock()
  }
  public setSignInEndpoint(endpoint: string) {
    return this.runtime.dispatcher.setSignInEndpoint(endpoint)
  }
  public authenticateUsingBrowser() {
    this.runtime.dispatcher.requestBrowserAuthentication()
  }
  public resetSignIn() {
    return this.runtime.dispatcher.resetSignInState()
  }

  public getRepository(reference: NativeRepositoryReference): Repository {
    const repository = this.getSelectableRepository(reference)
    if (!(repository instanceof Repository)) {
      throw new Error('This repository is still being cloned')
    }
    return repository
  }

  public getSelectableRepository(
    reference: NativeRepositoryReference
  ): Repository | CloningRepository {
    const state = this.getState()
    const candidates = [
      ...state.repositories,
      ...(state.selectedState?.repository instanceof Repository
        ? [state.selectedState.repository]
        : []),
    ]
    const repository = candidates.find(
      candidate =>
        candidate.id === reference.id && candidate.path === reference.path
    )
    if (repository === undefined) {
      throw new Error('This repository reference is no longer available')
    }
    return repository
  }

  private files(
    reference: NativeRepositoryReference,
    ids: ReadonlyArray<string>
  ) {
    const repository = this.getRepository(reference)
    const changes =
      this.runtime.repositoryStateManager.get(repository).changesState
        .workingDirectory.files
    const files = ids.map(id => changes.find(file => file.id === id))
    if (files.some(file => file === undefined)) {
      throw new Error('The file selection changed; refresh the frontend state')
    }
    return { repository, files: files as WorkingDirectoryFileChange[] }
  }

  public async loadInitialState() {
    await this.runtime.dispatcher.loadInitialState()
    this.dialogs.sync(this.getState())
    await this.onboarding.start()
  }

  public dispose() {
    this.dialogs.dispose()
    this.commit.dispose()
    this.menu.dispose()
    this.missing.dispose()
    this.dialogEvents.dispose()
  }

  public addRepositories(paths: ReadonlyArray<string>) {
    return this.runtime.dispatcher.addRepositories(paths)
  }

  public selectRepository(reference: NativeRepositoryReference) {
    return this.runtime.dispatcher.selectRepository(
      this.getSelectableRepository(reference)
    )
  }

  public refreshRepository(reference: NativeRepositoryReference) {
    return this.runtime.dispatcher.refreshRepository(
      this.getRepository(reference)
    )
  }

  public changeSection(
    reference: NativeRepositoryReference,
    section: RepositorySectionTab
  ) {
    return this.runtime.dispatcher.changeRepositorySection(
      this.getRepository(reference),
      section
    )
  }

  public selectWorkingDirectoryFiles(
    reference: NativeRepositoryReference,
    ids: ReadonlyArray<string>
  ) {
    const { repository, files } = this.files(reference, ids)
    return this.runtime.dispatcher.selectWorkingDirectoryFiles(
      repository,
      files
    )
  }

  public changeFileIncluded(
    reference: NativeRepositoryReference,
    ids: ReadonlyArray<string>,
    included: boolean
  ) {
    const { repository, files } = this.files(reference, ids)
    return this.runtime.dispatcher.changeFileIncluded(
      repository,
      files,
      included
    )
  }

  public changeFileLineSelection(
    reference: NativeRepositoryReference,
    id: string,
    selection: DiffSelection
  ) {
    const { repository, files } = this.files(reference, [id])
    return this.runtime.dispatcher.changeFileLineSelection(
      repository,
      files[0],
      selection
    )
  }

  public acknowledgeCommitMessageFocus() {
    return this.runtime.dispatcher.setCommitMessageFocus(false)
  }

  public executeImagePresentationCommand(
    command: NativeImagePresentationCommand
  ) {
    return executeImagePresentationCommand(this.runtime.dispatcher, command)
  }

  public executeGenerationCommand(command: NativeGenerationCommand) {
    return executeGenerationCommand(this.runtime, command)
  }

  public executeWorkspaceCommand(command: NativeWorkspaceCommand) {
    return executeNativeWorkspaceCommand(
      this.runtime,
      command.repository === null
        ? null
        : this.getRepository(command.repository),
      command
    )
  }

  public async executeCommitCommand(
    command: NativeCommitCommand
  ): Promise<{ readonly assisted?: KernelAssistedReceipt }> {
    const repository = this.getRepository(command.repository)
    const state = this.runtime.repositoryStateManager.get(repository)
    const dispatcher = this.runtime.dispatcher
    switch (command.name) {
      case 'commit.options':
        dispatcher.updateCommitOptions(repository, command.options)
        return {}
      case 'commit.amend':
        await this.commit.amend(repository, command.enabled)
        return {}
      case 'commit.mode':
        if (
          command.mode === 'copilot' &&
          this.commit.getState()?.assistedAvailable !== true
        )
          throw new Error('Copilot assisted commits are not available')
        dispatcher.setCommitMode(repository, command.mode)
        return {}
      case 'commit.coAuthors.show':
        dispatcher.setShowCoAuthoredBy(repository, command.enabled)
        return {}
      case 'commit.coAuthors.search':
        await this.commit.searchAuthors(repository, command.query)
        return {}
      case 'commit.coAuthors.add':
        await this.commit.addAuthor(repository, command.id)
        return {}
      case 'commit.coAuthors.remove':
        this.commit.removeAuthor(repository, command.id)
        return {}
      case 'commit.assisted.pushAfter':
        dispatcher.setPushAfterAssistedCommit(repository, command.enabled)
        return {}
      case 'commit.assisted.create':
        if (
          this.commit.getState()?.canCommit !== true ||
          this.commit.getState()?.mode !== 'copilot'
        )
          throw new Error(
            'Review the assisted commit selection and options before saving'
          )
        return { assisted: await this.commit.createAssisted(repository) }
      case 'commit.progress':
        if (state.subscribeToCommitOutput === null)
          throw new Error('Commit output is no longer available')
        await dispatcher.showPopup({
          type: PopupType.CommitProgress,
          subscribeToCommitOutput: state.subscribeToCommitOutput,
        })
        return {}
      default: {
        const run = state.changesState.assistedCommit
        if (!('runId' in run) || run.runId !== command.runID)
          throw new Error('This assisted commit run is no longer current')
        switch (command.name) {
          case 'commit.assisted.cancel':
            dispatcher.cancelCopilotAssistedCommits(repository, command.runID)
            return {}
          case 'commit.assisted.retry':
            if (command.kind === 'push') {
              const outcome = await dispatcher.retryCopilotAssistedCommitPush(
                repository,
                command.runID
              )
              return outcome === undefined
                ? {}
                : { assisted: projectAssistedReceipt(outcome) }
            }
            await dispatcher.retryCopilotAssistedCommitRecovery(
              repository,
              command.runID
            )
            return {}
          case 'commit.assisted.dismiss':
            dispatcher.dismissCopilotAssistedCommitError(
              repository,
              command.runID
            )
            return {}
          case 'commit.assisted.details':
            if (run.kind !== 'error' && run.kind !== 'push-error')
              throw new Error('There is no assisted commit error to inspect')
            await dispatcher.showPopup({
              type: PopupType.Error,
              error: run.error,
              showCauses: true,
            })
            return {}
        }
      }
    }
  }

  public includeDiffLines(
    reference: NativeRepositoryReference,
    fileID: string,
    identity: string,
    ids: ReadonlyArray<number>,
    included: boolean
  ) {
    return includeDiffLines(
      this.runtime,
      this.getRepository(reference),
      fileID,
      identity,
      ids,
      included
    )
  }

  public changeIncludeAllFiles(
    reference: NativeRepositoryReference,
    included: boolean
  ) {
    return this.runtime.dispatcher.changeIncludeAllFiles(
      this.getRepository(reference),
      included
    )
  }

  public setCommitMessage(
    reference: NativeRepositoryReference,
    message: ICommitMessage
  ) {
    return this.runtime.dispatcher.setCommitMessage(
      this.getRepository(reference),
      message
    )
  }

  public commitIncludedChanges(
    reference: NativeRepositoryReference,
    context: ICommitContext
  ) {
    return this.commit.createManual(this.getRepository(reference), context)
  }

  public generateCommitMessage(
    reference: NativeRepositoryReference,
    ids: ReadonlyArray<string>,
    overrideExistingMessage: boolean
  ) {
    const { repository, files } = this.files(reference, ids)
    this.runtime.dispatcher.incrementMetric(
      'generateCommitMessageButtonClickCount'
    )
    return overrideExistingMessage
      ? this.runtime.dispatcher.promptOverrideWithGeneratedCommitMessage(
          repository,
          files
        )
      : this.runtime.dispatcher.generateCommitMessage(repository, files)
  }

  public cancelGenerateCommitMessage(reference: NativeRepositoryReference) {
    return this.runtime.dispatcher.cancelGenerateCommitMessage(
      this.getRepository(reference)
    )
  }

  public fetch(reference: NativeRepositoryReference) {
    return this.runtime.dispatcher.fetch(
      this.getRepository(reference),
      FetchType.UserInitiatedTask
    )
  }

  public pull(reference: NativeRepositoryReference) {
    return this.runtime.dispatcher.pull(this.getRepository(reference))
  }

  public push(reference: NativeRepositoryReference) {
    const repository = this.getRepository(reference)
    const { branchesState, aheadBehind } =
      this.runtime.repositoryStateManager.get(repository)
    // Match Desktop's primary toolbar: a known published amend/rebase uses
    // the original confirmation/force-with-lease continuation. Normal Push
    // still owns the original PublishRepository path when no remote exists.
    return getCurrentBranchForcePushState(branchesState, aheadBehind) ===
      ForcePushBranchState.Recommended
      ? this.runtime.dispatcher.confirmOrForcePush(repository)
      : this.runtime.dispatcher.push(repository)
  }

  public loadNextCommitBatch(reference: NativeRepositoryReference) {
    return this.runtime.dispatcher.loadNextCommitBatch(
      this.getRepository(reference)
    )
  }

  /** Read visible row metadata with Desktop's exact rename/binary/merge parser.
   * This presentation read never changes AppStore's commit/file selection. */
  public async readHistorySummaries(
    reference: NativeRepositoryReference,
    shas: ReadonlyArray<string>
  ) {
    const repository = this.getRepository(reference)
    const state = this.runtime.repositoryStateManager.get(repository)
    if (
      shas.some(
        sha =>
          !state.compareState.commitSHAs.includes(sha) ||
          !state.commitLookup.has(sha)
      )
    ) {
      throw new Error('This history entry is no longer available')
    }
    return withRepositoryGitOperation(repository.path, 'read', async () => {
      const summaries = []
      for (const sha of shas) {
        const data = await getChangedFiles(repository, sha)
        summaries.push({
          sha,
          fileCount: data.files.length,
          linesAdded: data.linesAdded,
          linesDeleted: data.linesDeleted,
        })
      }
      return summaries
    })
  }

  public changeCommitSelection(
    reference: NativeRepositoryReference,
    shas: ReadonlyArray<string>,
    contiguous: boolean
  ) {
    return this.runtime.dispatcher.changeCommitSelection(
      this.getRepository(reference),
      shas,
      contiguous
    )
  }

  public loadChangedFilesForCurrentSelection(
    reference: NativeRepositoryReference
  ) {
    return this.runtime.dispatcher.loadChangedFilesForCurrentSelection(
      this.getRepository(reference)
    )
  }

  public selectHistoryFile(reference: NativeRepositoryReference, id: string) {
    const repository = this.getRepository(reference)
    const file = this.runtime.repositoryStateManager
      .get(repository)
      .commitSelection.changesetData.files.find(file => file.id === id)
    if (file === undefined) {
      throw new Error('This history file selection is no longer available')
    }
    return this.runtime.dispatcher.changeFileSelection(repository, file)
  }

  public showDotComSignInDialog() {
    return this.runtime.dispatcher.showDotComSignInDialog()
  }

  public showEnterpriseSignInDialog(endpoint?: string) {
    return this.runtime.dispatcher.showEnterpriseSignInDialog(endpoint)
  }

  public dispatchRawURL(url: string) {
    const protocol = new URL(url).protocol
    if (
      ![
        'gitowl:',
        'gitowl-auth:',
        'x-github-client:',
        'x-github-desktop-auth:',
        'x-github-desktop-dev-auth:',
        'github-windows:',
        'github-mac:',
      ].includes(protocol)
    ) {
      throw new Error('This application URL scheme is not supported')
    }
    return this.runtime.dispatcher.dispatchURLAction(parseAppURL(url))
  }

  public dispatchURLAction(action: URLActionType) {
    return this.runtime.dispatcher.dispatchURLAction(action)
  }

  public dispatchCLIAction(action: CLIAction) {
    return this.runtime.dispatcher.dispatchCLIAction(action)
  }

  public replyToPopup(reply: unknown) {
    return replyToKernelPopup(this.runtime, reply)
  }

  public closePopup(id: number) {
    const popup = this.getState().currentPopup
    // These original informational dialogs have no continuation. Callback
    // prompts instead use the exact verified upstream cancel semantics.
    if (
      popup?.id === id &&
      [
        PopupType.About,
        PopupType.Acknowledgements,
        PopupType.CLIInstalled,
        PopupType.ReleaseNotes,
        PopupType.ThankYou,
      ].includes(popup.type)
    ) {
      return this.runtime.dispatcher.closePopupById(id)
    }
    return closeKernelPopup(this.runtime, id)
  }
}

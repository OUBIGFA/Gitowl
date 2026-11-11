import { Emitter } from 'event-kit'
import type { AppRuntime } from '../ui/app-runtime'
import {
  IAppState,
  IRepositoryState,
  RepositorySectionTab,
  SelectionType,
} from '../lib/app-state'
import { Account } from '../models/account'
import { Author, UnknownAuthor, isKnownAuthor } from '../models/author'
import { ICommitContext } from '../models/commit'
import { ICommitMessage } from '../models/commit-message'
import { Repository } from '../models/repository'
import { PopupType } from '../models/popup'
import { TipState } from '../models/tip'
import { DiffSelectionType } from '../models/diff'
import { isAssistedCommitRepositoryLocked } from '../models/assisted-commit-run'
import type { AssistedCommitRunOutcome } from '../models/assisted-commit-run'
import { ICopilotAssistedCommitRequest } from '../models/copilot-assisted-commit'
import { WorkingDirectoryFileChange } from '../models/status'
import {
  getAccountForRepository,
  getAccountForCommitMessageGeneration,
} from '../lib/get-account-for-repository'
import { useRepoRulesLogic } from '../lib/helpers/repo-rules'
import { formatCommitMessage } from '../lib/format-commit-message'
import { enableHooksEnvironment } from '../lib/feature-flag'
import {
  assistedCommitRequestsEqual,
  freezeAssistedCommitRequest,
} from '../lib/assisted-commit-request'
import { AuthorInputController } from '../ui/lib/author-input/author-from-hit'
import {
  defaultCommitSummary,
  hasCommitRuleFailure,
  copilotAssistedCommitAvailable,
  CommitRuleStatus,
  evaluateCommitRules,
} from '../ui/changes/commit-policy'
import {
  hasActiveFilters,
  isCommittingFileHiddenByFilter,
} from '../ui/changes/filter-changes-logic'
import { showFilesToBeCommitted } from '../ui/changes/show-files-to-be-committed'
import { createWorkbenchCommit } from '../ui/changes/create-workbench-commit'
import { commitDialogContext } from './operation-dialogs'

export type KernelCommitView = {
  readonly mode: 'manual' | 'copilot'
  readonly allowEmpty: boolean
  readonly signOff: boolean
  readonly skipHooks: boolean
  readonly showSkipHooks: boolean
  readonly amending: boolean
  readonly canAmend: boolean
  readonly defaultSummary: string | null
  readonly canCommit: boolean
  readonly rulesPending: boolean
  readonly rulesBlocked: boolean
  readonly summaryLengthWarning: boolean
  readonly author: { readonly name: string; readonly email: string } | null
  readonly coAuthorsAvailable: boolean
  readonly showCoAuthors: boolean
  readonly coAuthors: ReadonlyArray<{
    readonly id: string
    readonly kind: Author['kind']
    readonly username: string | null
    readonly name: string
    readonly email: string
    readonly state: string | null
  }>
  readonly authorQuery: string
  readonly authorSearching: boolean
  readonly authorHits: ReadonlyArray<{
    readonly id: string
    readonly username: string
    readonly name: string
    readonly email: string
  }>
  readonly assistedAvailable: boolean
  readonly pushAfterAssisted: boolean
  readonly assisted: {
    readonly kind: string
    readonly runID: string | null
    readonly busy: boolean
    readonly canCancel: boolean
    readonly retry: string | null
    readonly canDismiss: boolean
    readonly title: string
    readonly index: number
    readonly total: number
  }
}

/** Only the original executor's acknowledgement can produce a local/pushed receipt. */
export type KernelAssistedReceipt = {
  readonly kind: AssistedCommitRunOutcome['kind'] | 'warning'
  readonly commits?: ReadonlyArray<string>
  readonly headSHA?: string | null
}

type AuthorSearchOwner = {
  readonly id: number
  readonly path: string
  readonly endpoint: string | null
  readonly account: Account | undefined
  readonly context: number
  readonly generationAccount: Account | undefined
}

/** Native input/presentation only; original stores still own authors, options and runs. */
export class NativeCommitPresenter {
  private readonly events = new Emitter()
  private readonly authorInput: AuthorInputController
  private contextID = 0
  private contextRepository: Repository | null = null
  private contextAccount: Account | undefined
  private contextGenerationAccount: Account | undefined
  private ruleInput: ReadonlyArray<unknown> = []
  private ruleID = 0
  private rulesPending = false
  private rules: CommitRuleStatus | null = null
  private disposed = false

  public constructor(
    private readonly runtime: AppRuntime,
    private readonly visibleFiles?: (
      state: IRepositoryState
    ) => ReadonlyArray<WorkingDirectoryFileChange>
  ) {
    this.authorInput = new AuthorInputController(
      runtime.gitHubUserStore,
      () => {
        const app = runtime.appStore.getState(),
          selected = this.selected(app)
        return selected === null
          ? null
          : {
              repository: selected.repository,
              account:
                getAccountForRepository(app.accounts, selected.repository) ??
                undefined,
            }
      },
      () => this.selected()?.state.changesState.coAuthors ?? [],
      authors => {
        const selected = this.selected()
        if (selected !== null) {
          runtime.dispatcher.setCoAuthors(selected.repository, authors)
        }
      },
      () => this.emit(),
      () => {
        const state = this.selected()?.state
        return (
          state !== undefined &&
          (state.isCommitting ||
            state.isGeneratingCommitMessage ||
            isAssistedCommitRepositoryLocked(state.changesState.assistedCommit))
        )
      }
    )
  }

  public onDidUpdate(listener: () => void) {
    return this.events.on('update', listener)
  }
  private emit() {
    if (!this.disposed) {
      this.events.emit('update')
    }
  }
  public dispose() {
    this.disposed = true
    this.authorInput.dispose()
    this.contextID++
    this.ruleID++
    this.events.dispose()
  }

  private selected(state = this.runtime.appStore.getState()) {
    return state.selectedState?.type === SelectionType.Repository
      ? state.selectedState
      : null
  }

  private context(
    repository: Repository,
    state: IRepositoryState,
    message: ICommitMessage = state.changesState.commitMessage
  ): ICommitContext {
    const files = state.changesState.workingDirectory.files.filter(
      file => file.selection.getSelectionType() !== DiffSelectionType.None
    )
    const prepopulate = files.length === 1 && !repository.isTutorialRepository
    return commitDialogContext(
      {
        ...message,
        summary:
          prepopulate && !message.summary
            ? defaultCommitSummary(files, true)
            : message.summary,
      },
      state.changesState.coAuthors,
      repository.gitHubRepository !== null,
      state.commitToAmend !== null
    )
  }

  /** AppStore changes invalidate only the matching asynchronous UI lookups. */
  public observe(state: IAppState) {
    const selected = this.selected(state)
    const account =
      selected === null
        ? undefined
        : getAccountForRepository(state.accounts, selected.repository) ??
          undefined
    const generationAccount =
      selected === null
        ? undefined
        : getAccountForCommitMessageGeneration(
            state.accounts,
            selected.repository
          )
    if (
      selected?.repository.hash !== this.contextRepository?.hash ||
      selected?.repository.gitHubRepository?.endpoint !==
        this.contextRepository?.gitHubRepository?.endpoint ||
      generationAccount !== this.contextGenerationAccount ||
      account !== this.contextAccount
    ) {
      this.contextID++
      this.contextRepository = selected?.repository ?? null
      this.contextAccount = account
      this.contextGenerationAccount = generationAccount
    }
    this.authorInput.observe()
    if (selected === null) {
      this.ruleID++
      this.ruleInput = []
      this.rules = null
      this.rulesPending = false
      return
    }
    const r = selected.state
    const info = r.changesState.currentRepoRulesInfo
    const input = [
      selected.repository.hash,
      account,
      r.changesState.workingDirectory,
      r.changesState.commitMessage,
      r.changesState.coAuthors,
      r.commitToAmend,
      r.commitAuthor?.email,
      r.branchesState.tip,
      info,
      r.aheadBehind,
    ]
    if (
      input.every((value, index) => value === this.ruleInput[index]) &&
      input.length === this.ruleInput.length
    ) {
      return
    }
    this.ruleInput = input
    const id = ++this.ruleID
    const enabled = useRepoRulesLogic(account ?? null, selected.repository)
    const branch =
      r.branchesState.tip.kind === TipState.Valid
        ? r.branchesState.tip.branch.name
        : null
    const base: CommitRuleStatus = {
      enabled,
      info,
      message: 'pass',
      author: r.commitAuthor?.email
        ? info.commitAuthorEmailPatterns.getFailedRules(r.commitAuthor.email)
            .status
        : 'pass',
      branch: branch
        ? info.branchNamePatterns.getFailedRules(branch).status
        : 'pass',
      aheadBehind: r.aheadBehind,
    }
    this.rules = base
    this.rulesPending = enabled
    if (!enabled) {
      return
    }
    const ruleContext = this.context(selected.repository, r)
    // Upstream only validates the placeholder when both text fields are empty.
    // The eventual commit context still uses the single-file default summary.
    void formatCommitMessage(selected.repository, {
      ...ruleContext,
      summary:
        !r.changesState.commitMessage.summary &&
        r.changesState.commitMessage.description
          ? ''
          : ruleContext.summary,
    })
      .then(message => {
        if (this.disposed || id !== this.ruleID) {
          return
        }
        this.rules = {
          ...base,
          message: info.commitMessagePatterns.getFailedRules(message).status,
        }
        this.rulesPending = false
        this.emit()
      })
      .catch(error => {
        if (this.disposed || id !== this.ruleID) {
          return
        }
        this.rulesPending = false
        this.rules = { ...base, message: 'fail' }
        void this.runtime.dispatcher.postError(error)
        this.emit()
      })
  }

  public getState(): KernelCommitView | null {
    const state = this.runtime.appStore.getState()
    this.observe(state)
    const selected = this.selected(state)
    if (selected === null) {
      return null
    }
    const r = selected.state,
      changes = r.changesState,
      run = changes.assistedCommit
    const busy =
      r.isCommitting ||
      r.isGeneratingCommitMessage ||
      isAssistedCommitRepositoryLocked(run)
    if (
      changes.showCoAuthoredBy &&
      selected.repository.gitHubRepository !== null &&
      !busy
    ) {
      void this.authorInput
        .resume(selected.repository)
        .catch(error => this.runtime.dispatcher.postError(error))
    }
    const files = changes.workingDirectory.files.filter(
      file => file.selection.getSelectionType() !== DiffSelectionType.None
    )
    const prepopulate =
      files.length === 1 && !selected.repository.isTutorialRepository
    const summary = this.context(selected.repository, r).summary
    const blocked = this.rules !== null && hasCommitRuleFailure(this.rules)
    const assistedAvailable = copilotAssistedCommitAvailable({
      repository: selected.repository,
      accounts: this.runtime.appStore.getState().accounts,
      amending: r.commitToAmend !== null,
      hasCreateAction: changes.assistedCommitAvailable,
      hasModeAction: true,
      hasMode: true,
    })
    const mode =
      changes.commitMode === 'copilot' &&
      (assistedAvailable || run.kind !== 'idle')
        ? 'copilot'
        : 'manual'
    const ready =
      !busy &&
      !this.rulesPending &&
      r.selectedSection === RepositorySectionTab.Changes
    const tipSHA =
      r.branchesState.tip.kind === TipState.Valid
        ? r.branchesState.tip.branch.tip.sha
        : null
    const canCancel =
      'cancelRequested' in run &&
      !run.cancelRequested &&
      (run.kind !== 'preparing-push' || run.canCancel)
    return {
      mode,
      allowEmpty: r.allowEmptyCommit,
      signOff: r.signOffCommits,
      skipHooks: r.skipCommitHooks,
      showSkipHooks: enableHooksEnvironment(),
      amending: r.commitToAmend !== null,
      canAmend: !busy && tipSHA !== null && r.commitLookup.has(tipSHA),
      defaultSummary: prepopulate ? defaultCommitSummary(files, true) : null,
      canCommit:
        mode === 'copilot'
          ? ready &&
            assistedAvailable &&
            (files.length > 0 || r.allowEmptyCommit) &&
            (this.rules === null || !hasCommitRuleFailure(this.rules, false))
          : ready &&
            !blocked &&
            summary.length > 0 &&
            (files.length > 0 ||
              r.allowEmptyCommit ||
              r.commitToAmend !== null),
      rulesPending: this.rulesPending,
      rulesBlocked: blocked,
      summaryLengthWarning:
        this.runtime.appStore.getState().showCommitLengthWarning &&
        (this.rules?.message ?? 'pass') === 'pass' &&
        changes.commitMessage.summary.length > 50,
      author:
        r.commitAuthor === null
          ? null
          : { name: r.commitAuthor.name, email: r.commitAuthor.email },
      coAuthorsAvailable: selected.repository.gitHubRepository !== null,
      showCoAuthors: changes.showCoAuthoredBy,
      ...this.authorInput.getState(),
      assistedAvailable,
      pushAfterAssisted: changes.pushAfterAssistedCommit,
      assisted: {
        kind: run.kind,
        runID: 'runId' in run ? run.runId : null,
        busy,
        canCancel,
        retry:
          run.kind === 'push-error' && !run.settling
            ? 'push'
            : run.kind === 'error' && !run.settling
            ? run.retry
            : null,
        canDismiss:
          ((run.kind === 'error' && run.retry === null) ||
            run.kind === 'push-error') &&
          !run.settling,
        title: run.kind === 'committing' ? run.title : '',
        index: run.kind === 'committing' ? run.index : 0,
        total: run.kind === 'committing' ? run.total : 0,
      },
    }
  }

  private authorOwner(repository: Repository): AuthorSearchOwner {
    const app = this.runtime.appStore.getState()
    const selected = this.selected(app)
    if (
      this.disposed ||
      selected === null ||
      selected.repository.hash !== repository.hash ||
      selected.repository.gitHubRepository?.endpoint !==
        repository.gitHubRepository?.endpoint
    ) {
      throw new Error('This author lookup belongs to a previous repository')
    }
    this.observe(app)
    return {
      id: repository.id,
      path: repository.path,
      endpoint: repository.gitHubRepository?.endpoint ?? null,
      account: getAccountForRepository(app.accounts, repository) ?? undefined,
      context: this.contextID,
      generationAccount: getAccountForCommitMessageGeneration(
        app.accounts,
        repository
      ),
    }
  }

  private ownerIsCurrent(
    repository: Repository,
    owner: AuthorSearchOwner
  ): boolean {
    const app = this.runtime.appStore.getState()
    this.observe(app)
    const selected = this.selected(app)
    return (
      !this.disposed &&
      selected !== null &&
      selected.repository.hash === repository.hash &&
      (selected.repository.gitHubRepository?.endpoint ?? null) ===
        owner.endpoint &&
      selected.repository.id === owner.id &&
      selected.repository.path === owner.path &&
      repository.id === owner.id &&
      repository.path === owner.path &&
      (repository.gitHubRepository?.endpoint ?? null) === owner.endpoint &&
      this.contextID === owner.context &&
      getAccountForCommitMessageGeneration(app.accounts, repository) ===
        owner.generationAccount &&
      (getAccountForRepository(app.accounts, repository) ?? undefined) ===
        owner.account
    )
  }

  public searchAuthors(repository: Repository, query: string) {
    return this.authorInput.search(repository, query)
  }

  public addAuthor(repository: Repository, id: string) {
    return this.authorInput.add(repository, id)
  }

  public removeAuthor(repository: Repository, id: string) {
    this.authorInput.remove(repository, id)
  }

  public async amend(repository: Repository, enabled: boolean) {
    if (!enabled) {
      return this.runtime.dispatcher.stopAmendingRepository(repository)
    }
    const state = this.runtime.repositoryStateManager.get(repository)
    const tip = state.branchesState.tip
    const commit =
      tip.kind === TipState.Valid
        ? state.commitLookup.get(tip.branch.tip.sha)
        : undefined
    if (commit === undefined) {
      throw new Error('The most recent commit is not available')
    }
    return this.runtime.dispatcher.startAmendingRepository(
      repository,
      commit,
      state.localCommitSHAs.includes(commit.sha)
    )
  }

  private hiddenFiles(repository: Repository, state: IRepositoryState) {
    if (!hasActiveFilters(state.changesState.fileListFilter)) {
      return false
    }
    if (this.visibleFiles === undefined) {
      throw new Error('Native file-filter presentation has not been connected')
    }
    const visible = new Map(
      this.visibleFiles(state).map(file => [file.id, file])
    )
    const selected = state.changesState.workingDirectory.files.filter(
      file => file.selection.getSelectionType() !== DiffSelectionType.None
    )
    return (
      this.runtime.appStore.getState()
        .askForConfirmationOnCommitFilteredChanges &&
      isCommittingFileHiddenByFilter(
        selected.map(file => file.id),
        visible,
        state.changesState.workingDirectory.files.length,
        state.changesState.fileListFilter
      )
    )
  }

  private consent(
    repository: Repository,
    state: IRepositoryState,
    action: () => Promise<unknown>,
    warnAuthors: boolean,
    warnFiltered: boolean
  ) {
    const dispatcher = this.runtime.dispatcher
    const continueAction = () => {
      void action().catch(error => dispatcher.postError(error))
    }
    const unknown = state.changesState.coAuthors.filter(
      (author): author is UnknownAuthor => !isKnownAuthor(author)
    )
    if (warnAuthors && unknown.length > 0) {
      void dispatcher.showPopup({
        type: PopupType.UnknownAuthors,
        authors: unknown,
        onCommit: continueAction,
      })
      return true
    }
    if (warnFiltered && this.hiddenFiles(repository, state)) {
      void dispatcher.showPopup({
        type: PopupType.ConfirmCommitFilteredChanges,
        onCommitAnyway: continueAction,
        showFilesToBeCommitted: () =>
          showFilesToBeCommitted(dispatcher, repository),
      })
      return true
    }
    return false
  }

  public async createManual(
    repository: Repository,
    context: ICommitContext,
    warnAuthors?: boolean,
    warnFiltered?: boolean
  ): Promise<boolean> {
    const state = this.runtime.repositoryStateManager.get(repository)
    const derived = this.context(repository, state, {
      ...state.changesState.commitMessage,
      summary: context.summary,
      description: context.description,
      generatedByCopilot: context.messageGeneratedByCopilot,
    })
    const canonical: ICommitContext = {
      ...derived,
      ...context,
      summary: derived.summary,
      trailers: context.trailers ?? derived.trailers,
      amend: context.amend ?? derived.amend,
    }
    if (
      this.consent(
        repository,
        state,
        () =>
          this.createManual(repository, context, false, warnFiltered === true),
        warnAuthors !== false,
        false
      )
    ) {
      return false
    }
    if (
      this.consent(
        repository,
        state,
        () =>
          this.createManual(repository, context, warnAuthors === true, false),
        false,
        warnFiltered !== false
      )
    ) {
      return false
    }
    const account = getAccountForRepository(
      this.runtime.appStore.getState().accounts,
      repository
    )
    if (useRepoRulesLogic(account ?? null, repository)) {
      const rules = await evaluateCommitRules(
        repository,
        state,
        account,
        canonical
      )
      if (hasCommitRuleFailure(rules)) {
        throw new Error(
          "The commit does not meet this repository's rules; review the message and author"
        )
      }
    }
    const committed = await createWorkbenchCommit(
      this.runtime.dispatcher,
      repository,
      state.changesState.workingDirectory,
      canonical
    )
    if (committed) {
      this.runtime.dispatcher.setChangesListFilterText(repository, '')
    }
    return committed
  }

  private assistedRequest(
    repository: Repository,
    state: IRepositoryState
  ): ICopilotAssistedCommitRequest {
    return {
      files: state.changesState.workingDirectory.files.filter(
        file => file.selection.getSelectionType() !== DiffSelectionType.None
      ),
      trailers: this.context(repository, state).trailers ?? [],
      skipCommitHooks: state.skipCommitHooks,
      signOffCommits: state.signOffCommits,
      allowEmptyCommit: state.allowEmptyCommit,
    }
  }

  public async createAssisted(
    repository: Repository,
    warnAuthors = true,
    warnFiltered = true,
    pending?: {
      request: ICopilotAssistedCommitRequest
      authors: string
      push: boolean
      owner: AuthorSearchOwner
    }
  ): Promise<KernelAssistedReceipt> {
    const owner = this.authorOwner(repository)
    const state = this.runtime.repositoryStateManager.get(repository)
    const current = this.assistedRequest(repository, state)
    if (
      pending &&
      (!this.ownerIsCurrent(repository, pending.owner) ||
        !assistedCommitRequestsEqual(pending.request, current) ||
        pending.authors !== JSON.stringify(state.changesState.coAuthors) ||
        pending.push !== state.changesState.pushAfterAssistedCommit)
    ) {
      throw new Error(
        'The selected changes or commit options changed; review the selection and try again'
      )
    }
    // Capture first-click page/account context BEFORE original preparation can
    // await Git reads. A newer page never authorizes an old prepared request.
    const original = pending ?? {
      request: freezeAssistedCommitRequest(current),
      authors: JSON.stringify(state.changesState.coAuthors),
      push: state.changesState.pushAfterAssistedCommit,
      owner,
    }
    const intent = pending ?? {
      ...original,
      request:
        await this.runtime.dispatcher.prepareCopilotAssistedCommitRequest(
          repository,
          original.request
        ),
    }
    const latest = this.runtime.repositoryStateManager.get(repository)
    if (
      !this.ownerIsCurrent(repository, intent.owner) ||
      !assistedCommitRequestsEqual(
        intent.request,
        this.assistedRequest(repository, latest)
      ) ||
      intent.authors !== JSON.stringify(latest.changesState.coAuthors) ||
      intent.push !== latest.changesState.pushAfterAssistedCommit
    ) {
      throw new Error(
        'The assisted commit page or selected input changed; review it and try again'
      )
    }
    if (
      this.consent(
        repository,
        latest,
        () => this.createAssisted(repository, false, warnFiltered, intent),
        warnAuthors,
        false
      )
    ) {
      return { kind: 'warning' }
    }
    if (
      this.consent(
        repository,
        latest,
        () => this.createAssisted(repository, false, false, intent),
        false,
        warnFiltered
      )
    ) {
      return { kind: 'warning' }
    }
    const outcome = await this.runtime.dispatcher.createCopilotAssistedCommits(
      repository,
      intent.request
    )
    return projectAssistedReceipt(outcome)
  }
}

export function projectAssistedReceipt(
  outcome: AssistedCommitRunOutcome
): KernelAssistedReceipt {
  return 'result' in outcome
    ? {
        kind: outcome.kind,
        commits: [...outcome.result.commits],
        headSHA: outcome.result.head.sha,
      }
    : { kind: outcome.kind }
}

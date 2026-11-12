import type { Dispatcher } from './dispatcher'
import { assertNever } from '../lib/fatal-error'
import { shell } from '../lib/app-shell'
import {
  IAppState,
  SelectionType,
  RepositorySectionTab,
  HistoryTabMode,
  FoldoutType,
} from '../lib/app-state'
import {
  MenuEvent,
  isTestMenuEvent,
  TestMenuEvent,
} from '../main-process/menu/menu-event'
import { Repository, getGitHubHtmlUrl } from '../models/repository'
import { CloningRepository } from '../models/cloning-repository'
import { PopupType } from '../models/popup'
import { PreferencesTab } from '../models/preferences'
import { CloneRepositoryTab } from '../models/clone-repository-tab'
import { FetchType } from '../models/fetch'
import { TipState } from '../models/tip'
import { findContributionTargetDefaultBranch } from '../lib/branch'
import {
  enableCopilotAppHandoff,
  enableWorktreeSupport,
} from '../lib/feature-flag'
import { installWindowsCLI, uninstallWindowsCLI } from './main-process-proxy'

/** Only these actions belong to the presentation toolkit, not to Dispatcher. */
export type AppMenuPresentationAction =
  | 'focus-history'
  | 'focus-changes'
  | 'select-all'
  | 'find-text'
  | 'increase-active-resizable-width'
  | 'decrease-active-resizable-width'

export interface IAppMenuPresentation {
  readonly perform: (action: AppMenuPresentationAction) => void
  readonly showTestUI: (name: TestMenuEvent) => Promise<unknown> | void
}

/**
 * The original App.tsx menu controller, shared by React and MyGo. All repository,
 * auth, recovery and mutation state remains in the same original Dispatcher.
 * The six focus/editing actions are the only toolkit-specific callbacks.
 */
export class AppMenuController {
  public constructor(
    private readonly dispatcher: Dispatcher,
    private readonly getState: () => IAppState,
    private readonly presentation: IAppMenuPresentation
  ) {}

  public handleEvent(name: MenuEvent): Promise<unknown> | void {
    const state = this.getState()
    // Preserve the original error-stack guard (not an invented cancellation).
    if (name !== 'test-app-error' && state.errorCount > 1) {
      return
    }
    const repository = state.selectedState?.repository ?? null
    const selected =
      state.selectedState?.type === SelectionType.Repository
        ? state.selectedState
        : null
    switch (name) {
      case 'push':
        return selected === null
          ? undefined
          : this.dispatcher.push(selected.repository)
      case 'force-push':
        return selected === null
          ? undefined
          : this.dispatcher.confirmOrForcePush(selected.repository)
      case 'pull':
        return selected === null
          ? undefined
          : this.dispatcher.pull(selected.repository)
      case 'fetch':
        return selected === null
          ? undefined
          : this.dispatcher.fetch(
              selected.repository,
              FetchType.UserInitiatedTask
            )
      case 'show-changes':
        return this.showChanges(true)
      case 'show-history':
        return this.showHistory(true)
      case 'choose-repository':
        return this.toggleFoldout(FoldoutType.Repository)
      case 'add-local-repository':
        return this.showAddLocalRepository()
      case 'create-repository':
        return this.showCreateRepository()
      case 'clone-repository':
        return this.showCloneRepository()
      case 'create-branch':
        if (
          selected !== null &&
          selected.state.branchesState.tip.kind !== TipState.Unknown
        ) {
          return this.dispatcher.showPopup({
            type: PopupType.CreateBranch,
            repository: selected.repository,
          })
        }
        return
      case 'show-branches':
        return selected === null
          ? undefined
          : this.toggleFoldout(FoldoutType.Branch)
      case 'show-worktrees':
        return this.showWorktrees()
      case 'create-worktree':
        if (enableWorktreeSupport() && selected !== null) {
          return this.dispatcher.showPopup({
            type: PopupType.AddWorktree,
            repository: selected.repository,
          })
        }
        return
      case 'remove-repository':
        return this.removeRepository(repository)
      case 'rename-branch':
        if (selected?.state.branchesState.tip.kind === TipState.Valid) {
          return this.dispatcher.showPopup({
            type: PopupType.RenameBranch,
            repository: selected.repository,
            branch: selected.state.branchesState.tip.branch,
          })
        }
        return
      case 'delete-branch': {
        if (selected?.state.branchesState.tip.kind !== TipState.Valid) {
          return
        }
        const branch = selected.state.branchesState.tip.branch
        const pullRequest = selected.state.branchesState.currentPullRequest
        return pullRequest !== null
          ? this.dispatcher.showPopup({
              type: PopupType.DeletePullRequest,
              repository: selected.repository,
              branch,
              pullRequest,
            })
          : this.dispatcher.showPopup({
              type: PopupType.DeleteBranch,
              repository: selected.repository,
              branch,
              existsOnRemote: selected.state.aheadBehind !== null,
            })
      }
      case 'discard-all-changes':
        return selected === null
          ? undefined
          : this.dispatcher.showPopup({
              type: PopupType.ConfirmDiscardChanges,
              repository: selected.repository,
              files: selected.state.changesState.workingDirectory.files,
              showDiscardChangesSetting: false,
              discardingAllChanges: true,
            })
      case 'stash-all-changes':
        return repository instanceof Repository
          ? this.dispatcher.createStashForCurrentBranch(repository)
          : undefined
      case 'show-preferences':
        return this.dispatcher.showPopup({
          type: PopupType.Preferences,
          initialSelectedTab: PreferencesTab.Appearance,
        })
      case 'open-working-directory':
        return selected === null
          ? undefined
          : shell.showFolderContents(selected.repository.path)
      case 'update-branch-with-contribution-target-branch': {
        this.dispatcher.incrementMetric('updateFromDefaultBranchMenuCount')
        if (selected === null) {
          return
        }
        const branch = findContributionTargetDefaultBranch(
          selected.repository,
          selected.state.branchesState
        )
        if (branch === null) {
          return
        }
        this.dispatcher.initializeMergeOperation(
          selected.repository,
          false,
          branch
        )
        return this.dispatcher.mergeBranch(
          selected.repository,
          branch,
          selected.state.compareState.mergeStatus
        )
      }
      case 'compare-to-branch':
        return this.showHistory(false, true)
      case 'merge-branch':
      case 'squash-and-merge-branch': {
        const squash = name === 'squash-and-merge-branch'
        this.dispatcher.recordMenuInitiatedMerge(squash)
        return selected === null
          ? undefined
          : this.dispatcher.startMergeBranchOperation(
              selected.repository,
              squash
            )
      }
      case 'rebase-branch':
        this.dispatcher.incrementMetric('rebaseCurrentBranchMenuCount')
        return repository instanceof Repository
          ? this.dispatcher.showRebaseDialog(repository)
          : undefined
      case 'show-repository-settings':
        return repository instanceof Repository
          ? this.dispatcher.showPopup({
              type: PopupType.RepositorySettings,
              repository,
            })
          : undefined
      case 'view-repository-on-github': {
        const url =
          repository instanceof Repository ? getGitHubHtmlUrl(repository) : null
        return url ? this.dispatcher.openInBrowser(url) : undefined
      }
      case 'compare-on-github':
      case 'branch-on-github': {
        if (
          selected === null ||
          repository instanceof CloningRepository ||
          !repository?.gitHubRepository
        ) {
          return
        }
        const tip = selected.state.branchesState.tip
        if (tip.kind !== TipState.Valid || !tip.branch.upstreamWithoutRemote) {
          return
        }
        const view = name === 'compare-on-github' ? 'compare' : 'tree'
        return this.dispatcher.openInBrowser(
          `${repository.gitHubRepository.htmlURL}/${view}/${encodeURIComponent(
            tip.branch.upstreamWithoutRemote
          )}`
        )
      }
      case 'create-issue-in-repository-on-github':
        return repository instanceof Repository
          ? this.dispatcher.openIssueCreationPage(repository)
          : undefined
      case 'open-in-shell':
        return this.openCurrentRepositoryInShell()
      case 'show-about':
        return this.dispatcher.showPopup({ type: PopupType.About })
      case 'go-to-commit-message':
        return this.goToCommitMessage()
      case 'open-pull-request':
        if (selected === null) {
          return
        }
        if (selected.state.branchesState.currentPullRequest === null) {
          const operation = this.dispatcher.createPullRequest(
            selected.repository
          )
          this.dispatcher.incrementMetric('createPullRequestCount')
          return operation
        }
        return this.dispatcher.showPullRequest(selected.repository)
      case 'preview-pull-request':
        return selected === null
          ? undefined
          : this.dispatcher.startPullRequest(selected.repository)
      case 'install-darwin-cli':
        return this.dispatcher.installDarwinCLI()
      case 'install-windows-cli':
        return installWindowsCLI()
      case 'uninstall-windows-cli':
        return uninstallWindowsCLI()
      case 'open-external-editor':
        return repository instanceof Repository
          ? this.dispatcher.openInExternalEditor(repository.path)
          : undefined
      case 'open-in-copilot-app':
        return enableCopilotAppHandoff() && selected !== null
          ? this.dispatcher.openInCopilotApp(selected.repository.path)
          : undefined
      case 'open-with-external-editor':
        return this.dispatcher.showPopup({
          type: PopupType.OpenWithExternalEditor,
        })
      case 'select-all':
      case 'find-text':
      case 'increase-active-resizable-width':
      case 'decrease-active-resizable-width':
        return this.presentation.perform(name)
      case 'show-stashed-changes':
        return selected === null
          ? undefined
          : this.dispatcher.selectStashedFile(selected.repository)
      case 'hide-stashed-changes':
        return selected === null
          ? undefined
          : this.dispatcher.hideStashedChanges(selected.repository)
      case 'toggle-changes-filter':
        return this.dispatcher.toggleChangesFilterVisibility()
      default:
        if (isTestMenuEvent(name)) {
          return this.presentation.showTestUI(name)
        }
        return assertNever(name, `Unknown menu event name: ${name}`)
    }
  }

  public async showHistory(focus: boolean, showBranchList = false) {
    const selected = this.getState().selectedState
    if (selected?.type !== SelectionType.Repository) {
      return
    }
    await this.dispatcher.closeCurrentFoldout()
    await this.dispatcher.initializeCompare(selected.repository, {
      kind: HistoryTabMode.History,
    })
    await this.dispatcher.changeRepositorySection(
      selected.repository,
      RepositorySectionTab.History
    )
    await this.dispatcher.updateCompareForm(selected.repository, {
      filterText: '',
      showBranchList,
    })
    if (focus) {
      this.presentation.perform('focus-history')
    }
  }

  public async showChanges(focus: boolean) {
    const selected = this.getState().selectedState
    if (selected?.type !== SelectionType.Repository) {
      return
    }
    this.dispatcher.closeCurrentFoldout()
    await this.dispatcher.changeRepositorySection(
      selected.repository,
      RepositorySectionTab.Changes
    )
    if (focus) {
      this.presentation.perform('focus-changes')
    }
  }

  private async goToCommitMessage() {
    await this.showChanges(false)
    return this.dispatcher.setCommitMessageFocus(true)
  }

  private toggleFoldout(
    type: FoldoutType.Repository | FoldoutType.Branch | FoldoutType.Worktree
  ) {
    return this.getState().currentFoldout?.type === type
      ? this.dispatcher.closeFoldout(type)
      : this.dispatcher.showFoldout({ type })
  }

  public showWorktrees() {
    return enableWorktreeSupport() &&
      this.getState().selectedState?.type === SelectionType.Repository
      ? this.toggleFoldout(FoldoutType.Worktree)
      : undefined
  }

  public showAddLocalRepository() {
    return this.dispatcher.showPopup({ type: PopupType.AddRepository })
  }

  public showCreateRepository() {
    return this.dispatcher.showPopup({ type: PopupType.CreateRepository })
  }

  public showCloneRepository(cloneURL?: string) {
    if (cloneURL !== undefined) {
      this.dispatcher.changeCloneRepositoriesTab(CloneRepositoryTab.Generic)
    }
    return this.dispatcher.showPopup({
      type: PopupType.CloneRepository,
      initialURL: cloneURL ?? null,
    })
  }

  public removeRepository(repository: Repository | CloningRepository | null) {
    if (repository === null) {
      return
    }
    if (
      repository instanceof CloningRepository ||
      repository.missing ||
      !this.getState().askForConfirmationOnRepositoryRemoval
    ) {
      return this.dispatcher.removeRepository(repository, false)
    }
    return this.dispatcher.showPopup({
      type: PopupType.RemoveRepository,
      repository,
    })
  }

  /** List actions retain their target even when another project is selected. */
  public async showRepositorySettingsFor(repository: Repository) {
    const selected = this.getState().selectedState?.repository
    if (
      !(selected instanceof Repository) ||
      selected.id !== repository.id ||
      selected.path !== repository.path
    ) {
      await this.dispatcher.refreshRepository(repository)
    }
    const latest = this.getState().repositories.find(
      r => r.id === repository.id && r.path === repository.path
    )
    if (!(latest instanceof Repository)) {
      return
    }
    return this.dispatcher.showPopup({
      type: PopupType.RepositorySettings,
      repository: latest,
    })
  }

  public async changeRepositoryAccount(repository: Repository, login: string) {
    try {
      await this.dispatcher.updateRepositoryAccount(repository, login)
    } catch (error) {
      await this.dispatcher.postError(error)
    }
  }

  public viewRepositoryOnGitHub(
    repository: Repository | CloningRepository | null
  ) {
    if (!(repository instanceof Repository)) {
      return
    }
    const url = getGitHubHtmlUrl(repository)
    if (url) {
      return this.dispatcher.openInBrowser(url)
    }
    return undefined
  }

  public openRepositoryInShell(repository: Repository | CloningRepository) {
    if (repository instanceof Repository) {
      return this.dispatcher.openShell(repository.path)
    }
    return undefined
  }

  public openRepositoryInEditor(repository: Repository | CloningRepository) {
    if (repository instanceof Repository) {
      return this.dispatcher.openInExternalEditor(repository.path)
    }
    return undefined
  }

  public revealRepository(repository: Repository | CloningRepository) {
    if (repository instanceof Repository) {
      return shell.showFolderContents(repository.path)
    }
  }

  public changeRepositoryAlias(repository: Repository) {
    return this.dispatcher.showPopup({
      type: PopupType.ChangeRepositoryAlias,
      repository,
    })
  }

  public removeRepositoryAlias(repository: Repository) {
    return this.dispatcher.changeRepositoryAlias(repository, null)
  }

  public createWorktree(repository: Repository) {
    return this.dispatcher.showPopup({
      type: PopupType.AddWorktree,
      repository,
    })
  }

  public showRepositoryWorktrees(repository: Repository) {
    this.dispatcher.selectRepository(repository)
    this.dispatcher.showWorktreesFoldout()
  }

  public openCurrentRepositoryInShell() {
    const repository = this.getState().selectedState?.repository
    return repository instanceof Repository
      ? this.dispatcher.openShell(repository.path)
      : undefined
  }
}

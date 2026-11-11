import { projectKernelDiff } from './diff-projection'
import { projectRepositoryMenus } from './repository-menu'
import { groupRepositories } from '../ui/repositories-list/group-repositories'
import { filterListItems } from '../ui/lib/filter-list-items'
import { formatDate } from '../lib/format-date'
import { getRelativeTimeInfoFromDate } from '../lib/relative-time-info'
import { withMediaProjection } from './media-presentation'
import type { KernelMissingRepository } from './missing-repository-presenter'
import {
  ForcePushBranchState,
  getCurrentBranchForcePushState,
} from '../lib/rebase'
import type { IAppState, IRepositoryState } from '../lib/app-state'
import {
  SelectionType as SelectionKind,
  RepositorySectionTab,
  ChangesSelectionKind,
} from '../lib/app-state'
import { Account } from '../models/account'
import { CloningRepository } from '../models/cloning-repository'
import { Repository } from '../models/repository'
import { TipState } from '../models/tip'
import {
  CommittedFileChange,
  WorkingDirectoryFileChange,
} from '../models/status'
import type { Commit } from '../models/commit'
import { projectImagePresentation } from './image-presentation'
import { projectNativeWorkspace } from './workspace-projection'
import type { KernelCommitView } from './commit-presenter'
import type { KernelMenuDTO } from './menu-presenter'
import { projectGenerationSettings } from './generation-presenter'
import { projectDiffSelection } from './line-selection'
import { projectKernelPopup } from './popup-presenter'
import { projectSignInView, KernelOnboardingView } from './onboarding-presenter'
import { resolveCommitGenerationAccount } from '../lib/commit-generation/generation-account'
import type {
  KernelFile,
  KernelRepository,
  NativeKernelSnapshot,
} from './protocol'

function repositoryView(
  repository: Repository | CloningRepository
): KernelRepository {
  if (repository instanceof CloningRepository) {
    return {
      id: repository.id,
      path: repository.path,
      name: repository.name,
      alias: null,
      missing: false,
      login: null,
      kind: 'cloning',
    }
  }

  return {
    id: repository.id,
    path: repository.path,
    name: repository.name,
    alias: repository.alias,
    missing: repository.missing,
    login: repository.login,
    kind: 'repository',
  }
}

function fileView(
  file: WorkingDirectoryFileChange | CommittedFileChange
): KernelFile {
  const selection =
    file instanceof WorkingDirectoryFileChange
      ? file.selection.getSelectionType()
      : 'None'
  return {
    id: file.id,
    path: file.path,
    status: file.status.kind,
    oldPath:
      'oldPath' in file.status && typeof file.status.oldPath === 'string'
        ? file.status.oldPath
        : null,
    inclusion:
      selection === 'All' ? 'all' : selection === 'None' ? 'none' : 'partial',
    isSubmodule: file.status.submoduleStatus !== undefined,
  }
}

function branchName(repositoryState: IRepositoryState): string | null {
  const tip = repositoryState.branchesState.tip
  return tip.kind === TipState.Valid
    ? tip.branch.name
    : tip.kind === TipState.Unborn
    ? tip.ref
    : null
}

function upstreamName(repositoryState: IRepositoryState): string | null {
  const tip = repositoryState.branchesState.tip
  return tip.kind === TipState.Valid && tip.branch.upstream !== null
    ? tip.branch.upstream
    : null
}

function headSHA(repositoryState: IRepositoryState): string | null {
  const tip = repositoryState.branchesState.tip
  return tip.kind === TipState.Valid
    ? tip.branch.tip.sha
    : tip.kind === TipState.Detached
    ? tip.currentSha
    : null
}

function historyCommitView(commit: Commit, preferAbsoluteDates: boolean) {
  const dateLabels: Record<string, { text: string; absolute: string }> = {}
  let dateRefreshAfter: number | undefined
  for (const locale of ['en', 'zh-Hans', 'zh-Hant']) {
    const info = getRelativeTimeInfoFromDate(commit.author.date, true, locale)
    dateLabels[locale] = {
      text: preferAbsoluteDates
        ? formatDate(commit.author.date, { locale })
        : info.relativeText,
      absolute: info.absoluteText,
    }
    if (!preferAbsoluteDates) dateRefreshAfter = info.duration
  }
  return {
    sha: commit.sha,
    shortSha: commit.shortSha,
    summary: commit.summary,
    body: commit.body,
    authorName: commit.author.name,
    authorEmail: commit.author.email,
    date: commit.author.date.toISOString(),
    dateLabels,
    dateRefreshAfter,
    tags: [...commit.tags],
  }
}

function selectedRepositoryState(state: IAppState) {
  const selected = state.selectedState
  return selected !== null && selected.type === SelectionKind.Repository
    ? selected
    : null
}

/** Project only the stable presentation contract; the remaining kernel state stays internal. */
export function projectKernelState(
  state: IAppState,
  revision: number,
  onboarding: KernelOnboardingView | null = null,
  commitView: KernelCommitView | null = null,
  menu?: KernelMenuDTO,
  missingRepository: KernelMissingRepository | null = null
): NativeKernelSnapshot {
  const repositories = state.repositories.map(repositoryView)
  const repositoryFilterText = state.repositoryFilterText ?? ''
  // Keep original alias/name fuzzy scoring and group order. The project rail
  // shows each repository once even when the original recent group repeats it.
  const filteredPaths = repositoryFilterText
    ? [
        ...new Set(
          groupRepositories(
            state.repositories,
            state.localRepositoryStateLookup,
            state.recentRepositories
          ).flatMap(group =>
            filterListItems(group.items, repositoryFilterText).map(
              result => result.item.repository.path
            )
          )
        ),
      ]
    : repositories.map(repository => repository.path)
  const selected = selectedRepositoryState(state)
  const selectedView =
    selected === null ? null : repositoryView(selected.repository)
  const repositoryState = selected?.state
  const workingDirectory =
    repositoryState?.changesState.workingDirectory.files ?? []
  const historySHAs = repositoryState?.compareState.commitSHAs ?? []
  const history = repositoryState
    ? historySHAs.flatMap(sha => {
        const commit = repositoryState.commitLookup.get(sha)
        return commit === undefined
          ? []
          : [historyCommitView(commit, state.preferAbsoluteDates)]
      })
    : []

  return withMediaProjection(
    () => ({
      protocol: 1,
      core: 'desktop/desktop',
      presentationComplete: false,
      focusCommitMessage: state.focusCommitMessage,
      revision,
      ...projectImagePresentation(state),
      appearance: {
        theme: state.selectedTheme,
        tabSize: state.selectedTabSize,
      },
      commitGeneration: projectGenerationSettings(state.commitGeneration),
      ...(menu === undefined ? {} : { menu }),
      workspace: projectNativeWorkspace(state),
      repositories,
      repositoryMenus: projectRepositoryMenus(state),
      repositoryFilter: { text: repositoryFilterText, paths: filteredPaths },
      cloningRepository:
        state.selectedState?.type === SelectionKind.CloningRepository
          ? {
              repository: repositoryView(state.selectedState.repository),
              progress: {
                value: state.selectedState.progress.value,
                description: state.selectedState.progress.description ?? null,
              },
            }
          : null,
      missingRepository:
        state.selectedState?.type === SelectionKind.MissingRepository
          ? missingRepository ?? {
              repository: {
                id: state.selectedState.repository.id,
                path: state.selectedState.repository.path,
                name: state.selectedState.repository.name,
              },
              checking: true,
              busy: false,
              unsafePath: null,
              canClone:
                !!state.selectedState.repository.gitHubRepository?.cloneURL,
              error: null,
            }
          : null,
      accounts: state.accounts.map((account: Account) => ({
        endpoint: account.endpoint,
        login: account.login,
        name: account.name,
        copilotEnabled: account.isCopilotDesktopEnabled === true,
      })),
      selected:
        selected === null || repositoryState === undefined
          ? null
          : {
              repository: selectedView!,
              section:
                repositoryState.selectedSection === RepositorySectionTab.History
                  ? 'history'
                  : 'changes',
              branch: branchName(repositoryState),
              upstream: upstreamName(repositoryState),
              headSHA: headSHA(repositoryState),
              hasRemote: repositoryState.remote !== null,
              forcePushRecommended:
                getCurrentBranchForcePushState(
                  repositoryState.branchesState,
                  repositoryState.aheadBehind
                ) === ForcePushBranchState.Recommended,
              ahead: repositoryState.aheadBehind?.ahead ?? 0,
              behind: repositoryState.aheadBehind?.behind ?? 0,
              files: workingDirectory.map(fileView),
              selectedFileIDs:
                repositoryState.changesState.selection.kind ===
                ChangesSelectionKind.WorkingDirectory
                  ? repositoryState.changesState.selection.selectedFileIDs
                  : [],
              message: repositoryState.changesState.commitMessage,
              commitView,
              diff: projectKernelDiff(
                repositoryState.selectedSection === RepositorySectionTab.History
                  ? repositoryState.commitSelection.diff
                  : repositoryState.changesState.selection.kind ===
                    ChangesSelectionKind.WorkingDirectory
                  ? repositoryState.changesState.selection.diff
                  : repositoryState.changesState.selection
                      .selectedStashedFileDiff
              ),
              diffSelection:
                repositoryState.selectedSection === RepositorySectionTab.Changes
                  ? projectDiffSelection(
                      repositoryState,
                      state.showDiffCheckMarks
                    )
                  : null,
              history,
              historySelection: repositoryState.commitSelection.shas,
              historyFiles:
                repositoryState.commitSelection.changesetData.files.map(
                  fileView
                ),
              historyFileID: repositoryState.commitSelection.file?.id ?? null,
              historyLinesAdded:
                repositoryState.commitSelection.changesetData.linesAdded,
              historyLinesDeleted:
                repositoryState.commitSelection.changesetData.linesDeleted,
              commitInProgress: repositoryState.isCommitting,
              generationInProgress: repositoryState.isGeneratingCommitMessage,
              generationAvailable:
                state.commitGeneration?.source === 'custom'
                  ? state.commitGeneration.selection !== null &&
                    state.commitGeneration.enabledModels.some(
                      model =>
                        model.providerId ===
                          state.commitGeneration!.selection!.providerId &&
                        model.modelId ===
                          state.commitGeneration!.selection!.modelId
                    )
                  : resolveCommitGenerationAccount(
                      state.accounts,
                      selected.repository
                    ) !== undefined,
              remoteInProgress: repositoryState.isPushPullFetchInProgress,
              conflictKind:
                repositoryState.changesState.conflictState?.kind ?? null,
            },
      popups: state.allPopups.map(popup => ({
        id: popup.id ?? null,
        type: String(popup.type),
      })),
      currentPopup: projectKernelPopup(state.currentPopup, state),
      bannerType:
        state.currentBanner === null ? null : String(state.currentBanner.type),
      foldoutType:
        state.currentFoldout === null
          ? null
          : Number(state.currentFoldout.type),
      welcome: state.showWelcomeFlow,
      onboarding,
      signIn: projectSignInView(state.signInState),
      errorCount: state.errorCount,
    }),
    state.allPopups
  )
}

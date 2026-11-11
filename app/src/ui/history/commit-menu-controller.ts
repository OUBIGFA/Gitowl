import { Commit, CommitOneLine } from '../../models/commit'
import { GitHubRepository } from '../../models/github-repository'
import { getDotComAPIEndpoint } from '../../lib/api'
import { writeClipboardText } from '../main-process-proxy'
import { WorkspaceMenuItem } from '../lib/workspace-menu'

export interface CommitMenuConfig {
  readonly gitHubRepository: GitHubRepository | null
  readonly localCommitSHAs: ReadonlyArray<string>
  readonly tagsToPush?: ReadonlyArray<string>
  readonly canUndoCommits?: boolean
  readonly canAmendCommits?: boolean
  readonly canResetToCommits?: boolean
  readonly isMultiCommitOperationInProgress?: boolean
  readonly isAssistedCommitRepositoryLocked?: boolean
  readonly disableReordering?: boolean
  readonly disableSquashing?: boolean
  readonly onUndoCommit?: (commit: Commit) => void
  readonly onAmendCommit?: (commit: Commit, isLocal: boolean) => void
  readonly onResetToCommit?: (commit: Commit) => void
  readonly onCheckoutCommit?: (commit: CommitOneLine) => void
  readonly onKeyboardReorder?: (commits: ReadonlyArray<Commit>) => void
  readonly onRevertCommit?: (commit: Commit) => void
  readonly onCreateBranch?: (commit: CommitOneLine) => void
  readonly onCreateTag?: (sha: string) => void
  readonly onDeleteTag?: (tag: string) => void
  readonly onCherryPick?: (commits: ReadonlyArray<CommitOneLine>) => void
  readonly onViewCommitOnGitHub?: (sha: string) => void
  readonly onSquash?: (
    commits: ReadonlyArray<Commit>,
    onto: Commit,
    contextMenu: boolean
  ) => void
}

/** Shared original history menu policy, including disabled rare operations and
 * unpushed-tag restrictions. The frontend supplies presentation-only reorder
 * selection; the original Dispatcher performs the eventual operation. */
export function generateCommitContextMenu(
  config: CommitMenuConfig,
  row: number,
  commit: Commit,
  selected: ReadonlyArray<Commit>
): WorkspaceMenuItem[] {
  const idle =
    config.isMultiCommitOperationInProgress === false &&
    config.isAssistedCommitRepositoryLocked !== true
  const cherry = config.onCherryPick !== undefined && idle
  const reorder =
    config.onKeyboardReorder !== undefined &&
    config.disableReordering === false &&
    idle
  const squash =
    config.onSquash !== undefined && config.disableSquashing === false && idle
  const item = (
    key: string,
    label: string,
    action: () => void,
    enabled = true,
    data: string[] = []
  ): WorkspaceMenuItem => ({
    labelKey: `workspace.${key}`,
    label,
    action,
    enabled,
    labelData: data,
  })
  if (selected.length > 1)
    return [
      item(
        'cherryPickMany',
        __DARWIN__
          ? `Cherry-pick ${selected.length} Commits…`
          : `Cherry-pick ${selected.length} commits…`,
        () => config.onCherryPick?.(selected),
        cherry,
        [String(selected.length)]
      ),
      item(
        'squashMany',
        __DARWIN__
          ? `Squash ${selected.length} Commits…`
          : `Squash ${selected.length} commits…`,
        () => config.onSquash?.(selected, commit, true),
        squash,
        [String(selected.length)]
      ),
      item(
        'reorderMany',
        __DARWIN__
          ? `Reorder ${selected.length} Commits…`
          : `Reorder ${selected.length} commits…`,
        () => config.onKeyboardReorder?.(selected),
        reorder,
        [String(selected.length)]
      ),
    ]
  const local = config.localCommitSHAs.includes(commit.sha)
  const items: WorkspaceMenuItem[] = []
  if (config.canAmendCommits === true && row === 0)
    items.push(
      item('amend', __DARWIN__ ? 'Amend Commit…' : 'Amend commit…', () =>
        config.onAmendCommit?.(commit, local)
      )
    )
  if (config.canUndoCommits === true && local && row === 0)
    items.push(
      item(
        'undoCommit',
        __DARWIN__ ? 'Undo Commit…' : 'Undo commit…',
        () => config.onUndoCommit?.(commit),
        config.onUndoCommit !== undefined
      )
    )
  items.push(
    item(
      'reset',
      __DARWIN__ ? 'Reset to Commit…' : 'Reset to commit…',
      () => config.onResetToCommit?.(commit),
      config.canResetToCommits === true &&
        row > 0 &&
        row <= config.localCommitSHAs.length &&
        config.onResetToCommit !== undefined
    ),
    item(
      'checkoutCommit',
      __DARWIN__ ? 'Checkout Commit' : 'Checkout commit',
      () => config.onCheckoutCommit?.(commit),
      row > 0 && config.onCheckoutCommit !== undefined
    ),
    item(
      'reorderCommit',
      __DARWIN__ ? 'Reorder Commit' : 'Reorder commit',
      () => config.onKeyboardReorder?.([commit]),
      reorder
    ),
    item(
      'revert',
      __DARWIN__ ? 'Revert Changes in Commit' : 'Revert changes in commit',
      () => config.onRevertCommit?.(commit),
      config.onRevertCommit !== undefined
    ),
    { type: 'separator' },
    item(
      'branchFromCommit',
      __DARWIN__ ? 'Create Branch from Commit' : 'Create branch from commit',
      () => config.onCreateBranch?.(commit)
    ),
    item(
      'createTag',
      'Create Tag…',
      () => config.onCreateTag?.(commit.sha),
      config.onCreateTag !== undefined
    )
  )
  if (config.onDeleteTag !== undefined && commit.tags.length > 0) {
    const unpushed = new Set(config.tagsToPush ?? [])
    const tags = commit.tags.map(tag =>
      item(
        'deleteTagNamed',
        `Delete tag ${tag}`,
        () => config.onDeleteTag?.(tag),
        unpushed.has(tag),
        [tag]
      )
    )
    items.push(
      { type: 'separator' },
      commit.tags.length === 1
        ? tags[0]
        : {
            labelKey: 'workspace.deleteTag',
            label: 'Delete tag…',
            submenu: commit.tags.map(tag =>
              item(
                'data',
                tag,
                () => config.onDeleteTag?.(tag),
                unpushed.has(tag),
                [tag]
              )
            ),
          }
    )
  }
  const enterprise =
    config.gitHubRepository !== null &&
    config.gitHubRepository.endpoint !== getDotComAPIEndpoint()
  items.push(
    item(
      'cherryPick',
      __DARWIN__ ? 'Cherry-pick Commit…' : 'Cherry-pick commit…',
      () => config.onCherryPick?.(selected),
      cherry
    ),
    { type: 'separator' },
    item('copySHA', 'Copy SHA', () => writeClipboardText(commit.sha)),
    item(
      'copyTags',
      __DARWIN__
        ? commit.tags.length > 1
          ? 'Copy Tags'
          : 'Copy Tag'
        : commit.tags.length > 1
        ? 'Copy tags'
        : 'Copy tag',
      () => writeClipboardText(commit.tags.join(' ')),
      commit.tags.length > 0
    ),
    item(
      enterprise ? 'viewEnterprise' : 'viewGitHub',
      enterprise ? 'View on GitHub Enterprise' : 'View on GitHub',
      () => config.onViewCommitOnGitHub?.(commit.sha),
      !local && !!config.gitHubRepository
    )
  )
  return items
}

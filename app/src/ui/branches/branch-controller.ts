import { Dispatcher } from '../dispatcher'
import {
  Repository,
  RepositoryWithGitHubRepository,
} from '../../models/repository'
import { Branch, BranchType } from '../../models/branch'
import { PullRequest } from '../../models/pull-request'
import { FoldoutType } from '../../lib/app-state'
import { PopupType } from '../../models/popup'
import { formatRelative } from '../../lib/format-relative'
import { startTimer } from '../lib/timing'

export function pullRequestSearchText(pr: PullRequest): string[] {
  const timeAgo = formatRelative(pr.created.getTime() - Date.now())
  return [
    pr.title,
    `#${pr.pullRequestNumber} opened ${timeAgo} by ${pr.author}`,
  ]
}

export function checkoutWorkspaceBranch(
  dispatcher: Dispatcher,
  repository: Repository,
  branch: Branch
) {
  dispatcher.closeFoldout(FoldoutType.Branch)
  const timer = startTimer('checkout branch from list', repository)
  return dispatcher.checkoutBranch(repository, branch).then(() => timer.done())
}

export function checkoutWorkspacePullRequest(
  dispatcher: Dispatcher,
  repository: RepositoryWithGitHubRepository,
  pr: PullRequest
) {
  dispatcher.closeFoldout(FoldoutType.Branch)
  const timer = startTimer('checkout pull request from list', repository)
  return dispatcher.checkoutPullRequest(repository, pr).then(() => timer.done())
}

/** Branch list deletion is deliberately not the toolbar's current-PR deletion.
 * Keep the original remote/local prompts and asynchronous ahead/behind lookup. */
export async function deleteWorkspaceBranch(
  dispatcher: Dispatcher,
  repository: Repository,
  branch: Branch
) {
  if (branch.type === BranchType.Remote) {
    dispatcher.showPopup({
      type: PopupType.DeleteRemoteBranch,
      repository,
      branch,
    })
    return
  }
  const aheadBehind = await dispatcher.getBranchAheadBehind(repository, branch)
  dispatcher.showPopup({
    type: PopupType.DeleteBranch,
    repository,
    branch,
    existsOnRemote: aheadBehind !== null,
  })
}

export function checkoutWorkspaceBranchInWorktree(
  dispatcher: Dispatcher,
  repository: Repository,
  branch: Branch
) {
  dispatcher.closeFoldout(FoldoutType.Branch)
  dispatcher.showPopup({
    type: PopupType.AddWorktree,
    repository,
    initialBranchName: branch.name,
    initialWorktreeName: `${repository.name}-${branch.nameWithoutRemote}`,
  })
}

export function checkoutWorkspacePRInWorktree(
  dispatcher: Dispatcher,
  repository: Repository,
  pr: PullRequest
) {
  dispatcher.closeFoldout(FoldoutType.Branch)
  dispatcher.showPopup({
    type: PopupType.AddWorktree,
    repository,
    initialBranchName: pr.head.ref,
    initialWorktreeName: `${repository.name}-${pr.pullRequestNumber}`,
  })
}

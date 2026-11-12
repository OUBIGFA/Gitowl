import { IWorkspaceMenuItem as IMenuItem } from '../lib/workspace-menu'
import { writeClipboardText } from '../main-process-proxy'
import { Branch, BranchType } from '../../models/branch'

interface IBranchContextMenuConfig {
  branch: Branch
  onRenameBranch?: (branchName: string) => void
  onViewBranchOnGitHub?: () => void
  onViewPullRequestOnGitHub?: () => void
  onDeleteBranch?: (branchName: string) => void
  onCheckoutInNewWorktree?: (branch: Branch) => void
}

export function generateBranchContextMenuItems(
  config: IBranchContextMenuConfig
): IMenuItem[] {
  const {
    branch,
    onRenameBranch,
    onViewBranchOnGitHub,
    onViewPullRequestOnGitHub,
    onDeleteBranch,
    onCheckoutInNewWorktree,
  } = config
  const items = new Array<IMenuItem>()

  if (onRenameBranch !== undefined) {
    items.push({
      labelKey: 'workspace.rename',
      label: 'Rename…',
      action: () => onRenameBranch(branch.name),
      enabled: branch.type === BranchType.Local,
    })
  }

  items.push({
    labelKey: 'workspace.copyBranch',
    label: __DARWIN__ ? 'Copy Branch Name' : 'Copy branch name',
    action: () => writeClipboardText(branch.name),
  })

  if (onViewBranchOnGitHub !== undefined) {
    items.push({
      labelKey: 'workspace.viewBranch',
      label: 'View Branch on GitHub',
      action: () => onViewBranchOnGitHub(),
    })
  }

  if (onViewPullRequestOnGitHub !== undefined) {
    items.push({
      labelKey: 'workspace.viewPR',
      label: 'View Pull Request on GitHub',
      action: () => onViewPullRequestOnGitHub(),
    })
  }

  if (onCheckoutInNewWorktree !== undefined) {
    items.push({
      labelKey: 'workspace.checkoutWorktree',
      label: __DARWIN__
        ? 'Checkout in New Worktree…'
        : 'Checkout in new worktree…',
      action: () => onCheckoutInNewWorktree(branch),
    })
  }

  items.push({ type: 'separator' })

  if (onDeleteBranch !== undefined) {
    items.push({
      labelKey: 'workspace.delete',
      label: 'Delete…',
      action: () => onDeleteBranch(branch.name),
    })
  }

  return items
}

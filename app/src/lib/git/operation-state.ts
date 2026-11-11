import { join } from 'path'
import { readFile } from 'fs/promises'
import { Repository } from '../../models/repository'
import { RebaseInternalState } from '../../models/rebase'
import { pathExists } from '../path-exists'

// Status readers share the upstream state inspection without loading mutation
// workflows, their credential dialogs, or the Electron renderer.

/** Check whether Git has recorded an in-progress rebase. */
export function isRebaseHeadSet(repository: Repository) {
  const path = join(repository.resolvedGitDir, 'REBASE_HEAD')
  return pathExists(path)
}

/** Read the upstream rebase snapshot, or null when absent or incomplete. */
export async function getRebaseInternalState(
  repository: Repository
): Promise<RebaseInternalState | null> {
  const isRebase = await isRebaseHeadSet(repository)

  if (!isRebase) {
    return null
  }

  let originalBranchTip: string | null = null
  let targetBranch: string | null = null
  let baseBranchTip: string | null = null

  try {
    originalBranchTip = await readFile(
      join(repository.resolvedGitDir, 'rebase-merge', 'orig-head'),
      'utf8'
    )

    originalBranchTip = originalBranchTip.trim()

    targetBranch = await readFile(
      join(repository.resolvedGitDir, 'rebase-merge', 'head-name'),
      'utf8'
    )

    if (targetBranch.startsWith('refs/heads/')) {
      targetBranch = targetBranch.substring(11).trim()
    }

    baseBranchTip = await readFile(
      join(repository.resolvedGitDir, 'rebase-merge', 'onto'),
      'utf8'
    )

    baseBranchTip = baseBranchTip.trim()
  } catch {}

  if (
    originalBranchTip != null &&
    targetBranch != null &&
    baseBranchTip != null
  ) {
    return { originalBranchTip, targetBranch, baseBranchTip }
  }

  return null
}

/** Check if the `.git/CHERRY_PICK_HEAD` file exists. */
export async function isCherryPickHeadFound(
  repository: Repository
): Promise<boolean> {
  try {
    return pathExists(join(repository.resolvedGitDir, 'CHERRY_PICK_HEAD'))
  } catch (err) {
    log.warn(
      `[cherryPick] a problem was encountered reading .git/CHERRY_PICK_HEAD,
       so it is unsafe to continue cherry-picking`,
      err
    )
    return false
  }
}

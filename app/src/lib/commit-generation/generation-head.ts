import { Repository } from '../../models/repository'
import { git } from '../git/core'

/** Read the generation baseline without applying a write transaction's rules.
 * Keep the original HEAD ref too: switching branches with the same tip still
 * changes the context. Quiet rev-parse permits an unborn repository. */
export async function readGenerationHead(
  repository: Repository
): Promise<string> {
  const [ref, tip] = await Promise.all([
    git(
      ['symbolic-ref', '--quiet', '--no-recurse', 'HEAD'],
      repository.path,
      'generationHeadRef',
      { successExitCodes: new Set([0, 1]) }
    ),
    git(
      ['rev-parse', '--verify', '--quiet', '--end-of-options', 'HEAD^{commit}'],
      repository.path,
      'generationHeadTip',
      { successExitCodes: new Set([0, 1]) }
    ),
  ])
  return JSON.stringify([
    ref.exitCode === 0 ? ref.stdout.trim() : null,
    tip.exitCode === 0 ? tip.stdout.trim() : null,
  ])
}

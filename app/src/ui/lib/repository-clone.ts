import * as Path from 'path'
import { readdir } from 'fs/promises'
import { Account } from '../../models/account'
import { CloneRepositoryTab } from '../../models/clone-repository-tab'
import { API, IAPIRepositoryCloneInfo } from '../../lib/api'
import {
  IRepositoryIdentifier,
  parseRemote,
  parseRepositoryIdentifier,
  sanitizeCloneName,
} from '../../lib/remote-parsing'
import { resolveURLCloneInfo } from '../../lib/clone-info'
import {
  canonicalMutationPath,
  isRepositoryAffectedByAssistedCommit,
} from '../../lib/git/repository-operation'

export interface IRepositoryCloneRequest {
  readonly tab: CloneRepositoryTab
  readonly url: string
  readonly lastParsedIdentifier: IRepositoryIdentifier | null
  readonly path: string | null
  readonly account: Account | null
}

/** Query/fragment data may authorize a Git URL but never belongs in a suggested
 * local directory or GitHub repository identifier. The request URL stays exact. */
export function parseRepositoryCloneIdentifier(
  url: string
): IRepositoryIdentifier | null {
  return parseRepositoryIdentifier(url.split(/[?#]/, 1)[0])
}

/** Original CloneRepository URL-to-destination update, shared without UI state. */
export function updatedRepositoryCloneURL(
  url: string,
  path: string | null,
  lastParsedIdentifier: IRepositoryIdentifier | null
) {
  const parsed = parseRepositoryCloneIdentifier(url)
  if (path === null) {
    return { url, path, lastParsedIdentifier }
  }
  const safeName = parsed ? sanitizeCloneName(parsed.name) : null
  const newPath = lastParsedIdentifier
    ? safeName
      ? Path.join(Path.dirname(path), safeName)
      : Path.dirname(path)
    : safeName
    ? Path.join(path, safeName)
    : path
  return { url, path: newPath, lastParsedIdentifier: parsed }
}

/** Original clone path validation, including assisted-run and macOS gates. */
export async function validateRepositoryClonePath(
  path: string | null,
  onCanonicalPath: (canonical: string) => void = () => {}
): Promise<Error | null> {
  if (path === null) {
    return new Error(
      'Unable to read path on disk. Please check the path and try again.'
    )
  }
  if (
    __DARWIN__ &&
    Path.basename(Path.resolve(path)).toLowerCase().endsWith('.app')
  ) {
    return new Error(
      'The local path cannot end in .app on macOS. Choose a different folder name to avoid creating an application bundle.'
    )
  }
  try {
    const canonical = await canonicalMutationPath(path)
    onCanonicalPath(canonical)
    if (isRepositoryAffectedByAssistedCommit(canonical)) {
      return new Error(
        'Finish or cancel the assisted commit run before cloning into this directory.'
      )
    }
    const directoryFiles = await readdir(path)
    return directoryFiles.length === 0
      ? null
      : new Error(
          'This folder contains files. Git can only clone to empty folders.'
        )
  } catch (error) {
    if (error.code === 'ENOTDIR') {
      return new Error(
        'There is already a file with this name. Git can only clone to a folder.'
      )
    }
    if (error.code === 'ENOENT') {
      return null
    }
    log.error(
      'CloneRepository: Path validation failed. Error: ' + error.message
    )
    return new Error(
      'Unable to read path on disk. Please check the path and try again.'
    )
  }
}

/** Original URL/wiki/selected-account clone-info resolution, not a new Git backend. */
export async function resolveRepositoryCloneInfo(
  request: IRepositoryCloneRequest,
  accounts: ReadonlyArray<Account>
): Promise<IAPIRepositoryCloneInfo | null> {
  const { tab, url, lastParsedIdentifier, account } = request
  if (url.endsWith('.wiki.git')) {
    return { url }
  }
  if (tab === CloneRepositoryTab.Generic) {
    return resolveURLCloneInfo(url, accounts)
  }
  if (lastParsedIdentifier !== null && account !== null) {
    const api = API.fromAccount(account)
    const { owner, name } = lastParsedIdentifier
    const protocol = parseRemote(url)?.protocol
    return api.fetchRepositoryCloneInfo(owner, name, protocol).catch(err => {
      log.error(`Failed to look up repository clone info for '${url}'`, err)
      return { url }
    })
  }
  return { url }
}

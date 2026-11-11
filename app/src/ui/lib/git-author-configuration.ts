import { unlink } from 'fs/promises'
import { setGlobalConfigValue } from '../../lib/git/config'
import {
  isConfigFileLockError,
  parseConfigLockFilePathFromError,
} from '../../lib/git'

export type GitAuthorSaveResult =
  | { readonly kind: 'saved' }
  | { readonly kind: 'locked'; readonly path: string }

/**
 * The existing ConfigureGitUser save contract, shared by the React baseline and
 * native presentation. Preserve conditional writes, write ordering and its lock
 * recovery decision; a frontend must not implement another Git configuration API.
 */
export async function saveConfiguredGitAuthor(
  name: string,
  email: string,
  globalUserName: string | undefined,
  globalUserEmail: string | undefined
): Promise<GitAuthorSaveResult> {
  try {
    if (name.length > 0 && name !== globalUserName) {
      await setGlobalConfigValue('user.name', name)
    }
    if (email.length > 0 && email !== globalUserEmail) {
      await setGlobalConfigValue('user.email', email)
    }
  } catch (error) {
    if (isConfigFileLockError(error)) {
      const path = parseConfigLockFilePathFromError(error.result)
      if (path !== null) {
        return { kind: 'locked', path }
      }
    }
    // The upstream form proceeds for non-lock failures. Do not silently change
    // that behavior during frontend migration; changing it is a separate fix.
  }
  return { kind: 'saved' }
}

/** Original ConfigLockFileExists cancellation/retry semantics, not automatic deletion. */
export async function deleteGitAuthorConfigLock(path: string): Promise<void> {
  try {
    await unlink(path)
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
      throw error
    }
  }
}

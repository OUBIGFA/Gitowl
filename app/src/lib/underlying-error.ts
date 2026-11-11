import { ErrorWithMetadata, getErrorCauses } from './error-with-metadata'
import { CopilotError } from './copilot-error'
import { GitError } from './git/core'

/** Original AppError selection, shared without importing React/Terminal/xterm.
 * Preserve the exact nested Copilot-error precedence and original identity. */
export function getUnderlyingError(error: Error): Error {
  const copilotError = getErrorCauses(error).find(
    (cause): cause is CopilotError => cause instanceof CopilotError
  )
  return (
    copilotError ?? (isErrorWithMetaData(error) ? error.underlyingError : error)
  )
}
export function isErrorWithMetaData(error: Error): error is ErrorWithMetadata {
  return error instanceof ErrorWithMetadata
}
export function isGitError(error: Error): error is GitError {
  return error instanceof GitError
}
export function isRawGitError(error: Error | null) {
  if (!error) {
    return false
  }
  const underlying = getUnderlyingError(error)
  return underlying instanceof GitError && underlying.isRawMessage
}

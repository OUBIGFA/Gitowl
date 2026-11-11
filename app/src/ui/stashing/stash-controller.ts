import { Dispatcher } from '../dispatcher'
import { Repository } from '../../models/repository'
import { IStashEntry } from '../../models/stash-entry'
import { PopupType } from '../../models/popup'
import { ErrorWithMetadata } from '../../lib/error-with-metadata'
import { RetryActionType } from '../../models/retry-actions'

/** Preserve the stash retry metadata in both frontends. A transport rejection
 * alone would bypass local-changes-overwritten/conflict recovery. */
export async function restoreWorkspaceStash(
  dispatcher: Dispatcher,
  repository: Repository,
  stashEntry: IStashEntry
) {
  try {
    await dispatcher.popStash(repository, stashEntry)
  } catch (err) {
    dispatcher.postError(
      new ErrorWithMetadata(err, {
        repository,
        retryAction: { type: RetryActionType.PopStash, stashEntry, repository },
      })
    )
  }
}

export async function discardWorkspaceStash(
  dispatcher: Dispatcher,
  repository: Repository,
  stashEntry: IStashEntry,
  confirm: boolean
) {
  if (confirm) {
    dispatcher.showPopup({
      type: PopupType.ConfirmDiscardStash,
      stash: stashEntry,
      repository,
    })
  } else {
    await dispatcher.dropStash(repository, stashEntry)
  }
}

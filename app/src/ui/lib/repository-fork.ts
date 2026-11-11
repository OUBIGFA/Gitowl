import { API } from '../../lib/api'
import { sendNonFatalException } from '../../lib/helpers/non-fatal-exception'
import { Account } from '../../models/account'
import {
  RepositoryWithGitHubRepository,
  isRepositoryWithForkedGitHubRepository,
} from '../../models/repository'
import { PopupType } from '../../models/popup'
import type { Dispatcher } from '../dispatcher'

/** Original CreateFork's API → convert → dismiss → choose-settings continuation. */
export async function createRepositoryFork(
  dispatcher: Dispatcher,
  repository: RepositoryWithGitHubRepository,
  account: Account,
  onCreated: () => void,
  onError: (error: Error) => void
): Promise<void> {
  const { gitHubRepository } = repository
  const api = API.fromAccount(account)
  try {
    const fork = await api.forkRepository(
      gitHubRepository.owner.login,
      gitHubRepository.name
    )
    dispatcher.incrementMetric('forksCreated')
    const updatedRepository = await dispatcher.convertRepositoryToFork(
      repository,
      fork
    )
    onCreated()
    if (isRepositoryWithForkedGitHubRepository(updatedRepository)) {
      dispatcher.showPopup({
        type: PopupType.ChooseForkSettings,
        repository: updatedRepository,
      })
    }
  } catch (e) {
    log.error(`Fork creation through API failed (${e})`)
    sendNonFatalException('forkCreation', e)
    onError(e)
  }
}

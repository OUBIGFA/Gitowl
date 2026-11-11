import { Repository } from '../models/repository'
import { addSafeDirectory, getRepositoryType } from '../lib/git'
import type { Dispatcher } from './dispatcher'

/** Shared Desktop missing-repository continuations; views do not replace Git
 * detection, repository identity, relocation, cloning or safe-directory policy. */
export const inspectMissingRepository = (repository: Repository) =>
  getRepositoryType(repository.path)

export const missingRepositoryCloneURL = (repository: Repository) =>
  repository.gitHubRepository?.cloneURL ?? null

export const checkMissingRepository = (
  dispatcher: Dispatcher,
  repository: Repository
) => dispatcher.refreshRepository(repository)

export const locateMissingRepository = (
  dispatcher: Dispatcher,
  repository: Repository
) => dispatcher.relocateRepository(repository)

export const removeMissingRepository = (
  dispatcher: Dispatcher,
  repository: Repository
) => dispatcher.removeRepository(repository, false)

/** unsafePath must come from the original Git detection shown to the user.
 * The native wire never accepts an arbitrary path to trust. */
export async function trustMissingRepository(
  dispatcher: Dispatcher,
  repository: Repository,
  unsafePath: string | undefined
) {
  if (!unsafePath) return
  await addSafeDirectory(unsafePath)
  const type = await inspectMissingRepository(repository)
  if (type.kind !== 'unsafe')
    await checkMissingRepository(dispatcher, repository)
}

export async function cloneMissingRepository(
  dispatcher: Dispatcher,
  repository: Repository
) {
  const cloneURL = missingRepositoryCloneURL(repository)
  if (!cloneURL) return
  try {
    await dispatcher.cloneAgain(cloneURL, repository.path)
  } catch (error) {
    await dispatcher.postError(error)
  }
}

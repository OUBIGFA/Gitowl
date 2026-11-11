import * as Path from 'path'
import { mkdir } from 'fs/promises'
import type { Dispatcher } from '../dispatcher'
import { Repository } from '../../models/repository'
import {
  initGitRepository,
  createCommit,
  getStatus,
  getAuthorIdentity,
} from '../../lib/git'
import { writeDefaultReadme } from '../add-repository/write-default-readme'
import { writeGitIgnore } from '../add-repository/gitignores'
import { ILicense, writeLicense } from '../add-repository/licenses'
import { writeGitAttributes } from '../add-repository/git-attributes'
import { writeGitDescription } from '../../lib/git/description'
import { pathExists } from '../../lib/path-exists'
import { CreateRepositoryError } from '../../lib/error-with-metadata'

export interface RepositoryCreationOptions {
  readonly name: string
  readonly description: string
  readonly createWithReadme: boolean
  readonly gitIgnore: string
  readonly license: string
  readonly licenses: ReadonlyArray<ILicense> | null
}

/**
 * Original CreateRepository file/initial-commit continuation, shared by the two
 * presentations. Dispatcher.createRepository still owns the mutation guard.
 * Errors deliberately retain upstream's per-file recovery and initial-commit
 * behavior; a failed optional file write does not discard the added repository.
 */
export async function createRepositoryCore(
  dispatcher: Dispatcher,
  fullPath: string,
  options: RepositoryCreationOptions,
  postCreationError: (error: Error) => void,
  onValidPath: (valid: boolean) => void
): Promise<Repository | undefined> {
  try {
    await mkdir(fullPath, { recursive: true })
    onValidPath(true)
  } catch (e) {
    if (e.code === 'EACCES' && e.errno === -13) {
      onValidPath(false)
      return
    }
    log.error(`createRepository: the directory at ${fullPath} is not valid`, e)
    postCreationError(e)
    return
  }
  try {
    await initGitRepository(fullPath)
  } catch (e) {
    log.error(
      `createRepository: unable to initialize a Git repository at ${fullPath}`,
      e
    )
    postCreationError(e)
    return
  }
  const repositories = await dispatcher.addRepositories([fullPath])
  if (repositories.length < 1) {
    return
  }
  const repository = repositories[0]
  if (options.createWithReadme) {
    try {
      await writeDefaultReadme(fullPath, options.name, options.description)
    } catch (e) {
      log.error(`createRepository: unable to write README at ${fullPath}`, e)
      postCreationError(e)
    }
  }
  if (options.gitIgnore !== 'None') {
    try {
      await writeGitIgnore(fullPath, options.gitIgnore)
    } catch (e) {
      log.error(
        `createRepository: unable to write .gitignore file at ${fullPath}`,
        e
      )
      postCreationError(e)
    }
  }
  if (options.description) {
    try {
      await writeGitDescription(fullPath, options.description)
    } catch (e) {
      log.error(
        `createRepository: unable to write .git/description file at ${fullPath}`,
        e
      )
      postCreationError(e)
    }
  }
  const licenseName = options.license === 'None' ? null : options.license
  const license = (options.licenses || []).find(l => l.name === licenseName)
  if (license) {
    try {
      const author = await getAuthorIdentity(repository)
      await writeLicense(fullPath, license, {
        fullname: author ? author.name : '',
        email: author ? author.email : '',
        year: new Date().getFullYear().toString(),
        description: '',
        project: options.name,
      })
    } catch (e) {
      log.error(`createRepository: unable to write LICENSE at ${fullPath}`, e)
      postCreationError(e)
    }
  }
  try {
    const gitAttributes = Path.join(fullPath, '.gitattributes')
    const gitAttributesExists = await pathExists(gitAttributes)
    if (!gitAttributesExists) {
      await writeGitAttributes(fullPath)
    }
  } catch (e) {
    log.error(
      `createRepository: unable to write .gitattributes at ${fullPath}`,
      e
    )
    postCreationError(e)
  }
  const status = await getStatus(repository, true, true).catch(e => {
    log.error(`createRepository: unable to get status for ${fullPath}`, e)
    postCreationError(new CreateRepositoryError(e))
    return null
  })
  if (status === null) {
    return
  }
  try {
    const files = status.workingDirectory.files
    if (files.length > 0) {
      await createCommit(repository, 'Initial commit', files)
    }
  } catch (e) {
    log.error(`createRepository: initial commit failed at ${fullPath}`, e)
    postCreationError(e)
  }
  return repository
}

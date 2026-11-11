import { join } from 'path'
import { getRepositoryType } from '../lib/git/rev-parse'
import { getStatus } from '../lib/git/status'
import { getCommits, getCommit, getChangedFiles } from '../lib/git/log'
import { createCommit } from '../lib/git/create-selected-commit'
import { getAuthorIdentity, getCommitterIdentity } from '../lib/git/var'
import { isRebaseHeadSet } from '../lib/git/operation-state'
import {
  withRepositoryGitOperation,
  withoutRepositoryGitAccess,
} from '../lib/git/repository-operation'
import { formatCommitMessage } from '../lib/format-commit-message'
import { pathExists } from '../lib/path-exists'
import { getCommitDiff, getWorkingDirectoryDiff } from '../lib/git/diff'
import { Repository } from '../models/repository'
import { FileChange } from '../models/status'
import { DiffType, IDiff, Image } from '../models/diff'
import { isRecord } from '../lib/is-record'
import { ServiceError } from './service-error'

export { ServiceError } from './service-error'

function stringParameter(params: Record<string, unknown>, key: string): string {
  const value = params[key]
  if (typeof value !== 'string' || value.length === 0 || value.includes('\0')) {
    throw new ServiceError(-32602, `Expected a non-empty ${key}`)
  }
  return value
}

function commitParameter(params: Record<string, unknown>): string {
  const value = stringParameter(params, 'commit')
  if (!/^[a-f0-9]{40}$|^[a-f0-9]{64}$/i.test(value)) {
    throw new ServiceError(-32602, 'Expected a full commit object ID')
  }
  return value
}

function selectedCommitParameters(params: Record<string, unknown>) {
  // Unsupported mutation options must not be silently treated as ordinary
  // commits (in particular amend, sign-off, partial lines or skipping hooks).
  if (
    Object.keys(params).some(
      key => !['id', 'paths', 'summary', 'description'].includes(key)
    )
  ) {
    throw new ServiceError(-32602, 'Unsupported commit parameter')
  }
  const summary = stringParameter(params, 'summary')
  if (summary.trim().length === 0) {
    throw new ServiceError(-32602, 'Expected a non-empty summary')
  }
  const { paths, description } = params
  if (
    !Array.isArray(paths) ||
    paths.length === 0 ||
    !paths.every(
      (path): path is string =>
        typeof path === 'string' && path.length > 0 && !path.includes('\0')
    )
  ) {
    throw new ServiceError(-32602, 'Expected non-empty selected paths')
  }
  if (new Set(paths).size !== paths.length) {
    throw new ServiceError(-32602, 'Selected paths must be unique')
  }
  if (
    description !== undefined &&
    (typeof description !== 'string' || description.includes('\0'))
  ) {
    throw new ServiceError(-32602, 'Expected a description string')
  }
  return { paths, summary, description: description ?? null }
}

function fileData(file: FileChange) {
  return {
    path: file.path,
    status: file.status.kind,
    ...('oldPath' in file.status ? { oldPath: file.status.oldPath } : {}),
    isSubmodule: file.status.submoduleStatus !== undefined,
  }
}

function imageData(image: Image | undefined) {
  return image === undefined
    ? undefined
    : {
        contents: image.contents,
        mediaType: image.mediaType,
        bytes: image.bytes,
      }
}

/** Keep the upstream diff classification and patch bytes, not a second diff algorithm. */
function diffData(diff: IDiff) {
  switch (diff.kind) {
    case DiffType.Text:
    case DiffType.LargeText:
      return {
        kind: diff.kind === DiffType.Text ? 'text' : 'large-text',
        text: diff.text,
        lineEndingsChange: diff.lineEndingsChange,
        hasHiddenBidiChars: diff.hasHiddenBidiChars,
      }
    case DiffType.Image:
      return {
        kind: 'image',
        previous: imageData(diff.previous),
        current: imageData(diff.current),
      }
    case DiffType.Binary:
      return { kind: 'binary' }
    case DiffType.Submodule:
      return {
        kind: 'submodule',
        submodule: {
          path: diff.path,
          url: diff.url,
          status: diff.status,
          oldSHA: diff.oldSHA,
          newSHA: diff.newSHA,
        },
      }
    case DiffType.Unrenderable:
      return { kind: 'unrenderable' }
  }
}

/**
 * Native repository access reuses upstream Git workflows. Mutations are limited
 * to ordinary full-file selected commits; operation recovery and credential UI
 * remain outside this transport. No arbitrary Git commands are exposed.
 */
export class RepositoryService {
  private readonly repositories = new Map<number, Repository>()
  private nextID = 1

  /** Resolve an already opened handle for other trusted native services. */
  public getRepository(id: unknown): Repository {
    if (typeof id !== 'number' || !Number.isSafeInteger(id)) {
      throw new ServiceError(-32602, 'Expected a repository ID')
    }
    const repository = this.repositories.get(id)
    if (repository === undefined) {
      throw new ServiceError(-32002, 'Repository is not open')
    }
    return repository
  }

  public async invoke(method: string, rawParams: unknown): Promise<unknown> {
    if (!isRecord(rawParams)) {
      throw new ServiceError(-32602, 'Expected an object for params')
    }
    const params = rawParams
    if (method === 'system.info') {
      return {
        protocol: 1,
        version: '0.0.1',
        capabilities: [
          'repository.open',
          'repository.status',
          'repository.history',
          'repository.changeset',
          'repository.diff',
          'repository.commit',
        ],
      }
    }
    if (method === 'repository.open') {
      const path = stringParameter(params, 'path')
      const type = await getRepositoryType(path)
      if (type.kind !== 'regular') {
        throw new ServiceError(-32001, `Cannot open repository: ${type.kind}`)
      }
      let repository = [...this.repositories.values()].find(
        r => r.path === type.topLevelWorkingDirectory
      )
      if (repository === undefined) {
        repository = new Repository(
          type.topLevelWorkingDirectory,
          this.nextID++,
          null,
          false,
          null,
          {},
          false,
          type.gitDir
        )
        this.repositories.set(repository.id, repository)
      }
      return { id: repository.id, name: repository.name, path: repository.path }
    }
    if (
      ![
        'repository.status',
        'repository.history',
        'repository.changeset',
        'repository.diff',
        'repository.commit',
      ].includes(method)
    ) {
      throw new ServiceError(-32601, `Unknown method: ${method}`)
    }
    const repository = this.getRepository(params.id)
    switch (method) {
      case 'repository.status': {
        const status = await getStatus(repository, true, true)
        return {
          branch: status.currentBranch ?? null,
          upstream: status.currentUpstreamBranch ?? null,
          ahead: status.branchAheadBehind?.ahead ?? 0,
          behind: status.branchAheadBehind?.behind ?? 0,
          conflicted: status.doConflictedFilesExist,
          operation:
            status.rebaseInternalState !== null
              ? 'rebase'
              : status.isCherryPickingHeadFound
              ? 'cherry-pick'
              : status.mergeHeadFound
              ? 'merge'
              : null,
          files: status.workingDirectory.files.map(fileData),
        }
      }
      case 'repository.commit': {
        const { paths, summary, description } = selectedCommitParameters(params)
        // Admission covers validation and the entire upstream reset + stage +
        // commit sequence, not just each individual Git subprocess.
        return withRepositoryGitOperation(
          repository.path,
          'mutation',
          async () => {
            const status = await getStatus(repository, true, true)
            // An incomplete rebase snapshot can be null. Its raw state directories
            // and sequencer still prohibit a plain commit, even after conflicts
            // have been staged as resolved. This service does not implement recovery.
            const operationInProgress =
              status.mergeHeadFound ||
              status.squashMsgFound ||
              status.rebaseInternalState !== null ||
              status.isCherryPickingHeadFound ||
              (await isRebaseHeadSet(repository)) ||
              (
                await Promise.all(
                  [
                    'rebase-merge',
                    'rebase-apply',
                    'sequencer',
                    'REVERT_HEAD',
                  ].map(name =>
                    pathExists(join(repository.resolvedGitDir, name))
                  )
                )
              ).some(Boolean)
            if (status.doConflictedFilesExist || operationInProgress) {
              throw new ServiceError(
                -32004,
                'Cannot create a selected commit during conflicts or an in-progress Git operation'
              )
            }
            const changedFiles = new Map(
              status.workingDirectory.files.map(file => [file.path, file])
            )
            const files = paths.map(path => {
              const file = changedFiles.get(path)
              if (file === undefined) {
                throw new ServiceError(
                  -32003,
                  `Selected file is no longer changed: ${path}`
                )
              }
              return file.withIncludeAll(true)
            })
            // Use Git's effective identities, not a second user/config parser, and
            // reject missing identities before createCommit changes the index.
            const author = await getAuthorIdentity(repository)
            const committer = await getCommitterIdentity(repository)
            if (author === null || committer === null) {
              throw new ServiceError(
                -32005,
                'Git author and committer identities are required'
              )
            }
            const message = await formatCommitMessage(repository, {
              summary,
              description,
            })
            await createCommit(repository, message, files, {
              // There is no interactive ignore decision over this protocol. Keep
              // observer callbacks out of operation access; never retry a write.
              onHookFailure: () =>
                withoutRepositoryGitAccess(async () => 'abort'),
            })
            // Upstream's stdout parser returns an abbreviated SHA (or a root-commit
            // marker). Resolve the complete object ID for changeset/diff requests.
            const commit = await getCommit(repository, 'HEAD')
            if (commit === null) {
              throw new ServiceError(
                -32000,
                'Commit completed but HEAD could not be read'
              )
            }
            return { sha: commit.sha }
          }
        )
      }
      case 'repository.history': {
        const skip = params.skip ?? 0
        const limit = params.limit ?? 100
        if (
          typeof skip !== 'number' ||
          !Number.isSafeInteger(skip) ||
          skip < 0 ||
          typeof limit !== 'number' ||
          !Number.isSafeInteger(limit) ||
          limit < 1 ||
          limit > 200
        ) {
          throw new ServiceError(-32602, 'Invalid history range')
        }
        const commits = await getCommits(repository, undefined, limit, skip)
        return commits.map(commit => ({
          sha: commit.sha,
          shortSha: commit.shortSha,
          summary: commit.summary,
          body: commit.body,
          authorName: commit.author.name,
          authorEmail: commit.author.email,
          date: commit.author.date.toISOString(),
          tags: commit.tags,
        }))
      }
      case 'repository.changeset': {
        const changeset = await getChangedFiles(
          repository,
          commitParameter(params)
        )
        return { ...changeset, files: changeset.files.map(fileData) }
      }
      case 'repository.diff': {
        const path = stringParameter(params, 'path')
        if (params.commit !== undefined) {
          const commit = commitParameter(params)
          const changeset = await getChangedFiles(repository, commit)
          const file = changeset.files.find(f => f.path === path)
          if (file === undefined) {
            throw new ServiceError(-32003, 'File is not in this changeset')
          }
          return diffData(await getCommitDiff(repository, file, commit))
        }
        // Re-read status before resolving a file. Paths from the UI cannot read
        // arbitrary files outside the opened repository or stale file records.
        const status = await getStatus(repository, true, true)
        const file = status.workingDirectory.files.find(f => f.path === path)
        if (file === undefined) {
          throw new ServiceError(-32003, 'File is no longer changed')
        }
        return diffData(await getWorkingDirectoryDiff(repository, file))
      }
      default:
        throw new ServiceError(-32601, `Unknown method: ${method}`)
    }
  }
}

import type { INativeRepositoryReference } from './desktop-presenter'
import type { CommitOptions } from '../lib/app-state'
import { isRecord } from '../lib/is-record'

type RepositoryCommand = {
  readonly repository: INativeRepositoryReference
}
export type NativeCommitCommand = RepositoryCommand &
  (
    | {
        readonly name: 'commit.options'
        readonly options: Partial<CommitOptions>
      }
    | { readonly name: 'commit.amend'; readonly enabled: boolean }
    | { readonly name: 'commit.mode'; readonly mode: 'manual' | 'copilot' }
    | { readonly name: 'commit.coAuthors.show'; readonly enabled: boolean }
    | { readonly name: 'commit.coAuthors.search'; readonly query: string }
    | { readonly name: 'commit.coAuthors.add'; readonly id: string }
    | { readonly name: 'commit.coAuthors.remove'; readonly id: string }
    | { readonly name: 'commit.assisted.pushAfter'; readonly enabled: boolean }
    | { readonly name: 'commit.assisted.create' }
    | { readonly name: 'commit.assisted.cancel'; readonly runID: string }
    | {
        readonly name: 'commit.assisted.retry'
        readonly runID: string
        readonly kind: 'recovery' | 'push'
      }
    | { readonly name: 'commit.assisted.dismiss'; readonly runID: string }
    | { readonly name: 'commit.assisted.details'; readonly runID: string }
    | { readonly name: 'commit.progress' }
  )

const text = (value: unknown): value is string =>
  typeof value === 'string' && !value.includes('\0')
const only = (value: Record<string, unknown>, keys: ReadonlyArray<string>) =>
  Object.keys(value).every(key => keys.includes(key))

/** A finite presentation protocol, not reflected Dispatcher method names. */
export function parseCommitCommand(
  value: Record<string, unknown>
): NativeCommitCommand | null {
  const ref = value.repository
  const exact = (keys: ReadonlyArray<string>) =>
    only(value, ['name', 'repository', ...keys])
  let valid = false
  switch (value.name) {
    case 'commit.options':
      valid =
        exact(['options']) &&
        isRecord(value.options) &&
        only(value.options, [
          'skipCommitHooks',
          'signOffCommits',
          'allowEmptyCommit',
        ]) &&
        Object.values(value.options).every(value => typeof value === 'boolean')
      break
    case 'commit.amend':
    case 'commit.coAuthors.show':
    case 'commit.assisted.pushAfter':
      valid = exact(['enabled']) && typeof value.enabled === 'boolean'
      break
    case 'commit.mode':
      valid =
        exact(['mode']) && (value.mode === 'manual' || value.mode === 'copilot')
      break
    case 'commit.coAuthors.search':
      valid = exact(['query']) && text(value.query)
      break
    case 'commit.coAuthors.add':
    case 'commit.coAuthors.remove':
      valid = exact(['id']) && text(value.id)
      break
    case 'commit.assisted.create':
    case 'commit.progress':
      valid = exact([])
      break
    case 'commit.assisted.cancel':
    case 'commit.assisted.dismiss':
    case 'commit.assisted.details':
      valid = exact(['runID']) && text(value.runID)
      break
    case 'commit.assisted.retry':
      valid =
        exact(['runID', 'kind']) &&
        text(value.runID) &&
        (value.kind === 'recovery' || value.kind === 'push')
      break
    default:
      return null
  }
  if (
    !valid ||
    !isRecord(ref) ||
    !only(ref, ['id', 'path']) ||
    !Number.isSafeInteger(ref.id) ||
    !text(ref.path)
  ) {
    throw new Error('Invalid commit presentation command')
  }
  return value as unknown as NativeCommitCommand
}

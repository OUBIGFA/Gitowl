import type { INativeRepositoryReference } from './desktop-presenter'
import { isRecord } from '../lib/is-record'

export type WorkspaceMenuTarget =
  | { readonly kind: 'branch'; readonly id: string }
  | { readonly kind: 'worktree'; readonly id: string }
  | { readonly kind: 'pullRequest'; readonly id: string }
  | { readonly kind: 'file'; readonly id: string }
  | { readonly kind: 'historyFile'; readonly id: string }
  | { readonly kind: 'commit'; readonly id: string }
  | { readonly kind: 'changes'; readonly id: '' }

export interface IWorkspaceFileFilter {
  readonly filterText: string
  readonly isIncludedInCommit: boolean
  readonly isExcludedFromCommit: boolean
  readonly isNewFile: boolean
  readonly isModifiedFile: boolean
  readonly isDeletedFile: boolean
}

/** Explicit presentation intents only. References are resolved back to the
 * current original Branch/File/Commit/Worktree; no objects or callbacks cross IPC. */
export type NativeWorkspaceAction =
  | { readonly type: 'submodule.open'; readonly fileID: string }
  | {
      readonly type: 'conflicts.open' | 'conflicts.continue' | 'conflicts.abort'
      readonly identity: string
    }
  | {
      readonly type: 'navigation.open' | 'navigation.close'
      readonly target: 'branches' | 'pullRequests' | 'worktrees'
    }
  | {
      readonly type: 'navigation.filter'
      readonly scope: 'branches' | 'pullRequests' | 'worktrees' | 'compare'
      readonly text: string
    }
  | { readonly type: 'pullRequest.refresh' }
  | { readonly type: 'branch.checkout'; readonly ref: string }
  | { readonly type: 'branch.create'; readonly initialName: string }
  | { readonly type: 'pullRequest.checkout'; readonly number: number }
  | { readonly type: 'worktree.select'; readonly path: string }
  | { readonly type: 'worktree.create' }
  | {
      readonly type: 'compare.branch'
      readonly ref: string
      readonly mode: 'ahead' | 'behind'
    }
  | { readonly type: 'compare.list'; readonly visible: boolean }
  | { readonly type: 'compare.history' }
  | {
      readonly type: 'compare.operation'
      readonly operation: 'merge' | 'squash' | 'rebase'
    }
  | { readonly type: 'compare.merge' }
  | { readonly type: 'history.select'; readonly shas: ReadonlyArray<string> }
  | {
      readonly type: 'history.reorder'
      readonly shas: ReadonlyArray<string>
      readonly beforeSHA: string | null
    }
  | { readonly type: 'history.reorder.cancel' }
  | { readonly type: 'stash.view' | 'stash.hide' | 'stash.create' }
  | { readonly type: 'stash.restore' | 'stash.discard'; readonly sha: string }
  | {
      readonly type: 'stash.file'
      readonly sha: string
      readonly fileID: string
    }
  | { readonly type: 'files.filter'; readonly filter: IWorkspaceFileFilter }
  | {
      readonly type: 'menu'
      readonly target: WorkspaceMenuTarget
      readonly lease: string
      readonly path: ReadonlyArray<number>
    }
  | {
      readonly type: 'banner'
      readonly id: number
      readonly action: 'dismiss' | 'open' | 'undo' | 'throwAway' | 'support'
    }
  | {
      readonly type: 'diff.prepare' | 'diff.expand' | 'diff.collapse'
      readonly fileID: string
      readonly identity: string
    }
  | {
      readonly type: 'diff.discard'
      readonly fileID: string
      readonly identity: string
      readonly lineIDs: ReadonlyArray<number>
    }
  | {
      readonly type: 'diff.option'
      readonly option: 'whitespace' | 'sideBySide' | 'checkMarks'
      readonly value: boolean
    }

export interface INativeWorkspaceCommand {
  readonly name: 'workspace.action'
  readonly repository: INativeRepositoryReference | null
  readonly action: NativeWorkspaceAction
}

const text = (v: unknown): v is string =>
  typeof v === 'string' && !v.includes('\0')
const integer = (v: unknown): v is number =>
  typeof v === 'number' && Number.isSafeInteger(v) && v >= 0
const keys = (v: Record<string, unknown>, allowed: ReadonlyArray<string>) =>
  Object.keys(v).every(k => allowed.includes(k))
const strings = (v: unknown): v is string[] =>
  Array.isArray(v) && v.every(text) && new Set(v).size === v.length

export function parseNativeWorkspaceCommand(
  value: Record<string, unknown>
): INativeWorkspaceCommand | null {
  if (value.name !== 'workspace.action') {
    return null
  }
  const { repository, action } = value
  if (
    !keys(value, ['name', 'repository', 'action']) ||
    !isRecord(action) ||
    (repository === null
      ? action.type !== 'banner'
      : !isRecord(repository) ||
        !keys(repository, ['id', 'path']) ||
        !integer(repository.id) ||
        !text(repository.path))
  ) {
    throw new Error('Invalid workspace command')
  }
  const exact = (allowed: string[]) => keys(action, ['type', ...allowed])
  let valid = false
  switch (action.type) {
    case 'submodule.open':
      valid = exact(['fileID']) && text(action.fileID)
      break
    case 'conflicts.open':
    case 'conflicts.continue':
    case 'conflicts.abort':
      valid = exact(['identity']) && text(action.identity)
      break
    case 'navigation.open':
    case 'navigation.close':
      valid =
        exact(['target']) &&
        ['branches', 'pullRequests', 'worktrees'].includes(
          String(action.target)
        )
      break
    case 'navigation.filter':
      valid =
        exact(['scope', 'text']) &&
        ['branches', 'pullRequests', 'worktrees', 'compare'].includes(
          String(action.scope)
        ) &&
        text(action.text)
      break
    case 'pullRequest.refresh':
      valid = exact([])
      break
    case 'branch.checkout':
      valid = exact(['ref']) && text(action.ref)
      break
    case 'branch.create':
      valid = exact(['initialName']) && text(action.initialName)
      break
    case 'pullRequest.checkout':
      valid = exact(['number']) && integer(action.number) && action.number > 0
      break
    case 'worktree.select':
      valid = exact(['path']) && text(action.path)
      break
    case 'compare.branch':
      valid =
        exact(['ref', 'mode']) &&
        text(action.ref) &&
        (action.mode === 'ahead' || action.mode === 'behind')
      break
    case 'compare.list':
      valid = exact(['visible']) && typeof action.visible === 'boolean'
      break
    case 'compare.operation':
      valid =
        exact(['operation']) &&
        ['merge', 'squash', 'rebase'].includes(String(action.operation))
      break
    case 'worktree.create':
    case 'compare.history':
    case 'compare.merge':
    case 'history.reorder.cancel':
    case 'stash.view':
    case 'stash.hide':
    case 'stash.create':
      valid = exact([])
      break
    case 'history.select':
      valid = exact(['shas']) && strings(action.shas)
      break
    case 'history.reorder':
      valid =
        exact(['shas', 'beforeSHA']) &&
        strings(action.shas) &&
        action.shas.length > 0 &&
        (action.beforeSHA === null || text(action.beforeSHA))
      break
    case 'stash.restore':
    case 'stash.discard':
      valid = exact(['sha']) && text(action.sha)
      break
    case 'stash.file':
      valid =
        exact(['sha', 'fileID']) && text(action.sha) && text(action.fileID)
      break
    case 'files.filter': {
      const f = action.filter
      const bools = [
        'isIncludedInCommit',
        'isExcludedFromCommit',
        'isNewFile',
        'isModifiedFile',
        'isDeletedFile',
      ]
      valid =
        exact(['filter']) &&
        isRecord(f) &&
        keys(f, ['filterText', ...bools]) &&
        text(f.filterText) &&
        bools.every(k => typeof f[k] === 'boolean')
      break
    }
    case 'menu': {
      const target = action.target
      valid =
        exact(['target', 'lease', 'path']) &&
        text(action.lease) &&
        isRecord(target) &&
        keys(target, ['kind', 'id']) &&
        text(target.id) &&
        [
          'branch',
          'worktree',
          'pullRequest',
          'file',
          'historyFile',
          'commit',
          'changes',
        ].includes(String(target.kind)) &&
        (target.kind !== 'changes' || target.id === '') &&
        Array.isArray(action.path) &&
        action.path.length > 0 &&
        action.path.length <= 8 &&
        action.path.every(integer)
      break
    }
    case 'banner':
      valid =
        exact(['id', 'action']) &&
        integer(action.id) &&
        ['dismiss', 'open', 'undo', 'throwAway', 'support'].includes(
          String(action.action)
        )
      break
    case 'diff.prepare':
    case 'diff.expand':
    case 'diff.collapse':
      valid =
        exact(['fileID', 'identity']) &&
        text(action.fileID) &&
        text(action.identity)
      break
    case 'diff.discard':
      valid =
        exact(['fileID', 'identity', 'lineIDs']) &&
        text(action.fileID) &&
        text(action.identity) &&
        Array.isArray(action.lineIDs) &&
        action.lineIDs.length > 0 &&
        action.lineIDs.every(integer) &&
        new Set(action.lineIDs).size === action.lineIDs.length
      break
    case 'diff.option':
      valid =
        exact(['option', 'value']) &&
        ['whitespace', 'sideBySide', 'checkMarks'].includes(
          String(action.option)
        ) &&
        typeof action.value === 'boolean'
      break
  }
  if (!valid) {
    throw new Error('Invalid workspace action parameters')
  }
  return value as unknown as INativeWorkspaceCommand
}

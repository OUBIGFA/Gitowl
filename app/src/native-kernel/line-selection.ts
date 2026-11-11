import { createHash } from 'crypto'
import type { AppRuntime } from '../ui/app-runtime'
import { ChangesSelectionKind } from '../lib/app-state'
import { DiffLineType, DiffType } from '../models/diff'
import type { IRepositoryState } from '../lib/app-state'
import type { NativeRepositoryReference } from './desktop-presenter'
import { Repository } from '../models/repository'

export interface KernelDiffSelection {
  readonly identity: string
  readonly fileID: string
  readonly showCheckMarks: boolean
  readonly lines: ReadonlyArray<{
    readonly id: number
    readonly kind: 'add' | 'delete'
    readonly oldBufferLine: number | null
    readonly newBufferLine: number | null
    readonly selected: boolean
  }>
}

/** Original patch indices, not coordinates guessed from the native diff text. */
export function projectDiffSelection(
  state: IRepositoryState,
  showCheckMarks: boolean
): KernelDiffSelection | null {
  const selection = state.changesState.selection
  if (
    selection.kind !== ChangesSelectionKind.WorkingDirectory ||
    selection.diff?.kind !== DiffType.Text ||
    selection.selectedFileIDs.length !== 1
  )
    return null
  const file = state.changesState.workingDirectory.files.find(
    file => file.id === selection.selectedFileIDs[0]
  )
  if (file === undefined) return null
  let oldBufferLine = 0
  let newBufferLine = 0
  const lines: KernelDiffSelection['lines'][number][] = []
  for (const hunk of selection.diff.hunks) {
    for (const line of hunk.lines) {
      const old = line.oldLineNumber === null ? null : oldBufferLine++
      const current = line.newLineNumber === null ? null : newBufferLine++
      if (!line.isIncludeableLine() || line.originalLineNumber === null)
        continue
      lines.push({
        id: line.originalLineNumber,
        kind: line.type === DiffLineType.Add ? 'add' : 'delete',
        oldBufferLine: old,
        newBufferLine: current,
        selected: file.selection.isSelected(line.originalLineNumber),
      })
    }
  }
  return {
    identity: createHash('sha256').update(selection.diff.text).digest('hex'),
    fileID: file.id,
    showCheckMarks,
    lines,
  }
}

/** Apply a native range using the very same original immutable DiffSelection. */
export function includeDiffLines(
  runtime: AppRuntime,
  repository: Repository,
  fileID: string,
  identity: string,
  ids: ReadonlyArray<number>,
  included: boolean
) {
  const state = runtime.repositoryStateManager.get(repository)
  const projection = projectDiffSelection(
    state,
    runtime.appStore.getState().showDiffCheckMarks
  )
  if (
    projection === null ||
    projection.fileID !== fileID ||
    projection.identity !== identity ||
    ids.length === 0 ||
    ids.some(id => !projection.lines.some(line => line.id === id))
  ) {
    throw new Error(
      'The diff selection changed; review the current patch before selecting lines'
    )
  }
  const file = state.changesState.workingDirectory.files.find(
    file => file.id === fileID
  )!
  let selection = file.selection
  for (const id of ids) selection = selection.withLineSelection(id, included)
  return runtime.dispatcher.changeFileLineSelection(repository, file, selection)
}

export interface IncludeDiffLinesCommand {
  readonly name: 'repository.lines.include'
  readonly repository: NativeRepositoryReference
  readonly fileID: string
  readonly identity: string
  readonly lineIDs: ReadonlyArray<number>
  readonly included: boolean
}

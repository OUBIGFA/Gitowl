import * as Path from 'path'
import { EOL } from 'os'
import { IWorkspaceMenuItem } from '../lib/workspace-menu'
import {
  WorkingDirectoryFileChange,
  WorkingDirectoryStatus,
  AppFileStatusKind,
} from '../../models/status'
import { Repository } from '../../models/repository'
import { revealInFileManager } from '../../lib/app-shell'
import { writeClipboardText } from '../main-process-proxy'
import {
  isSafeFileExtension,
  DefaultEditorLabel,
  CopyFilePathLabel,
  RevealInFileManagerLabel,
  OpenWithDefaultProgramLabel,
  CopyRelativeFilePathLabel,
  CopySelectedPathsLabel,
  CopySelectedRelativePathsLabel,
} from '../lib/context-menu'

export interface IChangesMenuConfig {
  readonly repository: Repository
  readonly workingDirectory: WorkingDirectoryStatus
  readonly selectedFileIDs: ReadonlyArray<string>
  readonly askForConfirmationOnDiscardChanges: boolean
  readonly externalEditorLabel?: string
  readonly onDiscard: (paths: ReadonlyArray<string>) => void
  readonly onIgnoreFile: (paths: string | string[]) => void
  readonly onIgnorePattern: (patterns: string | string[]) => void
  readonly onIncludeChanged: (
    file: WorkingDirectoryFileChange,
    included: boolean
  ) => void
  readonly onOpenItem: (path: string) => void
  readonly onOpenItemInExternalEditor: (path: string) => void
}

/** The original changes-list policy. In particular a right-click outside a
 * multi-selection acts on that file alone; rebase exposes only safe actions. */
export function generateChangesContextMenu(
  config: IChangesMenuConfig,
  file: WorkingDirectoryFileChange,
  rebase: boolean
): ReadonlyArray<IWorkspaceMenuItem> {
  const { path, status } = file
  const ids = config.selectedFileIDs.includes(file.id)
    ? config.selectedFileIDs
    : [file.id]
  const selected = ids.flatMap(id => {
    const found = config.workingDirectory.findFileWithID(id)
    return found === null || found === undefined ? [] : [found]
  })
  const paths = rebase ? [path] : selected.map(f => f.path)
  const items: IWorkspaceMenuItem[] = []
  const discard = (): IWorkspaceMenuItem => ({
    labelKey: config.askForConfirmationOnDiscardChanges
      ? 'workspace.discardConfirm'
      : 'workspace.discard',
    labelData: [String(paths.length)],
    label:
      (paths.length === 1
        ? __DARWIN__
          ? 'Discard Changes'
          : 'Discard changes'
        : __DARWIN__
        ? `Discard ${paths.length} Selected Changes`
        : `Discard ${paths.length} selected changes`) +
      (config.askForConfirmationOnDiscardChanges ? '…' : ''),
    action: () => config.onDiscard(paths),
  })
  if (!rebase || status.kind === AppFileStatusKind.Untracked) {
    items.push(discard(), { type: 'separator' })
  }
  if (!rebase) {
    const enabled = Path.basename(path) !== '.gitignore'
    if (paths.length === 1) {
      items.push({
        labelKey: 'workspace.ignoreFile',
        label: __DARWIN__
          ? 'Ignore File (Add to .gitignore)'
          : 'Ignore file (add to .gitignore)',
        action: () => config.onIgnoreFile(path),
        enabled,
      })
      const folders = path.split('/').slice(0, -1)
      if (folders.length > 0) {
        items.push({
          labelKey: 'workspace.ignoreFolder',
          label: __DARWIN__
            ? 'Ignore Folder (Add to .gitignore)'
            : 'Ignore folder (add to .gitignore)',
          enabled,
          submenu: folders.map((_, index) => {
            const folder = `/${folders
              .slice(0, folders.length - index)
              .join('/')}`
            return {
              label: folder,
              labelKey: 'workspace.data',
              labelData: [folder],
              action: () => config.onIgnoreFile(folder),
            }
          }),
        })
      }
    } else if (paths.length > 1) {
      items.push({
        labelKey: 'workspace.ignoreSelected',
        labelData: [String(paths.length)],
        label: __DARWIN__
          ? `Ignore ${paths.length} Selected Files (Add to .gitignore)`
          : `Ignore ${paths.length} selected files (add to .gitignore)`,
        action: () =>
          config.onIgnoreFile(
            paths.filter(p => Path.basename(p) !== '.gitignore')
          ),
        enabled: paths.some(p => Path.basename(p) !== '.gitignore'),
      })
    }
    const extensions = Array.from(
      new Set(paths.map(p => Path.extname(p)).filter(e => e.length > 0))
    )
    for (const extension of extensions.slice(0, 5)) {
      items.push({
        labelKey: 'workspace.ignoreExtension',
        labelData: [extension],
        label: __DARWIN__
          ? `Ignore All ${extension} Files (Add to .gitignore)`
          : `Ignore all ${extension} files (add to .gitignore)`,
        action: () => config.onIgnorePattern(`*${extension}`),
      })
    }
    if (paths.length > 1) {
      items.push(
        { type: 'separator' },
        {
          labelKey: 'workspace.includeSelected',
          label: __DARWIN__
            ? 'Include Selected Files'
            : 'Include selected files',
          action: () => {
            selected.map(f => config.onIncludeChanged(f, true))
          },
        },
        {
          labelKey: 'workspace.excludeSelected',
          label: __DARWIN__
            ? 'Exclude Selected Files'
            : 'Exclude selected files',
          action: () => {
            selected.map(f => config.onIncludeChanged(f, false))
          },
        }
      )
    }
    items.push({ type: 'separator' })
  }
  const multi = !rebase && paths.length > 1
  items.push(
    {
      labelKey: multi ? 'workspace.copyPaths' : 'workspace.copyPath',
      label: multi ? CopySelectedPathsLabel : CopyFilePathLabel,
      action: () =>
        writeClipboardText(
          (multi ? paths : [path])
            .map(p => Path.join(config.repository.path, p))
            .join(EOL)
        ),
    },
    {
      labelKey: multi
        ? 'workspace.copyRelativePaths'
        : 'workspace.copyRelativePath',
      label: multi ? CopySelectedRelativePathsLabel : CopyRelativeFilePathLabel,
      action: () =>
        writeClipboardText(
          (multi ? paths : [path]).map(p => Path.normalize(p)).join(EOL)
        ),
    },
    { type: 'separator' },
    {
      labelKey: 'workspace.reveal',
      label: RevealInFileManagerLabel,
      action: () => revealInFileManager(config.repository, path),
      enabled: status.kind !== AppFileStatusKind.Deleted,
    },
    {
      labelKey: config.externalEditorLabel
        ? 'workspace.openEditorNamed'
        : 'workspace.openEditor',
      labelData: config.externalEditorLabel ? [config.externalEditorLabel] : [],
      label: config.externalEditorLabel
        ? `Open in ${config.externalEditorLabel}`
        : DefaultEditorLabel,
      action: () => config.onOpenItemInExternalEditor(path),
      enabled: status.kind !== AppFileStatusKind.Deleted,
    },
    {
      labelKey: 'workspace.openDefault',
      label: OpenWithDefaultProgramLabel,
      action: () => config.onOpenItem(path),
      enabled:
        status.kind !== AppFileStatusKind.Deleted &&
        isSafeFileExtension(Path.extname(path)),
    }
  )
  return items
}

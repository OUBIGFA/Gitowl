import { createHash } from 'crypto'
import type { IAppState } from '../lib/app-state'
import { enableWorktreeSupport } from '../lib/feature-flag'
import { Repository, getGitHubHtmlUrl } from '../models/repository'
import { CloningRepository } from '../models/cloning-repository'
import type { AppMenuController } from '../ui/app-menu-controller'
import type { WorkspaceMenuItem } from '../ui/lib/workspace-menu'
import { generateRepositoryListContextMenu } from '../ui/repositories-list/repository-list-item-context-menu'
import type { NativeRepositoryReference } from './desktop-presenter'
import {
  projectContextMenuItems,
  KernelWorkspaceMenuItem,
} from './workspace-projection'

export interface KernelRepositoryMenu {
  readonly repository: NativeRepositoryReference
  readonly lease: string
  readonly items: ReadonlyArray<KernelWorkspaceMenuItem>
}

/** The upstream generator owns every item, account choice and disabled state.
 * Omitting the controller makes projection inert without inventing a second menu. */
function repositoryMenu(
  state: IAppState,
  repository: Repository | CloningRepository,
  controller?: AppMenuController
) {
  return generateRepositoryListContextMenu({
    repository,
    accounts: state.accounts,
    shellLabel: state.useCustomShell ? undefined : state.selectedShell,
    externalEditorLabel: state.useCustomEditor
      ? undefined
      : state.selectedExternalEditor ?? undefined,
    askForConfirmationOnRemoveRepository:
      state.askForConfirmationOnRepositoryRemoval,
    onViewOnGitHub: r => controller?.viewRepositoryOnGitHub(r),
    onOpenInShell: r => controller?.openRepositoryInShell(r),
    onShowRepository: r => controller?.revealRepository(r),
    onOpenInExternalEditor: r => controller?.openRepositoryInEditor(r),
    onRemoveRepository: r => controller?.removeRepository(r),
    onChangeRepositoryAlias: r => controller?.changeRepositoryAlias(r),
    onRemoveRepositoryAlias: r => controller?.removeRepositoryAlias(r),
    onChangeRepositoryAccount: (r, login) =>
      controller?.changeRepositoryAccount(r, login),
    onShowRepositorySettings: r => controller?.showRepositorySettingsFor(r),
    onCreateWorktree: enableWorktreeSupport()
      ? r => controller?.createWorktree(r)
      : undefined,
    onShowWorktrees: enableWorktreeSupport()
      ? r => controller?.showRepositoryWorktrees(r)
      : undefined,
  })
}

function projectRepositoryMenu(
  state: IAppState,
  repository: Repository | CloningRepository,
  items: ReadonlyArray<WorkspaceMenuItem>
): KernelRepositoryMenu {
  const projected = projectContextMenuItems(items)
  // Model hashes include account/workflow identity. Sensitive model metadata
  // participates only in this server-side digest, never in the wire DTO.
  const lease = createHash('sha256')
    .update(
      JSON.stringify({
        model: repository.hash,
        kind: repository instanceof Repository ? 'repository' : 'cloning',
        id: repository.id,
        path: repository.path,
        url:
          repository instanceof Repository
            ? getGitHubHtmlUrl(repository)
            : null,
        removalConfirmation: state.askForConfirmationOnRepositoryRemoval,
        items: projected,
      })
    )
    .digest('hex')
  return {
    repository: { id: repository.id, path: repository.path },
    lease,
    items: projected,
  }
}

export function projectRepositoryMenus(
  state: IAppState
): ReadonlyArray<KernelRepositoryMenu> {
  return state.repositories.map(repository =>
    projectRepositoryMenu(state, repository, repositoryMenu(state, repository))
  )
}

/** Resolve the clicked row, not the current selection, and reject stale leases. */
export async function executeRepositoryMenu(
  state: IAppState,
  controller: AppMenuController,
  reference: NativeRepositoryReference,
  lease: string,
  path: ReadonlyArray<number>
) {
  const repository = state.repositories.find(
    r => r.id === reference.id && r.path === reference.path
  )
  if (repository === undefined)
    throw new Error('This repository is no longer listed')
  if (state.currentPopup !== null)
    throw new Error('Close the current dialog before opening this menu')
  const items = repositoryMenu(state, repository, controller)
  if (projectRepositoryMenu(state, repository, items).lease !== lease)
    throw new Error('This context menu changed; open it again')
  let menu: ReadonlyArray<WorkspaceMenuItem> = items
  let item: WorkspaceMenuItem | undefined
  for (const index of path) {
    item = menu[index]
    if (
      item === undefined ||
      item.enabled === false ||
      item.type === 'separator'
    )
      throw new Error('This menu action is not available')
    menu = item.submenu ?? []
  }
  if (item === undefined || item.submenu !== undefined)
    throw new Error('This menu entry is not an action')
  // The original checked account is an enabled, inert leaf. Selecting it is
  // intentionally a no-op, not a failed application command.
  return item.action?.()
}

import { Emitter } from 'event-kit'
import type { AppRuntime } from '../ui/app-runtime'
import { executeRepositoryMenu } from './repository-menu'
import type { INativeRepositoryReference } from './desktop-presenter'
import {
  AppMenuController,
  AppMenuPresentationAction,
} from '../ui/app-menu-controller'
import { MenuEvent } from '../main-process/menu/menu-event'
import { getMenuState } from '../lib/menu-update'
import { IMenu, MenuItem, ExecutableMenuItem } from '../models/app-menu'
import type { MenuIDs } from '../models/menu-ids'
import { showTestUI } from '../ui/lib/test-ui-components/test-ui-components'
import labels from '../../../references/native-ui-menu-labels.json'

/** Data-only menu projection. No closures, raw application state or account
 * objects leave the kernel. IDs always address the original Electron menu. */
export interface IKernelMenuItem {
  readonly id: string
  readonly type: 'normal' | 'submenu' | 'separator' | 'checkbox' | 'radio'
  readonly labelKey?: string
  readonly labelData?: string
  readonly enabled: boolean
  readonly visible: boolean
  readonly checked?: boolean
  readonly accelerator?: string
  readonly accessKey?: string
  readonly role?: string
  readonly items?: ReadonlyArray<IKernelMenuItem>
}
export interface IKernelMenuDTO {
  readonly items: ReadonlyArray<IKernelMenuItem>
}

const normalize = (value: string) => value.replace(/&/g, '').toLowerCase()
const labelKeys = new Map(
  Object.entries(labels).map(([key, value]) => [normalize(value.en), key])
)
const nativeRoles = new Map(
  [
    'undo',
    'redo',
    'cut',
    'copy',
    'paste',
    'selectAll',
    'togglefullscreen',
    'minimize',
    'zoom',
    'close',
    'quit',
    'hide',
    'hideOthers',
    'unhide',
    'front',
    'services',
    'window',
    'help',
  ].map(role => [role.toLowerCase(), role])
)
const nativeRole = (role: string | undefined) =>
  role ? nativeRoles.get(role.toLowerCase()) : undefined

function menuLabel(item: Exclude<MenuItem, { type: 'separator' }>) {
  const text = item.label.replace(/&/g, '')
  // Existing fixed IDs disambiguate platform spelling and dynamic labels.
  if (item.id === 'about') {
    return { labelKey: 'menu.about' }
  }
  if (item.id === 'preferences') {
    return { labelKey: 'menu.preferences' }
  }
  const role = nativeRole(item.role)
  if (role) {
    return {
      labelKey: role === 'selectAll' ? 'menu.select-all' : `menu.${role}`,
    }
  }
  if (item.id === 'toggle-changes-filter') {
    return {
      labelKey: text.startsWith('Hide')
        ? 'menu.hide-filter'
        : 'menu.show-filter',
    }
  }
  if (item.id === 'open-in-shell' || item.id === 'open-external-editor') {
    const value = item.label.replace(/^(?:&?Open|O&pen) in /, '')
    return /^(shell|external editor)$/i.test(value)
      ? { labelKey: `menu.${item.id}` }
      : { labelKey: 'menu.open-in', labelData: value }
  }
  if (item.id === 'update-branch-with-contribution-target-branch') {
    const value = item.label.replace(/^&?Update from /, '')
    return /^default branch$/i.test(value)
      ? { labelKey: 'menu.update-default' }
      : { labelKey: 'menu.update-from', labelData: value }
  }
  const alias: Record<string, string> = {
    'github desktop': 'menu.app',
    'show changes': 'menu.show-changes',
    'show history': 'menu.show-history',
    'show repository list': 'menu.show-repository-list',
    'show branches list': 'menu.show-branches-list',
    'show worktrees list': 'menu.show-worktrees-list',
  }
  const labelKey = alias[normalize(text)] ?? labelKeys.get(normalize(text))
  if (!labelKey) {
    throw new Error(`Unregistered native menu label: ${item.id}`)
  }
  return { labelKey }
}

/** Uses the original tree and the original live state policy. Main-process IPC
 * can lag a repository change; re-evaluation here prevents a stale enabled
 * snapshot or an old menu click from bypassing popup/operation guards. */
export class NativeMenuPresenter {
  private readonly emitter = new Emitter()
  private readonly controller: AppMenuController

  public constructor(
    private readonly runtime: Pick<AppRuntime, 'appStore' | 'dispatcher'>
  ) {
    this.controller = new AppMenuController(
      runtime.dispatcher,
      () => runtime.appStore.getState(),
      {
        perform: action => this.emitter.emit('presentation-action', action),
        showTestUI: name => {
          const state = runtime.appStore.getState()
          return showTestUI(
            name,
            state.selectedState?.repository ?? null,
            runtime.dispatcher,
            state.emoji
          )
        },
      }
    )
  }

  public onDidPerformAction(
    callback: (action: AppMenuPresentationAction) => void
  ) {
    return this.emitter.on('presentation-action', callback)
  }

  public dispose() {
    this.emitter.dispose()
  }

  public executeRepositoryMenu(
    reference: INativeRepositoryReference,
    lease: string,
    path: ReadonlyArray<number>
  ) {
    return executeRepositoryMenu(
      this.runtime.appStore.getState(),
      this.controller,
      reference,
      lease,
      path
    )
  }

  public getState(): IKernelMenuDTO {
    const state = this.runtime.appStore.getState()
    const policy = getMenuState(state)
    const project = (
      menu: IMenu,
      parentEnabled = true,
      parentVisible = true
    ): IKernelMenuItem[] =>
      menu.items.map(item => {
        const visible = parentVisible && item.visible
        if (item.type === 'separator') {
          return { id: item.id, type: 'separator', enabled: false, visible }
        }
        const enabled =
          parentEnabled &&
          (policy.get(item.id as MenuIDs)?.enabled ?? item.enabled)
        const label = menuLabel(item)
        // These original click closures target the visible toolkit, not the
        // hidden Electron page. Their accelerators stay byte-for-byte upstream.
        const role =
          nativeRole(item.role) ??
          (
            {
              'menu.select-all': 'selectAll',
              'menu.resetZoom': 'resetZoom',
              'menu.zoomIn': 'zoomIn',
              'menu.zoomOut': 'zoomOut',
              'menu.show-devtools': 'toggleDevTools',
            } as Record<string, string>
          )[label.labelKey]
        return {
          id: item.id,
          type:
            item.type === 'menuItem'
              ? 'normal'
              : item.type === 'submenuItem'
              ? 'submenu'
              : item.type,
          ...label,
          enabled,
          visible,
          role,
          accessKey: item.accessKey ?? undefined,
          ...(item.type === 'submenuItem'
            ? { items: project(item.menu, enabled, visible) }
            : { accelerator: item.accelerator ?? undefined }),
          ...(item.type === 'checkbox' || item.type === 'radio'
            ? { checked: item.checked }
            : {}),
        }
      })
    const root = state.appMenuState[0]
    return { items: root === undefined ? [] : project(root) }
  }

  /** Only a currently visible, enabled leaf can reach the original ID-based
   * IPC path. The original click handler selects push/force-push/etc itself. */
  public async execute(id: string): Promise<void> {
    let admitted = false
    const visit = (items: ReadonlyArray<IKernelMenuItem>) => {
      for (const item of items) {
        if (
          item.id === id &&
          item.enabled &&
          item.visible &&
          item.role === undefined &&
          item.type !== 'submenu' &&
          item.type !== 'separator'
        ) {
          admitted = true
        }
        if (item.items) {
          visit(item.items)
        }
      }
    }
    visit(this.getState().items)
    if (!admitted) {
      throw new Error('Menu item is no longer available')
    }
    const find = (menu: IMenu): ExecutableMenuItem | undefined => {
      for (const item of menu.items) {
        if (item.type === 'submenuItem') {
          const found = find(item.menu)
          if (found) {
            return found
          }
        } else if (item.type !== 'separator' && item.id === id) {
          return item
        }
      }
      return undefined
    }
    const root = this.runtime.appStore.getState().appMenuState[0]
    const item = root && find(root)
    if (!item) {
      throw new Error('Menu item is no longer available')
    }
    await this.runtime.dispatcher.executeMenuItem(item)
  }

  /** Trusted Electron menu-event only, NOT a reflected native RPC method. */
  public handleEvent(name: MenuEvent) {
    return this.controller.handleEvent(name)
  }
}

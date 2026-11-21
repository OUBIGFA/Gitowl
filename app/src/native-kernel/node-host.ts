import { EventEmitter } from 'events'
import { join } from 'path'
import { homedir, tmpdir } from 'os'
import { randomUUID } from 'crypto'
import type { IDesktopHost } from '../lib/desktop-host'
import type { IMenu, MenuItem } from '../models/app-menu'
import type { MenuLabelsEvent } from '../models/menu-labels'
import type { IMenuTemplate } from '../models/menu-template'
import { buildDefaultMenuTemplate } from '../main-process/menu/build-default-menu'
import { NodeKernelTransport } from './node-transport'
import { getMainGUID, saveGUIDFile } from '../lib/get-main-guid'

/** One in-process event bus replaces main/renderer IPC. Native system calls
 * retain request/result semantics through the parent-owned pipe.
 */
export class NodeDesktopHost implements IDesktopHost {
  public readonly events = new EventEmitter()
  private template: ReadonlyArray<IMenuTemplate> = []
  private menu: IMenu = { type: 'menu', items: [] }
  private labels: MenuLabelsEvent = {} as MenuLabelsEvent
  private themeSet: Promise<unknown> = Promise.resolve()

  public constructor(
    public readonly userData: string,
    private readonly transport: NodeKernelTransport
  ) {
    this.events.setMaxListeners(100)
    for (const name of [
      'focus',
      'blur',
      'window-state-changed',
      'zoom-factor-changed',
      'native-theme-updated',
      'native-activity',
    ]) {
      transport.events.on(name, (...args) =>
        this.events.emit(name, undefined, ...args)
      )
    }
    transport.events.on('notification-event', (event, id) => {
      const stored = localStorage.getItem(`native-notification:${id}`)
      // An expired or foreign OS notification has no actionable event. Never
      // forward undefined into the original Alive event dispatcher.
      if (stored === null) {return}
      this.events.emit(
        'notification-event',
        undefined,
        event,
        id,
        JSON.parse(stored)
      )
      if (event === 'click') {localStorage.removeItem(`native-notification:${id}`)}
    })
  }

  private updateMenu(): void {
    this.template = buildDefaultMenuTemplate(this.labels)
    const project = (
      items: ReadonlyArray<IMenuTemplate>,
      id?: string
    ): IMenu => ({
      type: 'menu',
      id,
      items: items.map((item): MenuItem => {
        if (!item.id) {throw new Error('A menu definition has no stable ID')}
        if (item.type === 'separator')
          {return {
            id: item.id,
            type: 'separator',
            visible: item.visible !== false,
          }}
        const label = item.label ?? item.role ?? ''
        const base = {
          id: item.id,
          label,
          enabled: item.enabled !== false,
          visible: item.visible !== false,
          role: item.role,
          accessKey: /&([^&])/.exec(label)?.[1] ?? null,
        }
        if (item.submenu)
          {return {
            ...base,
            type: 'submenuItem',
            menu: project(item.submenu, item.id),
          }}
        const accelerator = item.accelerator ?? null
        if (item.type === 'checkbox' || item.type === 'radio')
          {return {
            ...base,
            type: item.type,
            accelerator,
            checked: item.checked ?? false,
          }}
        return { ...base, type: 'menuItem', accelerator }
      }),
    })
    this.menu = project(this.template)
    this.events.emit('app-menu', undefined, this.menu)
  }

  public send(channel: string, args: ReadonlyArray<unknown>): void {
    switch (channel) {
      case 'get-app-menu':
        this.updateMenu()
        return
      case 'update-preferred-app-menu-item-labels':
        this.labels = args[0] as MenuLabelsEvent
        this.updateMenu()
        return
      case 'update-menu-state': {
        const changes = args[0] as Array<{
          id: string
          state: { enabled?: boolean }
        }>
        let changed = false
        const update = (menu: IMenu): IMenu => ({
          ...menu,
          items: menu.items.map(item => {
            const state = changes.find(change => change.id === item.id)?.state
            let next = item
            if (
              item.type !== 'separator' &&
              state?.enabled !== undefined &&
              item.enabled !== state.enabled
            ) {
              changed = true
              next = { ...item, enabled: state.enabled }
            }
            return next.type === 'submenuItem'
              ? { ...next, menu: update(next.menu) }
              : next
          }),
        })
        const menu = update(this.menu)
        if (changed) {
          this.menu = menu
          this.events.emit('app-menu', undefined, menu)
        }
        return
      }
      case 'execute-menu-item-by-id': {
        const find = (
          items: ReadonlyArray<IMenuTemplate>
        ): IMenuTemplate | undefined => {
          for (const item of items) {
            if (item.id === args[0]) {return item}
            const child = item.submenu && find(item.submenu)
            if (child) {return child}
          }
          return undefined
        }
        const item = find(this.template)
        if (!item?.click)
          {throw new Error('The native menu action is unavailable')}
        void Promise.resolve(item.click()).catch(error =>
          log.error('Native menu action failed', error)
        )
        return
      }
      case 'set-window-zoom-factor':
        this.transport.setZoom(args[0] as number)
        return
      case 'set-native-theme-source':
        this.themeSet = this.transport.requestPlatform(channel, args)
        void this.themeSet.catch(error =>
          this.events.emit('platform-error', error)
        )
        return
      case 'install-windows-cli':
      case 'uninstall-windows-cli': {
        void import('../main-process/squirrel-updater')
          .then(module =>
            channel === 'install-windows-cli'
              ? module.installWindowsCLI()
              : module.uninstallWindowsCLI()
          )
          .catch(error => this.events.emit('platform-error', error))
        return
      }
      case 'update-window-background-color':
        return // MyGo's active theme owns native surfaces.
      case 'quit-app':
        this.transport.action('close')
        return
      case 'minimize-window':
        this.transport.action('minimize')
        return
      case 'maximize-window':
        this.transport.action('maximize')
        return
      case 'unmaximize-window':
        this.transport.action('restore')
        return
      case 'close-window':
        this.transport.action('close')
        return
      case 'focus-window':
        this.transport.action('focus')
        return
      case 'select-all-window-contents':
        this.transport.presentation('select-all')
        return
      case 'log': {
        const level = args[0] as keyof IDesktopLogger
        log[level](String(args[1]))
        return
      }
      case 'uncaught-exception':
        throw args[0]
      case 'send-error-report':
        log.error('Application error', args[0] as Error)
        return
      // These facts have no second process to synchronize. Their state owner
      // is already AppStore; no user/OS action is acknowledged by these events.
      case 'renderer-ready':
      case 'update-accounts':
      case 'dialog-did-open':
      case 'will-quit':
      case 'will-quit-even-if-updating':
      case 'cancel-quitting':
        return
      default:
        void this.transport.requestPlatform(channel, args).catch(error => {
          log.error(`Native platform action failed: ${channel}`, error)
          this.events.emit('platform-error', error)
        })
    }
  }

  public async invoke(
    channel: string,
    args: ReadonlyArray<unknown>
  ): Promise<unknown> {
    switch (channel) {
      case 'get-path': {
        const name = args[0]
        if (name === 'userData' || name === 'sessionData') {return this.userData}
        if (name === 'logs') {return join(this.userData, 'logs')}
        if (name === 'home') {return homedir()}
        if (name === 'temp') {return tmpdir()}
        if (name === 'exe')
          {return process.env.GITOWL_NATIVE_EXECUTABLE ?? process.execPath}
        return this.transport.requestPlatform(channel, args)
      }
      case 'get-app-architecture':
        return process.arch
      case 'get-app-path':
        return __dirname
      case 'get-exec-path':
        return process.env.GITOWL_NATIVE_EXECUTABLE ?? process.execPath
      case 'is-running-under-arm64-translation':
        return false
      case 'get-current-window-state':
        return this.transport.getState().state
      case 'get-current-window-zoom-factor':
        return this.transport.getState().zoom
      case 'is-window-focused':
        return this.transport.getState().focused
      case 'is-window-maximized':
        return this.transport.getState().state === 'maximized'
      case 'should-use-dark-colors':
        await this.themeSet
        return this.transport.requestPlatform(channel, args)
      case 'is-in-application-folder':
        return null
      case 'get-guid':
        return getMainGUID()
      case 'show-notification': {
        // MyGo preserves caller IDs across process restarts. Store the original
        // event before delivery so a quick click or restart cannot lose it.
        const id = randomUUID()
        const key = `native-notification:${id}`
        const indexKey = 'native-notification-ids'
        const ids: string[] = JSON.parse(localStorage.getItem(indexKey) ?? '[]')
        ids.push(id)
        // Match the original callback cache's bound; the OS may retain old
        // notification IDs, but it must not grow the profile without limit.
        for (const expired of ids.splice(0, Math.max(0, ids.length - 200))) {
          localStorage.removeItem(`native-notification:${expired}`)
        }
        localStorage.setItem(key, JSON.stringify(args[2] ?? null))
        localStorage.setItem(indexKey, JSON.stringify(ids))
        try {
          return await this.transport.requestPlatform(channel, [...args, id])
        } catch (error) {
          localStorage.removeItem(key)
          throw error
        }
      }
      case 'save-guid':
        return saveGUIDFile(args[0] as string)
      case 'open-external':
        return this.transport.openExternal(args[0] as string)
      case 'show-open-dialog':
        return (
          (
            await this.transport.showFileDialog(
              'open',
              args[0] as Electron.OpenDialogOptions
            )
          )[0] ?? null
        )
      case 'show-save-dialog':
        return (
          (
            await this.transport.showFileDialog(
              'save',
              args[0] as Electron.SaveDialogOptions
            )
          )[0] ?? null
        )
      case 'check-for-updates':
        throw new Error(
          'Automatic updates are not configured for this installation'
        )
      default:
        return this.transport.requestPlatform(channel, args)
    }
  }
}

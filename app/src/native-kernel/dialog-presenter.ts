import { Disposable } from 'event-kit'
import type { IAppState } from '../lib/app-state'
import type { Popup, PopupType } from '../models/popup'
import type { AppRuntime } from '../ui/app-runtime'
import { auxiliaryDialogs, mountAuxiliaryDialog } from './auxiliary-dialogs'
import { operationDialogs, mountOperationDialog } from './operation-dialogs'
import { repositoryDialogs, mountRepositoryDialog } from './repository-dialogs'
import { settingsDialogs, mountSettingsDialog } from './settings-dialogs'
import type {
  NativeDialogModule,
  NativeDialogPresentation,
  NativeDialogReply,
} from './dialog-module'
import { isRecord } from '../lib/is-record'

const modules: ReadonlyArray<NativeDialogModule> = [
  auxiliaryDialogs,
  operationDialogs,
  repositoryDialogs,
  settingsDialogs,
]

/** One explicit owner for each original PopupType; never Dispatcher reflection. */
const owners = new Map<PopupType, NativeDialogModule>()
for (const module of modules) {
  for (const type of module.types) {
    if (owners.has(type)) throw new Error('Duplicate native dialog partition')
    owners.set(type, module)
  }
}
export const nativeDialogTypes: ReadonlyArray<PopupType> = [...owners.keys()]

export function isNativeDialog(type: PopupType): boolean {
  return owners.has(type)
}

export function projectNativeDialog(
  popup: Popup,
  state: IAppState
): NativeDialogPresentation | null {
  return owners.get(popup.type)?.project(popup, state) ?? null
}

/** Validate only the wire envelope here. The owner validates its actual fields. */
export function parseNativeDialogReply(
  popup: Popup,
  value: unknown
): NativeDialogReply {
  if (
    !isRecord(value) ||
    value.popupID !== popup.id ||
    value.type !== popup.type ||
    typeof value.action !== 'string' ||
    Object.keys(value).some(
      key => !['popupID', 'type', 'action', 'values'].includes(key)
    )
  ) {
    throw new Error('Invalid native dialog reply')
  }
  return value as unknown as NativeDialogReply
}

export async function replyNativeDialog(
  runtime: AppRuntime,
  popup: Popup,
  reply: NativeDialogReply
): Promise<void> {
  const owner = owners.get(popup.type)
  if (owner === undefined) throw new Error('Unsupported native dialog')
  await owner.reply(runtime, popup, reply.action, reply.values)
}

export function cancelNativeDialog(runtime: AppRuntime, popup: Popup) {
  const owner = owners.get(popup.type)
  if (owner === undefined) throw new Error('Unsupported native dialog')
  return owner.cancel(runtime, popup)
}

/**
 * Match DialogStackContext lifetime: covered popups keep subscriptions, removed
 * popups dispose. A view's asynchronous load only asks for another whitelisted
 * projection; no second popup stack or core state lives in this presenter.
 */
export class NativeDialogLifecycle {
  private readonly mounted = new Map<Popup, Disposable>()
  private pendingChange = false
  private disposed = false

  public constructor(
    private readonly runtime: AppRuntime,
    private readonly onChange: () => void
  ) {}

  private changed = () => {
    // A mount can synchronously emit (e.g. update status). Defer until every
    // owner is recorded, and coalesce progress chunks without re-entrant sync.
    if (this.disposed || this.pendingChange) return
    this.pendingChange = true
    queueMicrotask(() => {
      this.pendingChange = false
      if (!this.disposed) this.onChange()
    })
  }

  public sync(state: IAppState) {
    if (this.disposed) return
    const present = new Set(state.allPopups)
    for (const [popup, subscription] of this.mounted) {
      if (!present.has(popup)) {
        subscription.dispose()
        this.mounted.delete(popup)
      }
    }
    for (const popup of present) {
      const owner = owners.get(popup.type)
      if (owner === undefined || this.mounted.has(popup)) continue
      const mount =
        owner === auxiliaryDialogs
          ? mountAuxiliaryDialog
          : owner === operationDialogs
          ? mountOperationDialog
          : owner === repositoryDialogs
          ? mountRepositoryDialog
          : mountSettingsDialog
      this.mounted.set(popup, mount(this.runtime, popup, this.changed))
    }
  }

  public dispose() {
    this.disposed = true
    for (const subscription of this.mounted.values()) subscription.dispose()
    this.mounted.clear()
  }
}

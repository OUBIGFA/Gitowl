import type { INativeKernelSnapshot } from './protocol'
import type { IKernelWorkspaceDTO } from './workspace-projection'

/** Progress and store callbacks can fire many times in one native frame.
 * Build the latest pushed snapshot once per frame, independent of Chromium's
 * hidden-window animation timer. Command replies remain immediate and complete.
 */
export class SnapshotScheduler {
  private timer: ReturnType<typeof setTimeout> | undefined
  private disposed = false

  public constructor(private readonly push: () => void) {}

  public schedule(): void {
    if (this.disposed || this.timer !== undefined) {
      return
    }
    this.timer = setTimeout(() => {
      this.timer = undefined
      if (!this.disposed) {
        this.push()
      }
    }, 16)
  }

  public dispose(): void {
    this.disposed = true
    clearTimeout(this.timer)
  }
}

type Selected = NonNullable<INativeKernelSnapshot['selected']>
type Loose<T, K extends keyof T> = Omit<T, K> & Partial<Pick<T, K>>

type WorkspaceSection = 'menus' | 'visibleFileIDs'
type SelectedSection = 'files' | 'history' | 'historyFiles' | 'diff'

/**
 * The heavy parts of a snapshot. Pre-generated context menus, loaded history
 * and file lists are about 95% of its bytes, yet most updates (operation
 * progress, a draft, a selection) leave them untouched.
 */
export type KernelSnapshotSection =
  | 'menu'
  | `workspace.${WorkspaceSection}`
  | `selected.${SelectedSection}`

/**
 * A pushed snapshot. A section named in `unchanged.sections` is absent and
 * equals that section of the previous push, whose revision is
 * `unchanged.base`. Command responses always carry the complete snapshot: the
 * native side decodes them on the calling goroutine, outside push order.
 */
export type NativeKernelStatePush = Omit<
  INativeKernelSnapshot,
  'workspace' | 'selected'
> & {
  readonly workspace?: Loose<IKernelWorkspaceDTO, WorkspaceSection>
  readonly selected: Loose<Selected, SelectedSection> | null
  readonly unchanged?: {
    readonly base: number
    readonly sections: ReadonlyArray<KernelSnapshotSection>
  }
}

const workspaceSections: ReadonlyArray<WorkspaceSection> = [
  'menus',
  'visibleFileIDs',
]
const selectedSections: ReadonlyArray<SelectedSection> = [
  'files',
  'history',
  'historyFiles',
  'diff',
]

/**
 * `dateRefreshAfter` is the kernel's own timer hint, recomputed against the
 * clock by every projection; the native side never reads it. Compared as
 * content it would make loaded history look changed on every push.
 */
function withoutRefreshHint(key: string, value: unknown) {
  return key === 'dateRefreshAfter' ? undefined : value
}

/**
 * Omits the sections of each pushed snapshot that the previous push already
 * delivered. One encoder owns one ordered push stream; every snapshot given
 * to `encode` must be sent, in that order.
 */
export class SnapshotPushEncoder {
  private base = 0
  /** Serialized form of each section as the receiver last saw it. */
  private readonly sent = new Map<KernelSnapshotSection, string>()

  public encode(snapshot: INativeKernelSnapshot): NativeKernelStatePush {
    const unchanged: KernelSnapshotSection[] = []
    // Projection rebuilds these arrays on every update, so identity cannot
    // tell an unchanged section. Its serialized text is the wire content.
    const repeats = (name: KernelSnapshotSection, value: unknown) => {
      const text =
        value === undefined
          ? undefined
          : JSON.stringify(
              value,
              name === 'selected.history' ? withoutRefreshHint : undefined
            )
      if (text !== undefined && text === this.sent.get(name)) {
        unchanged.push(name)
        return true
      }
      if (text === undefined) {
        this.sent.delete(name)
      } else {
        this.sent.set(name, text)
      }
      return false
    }

    let { menu, workspace, selected }: NativeKernelStatePush = snapshot
    if (repeats('menu', menu)) {
      menu = undefined
    }
    for (const key of workspaceSections) {
      if (repeats(`workspace.${key}`, snapshot.workspace?.[key])) {
        workspace = { ...workspace!, [key]: undefined }
      }
    }
    for (const key of selectedSections) {
      if (repeats(`selected.${key}`, snapshot.selected?.[key])) {
        selected = { ...selected!, [key]: undefined }
      }
    }

    const base = this.base
    this.base = snapshot.revision
    if (unchanged.length === 0) {
      return snapshot
    }
    // The transport's JSON encoding drops the undefined members.
    return {
      ...snapshot,
      menu,
      workspace,
      selected,
      unchanged: { base, sections: unchanged },
    }
  }
}

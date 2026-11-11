import type { KernelImage } from './image-presentation'
import type { KernelDiff } from './protocol'
import type { IAppState } from '../lib/app-state'
import type { AppRuntime } from '../ui/app-runtime'
import type { Popup, PopupType } from '../models/popup'

/** UI primitives rendered by MyGo, not HTML, raw React trees or Dispatcher reflection. */
export type NativeDialogField = {
  readonly id: string
  readonly labelKey: string
  readonly kind:
    | 'text'
    | 'multiline'
    | 'checkbox'
    | 'select'
    | 'readonly'
    | 'list'
    | 'progress'
    | 'icons'
    | 'image'
    | 'diff'
  readonly diff?: KernelDiff
  readonly diffPath?: string
  readonly sideBySide?: boolean
  readonly imageDiffType?: number
  readonly iconLibrary?: 'octicons'
  readonly image?: KernelImage
  readonly value?: string | boolean | number
  readonly text?: string
  readonly placeholder?: string
  readonly choices?: ReadonlyArray<{
    readonly id: string
    readonly label: string
    readonly disabled?: boolean
  }>
  readonly required?: boolean
  readonly readOnly?: boolean
  readonly disabled?: boolean
  readonly sensitive?: boolean
  readonly height?: number
  /** Explicit original form-change action; absent means edit locally until submit. */
  readonly changeAction?: string
}

export interface NativeDialogAction {
  readonly id: string
  readonly labelKey: string
  readonly primary?: boolean
  readonly destructive?: boolean
  readonly disabled?: boolean
  /** Exact field IDs accepted for this action. Absent means no form values. */
  readonly fields?: ReadonlyArray<string>
}

/** Text keys are translated by the native catalog; technical content remains original data. */
export interface NativeDialogPresentation {
  readonly popupID: number | null
  readonly type: PopupType
  readonly supported: true
  readonly layout: 'form'
  readonly titleKey: string
  readonly descriptionKey?: string
  readonly description?: string
  readonly fields: ReadonlyArray<NativeDialogField>
  readonly actions: ReadonlyArray<NativeDialogAction>
  /** Safe complete page views let native navigate without a draft round trip. */
  readonly layoutPages?: ReadonlyArray<{
    readonly id: string
    readonly fields: ReadonlyArray<NativeDialogField>
    readonly actions: ReadonlyArray<NativeDialogAction>
  }>
  readonly error?: string | null
  readonly width?: number
  readonly height?: number
}

export interface NativeDialogReply {
  readonly popupID: number
  readonly type: PopupType
  readonly action: string
  readonly values?: Readonly<Record<string, unknown>>
}

/**
 * Feature partitions own their view-field validation and explicit continuations.
 * All repository/account/Git objects are resolved from the current original
 * Popup/IAppState. A partition may extract a shared upstream UI controller but
 * never introduce a reduced backend, reflect arbitrary methods, or serialize
 * callbacks/tokens/raw state. The parent validates the opaque current popup ID.
 */
export interface NativeDialogModule {
  readonly types: ReadonlyArray<PopupType>
  project(popup: Popup, state: IAppState): NativeDialogPresentation
  reply(
    runtime: AppRuntime,
    popup: Popup,
    action: string,
    values: unknown
  ): Promise<void>
  cancel(runtime: AppRuntime, popup: Popup): void | Promise<void>
}

export function dialogValues(
  value: unknown,
  allowed: ReadonlyArray<string>
): Readonly<Record<string, unknown>> {
  if (
    typeof value !== 'object' ||
    value === null ||
    Array.isArray(value) ||
    Object.keys(value).some(key => !allowed.includes(key))
  ) {
    throw new Error('Invalid native dialog fields')
  }
  return value as Record<string, unknown>
}

export function dialogString(value: unknown): string {
  if (typeof value !== 'string' || value.includes('\0')) {
    throw new Error('Invalid native dialog text')
  }
  return value
}

export function dialogBoolean(value: unknown): boolean {
  if (typeof value !== 'boolean') {
    throw new Error('Invalid native dialog choice')
  }
  return value
}

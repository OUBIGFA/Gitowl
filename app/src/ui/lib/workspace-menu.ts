import type { IMenuItem } from '../../lib/menu-item'

/** Non-visual menu metadata shared by React and native presentation. Data is
 * restricted to user/Git names; ordinary labels are translated by semantic key. */
export interface IWorkspaceMenuItem extends IMenuItem {
  readonly labelKey?: string
  readonly labelData?: ReadonlyArray<string>
  readonly submenu?: ReadonlyArray<this>
}

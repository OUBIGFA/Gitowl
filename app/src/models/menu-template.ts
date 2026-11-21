/** Toolkit-independent input to the existing application menu definition. */
export interface IMenuTemplate {
  id?: string
  label?: string
  type?: 'normal' | 'separator' | 'submenu' | 'checkbox' | 'radio'
  role?: Electron.MenuItemConstructorOptions['role']
  accelerator?: string
  enabled?: boolean
  visible?: boolean
  checked?: boolean
  submenu?: IMenuTemplate[]
  click?: (...args: any[]) => void | Promise<void>
}

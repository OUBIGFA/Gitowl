import { Menu } from 'electron'
import { MenuLabelsEvent } from '../../models/menu-labels'
import { buildDefaultMenuTemplate } from './build-default-menu'

/** Upstream comparison-only adapter. The production Node build imports the
 * shared menu definition directly and never imports this Electron module.
 */
export function buildDefaultMenu(params: MenuLabelsEvent): Electron.Menu {
  return Menu.buildFromTemplate(
    buildDefaultMenuTemplate(params) as Electron.MenuItemConstructorOptions[]
  )
}

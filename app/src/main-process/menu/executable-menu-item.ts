/** Menu updates and clicks can cross in flight. Validate the whole live path,
 * not just the leaf, before calling the original click closure. */
export function executableMenuItem(
  menu: Electron.Menu,
  id: string
): Electron.MenuItem | undefined {
  for (const item of menu.items) {
    if (!item.visible || !item.enabled || item.type === 'separator') {
      continue
    }
    if (item.submenu) {
      const found = executableMenuItem(item.submenu, id)
      if (found) {
        return found
      }
    } else if (item.id === id) {
      return item
    }
  }
  return undefined
}

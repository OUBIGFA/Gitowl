import * as Path from 'path'
import { getDesktopHost } from '../desktop-host'

let logDirectoryPath: string | null = null

export function getLogDirectoryPath() {
  if (!logDirectoryPath) {
    const userData = getDesktopHost().userData
    logDirectoryPath = Path.join(userData, 'logs')
  }

  return logDirectoryPath
}

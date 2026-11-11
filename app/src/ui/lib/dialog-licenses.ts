import * as Path from 'path'
import * as Fs from 'fs'
import { getAppPath } from './app-proxy'

export interface IDialogLicense {
  readonly repository?: string
  readonly sourceText?: string
  readonly license?: string
}
export type DialogLicenses = { [key: string]: IDialogLicense }

/** Read the same packaged license asset as the upstream Acknowledgements UI. */
export async function loadDialogLicenses(
  update: (licenses: DialogLicenses) => void
) {
  const path = Path.join(await getAppPath(), 'static', 'licenses.json')
  Fs.readFile(path, 'utf8', (err, data) => {
    if (err) {
      log.error('Error loading licenses', err)
      return
    }
    const parsed = JSON.parse(data)
    if (!parsed) {
      log.warn(`Couldn't parse licenses!`)
      return
    }
    update(parsed)
  })
}

/** Original normalization, not an arbitrary URL supplied by the native client. */
export function normalizedLicenseURL(url: string) {
  return url
    .replace('git+https://github.com', 'https://github.com')
    .replace('git+ssh://git@github.com', 'https://github.com')
}

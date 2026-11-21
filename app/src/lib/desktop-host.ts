import type { EventEmitter } from 'events'

/** The actual presentation host owns OS operations. Business code depends on
 * this transport contract, never on a hidden browser or an Electron facade.
 */
export interface IDesktopHost {
  readonly userData: string
  readonly events: EventEmitter
  invoke(channel: string, args: ReadonlyArray<unknown>): Promise<unknown>
  send(channel: string, args: ReadonlyArray<unknown>): void
}

export type DesktopHostEvent = unknown
let current: IDesktopHost | undefined

export function installDesktopHost(host: IDesktopHost): void {
  if (current !== undefined) {
    throw new Error('The desktop host is already installed')
  }
  current = host
}

export function getDesktopHost(): IDesktopHost {
  if (current === undefined) {
    throw new Error(
      'The desktop host must be installed before business startup'
    )
  }
  return current
}

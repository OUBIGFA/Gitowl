import { RequestResponseChannels, RequestChannels } from './ipc-shared'
import { getDesktopHost, DesktopHostEvent } from './desktop-host'

/**
 * Send a message to the main process via channel and expect a result
 * asynchronously. This is the equivalent of ipcRenderer.invoke except with
 * strong typing guarantees.
 */
export function invoke<T extends keyof RequestResponseChannels>(
  channel: T,
  ...args: Parameters<RequestResponseChannels[T]>
): ReturnType<RequestResponseChannels[T]> {
  return getDesktopHost().invoke(channel, args) as any
}

/**
 * Send a message to the main process via channel asynchronously. This is the
 * equivalent of ipcRenderer.send except with strong typing guarantees.
 */
export function send<T extends keyof RequestChannels>(
  channel: T,
  ...args: Parameters<RequestChannels[T]>
): void {
  return getDesktopHost().send(channel, args)
}

/**
 * Send a message to the main process via channel synchronously. This is the
 * equivalent of ipcRenderer.sendSync except with strong typing guarantees.
 */
export function sendSync<T extends keyof RequestChannels>(
  channel: T,
  ...args: Parameters<RequestChannels[T]>
): void {
  // The single backend handles lifecycle signals in-process. No synchronous
  // cross-process request blocks Git while the presenter is waiting for it.
  return getDesktopHost().send(channel, args)
}

/**
 * Subscribes to the specified IPC channel and provides strong typing of
 * the channel name, and request parameters. This is the equivalent of
 * using ipcRenderer.on.
 */
export function on<T extends keyof RequestChannels>(
  channel: T,
  listener: (
    event: DesktopHostEvent,
    ...args: Parameters<RequestChannels[T]>
  ) => void
) {
  getDesktopHost().events.on(channel, listener)
}

/**
 * Subscribes to the specified IPC channel and provides strong typing of
 * the channel name, and request parameters. This is the equivalent of
 * using ipcRenderer.once
 */
export function once<T extends keyof RequestChannels>(
  channel: T,
  listener: (
    event: DesktopHostEvent,
    ...args: Parameters<RequestChannels[T]>
  ) => void
) {
  getDesktopHost().events.once(channel, listener)
}

/**
 * Unsubscribes from the specified IPC channel and provides strong typing of
 * the channel name, and request parameters. This is the equivalent of
 * using ipcRenderer.removeListener
 */
export function removeListener<T extends keyof RequestChannels>(
  channel: T,
  listener: (
    event: DesktopHostEvent,
    ...args: Parameters<RequestChannels[T]>
  ) => void
) {
  getDesktopHost().events.removeListener(channel, listener)
}

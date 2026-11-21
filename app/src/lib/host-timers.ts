/** Use the owning environment's real timers. Browser comparison/tests retain
 * window teardown; the production Node backend needs no window global.
 */
export type HostTimer = ReturnType<typeof globalThis.setTimeout> | number

export function scheduleTimeout(
  callback: (...args: any[]) => void,
  delay?: number,
  ...args: any[]
): HostTimer {
  return typeof window === 'undefined'
    ? globalThis.setTimeout(callback, delay, ...args)
    : window.setTimeout(callback, delay, ...args)
}

export function scheduleInterval(
  callback: (...args: any[]) => void,
  delay?: number,
  ...args: any[]
): HostTimer {
  return typeof window === 'undefined'
    ? globalThis.setInterval(callback, delay, ...args)
    : window.setInterval(callback, delay, ...args)
}

export function cancelTimer(timer: HostTimer | undefined | null): void {
  if (timer == null) {return}
  if (typeof timer === 'number' && typeof window !== 'undefined')
    {window.clearTimeout(timer)}
  else {globalThis.clearTimeout(timer)}
}

/// <reference lib="es2025.float16" />

/** IndexedDBShim's published types include the newer standard DOM exception;
 * the locked TypeScript DOM library has not added its name yet. Types only.
 */
// The name is the standard web platform interface, not an application model.
// eslint-disable-next-line @typescript-eslint/naming-convention
interface QuotaExceededError extends DOMException {
  readonly quota?: number
  readonly requested?: number
}

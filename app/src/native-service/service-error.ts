/** Stable transport errors are independent of any repository or provider state. */
export class ServiceError extends Error {
  public constructor(public readonly code: number, message: string) {
    super(message)
  }
}

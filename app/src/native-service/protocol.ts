import { Readable, Writable } from 'stream'
import { createInterface } from 'readline'
import { isRecord } from '../lib/is-record'
import { ServiceError } from './service-error'

/** Local RPC services receive one cancellation scope per submitted request. */
export interface NativeRPCService {
  invoke(
    method: string,
    params: unknown,
    signal?: AbortSignal
  ): Promise<unknown>
  close?(): Promise<void>
}

const failure = (id: number | null, code: number, message: string) => ({
  jsonrpc: '2.0',
  id,
  error: { code, message },
})

function isRequest(
  value: unknown
): value is Record<string, unknown> & { id: number; method: string } {
  return (
    isRecord(value) &&
    value.jsonrpc === '2.0' &&
    typeof value.id === 'number' &&
    Number.isSafeInteger(value.id) &&
    value.id > 0 &&
    typeof value.method === 'string'
  )
}

/** Validate once at the process boundary; no arbitrary command endpoint exists. */
export async function handleRequest(
  service: NativeRPCService,
  request: unknown,
  signal?: AbortSignal
) {
  if (!isRequest(request)) {
    return failure(null, -32600, 'Invalid request')
  }
  try {
    const result = await service.invoke(
      request.method,
      request.params ?? {},
      signal
    )
    return { jsonrpc: '2.0', id: request.id, result }
  } catch (error) {
    return failure(
      request.id,
      signal?.aborted
        ? -32800
        : error instanceof ServiceError
        ? error.code
        : -32000,
      signal?.aborted
        ? 'Request cancelled'
        : error instanceof Error
        ? error.message
        : 'Operation failed'
    )
  }
}

/**
 * Requests may settle out of order. A slow model request must not prevent its
 * cancellation or an authentication continuation from reaching the service.
 * Git mutation ordering remains owned by the upstream repository operation lock.
 */
export async function serve(
  input: Readable,
  output: Writable,
  service: NativeRPCService
) {
  const active = new Map<number, AbortController>()
  const pending = new Set<Promise<void>>()
  const lines = createInterface({ input, crlfDelay: Infinity })
  let writeError: Error | undefined
  const abortAll = () => {
    for (const controller of active.values()) {
      controller.abort()
    }
  }
  const onWriteError = (error: Error) => {
    writeError = error
    abortAll()
    lines.close()
  }
  output.on('error', onWriteError)
  const send = (response: unknown) =>
    new Promise<void>((resolve, reject) => {
      if (writeError) {
        reject(writeError)
        return
      }
      output.write(JSON.stringify(response) + '\n', error =>
        error ? reject(error) : resolve()
      )
    })
  try {
    for await (const line of lines) {
      let request: unknown
      if (Buffer.byteLength(line) > 8 * 1024 * 1024) {
        await send(failure(null, -32600, 'Request exceeds 8 MiB'))
        continue
      }
      try {
        request = JSON.parse(line)
      } catch {
        await send(failure(null, -32700, 'Invalid JSON'))
        continue
      }
      // This notification cancels only the matching request, never another
      // repository or provider operation sharing the same owned pipe.
      if (
        isRecord(request) &&
        request.jsonrpc === '2.0' &&
        request.method === '$/cancelRequest' &&
        request.id === undefined
      ) {
        if (isRecord(request.params) && typeof request.params.id === 'number') {
          active.get(request.params.id)?.abort()
        }
        continue
      }
      if (!isRequest(request)) {
        await send(failure(null, -32600, 'Invalid request'))
        continue
      }
      if (active.has(request.id)) {
        throw new Error('Duplicate active request ID')
      }
      const controller = new AbortController()
      const id = request.id
      active.set(id, controller)
      const task = handleRequest(service, request, controller.signal)
        .then(send)
        .catch((error: Error) => onWriteError(error))
        .finally(() => {
          active.delete(id)
          pending.delete(task)
        })
      pending.add(task)
    }
  } finally {
    lines.close()
    // The native parent owns this pipe. EOF means it has gone away; do not
    // leave generation requests or login flows running without their owner.
    abortAll()
    await Promise.allSettled(pending)
    await service.close?.()
    output.removeListener('error', onWriteError)
  }
  if (writeError) {
    throw writeError
  }
}

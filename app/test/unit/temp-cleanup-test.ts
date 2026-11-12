import assert from 'node:assert/strict'
import { before, it, mock, TestContext } from 'node:test'

let failures = 0
let calls = 0
let failureCode = 'EBUSY'
mock.module('fs/promises', {
  namedExports: {
    mkdtemp: async () => 'virtual-fixture-no-files-created',
    rm: async () => {
      calls++
      if (failures-- > 0) {
        throw Object.assign(new Error('fixture cleanup failure'), {
          code: failureCode,
          syscall: 'rm',
        })
      }
    },
  },
})
let createTempDirectory: typeof import('../helpers/temp').createTempDirectory
before(async () => {
  ;({ createTempDirectory } = await import('../helpers/temp'))
})

it(
  'finishes busy fixture cleanup while UI timers are suspended',
  { timeout: 1000 },
  async t => {
    t.mock.method(window, 'setTimeout', () => 0)
    failures = 1
    calls = 0
    let cleanup: (() => Promise<void>) | undefined
    await createTempDirectory({
      after: (fn: () => Promise<void>) => {
        cleanup = fn
      },
    } as unknown as TestContext)
    assert.ok(cleanup)
    await cleanup()
    assert.equal(calls, 2)
  }
)

it(
  'preserves cleanup errors and bounds busy retries',
  { timeout: 1000 },
  async () => {
    for (const [code, expectedCalls] of [
      ['EACCES', 1],
      ['EBUSY', 7],
    ] as const) {
      failureCode = code
      failures = 100
      calls = 0
      let cleanup: (() => Promise<void>) | undefined
      await createTempDirectory({
        after: (fn: () => Promise<void>) => {
          cleanup = fn
        },
      } as unknown as TestContext)
      assert.ok(cleanup)
      await assert.rejects(cleanup(), { code })
      assert.equal(calls, expectedCalls)
    }
  }
)

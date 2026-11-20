import assert from 'node:assert/strict'
import { it } from 'node:test'
import { SnapshotScheduler } from '../../src/native-kernel/snapshot-sections'

it('coalesces state bursts into one fresh snapshot and releases pending work on close', t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let state = 0
  const delivered: number[] = []
  const scheduler = new SnapshotScheduler(() => delivered.push(state))
  for (; state < 100; state++) {
    scheduler.schedule()
  }
  assert.deepEqual(delivered, [])
  t.mock.timers.tick(16)
  assert.deepEqual(delivered, [100])
  state = 101
  scheduler.schedule()
  t.mock.timers.tick(16)
  assert.deepEqual(delivered, [100, 101])
  scheduler.schedule()
  scheduler.dispose()
  scheduler.schedule()
  t.mock.timers.tick(100)
  assert.deepEqual(delivered, [100, 101])
})

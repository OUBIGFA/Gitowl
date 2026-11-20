import assert from 'node:assert/strict'
import { it } from 'node:test'
import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { Repository } from '../../src/models/repository'
import { NativeRepositoryWatcher } from '../../src/native-kernel/repository-watcher'
import { createTempDirectory } from '../helpers/temp'

async function eventually(condition: () => boolean) {
  for (let n = 0; n < 150 && !condition(); n++) {
    await delay(20)
  }
  assert.equal(condition(), true)
}

it('refreshes changes promptly, coalesces a burst, stays idle and closes its watchers', async t => {
  const path = await createTempDirectory(t)
  await mkdir(join(path, '.git'))
  const repository = new Repository(path, 1, null, false)
  const refreshes: string[] = []
  const errors: Error[] = []
  const watcher = new NativeRepositoryWatcher(
    () => [repository],
    () => repository,
    async (_repository, metadata) => {
      refreshes.push(metadata ? 'metadata' : 'files')
    },
    () => false,
    error => {
      errors.push(error)
    }
  )
  t.after(() => watcher.dispose())
  watcher.start()
  await eventually(() => refreshes.length === 1)
  for (let n = 0; n < 20; n++) {
    await writeFile(join(path, `file-${n}.txt`), 'changed')
  }
  await eventually(() => refreshes.length === 2)
  await delay(700)
  assert.deepEqual(refreshes, ['metadata', 'files'])
  await writeFile(join(path, '.git', 'HEAD'), 'ref: refs/heads/next\n')
  await eventually(() => refreshes.length === 3)
  assert.equal(refreshes[2], 'metadata')
  await delay(5100)
  assert.equal(refreshes.length, 3, 'an unchanged project was polled again')
  watcher.dispose()
  await writeFile(join(path, 'after-close.txt'), 'changed')
  await delay(400)
  assert.equal(refreshes.length, 3)
  assert.deepEqual(errors, [])
})

it('keeps changes made during a scan and defers while a repository is busy', async t => {
  const path = await createTempDirectory(t)
  await mkdir(join(path, '.git'))
  const repository = new Repository(path, 1, null, false)
  let busy = true
  let reads = 0
  let release!: () => void
  const held = new Promise<void>(resolve => {
    release = resolve
  })
  const watcher = new NativeRepositoryWatcher(
    () => [repository],
    () => repository,
    async () => {
      if (++reads === 1) {
        await held
      }
    },
    () => busy,
    error => {
      throw error
    }
  )
  t.after(() => {
    release()
    watcher.dispose()
  })
  watcher.start()
  await delay(500)
  assert.equal(reads, 0)
  busy = false
  await eventually(() => reads === 1)
  await writeFile(join(path, 'during.txt'), 'changed')
  await delay(350)
  release()
  await eventually(() => reads === 2)
})

it('observes linked worktree metadata and stops observing removed projects', async t => {
  const path = await createTempDirectory(t)
  const gitDir = await createTempDirectory(t)
  const commonDir = await createTempDirectory(t)
  await writeFile(join(path, '.git'), `gitdir: ${gitDir}\n`)
  await writeFile(join(gitDir, 'commondir'), `${commonDir}\n`)
  await mkdir(join(commonDir, 'refs'))
  const repository = new Repository(
    path,
    1,
    null,
    false,
    null,
    {},
    false,
    gitDir
  )
  let repositories = [repository]
  let reads = 0
  const errors: Error[] = []
  const watcher = new NativeRepositoryWatcher(
    () => repositories,
    () => repository,
    async () => {
      reads++
    },
    () => false,
    error => {
      errors.push(error)
    }
  )
  t.after(() => watcher.dispose())
  watcher.start()
  await eventually(() => reads === 1)
  await writeFile(join(commonDir, 'refs', 'new'), 'sha\n')
  await eventually(() => reads === 2)
  await writeFile(join(gitDir, 'HEAD'), 'ref: refs/heads/new\n')
  await eventually(() => reads === 3)
  repositories = []
  watcher.updateRepositories()
  await writeFile(join(path, 'removed.txt'), 'changed')
  await delay(400)
  assert.equal(reads, 3)
  assert.deepEqual(errors, [])
})

it('reports unavailable watching and retries during periodic reconciliation', async t => {
  const parent = await createTempDirectory(t)
  const path = join(parent, 'not-yet-created')
  const repository = new Repository(path, 1, null, false)
  let reads = 0
  let reconcile!: () => void
  t.mock.method(globalThis, 'setInterval', (callback: () => void) => {
    reconcile = callback
    return { unref() {} } as NodeJS.Timeout
  })
  t.mock.method(globalThis, 'clearInterval', () => {})
  const errors: Error[] = []
  const watcher = new NativeRepositoryWatcher(
    () => [repository],
    () => undefined,
    async () => {
      reads++
    },
    () => false,
    error => {
      errors.push(error)
    }
  )
  t.after(() => watcher.dispose())
  watcher.start()
  await eventually(() => reads === 1)
  assert.equal(errors.length, 1)
  await mkdir(join(path, '.git'), { recursive: true })
  reconcile()
  await eventually(() => reads === 2)
  await writeFile(join(path, 'returned.txt'), 'changed')
  await eventually(() => reads === 3)
  assert.equal(errors.length, 1, 'watching did not recover')
})

it('keeps minute reconciliation local and periodically reconciles full metadata', async t => {
  const path = await createTempDirectory(t)
  await mkdir(join(path, '.git'))
  const repository = new Repository(path, 1, null, false)
  let reconcile!: () => void
  t.mock.method(globalThis, 'setInterval', (callback: () => void) => {
    reconcile = callback
    return { unref() {} } as NodeJS.Timeout
  })
  t.mock.method(globalThis, 'clearInterval', () => {})
  const reads: boolean[] = []
  const watcher = new NativeRepositoryWatcher(
    () => [repository],
    () => repository,
    async (_repository, metadata) => {
      reads.push(metadata)
    },
    () => false,
    error => {
      throw error
    }
  )
  t.after(() => watcher.dispose())
  watcher.start()
  await eventually(() => reads.length === 1)
  for (let i = 0; i < 5; i++) {
    reconcile()
    await eventually(() => reads.length === i + 2)
  }
  assert.deepEqual(reads, [true, false, false, false, false, true])
})

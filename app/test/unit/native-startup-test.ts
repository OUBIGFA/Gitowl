import assert from 'node:assert/strict'
import { after, it } from 'node:test'
import { EventEmitter } from 'events'
import { ipcRenderer } from 'electron'
import { NativeStartup } from '../../src/native-kernel/startup'
import { Repository } from '../../src/models/repository'
import { CloningRepository } from '../../src/models/cloning-repository'
import { projectKernelState } from '../../src/native-kernel/state-projection'
import { seed } from '../helpers/assisted-commit'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
after(() => window.close())

it('runs the shared deferred startup only once and disposes recurring work and platform listeners', async t => {
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  const { getAppRuntime } = await import('../../src/ui/app-runtime')
  const runtime = getAppRuntime()
  // Keep original runtime/dispatch state. Isolate only asynchronous external
  // work; fake timers prove the host cannot leak a recurring startup job.
  const emoji = t.mock.method(runtime.appStore, 'loadEmoji', async () => {})
  const stats = t.mock.method(runtime.dispatcher, 'reportStats', async () => {})
  const lfs = t.mock.method(
    runtime.dispatcher,
    'installGlobalLFSFilters',
    async () => {}
  )
  const launch = t.mock.method(
    runtime.dispatcher,
    'recordLaunchStats',
    () => {}
  )
  const postError = t.mock.method(
    runtime.dispatcher,
    'postError',
    async () => {}
  )
  t.mock.timers.enable({ apis: ['setInterval'] })
  const before = ipc.listenerCount('launch-timing-stats')
  const startup = new NativeStartup(runtime)
  assert.equal(ipc.listenerCount('launch-timing-stats'), before + 1)
  await startup.start()
  await startup.start()
  assert.equal(emoji.mock.callCount(), 1)
  assert.equal(lfs.mock.callCount(), 1)
  assert.equal(stats.mock.callCount(), 1)
  ipc.emit(
    'launch-timing-stats',
    {},
    { mainReadyTime: 1, loadTime: 2, rendererReadyTime: 3 }
  )
  assert.equal(launch.mock.callCount(), 1)
  t.mock.timers.tick(4 * 60 * 60 * 1000)
  assert.equal(stats.mock.callCount(), 2)
  startup.checkForUpdates(false)
  assert.match(
    String(postError.mock.calls[0].arguments[0]),
    /not configured for Gitowl/
  )
  startup.dispose()
  startup.dispose()
  assert.equal(ipc.listenerCount('launch-timing-stats'), before)
  t.mock.timers.tick(4 * 60 * 60 * 1000)
  await startup.start()
  assert.equal(stats.mock.callCount(), 2)
})

it(
  'refreshes sidebar counts for selected and unselected projects without fetching, then stops on dispose',
  { timeout: 30000 },
  async t => {
    const { getAppRuntime } = await import('../../src/ui/app-runtime')
    const runtime = getAppRuntime()
    const appStore = runtime.appStore
    let startup: NativeStartup | undefined
    t.after(() => startup?.dispose())
    const selectedFixture = await seed(t, { 'selected.txt': 'before\n' })
    const otherFixture = await seed(t, {
      'first.txt': 'before\n',
      'second.txt': 'before\n',
    })
    const selected = new Repository(selectedFixture.path, 101, null, false)
    const other = new Repository(otherFixture.path, 102, null, false)
    const repositories = [selected, other]
    const cloning = new CloningRepository(
      join(other.path, 'pending-clone'),
      'https://example.invalid/pending.git'
    )
    const previousRepositories = appStore['repositories']
    const previousSelection = appStore['selectedRepository']
    const previousFetch = appStore['fetchForRepositoryIndicator']
    const previousShouldFetch = appStore['shouldBackgroundFetch']
    let fetches = 0
    appStore['repositories'] = repositories
    appStore['selectedRepository'] = selected
    appStore['shouldBackgroundFetch'] = async () => true
    appStore['fetchForRepositoryIndicator'] = async () => {
      fetches++
      return null
    }
    t.after(() => {
      appStore['repositories'] = previousRepositories
      appStore['selectedRepository'] = previousSelection
      appStore['fetchForRepositoryIndicator'] = previousFetch
      appStore['shouldBackgroundFetch'] = previousShouldFetch
    })
    t.mock.getter(appStore['cloningRepositoriesStore'], 'repositories', () => [
      cloning,
    ])
    t.mock.method(appStore, 'loadEmoji', async () => {})
    t.mock.method(runtime.dispatcher, 'reportStats', async () => {})
    const errors: unknown[] = []
    t.mock.method(runtime.dispatcher, 'postError', async (error: Error) => {
      errors.push(error)
    })
    let finishConfig!: () => void
    const globalConfig = new Promise<void>(resolve => {
      finishConfig = resolve
    })
    t.mock.method(
      runtime.dispatcher,
      'installGlobalLFSFilters',
      () => globalConfig
    )
    let snapshot = projectKernelState(appStore.getState(), 0)
    const subscription = appStore.onDidUpdate(state => {
      snapshot = projectKernelState(state, 0)
    })
    t.after(() => subscription.dispose())
    const counts = () =>
      repositories.map(
        repository =>
          snapshot.repositories.find(item => item.id === repository.id)?.local
            ?.changedFiles
      )
    const waitForCounts = async (expected: number[]) => {
      for (let attempt = 0; attempt < 900; attempt++) {
        if (counts().every((count, index) => count === expected[index])) {
          return
        }
        await delay(10)
      }
      assert.deepEqual(counts(), expected)
    }
    await writeFile(join(selected.path, 'selected.txt'), 'changed\n')
    await writeFile(join(other.path, 'first.txt'), 'changed\n')
    startup = new NativeStartup(runtime)
    const starting = startup.start()
    await delay(25)
    assert.deepEqual(counts(), [undefined, undefined])
    finishConfig()
    await starting
    await startup.start()
    await waitForCounts([1, 1])
    await writeFile(join(other.path, 'second.txt'), 'changed\n')
    await waitForCounts([1, 2])
    await writeFile(join(other.path, 'first.txt'), 'before\n')
    await writeFile(join(other.path, 'second.txt'), 'before\n')
    await waitForCounts([1, 0])
    assert.equal(appStore['selectedRepository'], selected)
    assert.equal(
      appStore.getState().localRepositoryStateLookup.has(cloning.id),
      false
    )
    assert.equal(fetches, 0)
    assert.deepEqual(errors, [])
    startup.dispose()
    await writeFile(join(other.path, 'first.txt'), 'changed again\n')
    await delay(5100)
    assert.deepEqual(counts(), [1, 0])
  }
)

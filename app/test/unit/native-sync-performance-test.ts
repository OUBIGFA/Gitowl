import assert from 'node:assert/strict'
import { after, it } from 'node:test'
import { EventEmitter } from 'node:events'
import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { ipcRenderer } from 'electron'
import { Repository } from '../../src/models/repository'
import { fetch } from '../../src/lib/git/fetch'
import { pull } from '../../src/lib/git/pull'
import { DesktopPresenter } from '../../src/native-kernel/desktop-presenter'
import { rawGit, seed, tip } from '../helpers/assisted-commit'
import { createTempDirectory } from '../helpers/temp'

const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
after(() => window.close())

for (const operation of ['fetch', 'pull'] as const) {
  it(`${operation} follows a changed remote HEAD in the existing transfer without changing config`, async t => {
    const upstream = await seed(t, { 'file.txt': 'initial\n' })
    const path = await createTempDirectory(t)
    await rawGit(upstream, ['clone', '--', upstream.path, path])
    const repository = new Repository(path, -1, null, false)
    await rawGit(upstream, ['checkout', '-b', 'next-default'])
    const config = await readFile(join(path, '.git', 'config'))
    const remote = { name: 'origin', url: upstream.path }
    await (operation === 'fetch'
      ? fetch(repository, remote)
      : pull(repository, remote))
    assert.equal(
      await rawGit(repository, ['symbolic-ref', 'refs/remotes/origin/HEAD']),
      'refs/remotes/origin/next-default'
    )
    assert.deepEqual(await readFile(join(path, '.git', 'config')), config)
    await rawGit(repository, [
      'config',
      'remote.origin.followRemoteHEAD',
      'never',
    ])
    await rawGit(upstream, ['checkout', '-b', 'explicitly-ignored'])
    await (operation === 'fetch'
      ? fetch(repository, remote)
      : pull(repository, remote))
    assert.equal(
      await rawGit(repository, ['symbolic-ref', 'refs/remotes/origin/HEAD']),
      'refs/remotes/origin/next-default'
    )
  })
}

it('native fetch and pull use one transfer, preserve other branches and local edits, and finish before optional metadata', async t => {
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  const upstream = await seed(t, {
    'file.txt': 'initial\n',
    'local.txt': 'initial\n',
  })
  const path = await createTempDirectory(t)
  await rawGit(upstream, ['clone', '--', upstream.path, path])
  const fixture = new Repository(path, -1, null, false)
  const branch = await rawGit(fixture, ['branch', '--show-current'])
  const original = await tip(fixture)
  await rawGit(fixture, ['branch', '--track', 'keep-local', `origin/${branch}`])
  await writeFile(join(path, 'local.txt'), 'unsaved work\n')
  await writeFile(join(upstream.path, 'file.txt'), 'remote update\n')
  await rawGit(upstream, ['commit', '-am', 'Remote update'])
  const latest = await tip(upstream)

  const runtime = (await import('../../src/ui/app-runtime')).getAppRuntime()
  const presenter = new DesktopPresenter(runtime)
  const [repository] = await runtime.dispatcher.addRepositories([path])
  t.after(async () => {
    presenter.dispose()
    await runtime.dispatcher.removeRepository(repository, false)
  })
  await presenter.selectRepository(repository)
  await presenter.refreshRepository(repository)

  for (const operation of ['fetch', 'pull'] as const) {
    await t.test(operation, async t => {
      const trace = join(await createTempDirectory(t), 'trace.json')
      const previous = process.env.GIT_TRACE2_EVENT
      process.env.GIT_TRACE2_EVENT = trace.replace(/\\/g, '/')
      let release!: () => void
      const metadata = new Promise<void>(resolve => {
        release = resolve
      })
      t.mock.method(
        runtime.appStore,
        'refreshBranchProtectionState' as never,
        () => metadata
      )
      const pending = presenter[operation](repository)
      try {
        const deadline = new Promise<string>(resolve => {
          const timer = setTimeout(
            () => resolve('metadata blocked sync'),
            60000
          )
          timer.unref()
          t.after(() => clearTimeout(timer))
        })
        assert.equal(
          await Promise.race([pending.then(() => 'done'), deadline]),
          'done'
        )
        await pending
        assert.equal(
          await tip(fixture),
          operation === 'fetch' ? original : latest
        )
        assert.equal(
          await rawGit(fixture, ['rev-parse', 'keep-local']),
          original
        )
        assert.equal(
          await readFile(join(path, 'local.txt'), 'utf8'),
          'unsaved work\n'
        )
        const events = (await readFile(trace, 'utf8'))
          .trim()
          .split('\n')
          .map(line => JSON.parse(line))
        const transfers = events.filter(
          event =>
            event.event === 'child_start' &&
            event.argv?.some((arg: string) => arg.includes('git-upload-pack'))
        )
        assert.equal(transfers.length, 1, JSON.stringify(transfers))
      } finally {
        release()
        await pending
        if (previous === undefined) {
          delete process.env.GIT_TRACE2_EVENT
        } else {
          process.env.GIT_TRACE2_EVENT = previous
        }
      }
    })
  }
})

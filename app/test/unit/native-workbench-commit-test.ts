import assert from 'node:assert/strict'
import { after, before, describe, it, TestContext } from 'node:test'
import { EventEmitter } from 'events'
import { mkdir, mkdtemp, open, rm, writeFile } from 'fs/promises'
import { join, resolve } from 'path'
import { ipcRenderer } from 'electron'
import { exec } from 'dugite'
import type { AppRuntime } from '../../src/ui/app-runtime'
import { DesktopPresenter } from '../../src/native-kernel/desktop-presenter'
import { PopupType } from '../../src/models/popup'
import { closeKernelPopup } from '../../src/native-kernel/popup-presenter'

const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})

// Git, repositories, status/LFS/conflict detection, AppStore and Dispatcher are
// real. Only Electron IPC is the repository's ordinary unit-platform boundary.
// The shared deadline includes both complete real-Git fixtures and their
// cleanup; it must not cancel the conflict case while Git still owns its cwd.
describe(
  'native save shares original ChangesSidebar preflight',
  { timeout: 600000 },
  () => {
    let runtime: AppRuntime
    let presenter: DesktopPresenter
    before(async () => {
      localStorage.clear()
      localStorage.setItem('stats-opt-out', '1')
      localStorage.setItem('has-sent-stats-opt-in-ping', '1')
      const { getAppRuntime } = await import('../../src/ui/app-runtime')
      runtime = getAppRuntime()
      presenter = new DesktopPresenter(runtime)
    })
    after(() => {
      presenter.dispose()
      window.close()
    })

    async function repository(t: TestContext) {
      const parent = resolve('_temp/native-workbench-commit-tests')
      await mkdir(parent, { recursive: true })
      const path = await mkdtemp(join(parent, 'repository-'))
      const git = async (args: string[], expected = 0) => {
        const result = await exec(args, path)
        assert.equal(result.exitCode, expected, result.stderr)
        return result.stdout.trim()
      }
      await git(['init', '-b', 'main'])
      await git(['config', 'user.name', 'Native Fixture'])
      await git(['config', 'user.email', 'fixture@example.invalid'])
      await writeFile(join(path, 'file.txt'), 'base\n')
      await git(['add', '.'])
      await git(['commit', '-m', 'base'])
      const [repo] = await runtime.dispatcher.addRepositories([path])
      assert.ok(repo !== undefined)
      t.after(async () => {
        while (runtime.appStore.getState().currentPopup !== null) {
          runtime.dispatcher.closePopupById(
            runtime.appStore.getState().currentPopup!.id!
          )
        }
        await runtime.dispatcher.removeRepository(repo, false)
        await rm(path, {
          recursive: true,
          force: true,
          maxRetries: 20,
          retryDelay: 50,
        })
      })
      return { repo, path, git }
    }

    it('warns for actual selected large files before any native save writes', async t => {
      const { repo, path, git } = await repository(t)
      const file = await open(join(path, 'large.bin'), 'w')
      try {
        await file.truncate(100 * 1024 * 1024 + 1)
      } finally {
        await file.close()
      }
      await presenter.selectRepository(repo)
      await presenter.refreshRepository(repo)
      const before = await git(['rev-parse', 'HEAD'])
      const committed = await presenter.commitIncludedChanges(repo, {
        summary: 'must first confirm',
        description: null,
      })
      assert.equal(committed, false)
      const popup = runtime.appStore.getState().currentPopup
      assert.equal(popup?.type, PopupType.OversizedFiles)
      assert.ok(
        popup?.type === PopupType.OversizedFiles && popup.id !== undefined
      )
      assert.deepEqual(popup.oversizedFiles, ['large.bin'])
      await closeKernelPopup(runtime, popup.id)
      assert.equal(await git(['rev-parse', 'HEAD']), before)
      assert.equal(await git(['diff', '--cached', '--name-only']), '')
      assert.equal(
        runtime.repositoryStateManager.get(repo).changesState.commitMessage
          .summary,
        ''
      )
    })

    it('keeps real conflict markers behind the original warning and cancel preserves the merge', async t => {
      const { repo, path, git } = await repository(t)
      await git(['checkout', '-b', 'fixture-other'])
      await writeFile(join(path, 'file.txt'), 'other\n')
      await git(['commit', '-am', 'other'])
      await git(['checkout', 'main'])
      await writeFile(join(path, 'file.txt'), 'main\n')
      await git(['commit', '-am', 'main'])
      await git(['merge', 'fixture-other'], 1)
      await presenter.selectRepository(repo)
      await presenter.refreshRepository(repo)
      const files =
        runtime.repositoryStateManager.get(repo).changesState.workingDirectory
          .files
      assert.deepEqual(
        files.map(file => file.path),
        ['file.txt']
      )
      await presenter.changeFileIncluded(
        repo,
        files.map(file => file.id),
        true
      )
      const before = await git(['rev-parse', 'HEAD'])
      const committed = await presenter.commitIncludedChanges(repo, {
        summary: 'unresolved merge',
        description: null,
      })
      assert.equal(committed, false)
      const popup = runtime.appStore.getState().currentPopup
      assert.ok(
        popup?.type === PopupType.CommitConflictsWarning &&
          popup.id !== undefined
      )
      assert.deepEqual(
        popup.files.map(file => file.path),
        ['file.txt']
      )
      await closeKernelPopup(runtime, popup.id)
      assert.equal(await git(['rev-parse', 'HEAD']), before)
      assert.ok(await git(['rev-parse', '--verify', 'MERGE_HEAD']))
      assert.match(await git(['diff', '--', 'file.txt']), /<<<<<<< HEAD/)
    })
  }
)

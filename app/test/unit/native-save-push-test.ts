import assert from 'node:assert/strict'
import { after, it } from 'node:test'
import { EventEmitter } from 'node:events'
import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { exec } from 'dugite'
import { ipcRenderer } from 'electron'
import { seed } from '../helpers/assisted-commit'
import { DesktopPresenter } from '../../src/native-kernel/desktop-presenter'
import { parseKernelCommand } from '../../src/native-kernel/command-validation'
import { PopupType } from '../../src/models/popup'

const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
after(() => window.close())

it('accepts only an explicit boolean push choice on a commit request', () => {
  const command = {
    name: 'repository.commit',
    repository: { id: 1, path: 'C:/fixture' },
    context: { summary: 'Save', description: null, pushAfterCommit: true },
  }
  assert.deepEqual(parseKernelCommand(command), command)
  assert.throws(() =>
    parseKernelCommand({
      ...command,
      context: { ...command.context, pushAfterCommit: 'true' },
    })
  )
})

it('saves locally by default and pushes only after the explicitly requested save succeeds', async t => {
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
  const fixture = await seed(t, {
    'selected.txt': 'base\n',
    'unselected.txt': 'base\n',
  })
  const git = async (args: string[]) => {
    const result = await exec(args, fixture.path)
    assert.equal(result.exitCode, 0, result.stderr)
    return result.stdout.trim()
  }
  const remote = join(fixture.path, '.git', 'test-origin.git')
  await git(['clone', '--bare', '.', remote])
  await git(['remote', 'add', 'origin', remote])
  const branch = await git(['branch', '--show-current'])
  await git(['fetch', 'origin'])
  await git(['branch', `--set-upstream-to=origin/${branch}`])
  const remoteHead = () =>
    git(['--git-dir', remote, 'rev-parse', `refs/heads/${branch}`])
  const original = await remoteHead()
  const runtime = (await import('../../src/ui/app-runtime')).getAppRuntime()
  const errors = runtime.appStore.onDidError(error => {
    void runtime.dispatcher.postError(error)
  })
  const presenter = new DesktopPresenter(runtime)
  const [repository] = await runtime.dispatcher.addRepositories([fixture.path])
  t.after(async () => {
    presenter.dispose()
    errors.dispose()
    for (const popup of runtime.appStore.getState().allPopups) {
      runtime.dispatcher.closePopupById(popup.id!)
    }
    await runtime.dispatcher.removeRepository(repository, false)
  })
  await presenter.selectRepository(repository)
  await presenter.refreshRepository(repository)
  const save = async (summary: string, pushAfterCommit: boolean) => {
    await writeFile(join(fixture.path, 'selected.txt'), summary + '\n')
    await writeFile(join(fixture.path, 'unselected.txt'), 'keep local\n')
    await presenter.refreshRepository(repository)
    const files =
      runtime.repositoryStateManager.get(repository).changesState
        .workingDirectory.files
    await presenter.changeIncludeAllFiles(repository, false)
    await presenter.changeFileIncluded(
      repository,
      files.filter(f => f.path === 'selected.txt').map(f => f.id),
      true
    )
    const context = { summary, description: null, pushAfterCommit }
    return presenter.commitIncludedChanges(repository, context)
  }
  assert.equal(await save('Local version', false), true)
  assert.equal(
    await remoteHead(),
    original,
    'ordinary Save pushed without a request'
  )
  assert.equal(await save('Save and push', true), true)
  let saved = await git(['rev-parse', 'HEAD'])
  // The original push may report progress after the commit receipt. Wait for
  // its observable local bare-remote ref rather than a mock call count.
  for (let i = 0; i < 100 && (await remoteHead()) !== saved; i++) {
    await delay(20)
  }
  assert.equal(await remoteHead(), saved)
  assert.equal(await git(['show', 'HEAD:unselected.txt']), 'base')
  assert.equal(await git(['diff', '--name-only']), 'unselected.txt')

  const published = runtime.repositoryStateManager
    .get(repository)
    .commitLookup.get(saved)
  assert.ok(published)
  runtime.dispatcher.setConfirmForcePushSetting(true)
  // Prepare the original already-approved amend; the action under test must
  // still ask before it force-pushes the new published commit.
  await runtime.appStore._startAmendingRepository(
    repository,
    published,
    false,
    true
  )
  assert.equal(await save('Amend and push', true), true)
  const force = runtime.appStore.getState().currentPopup
  assert.equal(force?.type, PopupType.ConfirmForcePush)
  assert.equal(
    await remoteHead(),
    saved,
    'a published amend bypassed force-push confirmation'
  )
  saved = await git(['rev-parse', 'HEAD'])
  await presenter.replyToPopup({
    popupID: force!.id!,
    type: PopupType.ConfirmForcePush,
    action: 'submit',
    askForConfirmationOnForcePush: true,
  })
  assert.equal(await remoteHead(), saved)

  await runtime.dispatcher.setCoAuthors(repository, [
    { kind: 'unknown', username: 'unresolved-fixture-author', state: 'error' },
  ])
  assert.equal(await save('Cancelled save must not push', true), false)
  const warning = runtime.appStore.getState().currentPopup
  assert.equal(warning?.type, PopupType.UnknownAuthors)
  await presenter.closePopup(warning!.id!)
  assert.equal(await git(['rev-parse', 'HEAD']), saved)
  assert.equal(await remoteHead(), saved)
  await runtime.dispatcher.setCoAuthors(repository, [])

  await mkdir(join(fixture.path, '.git', 'hooks'), { recursive: true })
  await writeFile(
    join(fixture.path, '.git', 'hooks', 'pre-commit'),
    '#!/bin/sh\nexit 1\n',
    // Keep real hook rejection active on POSIX runners as well as Windows.
    { mode: 0o755 }
  )
  const rejected = save('Rejected save must not push', true)
  for (
    let i = 0;
    i < 1000 && runtime.appStore.getState().currentPopup === null;
    i++
  ) {
    await delay(10)
  }
  const popup = runtime.appStore.getState().currentPopup
  assert.ok(
    popup?.type === PopupType.HookFailed || popup?.type === PopupType.Error
  )
  await presenter.closePopup(popup!.id!)
  assert.equal(await rejected, false)
  assert.equal(await git(['rev-parse', 'HEAD']), saved)
  assert.equal(await remoteHead(), saved)

  await writeFile(
    join(fixture.path, '.git', 'hooks', 'pre-commit'),
    '#!/bin/sh\nexit 0\n',
    { mode: 0o755 }
  )
  await mkdir(join(remote, 'hooks'), { recursive: true })
  await writeFile(
    join(remote, 'hooks', 'pre-receive'),
    '#!/bin/sh\necho "Fixture rejects push" >&2\nexit 1\n',
    { mode: 0o755 }
  )
  assert.equal(await save('Local save survives a rejected push', true), true)
  const afterRejectedPush = await git(['rev-parse', 'HEAD'])
  assert.notEqual(afterRejectedPush, saved)
  assert.equal(await remoteHead(), saved)
  assert.equal(
    await git(['log', '-1', '--format=%s']),
    'Local save survives a rejected push'
  )
  assert.equal(runtime.appStore.getState().currentPopup?.type, PopupType.Error)
  assert.equal(
    runtime.repositoryStateManager.get(repository).changesState.commitMessage
      .summary,
    ''
  )
})

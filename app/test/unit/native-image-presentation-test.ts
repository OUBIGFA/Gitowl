import assert from 'node:assert/strict'
import { after, describe, it } from 'node:test'
import { EventEmitter } from 'events'
import { ipcRenderer } from 'electron'
import { DesktopPresenter } from '../../src/native-kernel/desktop-presenter'
import { executeKernelCommand } from '../../src/native-kernel/execute-command'
import { parseKernelCommand } from '../../src/native-kernel/command-validation'
import { projectKernelState } from '../../src/native-kernel/state-projection'
import { ApplicationTheme } from '../../src/ui/lib/application-theme'
import { ImageDiffType } from '../../src/models/diff'
import {
  executeImagePresentationCommand,
  parseImagePresentationCommand,
  projectImagePresentation,
} from '../../src/native-kernel/image-presentation'

// Only Electron's host notifications are supplied. Mode mutation/persistence
// run through the real AppRuntime, Dispatcher and AppStore; no Git is invoked.
const ipc = new EventEmitter()
Object.assign(ipcRenderer, {
  send: () => {},
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
after(() => window.close())

describe('native image presentation', () => {
  it('accepts only original enum values, without a repository or reflected method', () => {
    assert.equal(parseImagePresentationCommand({ name: 'unrelated' }), null)
    for (const imageDiffType of [0, 1, 2, 3]) {
      assert.deepEqual(
        parseImagePresentationCommand({
          name: 'imageDiff.type',
          imageDiffType,
        }),
        { name: 'imageDiff.type', imageDiffType }
      )
    }
    for (const imageDiffType of [-1, 4, 0.1, NaN, '1', null, undefined, true]) {
      assert.throws(() =>
        parseImagePresentationCommand({ name: 'imageDiff.type', imageDiffType })
      )
    }
    assert.throws(() =>
      parseImagePresentationCommand({
        name: 'imageDiff.type',
        imageDiffType: 0,
        repository: { id: 1, path: 'not-a-repository-command' },
      })
    )
  })

  it('projects original appearance previews and rollback without a selected repository', async () => {
    const { getAppRuntime } = await import('../../src/ui/app-runtime')
    const runtime = getAppRuntime()
    const original = runtime.appStore.getState()
    try {
      for (const theme of [
        ApplicationTheme.Light,
        ApplicationTheme.Dark,
        ApplicationTheme.System,
      ]) {
        runtime.dispatcher.setSelectedTheme(theme)
        runtime.dispatcher.setSelectedTabSize(7)
        assert.deepEqual(
          projectKernelState(runtime.appStore.getState(), 1).appearance,
          { theme, tabSize: 7 }
        )
      }
    } finally {
      runtime.dispatcher.setSelectedTheme(original.selectedTheme)
      runtime.dispatcher.setSelectedTabSize(original.selectedTabSize)
    }
    assert.deepEqual(
      projectKernelState(runtime.appStore.getState(), 2).appearance,
      { theme: original.selectedTheme, tabSize: original.selectedTabSize }
    )
  })

  it('projects and persists the global original preference through Dispatcher', async () => {
    const { getAppRuntime } = await import('../../src/ui/app-runtime')
    const runtime = getAppRuntime()
    const presenter = new DesktopPresenter(runtime)
    const original = runtime.appStore.getState().imageDiffType
    const stored = localStorage.getItem('image-diff-type')
    try {
      for (const imageDiffType of [
        ImageDiffType.TwoUp,
        ImageDiffType.Swipe,
        ImageDiffType.OnionSkin,
        ImageDiffType.Difference,
      ]) {
        await executeKernelCommand(
          presenter,
          parseKernelCommand({
            name: 'imageDiff.type',
            imageDiffType,
          })
        )
        assert.equal(
          projectKernelState(runtime.appStore.getState(), 1).imageDiffType,
          imageDiffType
        )
        assert.deepEqual(
          projectImagePresentation(runtime.appStore.getState()),
          {
            imageDiffType,
          }
        )
        assert.equal(
          localStorage.getItem('image-diff-type'),
          JSON.stringify(imageDiffType)
        )
      }
    } finally {
      presenter.dispose()
      await executeImagePresentationCommand(runtime.dispatcher, {
        name: 'imageDiff.type',
        imageDiffType: original,
      })
      if (stored === null) {
        localStorage.removeItem('image-diff-type')
      } else {
        localStorage.setItem('image-diff-type', stored)
      }
    }
  })
})

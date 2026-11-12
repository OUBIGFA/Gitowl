import '../lib/hide-child-process-windows'
import '../lib/logging/renderer/install'

import * as Path from 'path'
import { getAppRuntime } from '../ui/app-runtime'
import { NativeStartup } from './startup'
import { DesktopPresenter } from './desktop-presenter'
import { KernelPopupError } from './popup-presenter'
import { kernelMediaErrorMessage } from './media-presentation'
import { executeKernelCommand } from './execute-command'
import { parseKernelCommand } from './command-validation'
import { projectKernelState } from './state-projection'
import * as ipcRenderer from '../lib/ipc-renderer'
import { sendReady } from '../ui/main-process-proxy'
import { migrateRendererGUID } from '../lib/get-renderer-guid'
import { shellNeedsPatching, updateEnvironmentForProcess } from '../lib/shell'
import { enableSourceMaps } from '../lib/source-map-support'
import { initializeRendererNotificationHandler } from '../lib/notifications/notification-handler'
import { SelectionType } from '../lib/app-state'
import type { NativeKernelResponse } from './protocol'

// This is a real Electron kernel realm, preserving upstream storage/network/IPC.
// It mounts no React component, stylesheet, canvas or user-visible web frontend.
migrateRendererGUID()
if (shellNeedsPatching(process)) {
  updateEnvironmentForProcess()
}
enableSourceMaps()
process.env.LOCAL_GIT_DIRECTORY = Path.resolve(__dirname, 'git')
delete process.env.GIT_EXEC_PATH

const startTime = performance.now()
const runtime = getAppRuntime()
const presenter = new DesktopPresenter(runtime)
const startup = new NativeStartup(runtime)
presenter.onMenuPresentationAction(action => {
  ipcRenderer.send('native-kernel-presentation-action', action)
})
let revision = 0
let loaded = false
let dateRefreshTimer: number | undefined
const snapshot = () => {
  const result = projectKernelState(
    presenter.getState(),
    ++revision,
    presenter.getOnboardingState(),
    presenter.getCommitState(),
    presenter.getMenuState(),
    presenter.getMissingRepositoryState()
  )
  window.clearTimeout(dateRefreshTimer)
  const delays =
    result.selected?.history.flatMap(commit =>
      commit.dateRefreshAfter === undefined ? [] : [commit.dateRefreshAfter]
    ) ?? []
  if (loaded && delays.length > 0) {
    // Follow the original relative-time scheduler even when Git state is idle.
    dateRefreshTimer = window.setTimeout(() => {
      ipcRenderer.send('native-kernel-state', snapshot())
    }, Math.max(1, Math.min(2147483647, ...delays)))
  }
  return result
}

const stateUpdates = presenter.onDidUpdate(() => {
  if (loaded) {
    ipcRenderer.send('native-kernel-state', snapshot())
  }
})
runtime.appStore.onDidError(error => {
  // Preserve the original error-handler chain and its popup/recovery requests.
  // It is not replaced with an always-abort or generic RPC recovery policy.
  void runtime.dispatcher.postError(error).catch(failure => {
    log.error('Desktop error handling failed', failure)
  })
})
initializeRendererNotificationHandler(runtime.notificationsStore)

ipcRenderer.on('focus', () => {
  const { selectedState } = presenter.getState()
  if (selectedState && selectedState.type !== SelectionType.CloningRepository) {
    void runtime.dispatcher.refreshRepository(selectedState.repository)
  }
  void runtime.dispatcher.setAppFocusState(true)
})
ipcRenderer.on('blur', () => {
  void runtime.dispatcher.setAccessKeyHighlightState(false)
  void runtime.dispatcher.setAppFocusState(false)
})
ipcRenderer.on('url-action', (_, action) => {
  void presenter
    .dispatchURLAction(action)
    .catch(error => runtime.dispatcher.postError(error))
})
ipcRenderer.on('cli-action', (_, action) => {
  void presenter
    .dispatchCLIAction(action)
    .catch(error => runtime.dispatcher.postError(error))
})
ipcRenderer.on('menu-event', (_, name) => {
  void Promise.resolve(presenter.handleMenuEvent(name)).catch(error =>
    runtime.dispatcher.postError(error)
  )
})
window.addEventListener('beforeunload', () => {
  window.clearTimeout(dateRefreshTimer)
  stateUpdates.dispose()
  startup.dispose()
  presenter.dispose()
})

ipcRenderer.on('native-kernel-request', (_, request) => {
  const respond = (response: NativeKernelResponse) =>
    ipcRenderer.send('native-kernel-response', response)
  void (async () => {
    try {
      const command = parseKernelCommand(request.command)
      // Startup rewrites the global Git config while commands are already
      // admitted. On Windows a concurrent Git read or write of that file fails
      // (Permission denied, or a silently skipped config write), so Git work
      // waits for those writes. Media frames never touch Git.
      if (command.name !== 'media.frame.read') {
        await startup.globalConfigSettled()
      }
      const result = await executeKernelCommand(presenter, command)
      respond({ id: request.id, result: { ...result, snapshot: snapshot() } })
    } catch (error) {
      // Revoked/failed media is a local preview error, never a new Git recovery
      // decision or an Error popup over another repository's active operation.
      if (
        request.command.name !== 'media.frame.read' &&
        !(error instanceof KernelPopupError && error.reason === 'unavailable')
      ) {
        await runtime.dispatcher.postError(error)
      }
      // Wire diagnostics do not dump credentials, model input or error objects.
      respond({
        id: request.id,
        error: {
          code: -32000,
          message:
            request.command.name === 'media.frame.read'
              ? kernelMediaErrorMessage(error)
              : 'Desktop action failed; inspect the application error or recovery dialog',
        },
      })
    }
  })().catch(error => {
    log.error('Native kernel request failed', error)
    respond({
      id: request.id,
      error: { code: -32000, message: 'Desktop error presentation failed' },
    })
  })
})

void presenter
  .loadInitialState()
  .then(() => {
    loaded = true
    ipcRenderer.send('native-kernel-state', snapshot())
    // Preserve upstream ordering: startup side effects run only after the
    // original main process has received renderer-ready.
    sendReady(performance.now() - startTime)
    void startup.start().catch(error => runtime.dispatcher.postError(error))
  })
  .catch(error => {
    log.error('Desktop kernel initialization failed', error)
    void runtime.dispatcher.postError(error)
    ipcRenderer.send('native-kernel-failure', 'initialization')
  })

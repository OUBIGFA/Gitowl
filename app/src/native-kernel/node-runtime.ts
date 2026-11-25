import '../lib/hide-child-process-windows'

import * as Path from 'path'
import { getAppRuntime } from '../ui/app-runtime'
import { NativeStartup } from './startup'
import { DesktopPresenter } from './desktop-presenter'
import { KernelPopupError } from './popup-presenter'
import { kernelMediaErrorMessage } from './media-presentation'
import { executeKernelCommand } from './execute-command'
import { parseKernelCommand } from './command-validation'
import { projectKernelState } from './state-projection'
import { SnapshotPushEncoder, SnapshotScheduler } from './snapshot-sections'
import * as ipcRenderer from '../lib/ipc-renderer'
import { migrateRendererGUID } from '../lib/get-renderer-guid'
import { shellNeedsPatching, updateEnvironmentForProcess } from '../lib/shell'
import { enableSourceMaps } from '../lib/source-map-support'
import { initializeRendererNotificationHandler } from '../lib/notifications/notification-handler'
import { SelectionType } from '../lib/app-state'
import type { NativeKernelResponse } from './protocol'
import { getDesktopHost } from '../lib/desktop-host'
import type { NodeKernelTransport } from './node-transport'
import { waitForRepositoryGitOperations } from '../lib/git/repository-operation'
import { stopImageWorker } from './image-worker-client'
import { getNativeCopilotRuntime } from '../lib/copilot-runtime-download'

/** The existing business runtime/presenters, hosted by Node without a DOM. */
export function startNodeRuntime(transport: NodeKernelTransport) {
  // Host storage and platform services are installed before business assembly.
  migrateRendererGUID()
  if (shellNeedsPatching(process)) {
    updateEnvironmentForProcess()
  }
  enableSourceMaps()
  process.env.LOCAL_GIT_DIRECTORY ??= Path.resolve(__dirname, 'git')
  delete process.env.GIT_EXEC_PATH

  const copilotDownload = getNativeCopilotRuntime(getDesktopHost().userData)
  const runtime = getAppRuntime()
  const presenter = new DesktopPresenter(runtime)
  const startup = new NativeStartup(runtime)
  presenter.onMenuPresentationAction(action => {
    transport.presentation(action)
  })
  let revision = 0
  let loaded = false
  let dateRefreshTimer: ReturnType<typeof setTimeout> | undefined
  const snapshot = () => {
    const result = projectKernelState(
      presenter.getState(),
      ++revision,
      presenter.getOnboardingState(),
      presenter.getCommitState(),
      presenter.getMenuState(),
      presenter.getMissingRepositoryState()
    )
    globalThis.clearTimeout(dateRefreshTimer)
    const delays =
      result.selected?.history.flatMap(commit =>
        commit.dateRefreshAfter === undefined ? [] : [commit.dateRefreshAfter]
      ) ?? []
    if (loaded && delays.length > 0) {
      // Follow the original relative-time scheduler even when Git state is idle.
      dateRefreshTimer = globalThis.setTimeout(
        pushState,
        Math.max(1, Math.min(2147483647, ...delays))
      )
    }
    return { ...result, copilotRuntime: copilotDownload.status }
  }
  // Every push goes through this one encoder, in order: a push names the
  // sections its predecessor already delivered instead of repeating them.
  const pushes = new SnapshotPushEncoder()
  function pushState() {
    transport.publish(pushes.encode(snapshot()))
  }
  const scheduledPush = new SnapshotScheduler(pushState)
  const downloadChanged = () => {
    if (loaded) {
      scheduledPush.schedule()
    }
  }
  copilotDownload.events.on('change', downloadChanged)
  transport.events.on('copilot-runtime-cancel', () => copilotDownload.cancel())
  transport.events.on('host-closed', () => copilotDownload.cancel())

  const stateUpdates = presenter.onDidUpdate(() => {
    if (loaded) {
      scheduledPush.schedule()
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
    if (
      selectedState &&
      selectedState.type !== SelectionType.CloningRepository
    ) {
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
  const dispose = async () => {
    copilotDownload.cancel()
    copilotDownload.events.off('change', downloadChanged)
    globalThis.clearTimeout(dateRefreshTimer)
    scheduledPush.dispose()
    stateUpdates.dispose()
    startup.dispose()
    presenter.dispose()
    stopImageWorker()
    runtime.appStore.stopBackgroundTasks()
    await startup.globalConfigSettled()
    await waitForRepositoryGitOperations()
  }

  transport.attach(request => {
    const respond = (response: NativeKernelResponse) =>
      transport.respond(response)
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
      pushState()
      // Preserve upstream ordering: startup side effects run only after the
      // original main process has received renderer-ready.
      void startup.start().catch(error => runtime.dispatcher.postError(error))
    })
    .catch(error => {
      log.error('Desktop kernel initialization failed', error)
      void runtime.dispatcher.postError(error)
      transport.fail('initialization')
    })

  getDesktopHost().events.on('platform-error', error => {
    void runtime.dispatcher
      .postError(error)
      .catch(failure =>
        log.error('Platform error presentation failed', failure)
      )
  })
  return dispose
}

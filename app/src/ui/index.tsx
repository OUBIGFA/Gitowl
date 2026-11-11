import '../lib/logging/renderer/install'

import * as React from 'react'
import * as ReactDOM from 'react-dom'
import * as Path from 'path'
import { App } from './app'
import { getAppRuntime } from './app-runtime'
import { SelectionType, IAppState } from '../lib/app-state'
import { shellNeedsPatching, updateEnvironmentForProcess } from '../lib/shell'
import { installDevGlobals } from './install-globals'
import { reportUncaughtException, sendErrorReport } from './main-process-proxy'
import { getOS } from '../lib/get-os'
import {
  enableSourceMaps,
  withSourceMappedStack,
} from '../lib/source-map-support'

import { sendNonFatalException } from '../lib/helpers/non-fatal-exception'
import { enableUnhandledRejectionReporting } from '../lib/feature-flag'
import {
  ApplicationTheme,
  supportsSystemThemeChanges,
} from './lib/application-theme'
import * as ipcRenderer from '../lib/ipc-renderer'
import { migrateRendererGUID } from '../lib/get-renderer-guid'
import { initializeRendererNotificationHandler } from '../lib/notifications/notification-handler'
import { Grid } from 'react-virtualized'

if (__DEV__) {
  installDevGlobals()
}

migrateRendererGUID()

if (shellNeedsPatching(process)) {
  updateEnvironmentForProcess()
}

enableSourceMaps()

// Tell dugite where to find the git environment,
// see https://github.com/desktop/dugite/pull/85
process.env['LOCAL_GIT_DIRECTORY'] = Path.resolve(__dirname, 'git')

// Ensure that dugite infers the GIT_EXEC_PATH
// based on the LOCAL_GIT_DIRECTORY env variable
// instead of just blindly trusting what's set in
// the current environment. See https://git.io/JJ7KF
delete process.env.GIT_EXEC_PATH

const startTime = performance.now()

if (!process.env.TEST_ENV) {
  /* This is the magic trigger for webpack to go compile
   * our sass into css and inject it into the DOM. */
  require('../../styles/desktop.scss')
}

// TODO (electron): Remove this once
// https://bugs.chromium.org/p/chromium/issues/detail?id=1113293
// gets fixed and propagated to electron.
if (__DARWIN__) {
  require('../lib/fix-emoji-spacing')
}

let currentState: IAppState | null = null

const sendErrorWithContext = (
  e: unknown,
  context: Record<string, string> = {},
  nonFatal?: boolean
) => {
  const error = withSourceMappedStack(e)

  console.error('Uncaught exception', error)

  if (__DEV__ || process.env.TEST_ENV) {
    console.error(
      `An uncaught exception was thrown. If this were a production build it would be reported to Central. Instead, maybe give it a lil lookyloo.`
    )
  } else {
    const extra: Record<string, string> = {
      osVersion: getOS(),
      ...context,
    }

    try {
      if (currentState) {
        if (currentState.currentBanner !== null) {
          extra.currentBanner = currentState.currentBanner.type
        }

        if (currentState.currentPopup !== null) {
          extra.currentPopup = `${currentState.currentPopup.type}`
        }

        if (currentState.selectedState !== null) {
          extra.selectedState = `${currentState.selectedState.type}`

          if (currentState.selectedState.type === SelectionType.Repository) {
            extra.selectedRepositorySection = `${currentState.selectedState.state.selectedSection}`
          }
        }

        if (currentState.currentFoldout !== null) {
          extra.currentFoldout = `${currentState.currentFoldout.type}`
        }

        if (currentState.showWelcomeFlow) {
          extra.inWelcomeFlow = 'true'
        }

        if (currentState.windowZoomFactor !== 1) {
          extra.windowZoomFactor = `${currentState.windowZoomFactor}`
        }

        if (currentState.errorCount > 0) {
          extra.activeAppErrors = `${currentState.errorCount}`
        }

        extra.repositoryCount = `${currentState.repositories.length}`
        extra.windowState = currentState.windowState ?? 'Unknown'
        extra.accounts = `${currentState.accounts.length}`

        extra.automaticallySwitchTheme = `${
          currentState.selectedTheme === ApplicationTheme.System &&
          supportsSystemThemeChanges()
        }`
      }
    } catch {
      /* ignore */
    }

    sendErrorReport(error, extra, nonFatal ?? false)
  }
}

const resizeLoopCompletedMessage =
  'ResizeObserver loop completed with undelivered notifications.'

const onUncaughtException = (error: unknown) => {
  // This is a known issue with the ResizeObserver API in Chromium 132 which is
  // fixed in 133 that we can safely ignore.
  // See: https://issues.chromium.org/issues/391393420
  if (
    error === resizeLoopCompletedMessage ||
    (error &&
      typeof error === 'object' &&
      'message' in error &&
      error.message === resizeLoopCompletedMessage)
  ) {
    sendNonFatalException(
      'resizeObserverLoopCompleted',
      withSourceMappedStack(error)
    )
    return
  }

  sendErrorWithContext(error)
  reportUncaughtException(withSourceMappedStack(error))

  // We used to subscribe to uncaughtException using process.once but we want
  // to be able to ignore the resize observer error above so we need to
  // unsubscribe manually once we encounter an error we actually want to crash
  // the app for.
  process.off('uncaughtException', onUncaughtException)
}

process.on('uncaughtException', onUncaughtException)

// See sendNonFatalException for more information
process.on(
  'send-non-fatal-exception',
  (error: Error, context?: { [key: string]: string }) => {
    sendErrorWithContext(error, context, true)
  }
)

/**
 * Chromium won't crash on an unhandled rejection (similar to how it won't crash
 * on an unhandled error). We've taken the approach that unhandled errors should
 * crash the app and very likely we should do the same thing for unhandled
 * promise rejections but that's a bit too risky to do until we've established
 * some sense of how often it happens. For now this simply stores the last
 * rejection so that we can pass it along with the crash report if the app does
 * crash. Note that this does not prevent the default browser behavior of
 * logging since we're not calling `preventDefault` on the event.
 *
 * See https://developer.mozilla.org/en-US/docs/Web/API/Window/unhandledrejection_event
 */
window.addEventListener('unhandledrejection', ev => {
  if (enableUnhandledRejectionReporting() && ev.reason instanceof Error) {
    sendNonFatalException('unhandledRejection', ev.reason)
  }
})

const {
  appStore,
  dispatcher,
  repositoryStateManager,
  issuesStore,
  gitHubUserStore,
  aheadBehindStore,
  notificationsStore,
  notificationsDebugStore,
} = getAppRuntime()

appStore.onDidUpdate(state => {
  currentState = state
})

document.body.classList.add(`platform-${process.platform}`)

dispatcher.initializeAppFocusState()

initializeRendererNotificationHandler(notificationsStore)

ipcRenderer.on('focus', () => {
  const { selectedState } = appStore.getState()

  // Refresh the currently selected repository on focus (if
  // we have a selected repository, that is not cloning).
  if (
    selectedState &&
    !(selectedState.type === SelectionType.CloningRepository)
  ) {
    dispatcher.refreshRepository(selectedState.repository)
  }

  dispatcher.setAppFocusState(true)
})

ipcRenderer.on('blur', () => {
  // Make sure we stop highlighting the menu button (on non-macOS)
  // when someone uses Alt+Tab to switch application since we won't
  // get the onKeyUp event for the Alt key in that case.
  dispatcher.setAccessKeyHighlightState(false)
  dispatcher.setAppFocusState(false)
})

ipcRenderer.on('url-action', (_, action) =>
  dispatcher
    .dispatchURLAction(action)
    .catch(e => log.error(`URL action ${action.name} failed`, e))
)

ipcRenderer.on('cli-action', (_, action) =>
  dispatcher
    .dispatchCLIAction(action)
    .catch(e => log.error(`CLI action ${action.kind} failed`, e))
)

// react-virtualized will use the literal string "grid" as the 'aria-label'
// attribute unless we override it. This is a problem because aria-label should
// not be set unless there's a compelling reason for it[1].
//
// Similarly the default props call for the 'aria-readonly' attribute to be set
// to true which according to MDN doesn't fit our use case[2]:
//
// > This indicates to the user that an interactive element that would normally
// > be focusable and copyable has been placed in a read-only (not disabled)
// > state.
//
// 1. https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Attributes/aria-label
// 2. https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Attributes/aria-readonly
;(function (
  defaults: Record<string, unknown> | undefined,
  types: Record<string, unknown> | undefined
) {
  ;['aria-label', 'aria-readonly'].forEach(k => {
    delete defaults?.[k]
    delete types?.[k]
  })
})(Grid.defaultProps, Grid.propTypes)

ReactDOM.render(
  <App
    dispatcher={dispatcher}
    appStore={appStore}
    repositoryStateManager={repositoryStateManager}
    issuesStore={issuesStore}
    gitHubUserStore={gitHubUserStore}
    aheadBehindStore={aheadBehindStore}
    notificationsDebugStore={notificationsDebugStore}
    startTime={startTime}
  />,
  document.getElementById('desktop-app-container')!
)

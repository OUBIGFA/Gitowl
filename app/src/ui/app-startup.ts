import { CompositeDisposable } from 'event-kit'
import type { AppStore } from '../lib/stores/app-store'
import type { Dispatcher } from './dispatcher'
import type { IAppState } from '../lib/app-state'
import type { ILaunchStats } from '../lib/stats'
import { PopupType } from '../models/popup'
import { Banner, BannerType } from '../models/banner'
import { isDotComAccount } from '../models/account'
import { updateStore, UpdateStatus } from './lib/update-store'
import { getVersion } from './lib/app-proxy'
import {
  getOS,
  isOSNoLongerSupportedByElectron,
  isMacOSAndNoLongerSupportedByElectron,
  isWindowsAndNoLongerSupportedByElectron,
} from '../lib/get-os'
import { isInApplicationFolder } from './main-process-proxy'
import * as ipcRenderer from '../lib/ipc-renderer'
import { isCertificateErrorSuppressedFor } from '../lib/suppress-certificate-error'
import { getNumber } from '../lib/local-storage'
import { offsetFromNow } from '../lib/offset-from'
import { UnsupportedOSBannerDismissedAtKey } from './banners/unsupported-os-state'
import {
  getUserContributions,
  hasUserAlreadyBeenCheckedOrThanked,
  updateLastThankYou,
} from '../lib/thank-you'

const FourHours = 4 * 60 * 60 * 1000

/**
 * Original App.tsx startup and platform notifications, without a view mount.
 * Both presentation hosts share its ordering and the original stores. Its
 * subscriptions and repeating work are released with the presentation host.
 */
export class AppStartup {
  private readonly subscriptions = new CompositeDisposable()
  private readonly intervals: Array<ReturnType<typeof setInterval>> = []
  private started = false
  private globalLFSFilters: Promise<void> | null = null
  private disposed = false

  public constructor(
    private readonly appStore: AppStore,
    private readonly dispatcher: Dispatcher,
    private readonly getState: () => IAppState,
    private readonly automaticUpdates = process.env.GITOWL_NATIVE_CORE !== '1'
  ) {
    this.subscriptions.add(
      updateStore.onDidChange(async state => {
        if (!this.automaticUpdates || this.disposed) {
          return
        }
        if (
          __RELEASE_CHANNEL__ !== 'development' &&
          state.status === UpdateStatus.UpdateReady
        ) {
          await this.dispatcher.setUpdateBannerVisibility(true)
        }
        if (
          state.status !== UpdateStatus.UpdateReady &&
          (await updateStore.isUpdateShowcase())
        ) {
          await this.dispatcher.setUpdateShowCaseVisibility(true)
        }
      })
    )
    this.subscriptions.add(
      updateStore.onError(error => {
        log.error('Error checking for updates', error)
        void this.dispatcher.postError(error)
      })
    )
    ipcRenderer.on('launch-timing-stats', this.onLaunchStats)
    ipcRenderer.on('certificate-error', this.onCertificateError)
  }

  private onLaunchStats = (
    _: Electron.IpcRendererEvent,
    stats: ILaunchStats
  ) => {
    console.info(`App ready time: ${stats.mainReadyTime}ms`)
    console.info(`Load time: ${stats.loadTime}ms`)
    console.info(`Renderer ready time: ${stats.rendererReadyTime}ms`)
    this.dispatcher.recordLaunchStats(stats)
  }

  private onCertificateError = (
    _: Electron.IpcRendererEvent,
    certificate: Electron.Certificate,
    _error: string,
    url: string
  ) => {
    if (!isCertificateErrorSuppressedFor(url)) {
      void this.dispatcher.showPopup({
        type: PopupType.UntrustedCertificate,
        certificate,
        url,
      })
    }
  }

  public dispose() {
    this.disposed = true
    for (const interval of this.intervals) {
      clearInterval(interval)
    }
    this.intervals.length = 0
    this.subscriptions.dispose()
    ipcRenderer.removeListener('launch-timing-stats', this.onLaunchStats)
    ipcRenderer.removeListener('certificate-error', this.onCertificateError)
  }

  /**
   * Settles when the startup `git lfs install --skip-repo` has finished writing
   * the global Git config. The original store reports its own errors; this
   * only orders later Git work after those writes.
   */
  public globalConfigSettled(): Promise<void> {
    return this.globalLFSFilters ?? Promise.resolve()
  }

  /** Call after renderer-ready; not before loading the original initial state. */
  public async start() {
    if (this.started || this.disposed) {
      return
    }
    this.started = true
    // Emoji must not delay first paint; its original loader owns errors/state.
    void this.appStore.loadEmoji()
    this.reportStats()
    this.intervals.push(setInterval(() => this.reportStats(), FourHours))
    this.globalLFSFilters = this.dispatcher.installGlobalLFSFilters(false)

    // The Electron Squirrel feed cannot update a MyGo/NSIS installation. Until
    // Gitowl publishes its own registered MyGo feed, no native auto-check is
    // scheduled and no Desktop release is presented as a Gitowl update.
    if (this.automaticUpdates) {
      if (
        __RELEASE_CHANNEL__ !== 'development' &&
        __RELEASE_CHANNEL__ !== 'test'
      ) {
        this.intervals.push(
          setInterval(() => this.checkForUpdates(true), FourHours)
        )
        this.checkForUpdates(true)
      } else if (await updateStore.isUpdateShowcase()) {
        await this.dispatcher.setUpdateShowCaseVisibility(true)
      }
    }
    log.info(`launching: ${getVersion()} (${getOS()})`)
    log.info(`execPath: '${process.execPath}'`)
    if (
      !this.disposed &&
      !__DEV__ &&
      this.getState().askToMoveToApplicationsFolderSetting &&
      __DARWIN__ &&
      (await isInApplicationFolder()) === false
    ) {
      await this.dispatcher.showPopup({
        type: PopupType.MoveToApplicationsFolder,
      })
    }
    if (!this.disposed) {
      await this.setOnOpenBanner()
    }
  }

  private reportStats() {
    void this.dispatcher
      .reportStats()
      .catch(error => this.dispatcher.postError(error))
  }

  public checkForUpdates(inBackground: boolean, skipGuidCheck = false) {
    if (!this.automaticUpdates) {
      if (!inBackground) {
        void this.dispatcher.postError(
          new Error(
            'Automatic updates are not configured for Gitowl. Install the fixed Gitowl test installer; GitHub Desktop updates cannot update this application.'
          )
        )
      }
      return
    }
    if (__LINUX__ || __RELEASE_CHANNEL__ === 'development') {
      return
    }
    if (isWindowsAndNoLongerSupportedByElectron()) {
      log.error(
        "Can't check for updates on Windows 8.1 or older. Next available update only supports Windows 10 and later"
      )
      return
    }
    if (isMacOSAndNoLongerSupportedByElectron()) {
      log.error(
        "Can't check for updates on macOS 12 or older. Next available update only supports macOS 13 and later"
      )
      return
    }
    void updateStore.checkForUpdates(inBackground, skipGuidCheck)
  }

  private async setOnOpenBanner() {
    if (isOSNoLongerSupportedByElectron()) {
      const dismissedAt = getNumber(UnsupportedOSBannerDismissedAtKey, 0)
      if (dismissedAt < offsetFromNow(-90, 'days')) {
        await this.dispatcher.setBanner({
          type: BannerType.OSVersionNoLongerSupported,
        })
        return
      }
    }
    await this.checkIfThankYouIsInOrder()
  }

  private async checkIfThankYouIsInOrder() {
    const state = this.getState()
    const account = state.accounts.find(isDotComAccount)
    if (
      account === undefined ||
      hasUserAlreadyBeenCheckedOrThanked(
        state.lastThankYou,
        account.login,
        getVersion()
      )
    ) {
      return
    }
    const { lastThankYou } = state
    const onlyLastRelease =
      lastThankYou !== undefined &&
      lastThankYou.checkedUsers.includes(account.login)
    const contributions = await getUserContributions(
      onlyLastRelease,
      account.login
    )
    if (this.disposed) {
      return
    }
    if (contributions === null) {
      updateLastThankYou(
        this.dispatcher,
        lastThankYou,
        account.login,
        getVersion()
      )
      return
    }
    const banner: Banner = {
      type: BannerType.OpenThankYouCard,
      // Keep the original current emoji map, which may still be loading.
      emoji: this.getState().emoji,
      onOpenCard: () =>
        this.dispatcher.showPopup({
          type: PopupType.ThankYou,
          userContributions: contributions,
          friendlyName: account.friendlyName,
          latestVersion: onlyLastRelease ? getVersion() : null,
        }),
      onThrowCardAway: () =>
        updateLastThankYou(
          this.dispatcher,
          lastThankYou,
          account.login,
          getVersion()
        ),
    }
    await this.dispatcher.setBanner(banner)
  }
}

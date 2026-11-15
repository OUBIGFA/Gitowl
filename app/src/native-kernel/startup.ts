import type { AppRuntime } from '../ui/app-runtime'
import { AppStartup } from '../ui/app-startup'
import { RepositoryIndicatorUpdater } from '../lib/stores/helpers/repository-indicator-updater'
import { Repository } from '../models/repository'

/** Shared original deferred-launch implementation, mounted without React. */
export class NativeStartup extends AppStartup {
  private readonly localIndicators: RepositoryIndicatorUpdater
  private localIndicatorsDisposed = false

  public constructor(runtime: Pick<AppRuntime, 'appStore' | 'dispatcher'>) {
    super(
      runtime.appStore,
      runtime.dispatcher,
      () => runtime.appStore.getState(),
      false
    )
    this.localIndicators = new RepositoryIndicatorUpdater(
      () =>
        runtime.appStore
          .getState()
          .repositories.filter(
            (repository): repository is Repository =>
              repository instanceof Repository
          ),
      async repository => {
        try {
          await runtime.dispatcher.refreshLocalRepositoryIndicator(repository)
        } catch (error) {
          await runtime.dispatcher.postError(error)
        }
      },
      5000,
      0
    )
  }

  public override async start() {
    await Promise.all([
      super.start(),
      this.globalConfigSettled().then(() => {
        if (!this.localIndicatorsDisposed) {
          this.localIndicators.start()
        }
      }),
    ])
  }

  public override dispose() {
    this.localIndicatorsDisposed = true
    this.localIndicators.stop()
    super.dispose()
  }
}

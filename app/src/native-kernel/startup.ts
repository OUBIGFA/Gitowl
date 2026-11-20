import type { AppRuntime } from '../ui/app-runtime'
import { AppStartup } from '../ui/app-startup'
import { NativeRepositoryWatcher } from './repository-watcher'
import { Repository } from '../models/repository'
import {
  hasRepositoryGitOperations,
  isRepositoryGitPaused,
} from '../lib/git/repository-operation'
import { Disposable } from 'event-kit'

/** Shared original deferred-launch implementation, mounted without React. */
export class NativeStartup extends AppStartup {
  private readonly localIndicators: NativeRepositoryWatcher
  private readonly repositorySubscription: Disposable
  private localIndicatorsDisposed = false

  public constructor(runtime: Pick<AppRuntime, 'appStore' | 'dispatcher'>) {
    super(
      runtime.appStore,
      runtime.dispatcher,
      () => runtime.appStore.getState(),
      false
    )
    const selected = () => {
      const repository = runtime.appStore.getState().selectedState?.repository
      return repository instanceof Repository ? repository : undefined
    }
    this.localIndicators = new NativeRepositoryWatcher(
      () =>
        runtime.appStore
          .getState()
          .repositories.filter(
            (repository): repository is Repository =>
              repository instanceof Repository
          ),
      selected,
      async (repository, metadata) => {
        try {
          if (selected()?.path === repository.path) {
            if (metadata) {
              await runtime.dispatcher.refreshRepository(repository)
            } else {
              await runtime.dispatcher.refreshWorkingDirectory(repository)
            }
          } else {
            await runtime.dispatcher.refreshLocalRepositoryIndicator(repository)
          }
        } catch (error) {
          await runtime.dispatcher.postError(error)
        }
      },
      repository =>
        hasRepositoryGitOperations(repository.path) ||
        isRepositoryGitPaused(repository.path),
      error =>
        log.warn(
          'Local change monitoring failed; periodic reconciliation remains enabled',
          error
        )
    )
    this.repositorySubscription = runtime.appStore.onDidUpdate(() =>
      this.localIndicators.updateRepositories()
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
    this.repositorySubscription.dispose()
    this.localIndicators.dispose()
    super.dispose()
  }
}

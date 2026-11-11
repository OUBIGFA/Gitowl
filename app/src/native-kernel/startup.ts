import type { AppRuntime } from '../ui/app-runtime'
import { AppStartup } from '../ui/app-startup'

/** Shared original deferred-launch implementation, mounted without React. */
export class NativeStartup extends AppStartup {
  public constructor(runtime: Pick<AppRuntime, 'appStore' | 'dispatcher'>) {
    super(
      runtime.appStore,
      runtime.dispatcher,
      () => runtime.appStore.getState(),
      false
    )
  }
}

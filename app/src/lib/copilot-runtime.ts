import { join } from 'path'

/** The standalone runtime entry point within a copied Copilot SDK package. */
export function getCopilotRuntimePath(
  runtimeRoot: string,
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch
): string {
  return join(
    runtimeRoot,
    'prebuilds',
    `${platform}-${arch}`,
    platform === 'win32' ? 'copilot-runtime.exe' : 'copilot-runtime'
  )
}

/** Select the SDK launch without changing the original runtime or its protocol. */
export function getCopilotRuntimeLaunch(
  runtimePath: string,
  nativeLauncher: string | undefined = process.env.GITOWL_COPILOT_LAUNCHER,
  platform: NodeJS.Platform = process.platform
): { path: string; args?: ReadonlyArray<string> } {
  // CREATE_NO_WINDOW alone leaves PowerShell hooks without a console, causing
  // their first native command to allocate and then hide a visible one. The
  // MyGo host supplies a hidden console from creation and owns the whole tree.
  if (platform === 'win32' && nativeLauncher) {
    return {
      path: nativeLauncher,
      args: ['--gitowl-copilot-runtime', runtimePath],
    }
  }
  return { path: runtimePath }
}

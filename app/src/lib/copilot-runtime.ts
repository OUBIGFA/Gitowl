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

/**
 * The application-owned Copilot home passed to the SDK as `baseDirectory`.
 *
 * The runtime otherwise uses the Copilot CLI's `~/.copilot`, whose user-level
 * hooks run in every session: `enableFileHooks: false` only covers repository
 * hooks. Those hook shells start with the SDK's reduced environment and open
 * visible terminal windows on Windows during commit message generation.
 * Sign-in is passed per client and sessions use the in-memory filesystem, so
 * nothing else is read from that directory.
 */
export function getCopilotBaseDirectory(userDataPath: string): string {
  return join(userDataPath, 'copilot')
}

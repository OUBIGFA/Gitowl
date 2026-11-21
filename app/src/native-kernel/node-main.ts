import '../lib/hide-child-process-windows'
import { Console } from 'console'
import { createHash } from 'crypto'
import { mkdirSync } from 'fs'
import { isAbsolute, join } from 'path'
import { isatty } from 'tty'
import { installDesktopHost } from '../lib/desktop-host'
import { installNodeDatabase } from './node-database'

// Stdout belongs to the native protocol. Runtime diagnostics stay on the
// inherited stderr pipe, already persisted by the MyGo process owner.
const logger = new Console(process.stderr, process.stderr)
Object.assign(globalThis, { console: logger, log: logger })
const userData = process.env.GITOWL_USER_DATA
if (!userData || !isAbsolute(userData) || isatty(0)) {
  throw new Error(
    'The Node kernel requires an owned pipe and absolute application data path'
  )
}
mkdirSync(userData, { recursive: true })
process.env.GITOWL_NATIVE_CORE = '1'
process.env.GITOWL_NODE_CORE = '1'
// Preserve the installed product's credential namespace and the existing
// isolated-profile test namespace. Never fall back to upstream credentials.
process.env.GITOWL_CREDENTIAL_NAMESPACE =
  process.env.TEST_ENV === '1'
    ? `io.github.oubigfa.gitowl.test.${createHash('sha256')
        .update(join(userData, 'desktop-kernel'))
        .digest('hex')}`
    : 'io.github.oubigfa.gitowl'
installNodeDatabase(join(userData, 'backend-data'))

async function main() {
  const { NodeKernelTransport } = await import('./node-transport')
  const { NodeDesktopHost } = await import('./node-host')
  // EOF can arrive while runtime imports are still pending; the callback must
  // see an initialized undefined value until its real owner has been created.
  // eslint-disable-next-line prefer-const
  let dispose: (() => Promise<void>) | undefined
  const transport = new NodeKernelTransport(
    process.stdin,
    process.stdout,
    code => {
      void Promise.resolve(dispose?.()).then(
        () => process.exit(code),
        error => {
          log.error('Node kernel could not settle during shutdown', error)
          process.exit(1)
        }
      )
    }
  )
  // Keep non-database paths (recovery journals, Copilot home, logs and GUID)
  // exactly where the old business runtime stored them. This user-data folder
  // contains no executable runtime and must not be confused with install files.
  const businessData = join(userData!, 'desktop-kernel')
  mkdirSync(businessData, { recursive: true })
  installDesktopHost(new NodeDesktopHost(businessData, transport))
  const { installNativeNetwork } = await import('./node-network')
  installNativeNetwork()
  const { startNodeRuntime } = await import('./node-runtime')
  process.on('uncaughtException', error => {
    log.error('Node kernel failed', error)
    transport.fatal()
  })
  dispose = startNodeRuntime(transport)
}
void main().catch(error => {
  log.error('Node kernel could not initialize', error)
  process.exitCode = 1
})

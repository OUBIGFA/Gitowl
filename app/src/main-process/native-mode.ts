import { Console } from 'console'
import { createHash } from 'crypto'
import { isAbsolute, join } from 'path'
import { createReadStream, mkdirSync } from 'fs'
import { isatty } from 'tty'
import { app } from 'electron'

/** Explicit internal launch mode; ordinary upstream launches remain unchanged. */
export const nativeKernelMode = process.argv.includes('--gitowl-native-core')

if (nativeKernelMode) {
  const dataDirectory = process.env.GITOWL_USER_DATA
  if (!dataDirectory || !isAbsolute(dataDirectory) || isatty(0)) {
    throw new Error(
      'The native kernel requires an owned pipe and an absolute Gitowl data directory'
    )
  }
  const kernelData = join(dataDirectory, 'desktop-kernel')
  // This profile belongs to Gitowl. Do not read or move an installed upstream
  // Desktop profile to make a native-frontend probe look authenticated.
  mkdirSync(kernelData, { recursive: true })
  app.setPath('userData', kernelData)
  app.setName('Gitowl')
  // The kernel window is hidden, yet it serves the visible native window.
  // Chromium otherwise treats its processes as background work: renderers
  // drop to IDLE_PRIORITY_CLASS and every non-blocking child gets EcoQoS.
  // When efficient cores are saturated those processes starve, and quitting
  // stalls in Chromium's bounded wait for the network service to flush.
  app.commandLine.appendSwitch('disable-renderer-backgrounding')
  app.commandLine.appendSwitch(
    'disable-features',
    'UseEcoQoSForBackgroundProcess'
  )
  process.env.GITOWL_NATIVE_CORE = '1'
  // Keep the original credential/session algorithms, but give this product its
  // own keytar resource names. Disposable test profiles never touch a user's
  // installed Gitowl or upstream Desktop credentials either.
  process.env.GITOWL_CREDENTIAL_NAMESPACE =
    process.env.TEST_ENV === '1'
      ? `io.github.oubigfa.gitowl.test.${createHash('sha256')
          .update(kernelData)
          .digest('hex')}`
      : 'io.github.oubigfa.gitowl'
  // Original logging may use console; stdout is reserved for the native pipe.
  Object.assign(globalThis, {
    console: new Console(process.stderr, process.stderr),
  })
}

// Electron v44's lib/common/init.ts replaces process.stdin with an EOF-only
// Readable on Windows. Read the inherited owned fd itself; never remove that
// Electron guard or turn the original kernel into ELECTRON_RUN_AS_NODE.
export const nativeKernelInput =
  nativeKernelMode && process.platform === 'win32'
    ? createReadStream('', { fd: 0, autoClose: false })
    : process.stdin

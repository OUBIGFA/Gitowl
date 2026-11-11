import { Console } from 'console'

// Stdout is a framed protocol, never a log sink. This is the production logger,
// not a browser shim: the native parent owns stderr and the persistent log file.
Object.assign(globalThis, { log: new Console(process.stderr, process.stderr) })

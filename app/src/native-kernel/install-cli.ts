import { Console } from 'console'
import { handleNativeCLIEvent } from '../main-process/squirrel-updater'
Object.assign(globalThis, { log: new Console(process.stderr, process.stderr) })
const operation = handleNativeCLIEvent(process.argv)
if (operation === null)
  {throw new Error('Expected an explicit CLI registration operation')}
void operation.catch(error => {
  console.error(error)
  process.exitCode = 1
})

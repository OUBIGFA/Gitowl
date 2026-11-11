import * as Path from 'path'

/** MyGo resources are not a Squirrel version directory. Keep the CLI in this
 * installation's bin and never overwrite an installed Desktop command. */
export function nativeCLIPaths(executable: string) {
  const kernel = Path.dirname(executable)
  const root = Path.resolve(kernel, '..')
  return {
    bin: Path.join(root, 'bin'),
    command: 'gitowl',
    batch: Path.join(kernel, 'app/static/github.bat'),
    shell: Path.join(kernel, 'app/static/github.sh'),
  }
}

import * as Path from 'path'

/** Original RepositoryPath filesystem-name policy (not GitHub-name policy). */
export const safeDirectoryName = (name: string) =>
  __WIN32__ ? name.replace(/[<>:"|?*]/g, '-').replace(/\s+$/, '') : name

/** Both presenters resolve name + base directory through the same policy. */
export function repositoryFullPath(
  name: string,
  path: string | null
): string | null {
  return path === null || path.length === 0 || name.trim().length === 0
    ? null
    : Path.join(path, safeDirectoryName(name))
}

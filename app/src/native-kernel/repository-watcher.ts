import { watch, FSWatcher } from 'fs'
import { readFile } from 'fs/promises'
import { isAbsolute, relative, resolve, sep } from 'path'
import { Repository } from '../models/repository'

interface IWatchedRepository {
  repository: Repository
  readonly roots: string
  readonly watchers: FSWatcher[]
  dirty: boolean
  metadata: boolean
  healthy: boolean
  attaching: boolean
}

/** Native change notifications replace repeated scans of every idle project.
 * A slow reconciliation also covers lost events, unavailable disks and shares.
 * Git remains the source of truth; notifications never predict repository state.
 */
export class NativeRepositoryWatcher {
  private readonly entries = new Map<string, IWatchedRepository>()
  private timer: ReturnType<typeof setTimeout> | undefined
  private reconciliation: ReturnType<typeof setInterval> | undefined
  private running = false
  private disposed = false
  private refreshing = false
  private selectedPath: string | undefined
  private reconciliations = 0

  public constructor(
    private readonly repositories: () => ReadonlyArray<Repository>,
    private readonly selected: () => Repository | undefined,
    private readonly refresh: (
      repository: Repository,
      metadata: boolean
    ) => Promise<void>,
    private readonly busy: (repository: Repository) => boolean,
    private readonly onError: (error: Error) => void
  ) {}

  public start(): void {
    if (this.running || this.disposed) {
      return
    }
    this.running = true
    this.updateRepositories()
    this.reconciliation = setInterval(() => {
      this.updateRepositories()
      // Lost file events need a cheap status read, not every branch, stash,
      // tag and worktree. Reconcile that metadata every fifth minute as well.
      const full = ++this.reconciliations % 5 === 0
      for (const entry of this.entries.values()) {
        entry.metadata ||= full || !entry.healthy
        if (!entry.healthy) {
          void this.attach(entry)
        }
        entry.dirty = true
      }
      this.schedule()
    }, 60000)
    this.reconciliation.unref?.()
  }

  public updateRepositories(): void {
    if (!this.running) {
      return
    }
    const repositories = this.repositories()
    const paths = new Set(repositories.map(repository => repository.path))
    for (const [path, entry] of this.entries) {
      if (!paths.has(path)) {
        entry.watchers.forEach(watcher => watcher.close())
        this.entries.delete(path)
      }
    }
    for (const repository of repositories) {
      const roots = `${repository.path}\0${repository.resolvedGitDir}`
      let entry = this.entries.get(repository.path)
      if (entry !== undefined && entry.roots !== roots) {
        entry.watchers.forEach(watcher => watcher.close())
        this.entries.delete(repository.path)
        entry = undefined
      }
      if (entry === undefined) {
        entry = {
          repository,
          roots,
          watchers: [],
          dirty: true,
          metadata: true,
          healthy: true,
          attaching: false,
        }
        this.entries.set(repository.path, entry)
        void this.attach(entry)
      } else {
        entry.repository = repository
      }
    }
    const selected = this.selected()?.path
    if (selected !== this.selectedPath) {
      this.selectedPath = selected
      const entry =
        selected === undefined ? undefined : this.entries.get(selected)
      if (entry !== undefined) {
        entry.dirty = entry.metadata = true
      }
    }
    this.schedule()
  }

  private async attach(entry: IWatchedRepository): Promise<void> {
    if (entry.attaching) {
      return
    }
    entry.attaching = true
    entry.watchers.forEach(watcher => watcher.close())
    entry.watchers.length = 0
    entry.healthy = true
    const root = resolve(entry.repository.path)
    const gitDir = resolve(entry.repository.resolvedGitDir)
    const roots = new Map<string, boolean>([[root, false]])
    const addMetadataRoot = (path: string) => {
      const child = relative(root, path)
      if (isAbsolute(child) || child === '..' || child.startsWith(`..${sep}`)) {
        roots.set(path, true)
      }
    }
    addMetadataRoot(gitDir)
    // Linked worktrees keep refs/config in a separate common directory.
    try {
      const common = (
        await readFile(resolve(gitDir, 'commondir'), 'utf8')
      ).trim()
      if (common.length > 0) {
        addMetadataRoot(resolve(gitDir, common))
      }
    } catch (error) {
      if (
        (error as NodeJS.ErrnoException).code !== 'ENOENT' &&
        (error as NodeJS.ErrnoException).code !== 'ENOTDIR'
      ) {
        this.onError(error as Error)
      }
    }
    entry.attaching = false
    if (!this.running || this.entries.get(entry.repository.path) !== entry) {
      return
    }
    for (const [path, metadataRoot] of roots) {
      try {
        const watcher = watch(
          path,
          { recursive: true, persistent: false },
          (_event, filename) => {
            if (
              !this.running ||
              this.entries.get(entry.repository.path) !== entry
            ) {
              return
            }
            const name = filename?.toString().replace(/\\/g, '/')
            const metadata =
              metadataRoot ||
              name === undefined ||
              name === '.git' ||
              name.startsWith('.git/')
            // Object downloads, reflogs and lock files do not change the visible
            // state. Their final refs/index updates will deliver the useful event.
            const gitPath = metadataRoot ? name : name?.replace(/^\.git\//, '')
            if (
              metadata &&
              gitPath !== undefined &&
              (/^(objects|logs|lfs)\//.test(gitPath) ||
                gitPath.endsWith('.lock'))
            ) {
              return
            }
            entry.dirty = true
            entry.metadata ||= metadata
            this.schedule()
          }
        )
        watcher.on('error', error => {
          entry.healthy = false
          watcher.close()
          this.onError(error)
        })
        entry.watchers.push(watcher)
      } catch (error) {
        entry.healthy = false
        this.onError(error as Error)
      }
    }
  }

  private schedule(): void {
    if (
      !this.running ||
      this.timer !== undefined ||
      this.refreshing ||
      ![...this.entries.values()].some(entry => entry.dirty)
    ) {
      return
    }
    // One bounded batch per burst. A continuous stream cannot postpone it forever.
    this.timer = setTimeout(() => {
      this.timer = undefined
      void this.flush()
    }, 200)
    this.timer.unref?.()
  }

  private async flush(): Promise<void> {
    if (!this.running) {
      return
    }
    this.refreshing = true
    try {
      const entries = [...this.entries.values()].sort(
        (a, b) =>
          Number(b.repository.path === this.selectedPath) -
          Number(a.repository.path === this.selectedPath)
      )
      for (const entry of entries) {
        if (
          !this.running ||
          this.entries.get(entry.repository.path) !== entry ||
          entry.attaching ||
          !entry.dirty ||
          this.busy(entry.repository)
        ) {
          continue
        }
        const metadata = entry.metadata
        entry.dirty = entry.metadata = false
        try {
          await this.refresh(entry.repository, metadata)
        } catch (error) {
          this.onError(error as Error)
        }
      }
    } finally {
      this.refreshing = false
      // A notification received during the scan still requires another read.
      this.schedule()
    }
  }

  public dispose(): void {
    this.disposed = true
    this.running = false
    clearTimeout(this.timer)
    clearInterval(this.reconciliation)
    for (const entry of this.entries.values()) {
      entry.watchers.forEach(watcher => watcher.close())
    }
    this.entries.clear()
  }
}

import type { AppRuntime } from '../ui/app-runtime'
import { IAppState, SelectionType } from '../lib/app-state'
import { Repository } from '../models/repository'
import {
  inspectMissingRepository,
  missingRepositoryCloneURL,
  checkMissingRepository,
  locateMissingRepository,
  removeMissingRepository,
  cloneMissingRepository,
  trustMissingRepository,
} from '../ui/missing-repository-controller'

export type MissingRepositoryAction =
  | 'check'
  | 'locate'
  | 'clone'
  | 'remove'
  | 'trust'
export interface IKernelMissingRepository {
  readonly repository: {
    readonly id: number
    readonly path: string
    readonly name: string
  }
  readonly checking: boolean
  readonly busy: boolean
  readonly unsafePath: string | null
  readonly canClone: boolean
  readonly error: string | null
}

/** Owns only the original missing view's async inspection/loading facts.
 * Repository identity and every recovery decision remain in Desktop. */
export class NativeMissingRepositoryPresenter {
  private repository: Repository | null = null
  private generation = 0
  private view: IKernelMissingRepository | null = null

  public constructor(
    private readonly runtime: AppRuntime,
    private readonly changed: () => void
  ) {}

  public dispose() {
    this.generation++
    this.repository = null
    this.view = null
  }

  public project(state: IAppState): IKernelMissingRepository | null {
    const selected = state.selectedState
    if (selected?.type !== SelectionType.MissingRepository) {
      this.dispose()
      return null
    }
    const repository = selected.repository
    if (
      this.repository?.id === repository.id &&
      this.repository.path === repository.path
    ) {
      this.repository = repository
      this.view = {
        ...this.view!,
        canClone: !!missingRepositoryCloneURL(repository),
      }
      return this.view
    }
    this.repository = repository
    const generation = ++this.generation
    this.view = {
      repository: {
        id: repository.id,
        path: repository.path,
        name: repository.name,
      },
      checking: true,
      busy: false,
      unsafePath: null,
      canClone: !!missingRepositoryCloneURL(repository),
      error: null,
    }
    // No synchronous Git/read in snapshot projection. An old path's result
    // must not update a newly selected repository or linked worktree.
    void inspectMissingRepository(repository).then(
      type => {
        if (!this.owns(repository, generation)) {
          return
        }
        this.view = {
          ...this.view!,
          checking: false,
          unsafePath: type.kind === 'unsafe' ? type.path : null,
        }
        this.changed()
      },
      error => {
        if (!this.owns(repository, generation)) {
          return
        }
        this.view = { ...this.view!, checking: false, error: String(error) }
        this.changed()
      }
    )
    return this.view
  }

  private owns(repository: Repository, generation = this.generation) {
    const selected = this.runtime.appStore.getState().selectedState
    return (
      generation === this.generation &&
      this.repository?.id === repository.id &&
      this.repository.path === repository.path &&
      selected?.type === SelectionType.MissingRepository &&
      selected.repository.id === repository.id &&
      selected.repository.path === repository.path
    )
  }

  public async execute(
    reference: { readonly id: number; readonly path: string },
    action: MissingRepositoryAction
  ) {
    this.project(this.runtime.appStore.getState())
    const repository = this.repository
    if (
      !repository ||
      reference.id !== repository.id ||
      reference.path !== repository.path ||
      !this.owns(repository) ||
      this.view?.busy
    ) {
      throw new Error(
        'The missing repository changed; reopen its recovery view'
      )
    }
    if (
      (action === 'locate' || action === 'clone') &&
      (this.view?.checking || this.view?.unsafePath)
    ) {
      throw new Error(
        'Inspect the repository path before choosing this recovery action'
      )
    }
    if (action === 'clone' && !missingRepositoryCloneURL(repository)) {
      throw new Error('This repository has no original clone URL')
    }
    const generation = this.generation
    this.view = { ...this.view!, busy: true, error: null }
    this.changed()
    try {
      const dispatcher = this.runtime.dispatcher
      switch (action) {
        case 'check':
          await checkMissingRepository(dispatcher, repository)
          break
        case 'locate':
          await locateMissingRepository(dispatcher, repository)
          break
        case 'clone':
          await cloneMissingRepository(dispatcher, repository)
          break
        case 'remove':
          await removeMissingRepository(dispatcher, repository)
          break
        case 'trust': {
          const shownPath = this.view.unsafePath
          if (!shownPath) {
            throw new Error('There is no detected unsafe directory to trust')
          }
          const detected = await inspectMissingRepository(repository)
          if (
            !this.owns(repository, generation) ||
            detected.kind !== 'unsafe' ||
            detected.path !== shownPath
          ) {
            throw new Error(
              'The detected unsafe directory changed; check it again'
            )
          }
          await trustMissingRepository(dispatcher, repository, shownPath)
          break
        }
      }
    } catch (error) {
      if (this.owns(repository, generation)) {
        this.view = { ...this.view!, error: String(error) }
      }
      throw error
    } finally {
      if (this.owns(repository, generation)) {
        this.view = { ...this.view!, busy: false }
        this.changed()
      }
    }
  }
}

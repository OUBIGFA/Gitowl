import type {
  INativeMediaCommand,
  IKernelMediaChunk,
} from './media-presentation'
import type { IKernelRepositoryMenu } from './repository-menu'
import type {
  IKernelMissingRepository,
  MissingRepositoryAction,
} from './missing-repository-presenter'
import type { ICommitContext } from '../models/commit'
import type { ICommitMessage } from '../models/commit-message'
import type { INativeRepositoryReference } from './desktop-presenter'
import type { KernelPopupDTO } from './popup-presenter'
import type {
  IKernelOnboardingView,
  IKernelSignInView,
} from './onboarding-presenter'
import type { WelcomeStep } from '../ui/welcome/welcome-step'
import type { INativeWorkspaceCommand } from './workspace-protocol'
import type { IKernelWorkspaceDTO } from './workspace-projection'
import type { NativeCommitCommand } from './commit-protocol'
import type {
  KernelCommitView,
  KernelAssistedReceipt,
} from './commit-presenter'
import type { IKernelMenuDTO } from './menu-presenter'
import type {
  NativeGenerationCommand,
  KernelGenerationSettings,
} from './generation-presenter'
import type {
  IIncludeDiffLinesCommand,
  IKernelDiffSelection,
} from './line-selection'
import type {
  INativeImagePresentationCommand,
  INativeImagePresentation,
  IKernelImage,
} from './image-presentation'
import type { ApplicationTheme } from '../ui/lib/application-theme'
import type { CLIAction } from '../lib/cli-action'

/** Only presentation requests are admitted; no reflected Dispatcher methods. */
export type NativeKernelCommand =
  | INativeMediaCommand
  | INativeWorkspaceCommand
  | INativeImagePresentationCommand
  | NativeCommitCommand
  | { readonly name: 'menu.execute'; readonly id: string }
  | NativeGenerationCommand
  | IIncludeDiffLinesCommand
  | { readonly name: 'presentation.commit-message-focused' }
  | { readonly name: 'presentation.resizable-focus'; readonly active: boolean }
  | { readonly name: 'presentation.repository-list-focused' }
  | { readonly name: 'state' }
  | { readonly name: 'repositories.add'; readonly paths: ReadonlyArray<string> }
  | { readonly name: 'repositories.filter'; readonly text: string }
  | {
      readonly name: 'repository.menu'
      readonly repository: INativeRepositoryReference
      readonly lease: string
      readonly path: ReadonlyArray<number>
    }
  | {
      readonly name: 'repository.select'
      readonly repository: INativeRepositoryReference
    }
  | {
      readonly name: 'repository.refresh'
      readonly repository: INativeRepositoryReference
    }
  | {
      readonly name: 'repository.missing'
      readonly repository: INativeRepositoryReference
      readonly action: MissingRepositoryAction
    }
  | {
      readonly name: 'repository.section'
      readonly repository: INativeRepositoryReference
      readonly section: 'changes' | 'history'
    }
  | {
      readonly name: 'repository.files.select'
      readonly repository: INativeRepositoryReference
      readonly fileIDs: ReadonlyArray<string>
    }
  | {
      readonly name: 'repository.files.include'
      readonly repository: INativeRepositoryReference
      readonly fileIDs: ReadonlyArray<string>
      readonly included: boolean
    }
  | {
      readonly name: 'repository.files.includeAll'
      readonly repository: INativeRepositoryReference
      readonly included: boolean
    }
  | {
      readonly name: 'repository.message'
      readonly repository: INativeRepositoryReference
      readonly message: ICommitMessage
    }
  | {
      readonly name: 'repository.commit'
      readonly repository: INativeRepositoryReference
      readonly context: ICommitContext
    }
  | {
      readonly name: 'repository.generate'
      readonly repository: INativeRepositoryReference
      readonly fileIDs: ReadonlyArray<string>
      readonly overrideExistingMessage: boolean
    }
  | {
      readonly name: 'repository.generate.cancel'
      readonly repository: INativeRepositoryReference
    }
  | {
      readonly name: 'repository.fetch'
      readonly repository: INativeRepositoryReference
    }
  | {
      readonly name: 'repository.pull'
      readonly repository: INativeRepositoryReference
    }
  | {
      readonly name: 'repository.push'
      readonly repository: INativeRepositoryReference
    }
  | {
      readonly name: 'history.loadNext'
      readonly repository: INativeRepositoryReference
    }
  | {
      readonly name: 'history.summaries'
      readonly repository: INativeRepositoryReference
      readonly shas: ReadonlyArray<string>
    }
  | {
      readonly name: 'history.select'
      readonly repository: INativeRepositoryReference
      readonly shas: ReadonlyArray<string>
      readonly contiguous: boolean
    }
  | {
      readonly name: 'history.file.select'
      readonly repository: INativeRepositoryReference
      readonly fileID: string
    }
  | { readonly name: 'popup.reply'; readonly reply: unknown }
  | { readonly name: 'popup.close'; readonly popupID: number }
  | { readonly name: 'signIn.dotcom' }
  | { readonly name: 'signIn.enterprise'; readonly endpoint?: string }
  | { readonly name: 'signIn.endpoint'; readonly endpoint: string }
  | { readonly name: 'signIn.browser' }
  | { readonly name: 'signIn.reset' }
  | { readonly name: 'welcome.advance'; readonly step: WelcomeStep }
  | {
      readonly name: 'welcome.configureGit'
      readonly nameValue: string
      readonly email: string
      readonly useAccount: boolean
    }
  | { readonly name: 'launch.cli'; readonly action: CLIAction }
  | { readonly name: 'launch.url'; readonly url: string }
  | { readonly name: 'welcome.unlockGit' }

export interface INativeKernelRequest {
  readonly id: number
  readonly command: NativeKernelCommand
}

export interface IKernelRepository extends INativeRepositoryReference {
  readonly name: string
  readonly alias: string | null
  readonly missing: boolean
  readonly login: string | null
  readonly kind: 'repository' | 'cloning'
  /**
   * The original repository list's local state: the current branch's
   * ahead/behind (null without a tracking branch) and uncommitted file count.
   * Present only in the repository list; absent until Desktop has read it.
   */
  readonly local?: {
    readonly ahead: number | null
    readonly behind: number | null
    readonly changedFiles: number
  }
}

export interface IKernelFile {
  readonly id: string
  readonly path: string
  readonly status: string
  readonly oldPath: string | null
  readonly inclusion: 'all' | 'none' | 'partial'
  readonly isSubmodule: boolean
}

export interface IKernelDiff {
  readonly kind:
    | 'text'
    | 'large-text'
    | 'image'
    | 'binary'
    | 'submodule'
    | 'unrenderable'
  readonly text?: string
  readonly previous?: IKernelImage
  readonly current?: IKernelImage
  readonly submodule?: {
    readonly path: string
    readonly url: string | null
    readonly repositoryURL: string | null
    readonly oldSHA: string | null
    readonly newSHA: string | null
    readonly status: {
      readonly commitChanged: boolean
      readonly modifiedChanges: boolean
      readonly untrackedChanges: boolean
    }
  }
}

/** Whitelisted view state only; never serialize a raw IAppState or Account. */
export interface INativeKernelSnapshot extends INativeImagePresentation {
  readonly protocol: 1
  readonly core: 'desktop/desktop'
  /** Remains false until all frontend contracts have been implemented and accepted. */
  readonly presentationComplete: false
  readonly focusCommitMessage?: boolean
  readonly revision: number
  /** Original global preference state, including immediate preview/rollback. */
  readonly appearance?: {
    readonly theme: ApplicationTheme
    readonly tabSize: number
  }
  readonly commitGeneration?: KernelGenerationSettings | null
  readonly menu?: IKernelMenuDTO
  readonly workspace?: IKernelWorkspaceDTO
  readonly repositories: ReadonlyArray<IKernelRepository>
  readonly repositoryMenus?: ReadonlyArray<IKernelRepositoryMenu>
  readonly repositoryFilter?: {
    readonly text: string
    readonly paths: ReadonlyArray<string>
  }
  readonly accounts: ReadonlyArray<{
    readonly endpoint: string
    readonly login: string
    readonly name: string
    readonly copilotEnabled: boolean
  }>
  readonly missingRepository?: IKernelMissingRepository | null
  readonly cloningRepository?: {
    readonly repository: IKernelRepository
    readonly progress: {
      readonly value: number
      readonly description: string | null
    }
  } | null
  readonly selected: {
    readonly repository: IKernelRepository
    readonly section: 'changes' | 'history'
    readonly branch: string | null
    readonly upstream: string | null
    readonly headSHA: string | null
    readonly hasRemote: boolean
    readonly forcePushRecommended?: boolean
    readonly ahead: number
    readonly behind: number
    readonly files: ReadonlyArray<IKernelFile>
    readonly selectedFileIDs: ReadonlyArray<string>
    readonly message: ICommitMessage
    readonly commitView?: KernelCommitView | null
    readonly diff: IKernelDiff | null
    readonly diffSelection?: IKernelDiffSelection | null
    readonly history: ReadonlyArray<{
      readonly sha: string
      readonly shortSha: string
      readonly summary: string
      readonly body: string
      readonly authorName: string
      readonly authorEmail: string
      readonly date: string
      readonly dateLabels?: Readonly<
        Record<string, { readonly text: string; readonly absolute: string }>
      >
      readonly dateRefreshAfter?: number
      readonly tags: ReadonlyArray<string>
    }>
    readonly historySelection: ReadonlyArray<string>
    readonly historyFiles: ReadonlyArray<IKernelFile>
    readonly historyFileID: string | null
    readonly historyLinesAdded: number
    readonly historyLinesDeleted: number
    readonly commitInProgress: boolean
    readonly generationInProgress: boolean
    readonly generationAvailable: boolean
    readonly remoteInProgress: boolean
    readonly conflictKind: string | null
  } | null
  readonly popups: ReadonlyArray<{
    readonly id: number | null
    readonly type: string
  }>
  readonly currentPopup: KernelPopupDTO | null
  readonly bannerType: string | null
  readonly foldoutType: number | null
  readonly welcome: boolean
  readonly onboarding: IKernelOnboardingView | null
  readonly signIn: IKernelSignInView | null
  readonly errorCount: number
}

export type NativeKernelResponse =
  | {
      readonly id: number
      readonly result: {
        readonly mediaChunk?: IKernelMediaChunk
        readonly snapshot: INativeKernelSnapshot
        readonly committed?: boolean
        readonly assisted?: KernelAssistedReceipt
        readonly addedRepositories?: ReadonlyArray<INativeRepositoryReference>
      }
    }
  | {
      readonly id: number
      readonly error: { readonly code: number; readonly message: string }
    }

export type NativeWindowAction =
  | 'show'
  | 'focus'
  | 'restore'
  | 'minimize'
  | 'maximize'
  | 'unmaximize'
  | 'close'
  | 'select-all'
  | 'attention'

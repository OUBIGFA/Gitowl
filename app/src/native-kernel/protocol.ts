import type { NativeMediaCommand, KernelMediaChunk } from './media-presentation'
import type { KernelRepositoryMenu } from './repository-menu'
import type {
  KernelMissingRepository,
  MissingRepositoryAction,
} from './missing-repository-presenter'
import type { ICommitContext } from '../models/commit'
import type { ICommitMessage } from '../models/commit-message'
import type { NativeRepositoryReference } from './desktop-presenter'
import type { KernelPopupDTO } from './popup-presenter'
import type {
  KernelOnboardingView,
  KernelSignInView,
} from './onboarding-presenter'
import type { WelcomeStep } from '../ui/welcome/welcome-step'
import type { NativeWorkspaceCommand } from './workspace-protocol'
import type { KernelWorkspaceDTO } from './workspace-projection'
import type { NativeCommitCommand } from './commit-protocol'
import type {
  KernelCommitView,
  KernelAssistedReceipt,
} from './commit-presenter'
import type { KernelMenuDTO } from './menu-presenter'
import type {
  NativeGenerationCommand,
  KernelGenerationSettings,
} from './generation-presenter'
import type {
  IncludeDiffLinesCommand,
  KernelDiffSelection,
} from './line-selection'
import type {
  NativeImagePresentationCommand,
  NativeImagePresentation,
  KernelImage,
} from './image-presentation'
import type { ApplicationTheme } from '../ui/lib/application-theme'
import type { CLIAction } from '../lib/cli-action'

/** Only presentation requests are admitted; no reflected Dispatcher methods. */
export type NativeKernelCommand =
  | NativeMediaCommand
  | NativeWorkspaceCommand
  | NativeImagePresentationCommand
  | NativeCommitCommand
  | { readonly name: 'menu.execute'; readonly id: string }
  | NativeGenerationCommand
  | IncludeDiffLinesCommand
  | { readonly name: 'presentation.commit-message-focused' }
  | { readonly name: 'presentation.resizable-focus'; readonly active: boolean }
  | { readonly name: 'presentation.repository-list-focused' }
  | { readonly name: 'state' }
  | { readonly name: 'repositories.add'; readonly paths: ReadonlyArray<string> }
  | { readonly name: 'repositories.filter'; readonly text: string }
  | {
      readonly name: 'repository.menu'
      readonly repository: NativeRepositoryReference
      readonly lease: string
      readonly path: ReadonlyArray<number>
    }
  | {
      readonly name: 'repository.select'
      readonly repository: NativeRepositoryReference
    }
  | {
      readonly name: 'repository.refresh'
      readonly repository: NativeRepositoryReference
    }
  | {
      readonly name: 'repository.missing'
      readonly repository: NativeRepositoryReference
      readonly action: MissingRepositoryAction
    }
  | {
      readonly name: 'repository.section'
      readonly repository: NativeRepositoryReference
      readonly section: 'changes' | 'history'
    }
  | {
      readonly name: 'repository.files.select'
      readonly repository: NativeRepositoryReference
      readonly fileIDs: ReadonlyArray<string>
    }
  | {
      readonly name: 'repository.files.include'
      readonly repository: NativeRepositoryReference
      readonly fileIDs: ReadonlyArray<string>
      readonly included: boolean
    }
  | {
      readonly name: 'repository.files.includeAll'
      readonly repository: NativeRepositoryReference
      readonly included: boolean
    }
  | {
      readonly name: 'repository.message'
      readonly repository: NativeRepositoryReference
      readonly message: ICommitMessage
    }
  | {
      readonly name: 'repository.commit'
      readonly repository: NativeRepositoryReference
      readonly context: ICommitContext
    }
  | {
      readonly name: 'repository.generate'
      readonly repository: NativeRepositoryReference
      readonly fileIDs: ReadonlyArray<string>
      readonly overrideExistingMessage: boolean
    }
  | {
      readonly name: 'repository.generate.cancel'
      readonly repository: NativeRepositoryReference
    }
  | {
      readonly name: 'repository.fetch'
      readonly repository: NativeRepositoryReference
    }
  | {
      readonly name: 'repository.pull'
      readonly repository: NativeRepositoryReference
    }
  | {
      readonly name: 'repository.push'
      readonly repository: NativeRepositoryReference
    }
  | {
      readonly name: 'history.loadNext'
      readonly repository: NativeRepositoryReference
    }
  | {
      readonly name: 'history.summaries'
      readonly repository: NativeRepositoryReference
      readonly shas: ReadonlyArray<string>
    }
  | {
      readonly name: 'history.select'
      readonly repository: NativeRepositoryReference
      readonly shas: ReadonlyArray<string>
      readonly contiguous: boolean
    }
  | {
      readonly name: 'history.file.select'
      readonly repository: NativeRepositoryReference
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

export interface NativeKernelRequest {
  readonly id: number
  readonly command: NativeKernelCommand
}

export interface KernelRepository extends NativeRepositoryReference {
  readonly name: string
  readonly alias: string | null
  readonly missing: boolean
  readonly login: string | null
  readonly kind: 'repository' | 'cloning'
}

export interface KernelFile {
  readonly id: string
  readonly path: string
  readonly status: string
  readonly oldPath: string | null
  readonly inclusion: 'all' | 'none' | 'partial'
  readonly isSubmodule: boolean
}

export interface KernelDiff {
  readonly kind:
    | 'text'
    | 'large-text'
    | 'image'
    | 'binary'
    | 'submodule'
    | 'unrenderable'
  readonly text?: string
  readonly previous?: KernelImage
  readonly current?: KernelImage
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
export interface NativeKernelSnapshot extends NativeImagePresentation {
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
  readonly menu?: KernelMenuDTO
  readonly workspace?: KernelWorkspaceDTO
  readonly repositories: ReadonlyArray<KernelRepository>
  readonly repositoryMenus?: ReadonlyArray<KernelRepositoryMenu>
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
  readonly missingRepository?: KernelMissingRepository | null
  readonly cloningRepository?: {
    readonly repository: KernelRepository
    readonly progress: {
      readonly value: number
      readonly description: string | null
    }
  } | null
  readonly selected: {
    readonly repository: KernelRepository
    readonly section: 'changes' | 'history'
    readonly branch: string | null
    readonly upstream: string | null
    readonly headSHA: string | null
    readonly hasRemote: boolean
    readonly forcePushRecommended?: boolean
    readonly ahead: number
    readonly behind: number
    readonly files: ReadonlyArray<KernelFile>
    readonly selectedFileIDs: ReadonlyArray<string>
    readonly message: ICommitMessage
    readonly commitView?: KernelCommitView | null
    readonly diff: KernelDiff | null
    readonly diffSelection?: KernelDiffSelection | null
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
    readonly historyFiles: ReadonlyArray<KernelFile>
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
  readonly onboarding: KernelOnboardingView | null
  readonly signIn: KernelSignInView | null
  readonly errorCount: number
}

export type NativeKernelResponse =
  | {
      readonly id: number
      readonly result: {
        readonly mediaChunk?: KernelMediaChunk
        readonly snapshot: NativeKernelSnapshot
        readonly committed?: boolean
        readonly assisted?: KernelAssistedReceipt
        readonly addedRepositories?: ReadonlyArray<NativeRepositoryReference>
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

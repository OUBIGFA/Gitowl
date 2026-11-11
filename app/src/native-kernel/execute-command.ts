import { readKernelMedia, KernelMediaChunk } from './media-presentation'
import { RepositorySectionTab } from '../lib/app-state'
import {
  DesktopPresenter,
  NativeRepositoryReference,
} from './desktop-presenter'
import type { KernelAssistedReceipt } from './commit-presenter'
import type { NativeKernelCommand } from './protocol'

/** Dispatch only named presentation actions to the original Desktop kernel. */
export async function executeKernelCommand(
  presenter: DesktopPresenter,
  command: NativeKernelCommand
): Promise<{
  readonly mediaChunk?: KernelMediaChunk
  readonly committed?: boolean
  readonly assisted?: KernelAssistedReceipt
  readonly addedRepositories?: ReadonlyArray<NativeRepositoryReference>
  readonly historySummaries?: ReadonlyArray<{
    readonly sha: string
    readonly fileCount: number
    readonly linesAdded: number
    readonly linesDeleted: number
  }>
}> {
  switch (command.name) {
    case 'history.summaries':
      return {
        historySummaries: await presenter.readHistorySummaries(
          command.repository,
          command.shas
        ),
      }
    case 'media.frame.read':
      return { mediaChunk: await readKernelMedia(command) }
    case 'launch.cli':
      await presenter.dispatchCLIAction(command.action)
      return {}
    case 'launch.url':
      await presenter.dispatchRawURL(command.url)
      return {}
    case 'presentation.resizable-focus':
      await presenter.reportResizableFocus(command.active)
      return {}
    case 'presentation.repository-list-focused':
      await presenter.acknowledgeRepositoryListFocus()
      return {}
    case 'presentation.commit-message-focused':
      await presenter.acknowledgeCommitMessageFocus()
      return {}
    case 'state':
      return {}
    case 'repositories.filter':
      await presenter.filterRepositories(command.text)
      return {}
    case 'repositories.add': {
      const repositories = await presenter.addRepositories(command.paths)
      return {
        addedRepositories: repositories.map(repository => ({
          id: repository.id,
          path: repository.path,
        })),
      }
    }
    case 'repository.missing':
      await presenter.recoverMissingRepository(
        command.repository,
        command.action
      )
      return {}
    case 'repository.select':
      await presenter.selectRepository(command.repository)
      return {}
    case 'repository.refresh':
      await presenter.refreshRepository(command.repository)
      return {}
    case 'repository.menu':
      await presenter.executeRepositoryMenu(
        command.repository,
        command.lease,
        command.path
      )
      return {}
    case 'repository.section':
      await presenter.changeSection(
        command.repository,
        command.section === 'changes'
          ? RepositorySectionTab.Changes
          : RepositorySectionTab.History
      )
      return {}
    case 'repository.files.select':
      await presenter.selectWorkingDirectoryFiles(
        command.repository,
        command.fileIDs
      )
      return {}
    case 'imageDiff.type':
      await presenter.executeImagePresentationCommand(command)
      return {}
    case 'workspace.action':
      await presenter.executeWorkspaceCommand(command)
      return {}
    case 'menu.execute':
      await presenter.executeMenu(command.id)
      return {}
    case 'commit.options':
    case 'commit.amend':
    case 'commit.mode':
    case 'commit.coAuthors.show':
    case 'commit.coAuthors.search':
    case 'commit.coAuthors.add':
    case 'commit.coAuthors.remove':
    case 'commit.assisted.pushAfter':
    case 'commit.assisted.create':
    case 'commit.assisted.cancel':
    case 'commit.assisted.retry':
    case 'commit.assisted.dismiss':
    case 'commit.assisted.details':
    case 'commit.progress':
      return presenter.executeCommitCommand(command)
    case 'generation.copilot.settings':
    case 'generation.provider.save':
    case 'generation.provider.rename':
    case 'generation.provider.delete':
    case 'generation.provider.models.refresh':
    case 'generation.provider.models.enabled':
    case 'generation.provider.deployments.set':
    case 'generation.select':
    case 'generation.language':
      await presenter.executeGenerationCommand(command)
      return {}
    case 'repository.lines.include':
      await presenter.includeDiffLines(
        command.repository,
        command.fileID,
        command.identity,
        command.lineIDs,
        command.included
      )
      return {}
    case 'repository.files.include':
      await presenter.changeFileIncluded(
        command.repository,
        command.fileIDs,
        command.included
      )
      return {}
    case 'repository.files.includeAll':
      await presenter.changeIncludeAllFiles(
        command.repository,
        command.included
      )
      return {}
    case 'repository.message':
      await presenter.setCommitMessage(command.repository, command.message)
      return {}
    case 'repository.commit':
      return {
        committed: await presenter.commitIncludedChanges(
          command.repository,
          command.context
        ),
      }
    case 'repository.generate':
      await presenter.generateCommitMessage(
        command.repository,
        command.fileIDs,
        command.overrideExistingMessage
      )
      return {}
    case 'repository.generate.cancel':
      await presenter.cancelGenerateCommitMessage(command.repository)
      return {}
    case 'repository.fetch':
      await presenter.fetch(command.repository)
      return {}
    case 'repository.pull':
      await presenter.pull(command.repository)
      return {}
    case 'repository.push':
      await presenter.push(command.repository)
      return {}
    case 'history.loadNext':
      await presenter.loadNextCommitBatch(command.repository)
      return {}
    case 'history.select':
      presenter.changeCommitSelection(
        command.repository,
        command.shas,
        command.contiguous
      )
      await presenter.loadChangedFilesForCurrentSelection(command.repository)
      return {}
    case 'history.file.select':
      await presenter.selectHistoryFile(command.repository, command.fileID)
      return {}
    case 'popup.reply':
      await presenter.replyToPopup(command.reply)
      return {}
    case 'popup.close':
      await presenter.closePopup(command.popupID)
      return {}
    case 'signIn.endpoint':
      await presenter.setSignInEndpoint(command.endpoint)
      return {}
    case 'signIn.browser':
      presenter.authenticateUsingBrowser()
      return {}
    case 'signIn.reset':
      await presenter.resetSignIn()
      return {}
    case 'welcome.advance':
      await presenter.advanceWelcome(command.step)
      return {}
    case 'welcome.configureGit':
      await presenter.configureWelcomeAuthor(
        command.nameValue,
        command.email,
        command.useAccount
      )
      return {}
    case 'welcome.unlockGit':
      await presenter.deleteWelcomeConfigLock()
      return {}
    case 'signIn.dotcom':
      await presenter.showDotComSignInDialog()
      return {}
    case 'signIn.enterprise':
      await presenter.showEnterpriseSignInDialog(command.endpoint)
      return {}
  }
}

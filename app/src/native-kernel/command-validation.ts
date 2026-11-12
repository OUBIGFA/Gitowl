import { parseNativeMediaCommand } from './media-presentation'
import type { ICommitContext } from '../models/commit'
import type { ICommitMessage } from '../models/commit-message'
import type { NativeKernelCommand } from './protocol'
import type { INativeRepositoryReference } from './desktop-presenter'
import { parseNativeWorkspaceCommand } from './workspace-protocol'
import { parseCommitCommand } from './commit-protocol'
import { parseImagePresentationCommand } from './image-presentation'
import { parseGenerationCommand } from './generation-presenter'
import { WelcomeStep } from '../ui/welcome/welcome-step'

export class KernelProtocolError extends Error {
  public constructor(
    public readonly code: -32600 | -32601 | -32602,
    message: string
  ) {
    super(message)
    this.name = 'KernelProtocolError'
  }
}

const invalidRequest = () => new KernelProtocolError(-32600, 'Invalid request')
const unknownCommand = () => new KernelProtocolError(-32601, 'Unknown command')
const invalidParams = () =>
  new KernelProtocolError(-32602, 'Invalid command parameters')

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasOnly(value: Record<string, unknown>, keys: ReadonlyArray<string>) {
  return Object.keys(value).every(key => keys.includes(key))
}

function safeString(value: unknown): value is string {
  return typeof value === 'string' && !value.includes('\0')
}

function stringArray(value: unknown): value is ReadonlyArray<string> {
  return Array.isArray(value) && value.every(safeString)
}

function repositoryReference(
  value: unknown
): value is INativeRepositoryReference {
  return (
    isRecord(value) &&
    hasOnly(value, ['id', 'path']) &&
    typeof value.id === 'number' &&
    Number.isSafeInteger(value.id) &&
    safeString(value.path)
  )
}

function commitMessage(value: unknown): value is ICommitMessage {
  return (
    isRecord(value) &&
    hasOnly(value, [
      'summary',
      'description',
      'timestamp',
      'generatedByCopilot',
    ]) &&
    safeString(value.summary) &&
    (value.description === null || safeString(value.description)) &&
    typeof value.timestamp === 'number' &&
    Number.isFinite(value.timestamp) &&
    (value.generatedByCopilot === undefined ||
      typeof value.generatedByCopilot === 'boolean')
  )
}

function commitContext(value: unknown): value is ICommitContext {
  if (
    !isRecord(value) ||
    !hasOnly(value, [
      'summary',
      'description',
      'messageGeneratedByCopilot',
      'amend',
      'trailers',
    ]) ||
    !safeString(value.summary) ||
    (value.description !== null && !safeString(value.description)) ||
    (value.messageGeneratedByCopilot !== undefined &&
      typeof value.messageGeneratedByCopilot !== 'boolean') ||
    (value.amend !== undefined && typeof value.amend !== 'boolean')
  ) {
    return false
  }
  return (
    value.trailers === undefined ||
    (Array.isArray(value.trailers) &&
      value.trailers.every(
        trailer =>
          isRecord(trailer) &&
          hasOnly(trailer, ['token', 'value']) &&
          safeString(trailer.token) &&
          safeString(trailer.value)
      ))
  )
}

function cliAction(value: unknown): boolean {
  if (!isRecord(value)) {
    return false
  }
  if (value.kind === 'open-repository') {
    return hasOnly(value, ['kind', 'path']) && safeString(value.path)
  }
  return (
    value.kind === 'clone-url' &&
    hasOnly(value, ['kind', 'url', 'branch']) &&
    safeString(value.url) &&
    (value.branch === undefined || safeString(value.branch))
  )
}

/** Validate the complete wire shape before the presenter can resolve a command. */
export function parseKernelCommand(value: unknown): NativeKernelCommand {
  if (!isRecord(value) || !safeString(value.name)) {
    throw invalidRequest()
  }
  const name = value.name
  if (name === 'media.frame.read') {
    try {
      return parseNativeMediaCommand(value)
    } catch {
      throw invalidParams()
    }
  }
  if (name === 'imageDiff.type') {
    try {
      const image = parseImagePresentationCommand(value)
      if (image !== null) {
        return image
      }
    } catch {
      throw invalidParams()
    }
    throw unknownCommand()
  }
  if (name.startsWith('generation.')) {
    let generation: ReturnType<typeof parseGenerationCommand>
    try {
      generation = parseGenerationCommand(value)
    } catch {
      throw invalidParams()
    }
    if (generation === null) {
      throw unknownCommand()
    }
    return generation
  }
  if (name.startsWith('commit.')) {
    let commit: ReturnType<typeof parseCommitCommand>
    try {
      commit = parseCommitCommand(value)
    } catch {
      throw invalidParams()
    }
    if (commit === null) {
      throw unknownCommand()
    }
    return commit
  }
  if (name === 'workspace.action') {
    let workspace: ReturnType<typeof parseNativeWorkspaceCommand>
    try {
      workspace = parseNativeWorkspaceCommand(value)
    } catch {
      throw invalidParams()
    }
    if (workspace === null) {
      throw unknownCommand()
    }
    return workspace
  }
  const commonRepository = () => repositoryReference(value.repository)
  const exact = (keys: ReadonlyArray<string>) => hasOnly(value, keys)
  const command = () => {
    switch (name) {
      case 'menu.execute':
        return exact(['name', 'id']) && safeString(value.id)
      case 'launch.cli':
        return exact(['name', 'action']) && cliAction(value.action)
      case 'launch.url':
        return exact(['name', 'url']) && safeString(value.url)
      case 'repository.lines.include':
        return (
          exact([
            'name',
            'repository',
            'fileID',
            'identity',
            'lineIDs',
            'included',
          ]) &&
          commonRepository() &&
          safeString(value.fileID) &&
          typeof value.identity === 'string' &&
          /^[a-f0-9]{64}$/.test(value.identity) &&
          Array.isArray(value.lineIDs) &&
          value.lineIDs.every(id => Number.isSafeInteger(id) && id >= 0) &&
          typeof value.included === 'boolean'
        )
      case 'presentation.resizable-focus':
        return exact(['name', 'active']) && typeof value.active === 'boolean'
      case 'presentation.repository-list-focused':
      case 'presentation.commit-message-focused':
      case 'state':
        return exact(['name'])
      case 'repositories.add':
        return exact(['name', 'paths']) && stringArray(value.paths)
      case 'repositories.filter':
        return exact(['name', 'text']) && safeString(value.text)
      case 'repository.missing':
        return (
          exact(['name', 'repository', 'action']) &&
          commonRepository() &&
          typeof value.action === 'string' &&
          ['check', 'locate', 'clone', 'remove', 'trust'].includes(value.action)
        )
      case 'repository.select':
      case 'repository.refresh':
      case 'repository.fetch':
      case 'repository.pull':
      case 'repository.push':
        return exact(['name', 'repository']) && commonRepository()
      case 'repository.section':
        return (
          exact(['name', 'repository', 'section']) &&
          commonRepository() &&
          (value.section === 'changes' || value.section === 'history')
        )
      case 'repository.menu':
        return (
          exact(['name', 'repository', 'lease', 'path']) &&
          commonRepository() &&
          typeof value.lease === 'string' &&
          /^[a-f0-9]{64}$/.test(value.lease) &&
          Array.isArray(value.path) &&
          value.path.length > 0 &&
          value.path.length <= 8 &&
          value.path.every(i => Number.isSafeInteger(i) && i >= 0)
        )
      case 'repository.files.select':
        return (
          exact(['name', 'repository', 'fileIDs']) &&
          commonRepository() &&
          stringArray(value.fileIDs)
        )
      case 'repository.files.include':
        return (
          exact(['name', 'repository', 'fileIDs', 'included']) &&
          commonRepository() &&
          stringArray(value.fileIDs) &&
          typeof value.included === 'boolean'
        )
      case 'repository.files.includeAll':
        return (
          exact(['name', 'repository', 'included']) &&
          commonRepository() &&
          typeof value.included === 'boolean'
        )
      case 'repository.message':
        return (
          exact(['name', 'repository', 'message']) &&
          commonRepository() &&
          commitMessage(value.message)
        )
      case 'repository.commit':
        return (
          exact(['name', 'repository', 'context']) &&
          commonRepository() &&
          commitContext(value.context)
        )
      case 'repository.generate':
        return (
          exact(['name', 'repository', 'fileIDs', 'overrideExistingMessage']) &&
          commonRepository() &&
          stringArray(value.fileIDs) &&
          typeof value.overrideExistingMessage === 'boolean'
        )
      case 'repository.generate.cancel':
        return exact(['name', 'repository']) && commonRepository()
      case 'history.loadNext':
        return exact(['name', 'repository']) && commonRepository()
      case 'history.select':
        return (
          exact(['name', 'repository', 'shas', 'contiguous']) &&
          commonRepository() &&
          stringArray(value.shas) &&
          typeof value.contiguous === 'boolean'
        )
      case 'history.summaries':
        return (
          exact(['name', 'repository', 'shas']) &&
          commonRepository() &&
          stringArray(value.shas) &&
          value.shas.length > 0 &&
          value.shas.length <= 32 &&
          new Set(value.shas).size === value.shas.length
        )
      case 'history.file.select':
        return (
          exact(['name', 'repository', 'fileID']) &&
          commonRepository() &&
          safeString(value.fileID)
        )
      case 'popup.reply':
        // Only the renderer owns the current Popup and can validate/resolve its
        // opaque continuation. This outer envelope exposes no reflected method.
        return exact(['name', 'reply']) && isRecord(value.reply)
      case 'popup.close':
        return (
          exact(['name', 'popupID']) &&
          typeof value.popupID === 'number' &&
          Number.isSafeInteger(value.popupID)
        )
      case 'signIn.browser':
      case 'signIn.reset':
      case 'welcome.unlockGit':
        return exact(['name'])
      case 'signIn.endpoint':
        return exact(['name', 'endpoint']) && safeString(value.endpoint)
      case 'welcome.advance':
        return (
          exact(['name', 'step']) &&
          Object.values(WelcomeStep).some(step => step === value.step)
        )
      case 'welcome.configureGit':
        return (
          exact(['name', 'nameValue', 'email', 'useAccount']) &&
          safeString(value.nameValue) &&
          safeString(value.email) &&
          typeof value.useAccount === 'boolean'
        )
      case 'signIn.dotcom':
        return exact(['name'])
      case 'signIn.enterprise':
        return (
          exact(['name']) ||
          (exact(['name', 'endpoint']) && safeString(value.endpoint))
        )
      default:
        return null
    }
  }
  const result = command()
  if (result === null) {
    throw unknownCommand()
  }
  if (!result) {
    throw invalidParams()
  }
  return value as NativeKernelCommand
}

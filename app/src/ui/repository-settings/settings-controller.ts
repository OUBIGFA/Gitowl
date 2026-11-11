import type {
  IRepositorySettingsProps,
  IRepositorySettingsState,
} from './repository-settings'
import { readGitIgnoreAtRoot } from '../../lib/git'
import { getConfigValue, getGlobalConfigValue } from '../../lib/git/config'

// Leaf enums let the native controller use the original option values without loading React views.
export enum RepositorySettingsTab {
  Remote = 0,
  IgnoredFiles,
  GitConfig,
  ForkSettings,
}
export enum GitConfigLocation {
  Global = 'Global',
  Local = 'Local',
}

export async function loadRepositorySettings(
  repository: IRepositorySettingsProps['repository']
): Promise<Partial<IRepositorySettingsState>> {
  let ignoreText: string | null = null
  let errors: string[] | undefined
  try {
    ignoreText = await readGitIgnoreAtRoot(repository)
  } catch (e) {
    log.error(
      `RepositorySettings: unable to read root .gitignore file for ${repository.path}`,
      e
    )
    errors = [`Could not read root .gitignore: ${e}`]
  }
  const initialCommitterName = await getConfigValue(
    repository,
    'user.name',
    true
  )
  const initialCommitterEmail = await getConfigValue(
    repository,
    'user.email',
    true
  )
  const globalCommitterName = (await getGlobalConfigValue('user.name')) || ''
  const globalCommitterEmail = (await getGlobalConfigValue('user.email')) || ''
  const gitConfigLocation =
    initialCommitterName === null && initialCommitterEmail === null
      ? GitConfigLocation.Global
      : GitConfigLocation.Local
  return {
    ignoreText,
    errors,
    initialCommitterName,
    initialCommitterEmail,
    globalCommitterName,
    globalCommitterEmail,
    gitConfigLocation,
    initialGitConfigLocation: gitConfigLocation,
    committerName:
      gitConfigLocation === GitConfigLocation.Global
        ? globalCommitterName
        : initialCommitterName ?? '',
    committerEmail:
      gitConfigLocation === GitConfigLocation.Global
        ? globalCommitterEmail
        : initialCommitterEmail ?? '',
    isLoadingGitConfig: false,
  }
}

/** Exact upstream save sequencing and error accumulation; account replacement precedes remote refresh. */
export async function saveRepositorySettings(
  props: IRepositorySettingsProps,
  state: IRepositorySettingsState,
  update: (change: Partial<IRepositorySettingsState>) => void,
  onSaved?: () => Promise<void>
): Promise<void> {
  update({ disabled: true, errors: undefined })
  const errors: string[] = []
  let repository = props.repository
  if (
    repository.gitHubRepository !== null &&
    state.selectedLogin !== undefined &&
    state.selectedLogin !== repository.login
  ) {
    try {
      repository = await props.dispatcher.updateRepositoryAccount(
        repository,
        state.selectedLogin
      )
    } catch (e) {
      errors.push(`Failed saving the repository account: ${e}`)
    }
  }
  if (
    state.remote &&
    props.remote &&
    state.remote.url.trim() !== props.remote.url
  ) {
    try {
      await props.dispatcher.setRemoteURL(
        repository,
        props.remote.name,
        state.remote.url.trim()
      )
    } catch (e) {
      log.error(
        `RepositorySettings: unable to set remote URL at ${props.repository.path}`,
        e
      )
      errors.push(`Failed setting the remote URL: ${e}`)
    }
  }
  if (state.ignoreTextHasChanged && state.ignoreText !== null) {
    try {
      await props.dispatcher.saveGitIgnore(props.repository, state.ignoreText)
    } catch (e) {
      log.error(
        `RepositorySettings: unable to save gitignore at ${props.repository.path}`,
        e
      )
      errors.push(`Failed saving the .gitignore file: ${e}`)
    }
  }
  if (
    state.forkContributionTarget !==
    props.repository.workflowPreferences.forkContributionTarget
  ) {
    await props.dispatcher.updateRepositoryWorkflowPreferences(
      props.repository,
      {
        ...props.repository.workflowPreferences,
        forkContributionTarget: state.forkContributionTarget,
      }
    )
  }
  let shouldRefreshAuthor = false
  try {
    if (
      state.gitConfigLocation !== state.initialGitConfigLocation &&
      state.gitConfigLocation === GitConfigLocation.Global
    ) {
      shouldRefreshAuthor = true
      await props.dispatcher.updateRepositoryCommitter(
        props.repository,
        null,
        null
      )
    } else if (state.gitConfigLocation === GitConfigLocation.Local) {
      const name =
        state.committerName !== state.initialCommitterName
          ? state.committerName
          : undefined
      const email =
        state.committerEmail !== state.initialCommitterEmail
          ? state.committerEmail
          : undefined
      if (name !== undefined || email !== undefined) {
        shouldRefreshAuthor = true
        await props.dispatcher.updateRepositoryCommitter(
          props.repository,
          name,
          email
        )
      }
    }
  } catch (e) {
    log.error(
      `RepositorySettings: unable to update Git identity at ${props.repository.path}`,
      e
    )
    errors.push(`Failed updating Git identity: ${e}`)
  }
  if (shouldRefreshAuthor) {
    props.dispatcher.refreshAuthor(props.repository)
  }
  if (errors.length === 0) {
    if (onSaved) {
      await onSaved()
    }
    props.onDismissed()
  } else {
    update({ disabled: false, errors })
  }
}

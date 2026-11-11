import type { IPreferencesProps, IPreferencesState } from './preferences'
import {
  getGlobalConfigValue,
  setGlobalConfigValue,
} from '../../lib/git/config'
import {
  getDefaultBranch,
  setDefaultBranch,
} from '../../lib/helpers/default-branch'
import { lookupPreferredEmail } from '../../lib/email'
import { isDotComAccount } from '../../models/account'
import { getAvailableEditors } from '../../lib/editors/lookup'
import { getAvailableShells } from '../../lib/shells'
import {
  isConfigFileLockError,
  parseConfigLockFilePathFromError,
} from '../../lib/git'
import {
  setCacheHooksEnv,
  setGitHookEnvShell,
  setHooksEnvEnabled,
} from '../../lib/hooks/config'
import { isValidCustomIntegration } from '../../lib/custom-integration'
import {
  enableCopilotAppHandoff,
  enableFormattingPreferences,
} from '../../lib/feature-flag'
import { validateCopilotAppPath } from '../../lib/copilot-app'
import {
  setDateFormatPreference,
  setTimeFormatPreference,
  setNumberFormatPreference,
} from '../../models/formatting-preferences'
import { PreferencesTab } from '../../models/preferences'

/** Nonvisual upstream controller shared by React and native forms. No state owner or Git substitute. */
export async function loadPreferences(
  props: Pick<IPreferencesProps, 'accounts'>
): Promise<Partial<IPreferencesState>> {
  const account = props.accounts.find(isDotComAccount) ?? props.accounts.at(0)
  // These independent reads do not need to serialize before the dialog opens.
  const [
    initialCommitterName,
    initialCommitterEmail,
    initialDefaultBranch,
    editors,
    shells,
  ] = await Promise.all([
    getGlobalConfigValue('user.name'),
    getGlobalConfigValue('user.email'),
    getDefaultBranch(),
    getAvailableEditors(),
    getAvailableShells(),
  ])
  return {
    committerName: initialCommitterName || account?.login || '',
    committerEmail:
      initialCommitterEmail || (account ? lookupPreferredEmail(account) : ''),
    defaultBranch: initialDefaultBranch,
    initialCommitterName,
    initialCommitterEmail,
    initialDefaultBranch,
    availableEditors: editors.map(e => e.editor),
    availableShells: shells.map(e => e.shell),
    isLoadingGitConfig: false,
  }
}

/** Preserve upstream save order: author/branch/hooks and lock recovery precede all other writes. */
export async function savePreferences(
  props: IPreferencesProps,
  state: IPreferencesState,
  update: (change: Partial<IPreferencesState>) => void,
  onSaved?: () => Promise<void>
): Promise<void> {
  const { dispatcher } = props
  const copilotAppPath = state.copilotAppPath.trim()
  const copilotAppPathChanged =
    copilotAppPath !== (props.copilotAppPath?.trim() ?? '')
  if (
    enableCopilotAppHandoff() &&
    copilotAppPathChanged &&
    copilotAppPath.length > 0 &&
    !(await validateCopilotAppPath(copilotAppPath))
  ) {
    update({
      selectedIndex: PreferencesTab.Integrations,
      copilotAppPathError: __DARWIN__
        ? 'Choose the GitHub Copilot application (.app).'
        : 'Choose the GitHub Copilot executable (github.exe).',
    })
    return
  }
  try {
    let shouldRefreshAuthor = false
    if (state.committerName !== state.initialCommitterName) {
      await setGlobalConfigValue('user.name', state.committerName)
      shouldRefreshAuthor = true
    }
    if (state.committerEmail !== state.initialCommitterEmail) {
      await setGlobalConfigValue('user.email', state.committerEmail)
      shouldRefreshAuthor = true
    }
    if (props.repository !== null && shouldRefreshAuthor) {
      dispatcher.refreshAuthor(props.repository)
    }
    // Empty default branch intentionally retains the original value.
    if (
      state.defaultBranch.length > 0 &&
      state.defaultBranch !== state.initialDefaultBranch
    ) {
      await setDefaultBranch(state.defaultBranch)
    }
    if (
      props.repositoryIndicatorsEnabled !== state.repositoryIndicatorsEnabled
    ) {
      dispatcher.setRepositoryIndicatorsEnabled(
        state.repositoryIndicatorsEnabled
      )
    }
    if (state.hooksPreferencesDirty) {
      if (state.enableGitHookEnv !== undefined) {
        setHooksEnvEnabled(state.enableGitHookEnv)
      }
      if (state.cacheGitHookEnv !== undefined) {
        setCacheHooksEnv(state.cacheGitHookEnv)
      }
      if (state.selectedGitHookEnvShell !== undefined) {
        setGitHookEnvShell(state.selectedGitHookEnvShell)
      }
    }
  } catch (e) {
    if (isConfigFileLockError(e)) {
      const path = parseConfigLockFilePathFromError(e.result)
      if (path !== null) {
        update({
          existingLockFilePath: path,
          selectedIndex: PreferencesTab.Git,
        })
        return
      }
    }
    props.onDismissed()
    dispatcher.postError(e)
    return
  }
  dispatcher.setUseWindowsOpenSSH(state.useWindowsOpenSSH)
  dispatcher.setShowCommitLengthWarning(state.showCommitLengthWarning)
  dispatcher.setNotificationsEnabled(state.notificationsEnabled)
  await dispatcher.setStatsOptOut(state.optOutOfUsageTracking, false)
  const validEditor =
    state.customEditor && (await isValidCustomIntegration(state.customEditor))
  dispatcher.setUseCustomEditor(state.useCustomEditor && validEditor)
  if (validEditor) {
    dispatcher.setCustomEditor(state.customEditor)
  }
  const validShell =
    state.customShell && (await isValidCustomIntegration(state.customShell))
  dispatcher.setUseCustomShell(state.useCustomShell && validShell)
  if (validShell) {
    dispatcher.setCustomShell(state.customShell)
  }
  if (enableCopilotAppHandoff() && copilotAppPathChanged) {
    await dispatcher.setCopilotAppPath(
      copilotAppPath.length === 0 ? null : copilotAppPath
    )
  }
  if (props.useExternalCredentialHelper !== state.useExternalCredentialHelper) {
    dispatcher.setUseExternalCredentialHelper(state.useExternalCredentialHelper)
  }
  await dispatcher.setConfirmRepoRemovalSetting(state.confirmRepositoryRemoval)
  await dispatcher.setConfirmForcePushSetting(state.confirmForcePush)
  await dispatcher.setConfirmDiscardStashSetting(state.confirmDiscardStash)
  await dispatcher.setConfirmCheckoutCommitSetting(state.confirmCheckoutCommit)
  await dispatcher.setConfirmUndoCommitSetting(state.confirmUndoCommit)
  await dispatcher.setConfirmCommitFilteredChanges(
    state.askForConfirmationOnCommitFilteredChanges
  )
  await dispatcher.setConfirmCommitMessageOverrideSetting(
    state.confirmCommitMessageOverride
  )
  await dispatcher.setConfirmWorktreeRemovalSetting(
    state.confirmWorktreeRemoval
  )
  if (state.selectedExternalEditor) {
    await dispatcher.setExternalEditor(state.selectedExternalEditor)
  }
  await dispatcher.setShell(state.selectedShell)
  await dispatcher.setConfirmDiscardChangesSetting(state.confirmDiscardChanges)
  await dispatcher.setConfirmDiscardChangesPermanentlySetting(
    state.confirmDiscardChangesPermanently
  )
  await dispatcher.setUncommittedChangesStrategySetting(
    state.uncommittedChangesStrategy
  )
  dispatcher.setUnderlineLinksSetting(state.underlineLinks)
  dispatcher.setDiffCheckMarksSetting(state.showDiffCheckMarks)
  dispatcher.setAlwaysShowWorktreeList(state.alwaysShowWorktreeList)
  dispatcher.setSelectedCopilotModelsByAccount(
    state.selectedCopilotModelsByAccount
  )
  dispatcher.setAlwaysUseCopilotForConflictResolution(
    state.alwaysUseCopilotForConflictResolution
  )
  if (enableFormattingPreferences()) {
    if (state.selectedDateFormat !== undefined) {
      setDateFormatPreference(state.selectedDateFormat)
    }
    if (state.selectedTimeFormat !== undefined) {
      setTimeFormatPreference(state.selectedTimeFormat)
    }
    if (state.selectedNumberFormat !== undefined) {
      setNumberFormatPreference(state.selectedNumberFormat)
    }
    if (state.preferAbsoluteDates !== undefined) {
      dispatcher.setPreferAbsoluteDates(state.preferAbsoluteDates)
    }
  }
  if (onSaved) {
    await onSaved()
  }
  props.onDismissed()
}

import { isRebaseConflictState } from '../../lib/app-state'
import {
  continueOperation,
  abortOperation,
} from '../../native-kernel/operation-dialogs'
import {
  instanceOfIBaseRebaseDetails,
  MultiCommitOperationKind,
} from '../../models/multi-commit-operation'
import { BaseMultiCommitOperation } from './base-multi-commit-operation'

export abstract class BaseRebase extends BaseMultiCommitOperation {
  protected abstract conflictDialogOperationPrefix: string
  protected abstract rebaseKind: MultiCommitOperationKind

  protected onContinueAfterConflicts = async (): Promise<void> => {
    const { state, conflictState } = this.props
    const { operationDetail, originalBranchTip } = state

    // Conflicts were resolved externally — nothing left to continue.
    if (conflictState === null) {
      this.onFlowEnded()
      return
    }

    if (
      originalBranchTip === null ||
      !isRebaseConflictState(conflictState) ||
      !instanceOfIBaseRebaseDetails(operationDetail)
    ) {
      this.endFlowInvalidState()
      return
    }

    await continueOperation(this.props)
  }

  protected onAbort = async (): Promise<void> => {
    return abortOperation(this.props)
  }

  protected onConflictsDialogDismissed = () => {
    this.onInvokeConflictsDialogDismissed(this.conflictDialogOperationPrefix)
  }
}

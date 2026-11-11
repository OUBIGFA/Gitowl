import * as React from 'react'
import { switchBranchOperation } from '../../native-kernel/operation-dialogs'
import { Dialog, DialogContent, DialogFooter } from '../dialog'
import { Repository } from '../../models/repository'
import { Dispatcher } from '../dispatcher'
import { VerticalSegmentedControl } from '../lib/vertical-segmented-control'
import { Row } from '../lib/row'
import { Branch } from '../../models/branch'
import { Octicon } from '../octicons'
import * as octicons from '../octicons/octicons.generated'
import { PopupType } from '../../models/popup'
import { OkCancelButtonGroup } from '../dialog/ok-cancel-button-group'

enum StashAction {
  StashOnCurrentBranch,
  MoveToNewBranch,
}

interface ISwitchBranchProps {
  readonly repository: Repository
  readonly dispatcher: Dispatcher
  readonly currentBranch: Branch

  /** The branch to checkout after the user selects a stash action */
  readonly branchToCheckout: Branch

  /** Whether `currentBranch` has an existing stash association */
  readonly hasAssociatedStash: boolean
  readonly onDismissed: () => void
}

interface ISwitchBranchState {
  readonly isStashingChanges: boolean
  readonly selectedStashAction: StashAction
  readonly currentBranchName: string
}

/**
 * Dialog that alerts users that their changes may be lost and offers them the
 * chance to stash them or potentially take them to another branch
 */
export class StashAndSwitchBranch extends React.Component<
  ISwitchBranchProps,
  ISwitchBranchState
> {
  public constructor(props: ISwitchBranchProps) {
    super(props)

    this.state = {
      isStashingChanges: false,
      selectedStashAction: StashAction.StashOnCurrentBranch,
      currentBranchName: props.currentBranch.name,
    }
  }

  public render() {
    const { isStashingChanges } = this.state
    return (
      <Dialog
        id="stash-changes"
        title={__DARWIN__ ? 'Switch Branch' : 'Switch branch'}
        onSubmit={this.onSubmit}
        onDismissed={this.props.onDismissed}
        loading={isStashingChanges}
        disabled={isStashingChanges}
      >
        <DialogContent>
          {this.renderStashActions()}
          {this.renderStashOverwriteWarning()}
        </DialogContent>
        <DialogFooter>
          <OkCancelButtonGroup
            okButtonText={__DARWIN__ ? 'Switch Branch' : 'Switch branch'}
            okButtonDisabled={isStashingChanges}
          />
        </DialogFooter>
      </Dialog>
    )
  }

  private renderStashOverwriteWarning() {
    if (
      !this.props.hasAssociatedStash ||
      this.state.selectedStashAction !== StashAction.StashOnCurrentBranch
    ) {
      return null
    }

    return (
      <Row>
        <Octicon symbol={octicons.alert} /> Your current stash will be
        overwritten by creating a new stash
      </Row>
    )
  }

  private renderStashActions() {
    const { branchToCheckout } = this.props
    const items = [
      {
        title: `Leave my changes on ${this.state.currentBranchName}`,
        description:
          'Your in-progress work will be stashed on this branch for you to return to later',
        key: StashAction.StashOnCurrentBranch,
      },
      {
        title: `Bring my changes to ${branchToCheckout.name}`,
        description: 'Your in-progress work will follow you to the new branch',
        key: StashAction.MoveToNewBranch,
      },
    ]

    return (
      <Row>
        <VerticalSegmentedControl
          label="You have changes on this branch. What would you like to do with them?"
          items={items}
          selectedKey={this.state.selectedStashAction}
          onSelectionChanged={this.onSelectionChanged}
        />
      </Row>
    )
  }

  private onSelectionChanged = (action: StashAction) => {
    this.setState({ selectedStashAction: action })
  }

  private onSubmit = async () => {
    const { repository, branchToCheckout, dispatcher, hasAssociatedStash } =
      this.props
    const { selectedStashAction } = this.state

    if (
      selectedStashAction === StashAction.StashOnCurrentBranch &&
      hasAssociatedStash
    ) {
      return switchBranchOperation(
        dispatcher,
        {
          type: PopupType.StashAndSwitchBranch,
          repository,
          branchToCheckout,
        },
        hasAssociatedStash,
        false,
        this.props.onDismissed
      )
    }

    this.setState({ isStashingChanges: true })

    await switchBranchOperation(
      dispatcher,
      {
        type: PopupType.StashAndSwitchBranch,
        repository,
        branchToCheckout,
      },
      hasAssociatedStash,
      selectedStashAction === StashAction.MoveToNewBranch,
      () => this.setState({ isStashingChanges: false }, this.props.onDismissed)
    )
  }
}

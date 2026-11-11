import * as React from 'react'
import { Account } from '../../models/account'
import type { CopilotQuotaSnapshots } from '../../lib/stores/copilot-store'
import {
  SnapshotKey,
  ISnapshotDefinition,
  IEditableSnapshot,
  SnapshotTestDraft as ITestCopilotSnapshotCardDialogState,
  snapshotDefinitions,
  getDefaultSnapshots,
  getInitialAccount,
  getTestSnapshotAccount,
  getTestSnapshots,
  tokenBasedBillingTestPreset,
  rateLimitTestPreset,
} from './test-snapshot-controller'
import { Dialog, DialogContent, DialogFooter } from '../dialog'
import { OkCancelButtonGroup } from '../dialog/ok-cancel-button-group'
import { TextBox } from '../lib/text-box'
import { Checkbox, CheckboxValue } from '../lib/checkbox'
import { Button } from '../lib/button'
import { Row } from '../lib/row'
import { SnapshotCard } from './snapshot-card'

interface ITestCopilotSnapshotCardDialogProps {
  readonly accounts: ReadonlyArray<Account>
  readonly onDismissed: () => void
}

function checkboxValue(value: boolean): CheckboxValue {
  return value ? CheckboxValue.On : CheckboxValue.Off
}

export class TestCopilotSnapshotCardDialog extends React.Component<
  ITestCopilotSnapshotCardDialogProps,
  ITestCopilotSnapshotCardDialogState
> {
  public constructor(props: ITestCopilotSnapshotCardDialogProps) {
    super(props)
    const account = getInitialAccount(props.accounts)

    this.state = {
      ...account,
      snapshots: getDefaultSnapshots(),
    }
  }

  public render() {
    return (
      <Dialog
        id="test-copilot-snapshot-card"
        title="Test Copilot Snapshot Card"
        onSubmit={this.props.onDismissed}
        onDismissed={this.props.onDismissed}
      >
        <DialogContent>
          <div className="test-copilot-snapshot-card-preview">
            <SnapshotCard
              account={this.getAccount()}
              snapshots={this.getSnapshots()}
            />
          </div>
          <div className="test-copilot-snapshot-card-controls">
            {this.renderAccountControls()}
            {snapshotDefinitions.map(snapshot =>
              this.renderSnapshotControls(snapshot)
            )}
          </div>
        </DialogContent>
        <DialogFooter>
          <div className="test-copilot-snapshot-card-footer-buttons">
            <Button type="button" onClick={this.onUseTokenBasedBillingPreset}>
              AI credits preset
            </Button>
            <Button type="button" onClick={this.onUseRateLimitPreset}>
              Rate limit preset
            </Button>
          </div>
          <OkCancelButtonGroup okButtonText="Done" />
        </DialogFooter>
      </Dialog>
    )
  }

  private getAccount(): Account {
    return getTestSnapshotAccount(this.state)
  }

  private getSnapshots(): CopilotQuotaSnapshots {
    return getTestSnapshots(this.state.snapshots)
  }

  private renderAccountControls(): JSX.Element {
    return (
      <fieldset className="test-copilot-snapshot-card-fieldset">
        <legend>Fake account</legend>
        <div className="test-copilot-snapshot-card-grid">
          <TextBox
            label="Login"
            value={this.state.login}
            onValueChanged={this.onLoginChanged}
            autoFocus={true}
          />
          <TextBox
            label="Name"
            value={this.state.name}
            onValueChanged={this.onNameChanged}
          />
          <TextBox
            label="Endpoint"
            value={this.state.endpoint}
            onValueChanged={this.onEndpointChanged}
          />
          <TextBox
            label="Avatar URL"
            value={this.state.avatarURL}
            onValueChanged={this.onAvatarURLChanged}
          />
        </div>
      </fieldset>
    )
  }

  private renderSnapshotControls(definition: ISnapshotDefinition): JSX.Element {
    const snapshot = this.state.snapshots[definition.key]

    return (
      <fieldset
        className="test-copilot-snapshot-card-fieldset"
        key={definition.key}
      >
        <legend>{definition.label}</legend>
        <Row className="test-copilot-snapshot-card-checkbox-row">
          <Checkbox
            label="Enabled"
            value={checkboxValue(snapshot.enabled)}
            onChange={this.onSnapshotEnabledChanged(definition.key)}
          />
          <Checkbox
            label="Token-based billing"
            value={checkboxValue(snapshot.tokenBasedBilling)}
            onChange={this.onSnapshotTokenBasedBillingChanged(definition.key)}
          />
          <Checkbox
            label="Unlimited"
            value={checkboxValue(snapshot.isUnlimitedEntitlement)}
            onChange={this.onSnapshotUnlimitedChanged(definition.key)}
          />
        </Row>
        <div className="test-copilot-snapshot-card-grid">
          <TextBox
            label="Entitlement requests"
            value={snapshot.entitlementRequests}
            onValueChanged={this.onSnapshotEntitlementRequestsChanged(
              definition.key
            )}
          />
          <TextBox
            label="Used requests"
            value={snapshot.usedRequests}
            onValueChanged={this.onSnapshotUsedRequestsChanged(definition.key)}
          />
          <TextBox
            label="Remaining percentage"
            value={snapshot.remainingPercentage}
            onValueChanged={this.onSnapshotRemainingPercentageChanged(
              definition.key
            )}
          />
          <TextBox
            label="Overage"
            value={snapshot.overage}
            onValueChanged={this.onSnapshotOverageChanged(definition.key)}
          />
          <TextBox
            label="Reset date"
            value={snapshot.resetDate}
            placeholder="2026-07-10T12:00:00Z"
            onValueChanged={this.onSnapshotResetDateChanged(definition.key)}
          />
        </div>
        <Row className="test-copilot-snapshot-card-checkbox-row">
          <Checkbox
            label="Usage allowed with exhausted quota"
            value={checkboxValue(snapshot.usageAllowedWithExhaustedQuota)}
            onChange={this.onSnapshotUsageAllowedChanged(definition.key)}
          />
          <Checkbox
            label="Overage allowed with exhausted quota"
            value={checkboxValue(snapshot.overageAllowedWithExhaustedQuota)}
            onChange={this.onSnapshotOverageAllowedChanged(definition.key)}
          />
        </Row>
      </fieldset>
    )
  }

  private updateSnapshot(
    key: SnapshotKey,
    update: (snapshot: IEditableSnapshot) => IEditableSnapshot
  ) {
    this.setState(state => ({
      snapshots: {
        ...state.snapshots,
        [key]: update(state.snapshots[key]),
      },
    }))
  }

  private onLoginChanged = (login: string) => {
    this.setState({ login })
  }

  private onNameChanged = (name: string) => {
    this.setState({ name })
  }

  private onEndpointChanged = (endpoint: string) => {
    this.setState({ endpoint })
  }

  private onAvatarURLChanged = (avatarURL: string) => {
    this.setState({ avatarURL })
  }

  private onSnapshotEnabledChanged =
    (key: SnapshotKey) => (event: React.FormEvent<HTMLInputElement>) => {
      const enabled = event.currentTarget.checked
      this.updateSnapshot(key, snapshot => ({ ...snapshot, enabled }))
    }

  private onSnapshotTokenBasedBillingChanged =
    (key: SnapshotKey) => (event: React.FormEvent<HTMLInputElement>) => {
      const tokenBasedBilling = event.currentTarget.checked
      this.updateSnapshot(key, snapshot => ({ ...snapshot, tokenBasedBilling }))
    }

  private onSnapshotUnlimitedChanged =
    (key: SnapshotKey) => (event: React.FormEvent<HTMLInputElement>) => {
      const isUnlimitedEntitlement = event.currentTarget.checked
      this.updateSnapshot(key, snapshot => ({
        ...snapshot,
        isUnlimitedEntitlement,
      }))
    }

  private onSnapshotUsageAllowedChanged =
    (key: SnapshotKey) => (event: React.FormEvent<HTMLInputElement>) => {
      const usageAllowedWithExhaustedQuota = event.currentTarget.checked
      this.updateSnapshot(key, snapshot => ({
        ...snapshot,
        usageAllowedWithExhaustedQuota,
      }))
    }

  private onSnapshotOverageAllowedChanged =
    (key: SnapshotKey) => (event: React.FormEvent<HTMLInputElement>) => {
      const overageAllowedWithExhaustedQuota = event.currentTarget.checked
      this.updateSnapshot(key, snapshot => ({
        ...snapshot,
        overageAllowedWithExhaustedQuota,
      }))
    }

  private onSnapshotEntitlementRequestsChanged =
    (key: SnapshotKey) => (entitlementRequests: string) => {
      this.updateSnapshot(key, snapshot => ({
        ...snapshot,
        entitlementRequests,
      }))
    }

  private onSnapshotUsedRequestsChanged =
    (key: SnapshotKey) => (usedRequests: string) => {
      this.updateSnapshot(key, snapshot => ({ ...snapshot, usedRequests }))
    }

  private onSnapshotRemainingPercentageChanged =
    (key: SnapshotKey) => (remainingPercentage: string) => {
      this.updateSnapshot(key, snapshot => ({
        ...snapshot,
        remainingPercentage,
      }))
    }

  private onSnapshotOverageChanged =
    (key: SnapshotKey) => (overage: string) => {
      this.updateSnapshot(key, snapshot => ({ ...snapshot, overage }))
    }

  private onSnapshotResetDateChanged =
    (key: SnapshotKey) => (resetDate: string) => {
      this.updateSnapshot(key, snapshot => ({ ...snapshot, resetDate }))
    }

  private onUseTokenBasedBillingPreset = () => {
    this.setState({ snapshots: tokenBasedBillingTestPreset() })
  }

  private onUseRateLimitPreset = () => {
    this.setState({ snapshots: rateLimitTestPreset() })
  }
}

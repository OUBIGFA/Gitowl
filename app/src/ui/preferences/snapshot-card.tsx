import * as React from 'react'
import { getHTMLURL } from '../../lib/api'
import { lookupPreferredEmail } from '../../lib/email'
import type {
  CopilotQuotaSnapshots,
  ICopilotQuotaSnapshot,
} from '../../lib/stores/copilot-store'
import { isEnterpriseAccount, type Account } from '../../models/account'
import type { IAvatarUser } from '../../models/avatar'
import { Avatar } from '../lib/avatar'
import { Button } from '../lib/button'
import { TooltippedContent } from '../lib/tooltipped-content'
import { TooltipDirection } from '../lib/tooltip'
import { formatNumber } from '../../lib/format-number'
import { getNumberFormatPreference } from '../../models/formatting-preferences'
import {
  ICategorizedSnapshot,
  hasTokenBasedBilling,
  getUsedPercentage,
  getVisibleQuotaSnapshots,
  getVisibleRateLimitSnapshots,
} from './snapshot-card-data'
export {
  ICategorizedSnapshot,
  getVisibleQuotaSnapshots,
  getVisibleRateLimitSnapshots,
} from './snapshot-card-data'

interface ISnapshotCardProps {
  readonly account: Account
  readonly snapshots: CopilotQuotaSnapshots | null
  readonly onConfigureModels?: (account: Account) => void
}

interface ISnapshotUsageItemProps {
  readonly item: ICategorizedSnapshot
  readonly tokenBasedBilling?: boolean
}

interface IQuotaProgressBarProps {
  readonly snapshot: ICopilotQuotaSnapshot
}

function getAccountAvatarUser(account: Account): IAvatarUser {
  return {
    name: account.name,
    email: lookupPreferredEmail(account),
    avatarURL: account.avatarURL,
    endpoint: account.endpoint,
  }
}

function formatAiCreditValue(credits: number): string {
  if (!Number.isFinite(credits) || credits <= 0) {
    return '0'
  }

  if (credits < 0.01) {
    return `<${formatNumber(0.01)}`
  }

  const maximumFractionDigits = credits >= 100 ? 0 : credits >= 10 ? 1 : 2

  return formatNumber(credits, {
    ...getNumberFormatPreference(),
    maximumFractionDigits,
  })
}

function formatUsedPercentage(snapshot: ICopilotQuotaSnapshot): string {
  return `${getUsedPercentage(snapshot)}%`
}

function formatUsageTooltip(
  snapshot: ICopilotQuotaSnapshot,
  displayName: string
): string | undefined {
  if (snapshot.isUnlimitedEntitlement || snapshot.entitlementRequests <= 0) {
    return undefined
  }

  if (displayName === 'AI credits') {
    return `${formatAiCreditValue(
      snapshot.usedRequests
    )} / ${formatAiCreditValue(snapshot.entitlementRequests)} AI credits used`
  }

  const formatRequests = (value: number) =>
    formatNumber(value, {
      ...getNumberFormatPreference(),
      maximumFractionDigits: 2,
    })

  return `${formatRequests(snapshot.usedRequests)} / ${formatRequests(
    snapshot.entitlementRequests
  )} ${displayName.toLowerCase()} used`
}

function isFutureResetDate(resetDate: string | undefined): boolean {
  if (resetDate === undefined) {
    return false
  }

  return new Date(resetDate).getTime() > Date.now()
}

function pluralize(value: number, singular: string): string {
  return value === 1 ? singular : `${singular}s`
}

function formatResetText(resetDate: string): string | null {
  const millisecondsUntilReset = new Date(resetDate).getTime() - Date.now()
  if (!Number.isFinite(millisecondsUntilReset) || millisecondsUntilReset <= 0) {
    return null
  }

  const minutes = Math.ceil(millisecondsUntilReset / (60 * 1000))
  if (minutes < 60) {
    return `resets in ${minutes} ${pluralize(minutes, 'minute')}`
  }

  const hours = Math.ceil(minutes / 60)
  if (hours < 24) {
    return `resets in ${hours} ${pluralize(hours, 'hour')}`
  }

  const days = Math.ceil(hours / 24)
  return `resets in ${days} ${pluralize(days, 'day')}`
}

function QuotaProgressBar({ snapshot }: IQuotaProgressBarProps) {
  const usedPercentage = getUsedPercentage(snapshot)
  const disabled = snapshot.isUnlimitedEntitlement

  return (
    <div
      className={`copilot-snapshot-progress${disabled ? ' disabled' : ''}`}
      role="progressbar"
      aria-valuenow={usedPercentage}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuetext={disabled ? 'No usage limit' : undefined}
      aria-label={disabled ? 'No usage limit' : `${usedPercentage}% quota used`}
    >
      <div
        className="copilot-snapshot-progress-value"
        style={{ width: `${usedPercentage}%` }}
      />
    </div>
  )
}

function SnapshotUsageItem({
  item,
  tokenBasedBilling = false,
}: ISnapshotUsageItemProps) {
  const { snapshot, displayName } = item
  const usageLabel = snapshot.isUnlimitedEntitlement
    ? 'No usage limit'
    : formatUsedPercentage(snapshot)
  const usageTooltip = formatUsageTooltip(snapshot, displayName)
  const resetText =
    snapshot.resetDate !== undefined && isFutureResetDate(snapshot.resetDate)
      ? formatResetText(snapshot.resetDate)
      : null
  const showMonthlyResetFallback =
    item.key === 'premium_interactions' &&
    tokenBasedBilling &&
    !snapshot.isUnlimitedEntitlement &&
    resetText === null

  return (
    <div className="copilot-snapshot-item">
      <div className="copilot-snapshot-header">
        <span className="copilot-snapshot-title">
          <span className="copilot-snapshot-name">{displayName}</span>
          {!snapshot.isUnlimitedEntitlement && resetText !== null ? (
            <span className="copilot-snapshot-reset">({resetText})</span>
          ) : showMonthlyResetFallback ? (
            <span className="copilot-snapshot-reset">(resets monthly)</span>
          ) : null}
        </span>
        <TooltippedContent
          tooltip={usageTooltip}
          direction={TooltipDirection.NORTH}
          className="copilot-snapshot-usage"
        >
          {usageLabel}
        </TooltippedContent>
      </div>
      <QuotaProgressBar snapshot={snapshot} />
    </div>
  )
}

export class SnapshotCard extends React.Component<ISnapshotCardProps> {
  public render() {
    const { account, snapshots, onConfigureModels } = this.props
    const avatarUser = getAccountAvatarUser(account)

    return (
      <div className="copilot-snapshot-card">
        <div className="copilot-snapshot-account">
          <div className="copilot-snapshot-account-identity">
            <Avatar
              accounts={[account]}
              user={avatarUser}
              size={34}
              tooltip={false}
            />
            <div className="copilot-snapshot-account-info">
              {isEnterpriseAccount(account) ? (
                <>
                  <div className="account-title">
                    {account.name === account.login
                      ? `@${account.login}`
                      : `@${account.login} (${account.name})`}
                  </div>
                  <div className="endpoint">{getHTMLURL(account.endpoint)}</div>
                </>
              ) : (
                <>
                  <div className="name">{account.name}</div>
                  <div className="login">@{account.login}</div>
                </>
              )}
            </div>
          </div>
          {onConfigureModels !== undefined && (
            <Button onClick={this.onConfigureModelsClick}>Configure…</Button>
          )}
        </div>
        {snapshots === null
          ? renderLoadingSnapshots()
          : renderSnapshots(snapshots)}
      </div>
    )
  }

  private onConfigureModelsClick = () => {
    this.props.onConfigureModels?.(this.props.account)
  }
}

function renderLoadingSnapshots(): JSX.Element {
  return <p className="copilot-usage-empty">Loading Copilot usage…</p>
}

function renderSnapshots(snapshots: CopilotQuotaSnapshots): JSX.Element {
  const tokenBasedBilling = hasTokenBasedBilling(snapshots)
  const rateLimits = getVisibleRateLimitSnapshots(snapshots, tokenBasedBilling)
  const quotas = getVisibleQuotaSnapshots(snapshots, tokenBasedBilling)

  if (rateLimits.length === 0 && quotas.length === 0) {
    return (
      <p className="copilot-usage-empty">
        No Copilot usage data available yet.
      </p>
    )
  }

  return (
    <>
      {rateLimits.length > 0 && (
        <div className="copilot-snapshot-card-list">
          {rateLimits.map(item => (
            <SnapshotUsageItem key={item.key} item={item} />
          ))}
        </div>
      )}
      {quotas.length > 0 && (
        <div className="copilot-snapshot-card-list">
          {quotas.map(item => (
            <SnapshotUsageItem
              key={item.key}
              item={item}
              tokenBasedBilling={tokenBasedBilling}
            />
          ))}
        </div>
      )}
    </>
  )
}

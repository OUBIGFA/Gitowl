import type {
  CopilotQuotaSnapshots,
  ICopilotQuotaSnapshot,
} from '../../lib/stores/copilot-store'

/** Original SnapshotCard visibility and quota math. Both presentation hosts use
 * these pure functions; no Avatar/React/Web component is imported natively. */
export interface ICategorizedSnapshot {
  readonly key: string
  readonly displayName: string
  readonly snapshot: ICopilotQuotaSnapshot
}
const snapshotDisplayNames: Record<string, string> = {
  chat: 'Chat messages',
  completions: 'Code completions',
  premium_interactions: 'Premium requests',
  session: 'Session limits',
  weekly: 'Weekly limits',
}
const tokenBasedSnapshotDisplayNames: Record<string, string> = {
  premium_interactions: 'AI credits',
}
const quotaKeys = ['chat', 'completions', 'premium_interactions']
const rateLimitKeys = ['session', 'weekly']
export function hasTokenBasedBilling(
  snapshots: CopilotQuotaSnapshots
): boolean {
  for (const snapshot of snapshots.values()) {
    if (snapshot.tokenBasedBilling) {
      return true
    }
  }
  return false
}
export function getUsedPercentage(snapshot: ICopilotQuotaSnapshot): number {
  return snapshot.isUnlimitedEntitlement
    ? 0
    : Math.max(0, Math.min(100, Math.round(100 - snapshot.remainingPercentage)))
}
function isQuotaVisible(snapshot: ICopilotQuotaSnapshot): boolean {
  return snapshot.isUnlimitedEntitlement || snapshot.entitlementRequests > 0
}
function isRateLimitVisible(snapshot: ICopilotQuotaSnapshot): boolean {
  return !snapshot.isUnlimitedEntitlement && snapshot.remainingPercentage < 100
}
function getCategorizedSnapshot(
  key: string,
  snapshot: ICopilotQuotaSnapshot,
  tokenBasedBilling: boolean
): ICategorizedSnapshot {
  const displayName = tokenBasedBilling
    ? tokenBasedSnapshotDisplayNames[key]
    : undefined
  return {
    key,
    displayName: displayName ?? snapshotDisplayNames[key] ?? key,
    snapshot,
  }
}
export function getVisibleQuotaSnapshots(
  snapshots: CopilotQuotaSnapshots,
  tokenBasedBilling: boolean
): ReadonlyArray<ICategorizedSnapshot> {
  const visible: ICategorizedSnapshot[] = []
  const premium = snapshots.get('premium_interactions')
  const hasPremium = premium !== undefined && isQuotaVisible(premium)
  for (const key of quotaKeys) {
    const snapshot = snapshots.get(key)
    if (
      snapshot === undefined ||
      !isQuotaVisible(snapshot) ||
      (tokenBasedBilling && key === 'chat' && hasPremium)
    ) {
      continue
    }
    visible.push(getCategorizedSnapshot(key, snapshot, tokenBasedBilling))
  }
  return visible
}
export function getVisibleRateLimitSnapshots(
  snapshots: CopilotQuotaSnapshots,
  tokenBasedBilling: boolean
): ReadonlyArray<ICategorizedSnapshot> {
  const visible: ICategorizedSnapshot[] = []
  for (const key of rateLimitKeys) {
    const snapshot = snapshots.get(key)
    if (snapshot !== undefined && isRateLimitVisible(snapshot)) {
      visible.push(getCategorizedSnapshot(key, snapshot, tokenBasedBilling))
    }
  }
  return visible
}

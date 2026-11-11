import { Account } from '../../models/account'
import type {
  CopilotQuotaSnapshots,
  ICopilotQuotaSnapshot,
} from '../../lib/stores/copilot-store'

/** Original editable fake snapshots. They are a UI fixture, never saved to an
 * account store or passed to the real Copilot SDK. */
export type SnapshotKey =
  | 'chat'
  | 'completions'
  | 'premium_interactions'
  | 'session'
  | 'weekly'
export interface IEditableSnapshot {
  readonly enabled: boolean
  readonly tokenBasedBilling: boolean
  readonly isUnlimitedEntitlement: boolean
  readonly entitlementRequests: string
  readonly usedRequests: string
  readonly usageAllowedWithExhaustedQuota: boolean
  readonly remainingPercentage: string
  readonly overage: string
  readonly overageAllowedWithExhaustedQuota: boolean
  readonly resetDate: string
}
export type IEditableSnapshots = Readonly<
  Record<SnapshotKey, IEditableSnapshot>
>
export interface SnapshotTestDraft {
  readonly login: string
  readonly name: string
  readonly endpoint: string
  readonly avatarURL: string
  readonly snapshots: IEditableSnapshots
}
export interface ISnapshotDefinition {
  readonly key: SnapshotKey
  readonly label: string
}
export const snapshotDefinitions: ReadonlyArray<ISnapshotDefinition> = [
  { key: 'chat', label: 'Chat messages' },
  { key: 'completions', label: 'Code completions' },
  { key: 'premium_interactions', label: 'Premium requests / AI credits' },
  { key: 'session', label: 'Session limits' },
  { key: 'weekly', label: 'Weekly limits' },
]
export function defaultSnapshot(
  overrides: Partial<IEditableSnapshot> = {}
): IEditableSnapshot {
  return {
    enabled: true,
    tokenBasedBilling: false,
    isUnlimitedEntitlement: false,
    entitlementRequests: '100',
    usedRequests: '25',
    usageAllowedWithExhaustedQuota: false,
    remainingPercentage: '75',
    overage: '0',
    overageAllowedWithExhaustedQuota: false,
    resetDate: '',
    ...overrides,
  }
}
export function getDefaultSnapshots(): IEditableSnapshots {
  return {
    chat: defaultSnapshot(),
    completions: defaultSnapshot({ enabled: false }),
    premium_interactions: defaultSnapshot({
      entitlementRequests: '300',
      usedRequests: '90',
      remainingPercentage: '70',
    }),
    session: defaultSnapshot({
      enabled: false,
      entitlementRequests: '0',
      usedRequests: '0',
      remainingPercentage: '100',
    }),
    weekly: defaultSnapshot({
      enabled: false,
      entitlementRequests: '0',
      usedRequests: '0',
      remainingPercentage: '100',
    }),
  }
}
function parseNumber(value: string, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}
function getResetDate(value: string): string | undefined {
  const trimmed = value.trim()
  return trimmed.length > 0 ? trimmed : undefined
}
function getSnapshot(snapshot: IEditableSnapshot): ICopilotQuotaSnapshot {
  return {
    tokenBasedBilling: snapshot.tokenBasedBilling,
    isUnlimitedEntitlement: snapshot.isUnlimitedEntitlement,
    entitlementRequests: parseNumber(snapshot.entitlementRequests, 0),
    usedRequests: parseNumber(snapshot.usedRequests, 0),
    usageAllowedWithExhaustedQuota: snapshot.usageAllowedWithExhaustedQuota,
    remainingPercentage: parseNumber(snapshot.remainingPercentage, 100),
    overage: parseNumber(snapshot.overage, 0),
    overageAllowedWithExhaustedQuota: snapshot.overageAllowedWithExhaustedQuota,
    resetDate: getResetDate(snapshot.resetDate),
  }
}
export function getInitialAccount(
  accounts: ReadonlyArray<Account>
): Omit<SnapshotTestDraft, 'snapshots'> {
  const account = accounts.find(a => a.login.trim().length > 0)
  return {
    login: account?.login ?? 'mona',
    name: account?.name ?? 'Mona Lisa',
    endpoint: account?.endpoint ?? 'https://api.github.com',
    avatarURL:
      account?.avatarURL ?? 'https://avatars.githubusercontent.com/u/1',
  }
}
export function getTestSnapshotAccount(
  draft: Omit<SnapshotTestDraft, 'snapshots'>
): Account {
  return new Account(
    draft.login.trim() || 'mona',
    draft.endpoint.trim() || 'https://api.github.com',
    'fake-token',
    [],
    draft.avatarURL.trim(),
    1,
    draft.name.trim(),
    'free',
    'https://copilot-proxy.githubusercontent.com',
    true,
    [],
    'COPILOT_INDIVIDUAL'
  )
}
export function getTestSnapshots(
  editable: IEditableSnapshots
): CopilotQuotaSnapshots {
  const snapshots = new Map<string, ICopilotQuotaSnapshot>()
  for (const definition of snapshotDefinitions) {
    const snapshot = editable[definition.key]
    if (snapshot.enabled) {
      snapshots.set(definition.key, getSnapshot(snapshot))
    }
  }
  return snapshots
}
export function tokenBasedBillingTestPreset(): IEditableSnapshots {
  return {
    chat: defaultSnapshot({
      tokenBasedBilling: true,
      isUnlimitedEntitlement: true,
      entitlementRequests: '-1',
      usedRequests: '0',
      remainingPercentage: '100',
    }),
    completions: defaultSnapshot({
      tokenBasedBilling: true,
      isUnlimitedEntitlement: true,
      entitlementRequests: '-1',
      usedRequests: '0',
      remainingPercentage: '100',
    }),
    premium_interactions: defaultSnapshot({
      tokenBasedBilling: true,
      entitlementRequests: '12.5',
      usedRequests: '2.5',
      remainingPercentage: '80',
    }),
    session: defaultSnapshot({
      enabled: false,
      tokenBasedBilling: true,
      entitlementRequests: '0',
      usedRequests: '0',
      remainingPercentage: '100',
    }),
    weekly: defaultSnapshot({
      enabled: false,
      tokenBasedBilling: true,
      entitlementRequests: '0',
      usedRequests: '0',
      remainingPercentage: '100',
    }),
  }
}
export function rateLimitTestPreset(): IEditableSnapshots {
  return {
    ...getDefaultSnapshots(),
    session: defaultSnapshot({
      enabled: true,
      entitlementRequests: '0',
      usedRequests: '0',
      remainingPercentage: '35',
      resetDate: new Date(Date.now() + 45 * 60 * 1000).toISOString(),
    }),
    weekly: defaultSnapshot({
      enabled: true,
      entitlementRequests: '0',
      usedRequests: '0',
      remainingPercentage: '92',
    }),
  }
}

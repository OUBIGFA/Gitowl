import { getBoolean } from '../local-storage'

/** Shared by telemetry and Git error reporting without loading renderer stores. */
export const StatsOptOutKey = 'stats-opt-out'

/** Return the saved reporting choice; undefined means no choice was stored. */
export function getHasOptedOutOfStats() {
  return getBoolean(StatsOptOutKey)
}

import { formatDate } from './format-date'
import { formatRelative } from './format-relative'

// Shared with the original React component; native presentation keeps its timing policy.
const SECOND = 1000
const MINUTE = SECOND * 60
const HOUR = MINUTE * 60
const DAY = HOUR * 24

type RelativeTimeInfo = {
  absoluteText: string
  relativeText: string
  duration?: number
}

export function getRelativeTimeInfoFromDate(
  then: Date,
  onlyRelative: boolean = true,
  locale: string = 'en-US'
): RelativeTimeInfo {
  const diff = then.getTime() - Date.now()
  const duration = Math.abs(diff)

  const absoluteText = formatDate(then, {
    locale,
    dateStyle: 'full',
    timeStyle: 'short',
  })

  const relativeText = formatRelative(diff, locale)

  // Future date, let's just show as absolute and reschedule. If it's less
  // than a minute into the future we'll treat it as 'just now'.
  if (diff > 0 && duration > MINUTE) {
    return {
      absoluteText,
      relativeText: formatDate(then, {
        locale,
        dateStyle: 'medium',
        timeStyle: 'short',
      }),
      duration,
    }
  } else if (duration < MINUTE) {
    return {
      absoluteText,
      relativeText: locale.startsWith('en')
        ? 'just now'
        : formatRelative(0, locale),
      duration: MINUTE - duration,
    }
  } else if (duration < HOUR) {
    return { absoluteText, relativeText, duration: MINUTE }
  } else if (duration < DAY) {
    return { absoluteText, relativeText, duration: HOUR }
  } else if (duration < 7 * DAY) {
    return { absoluteText, relativeText, duration: 6 * HOUR }
  } else {
    if (onlyRelative) {
      return { absoluteText, relativeText, duration: 6 * HOUR }
    } else {
      // More than a week ago, just the date will suffice
      return {
        absoluteText,
        relativeText: formatDate(then, {
          time: false,
          dateStyle: 'medium',
          locale,
        }),
      }
    }
  }
}

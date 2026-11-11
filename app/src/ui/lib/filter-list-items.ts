import { IMatch, match } from '../../lib/fuzzy-find'

/** Shared exact filtering order for augmented lists: options first, then the
 * original fuzzaldrin scoring/sort (not substring matching). */
export function filterListItems<
  T extends { readonly text: ReadonlyArray<string> }
>(
  items: ReadonlyArray<T>,
  filterText: string,
  filterMethod?: (item: T) => boolean
): ReadonlyArray<IMatch<T>> {
  const filter = filterText.toLowerCase()
  const candidates = filterMethod ? items.filter(filterMethod) : items
  return filter
    ? match(filter, candidates, item => item.text)
    : candidates.map(item => ({
        score: 1,
        matches: { title: [], subtitle: [] },
        item,
      }))
}

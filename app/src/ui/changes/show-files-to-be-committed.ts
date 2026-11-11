import type { Dispatcher } from '../dispatcher'
import type { Repository } from '../../models/repository'

/** Original FilterChangesList recovery action for hidden selected changes. */
export function showFilesToBeCommitted(dispatcher: Dispatcher, repository: Repository) {
  dispatcher.incrementMetric('adjustedFiltersForHiddenChangesCount')
  dispatcher.setChangesListFilterText(repository, '')
  dispatcher.setFilterExcludedFiles(repository, false)
  dispatcher.setFilterNewFiles(repository, false)
  dispatcher.setFilterModifiedFiles(repository, false)
  dispatcher.setFilterDeletedFiles(repository, false)
  dispatcher.setIncludedChangesInCommitFilter(repository, true)
}

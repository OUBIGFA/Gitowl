import * as React from 'react'
import memoize from 'memoize-one'
import { GitHubRepository } from '../../models/github-repository'
import { Commit, CommitOneLine } from '../../models/commit'
import { CommitListItem } from './commit-list-item'
import { KeyboardInsertionData, List } from '../lib/list'
import { arrayEquals } from '../../lib/equality'
import { DragData, DragType } from '../../models/drag-drop'
import classNames from 'classnames'
import memoizeOne from 'memoize-one'
import { showContextualMenu } from '../../lib/menu-item'
import { planWorkspaceCommitInsertion } from './history-workspace-controller'
import { generateCommitContextMenu } from './commit-menu-controller'
import { RowIndexPath } from '../lib/list/list-row-index-path'
import { assertNever } from '../../lib/fatal-error'
import { CommitDragElement } from '../drag-elements/commit-drag-element'
import { AriaLiveContainer } from '../accessibility/aria-live-container'
import debounce from 'lodash/debounce'
import {
  Popover,
  PopoverAnchorPosition,
  PopoverScreenBorderPadding,
} from '../lib/popover'
import { KeyboardShortcut } from '../keyboard-shortcut/keyboard-shortcut'
import { Account } from '../../models/account'
import { Emoji } from '../../lib/emoji'
import { getAvatarUsersForCommit, IAvatarUser } from '../../models/avatar'
import { formatDate } from '../../lib/format-date'
import { Avatar } from '../lib/avatar'
import { Octicon } from '../octicons'
import * as octicons from '../octicons/octicons.generated'

const RowHeight = 50

interface ICommitListProps {
  /** The GitHub repository associated with this commit (if found) */
  readonly gitHubRepository: GitHubRepository | null

  /** The list of commits SHAs to display, in order. */
  readonly commitSHAs: ReadonlyArray<string>

  /** The commits loaded, keyed by their full SHA. */
  readonly commitLookup: Map<string, Commit>

  /** The SHAs of the selected commits */
  readonly selectedSHAs: ReadonlyArray<string>

  /** Whether or not commits in this list can be undone. */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly canUndoCommits?: boolean

  /** Whether or not commits in this list can be amended. */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly canAmendCommits?: boolean

  /** Whether or the user can reset to commits in this list. */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly canResetToCommits?: boolean

  /** The emoji lookup to render images inline */
  readonly emoji: Map<string, Emoji>

  /** The list of known local commits for the current branch */
  readonly localCommitSHAs: ReadonlyArray<string>

  /** The message to display inside the list when no results are displayed */
  readonly emptyListMessage?: JSX.Element | string

  /** Data to be reordered via keyboard */
  readonly keyboardReorderData?: KeyboardInsertionData

  /** Callback which fires when a commit has been selected in the list */
  readonly onCommitsSelected?: (
    commits: ReadonlyArray<Commit>,
    isContiguous: boolean
  ) => void

  /** Callback that fires when a scroll event has occurred */
  readonly onScroll?: (start: number, end: number) => void

  /** Callback to fire to undo a given commit in the current repository */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onUndoCommit?: (commit: Commit) => void

  /** Callback to fire to reset to a given commit in the current repository */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onResetToCommit?: (commit: Commit) => void

  /** Callback to fire to revert a given commit in the current repository */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onRevertCommit?: (commit: Commit) => void

  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onAmendCommit?: (commit: Commit, isLocalCommit: boolean) => void

  /** Callback to fire to open a given commit on GitHub */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onViewCommitOnGitHub?: (sha: string) => void

  /** Callback to fire to cancel a keyboard reordering operation */
  readonly onCancelKeyboardReorder?: () => void

  /**
   * Callback to fire to create a branch from a given commit in the current
   * repository
   */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onCreateBranch?: (commit: CommitOneLine) => void

  /**
   * Callback to fire to checkout the selected commit in the current
   * repository
   */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onCheckoutCommit?: (commit: CommitOneLine) => void

  /** Callback to fire to open the dialog to create a new tag on the given commit */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onCreateTag?: (targetCommitSha: string) => void

  /** Callback to fire to delete an unpushed tag */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onDeleteTag?: (tagName: string) => void

  /**
   * A handler called whenever the user drops commits on the list to be inserted.
   *
   * @param baseCommit - The commit before the selected commits will be inserted.
   *                     This will be null when commits must be inserted at the
   *                     end of the list.
   * @param commitsToInsert -  The commits dropped by the user.
   */
  readonly onDropCommitInsertion?: (
    baseCommit: Commit | null,
    commitsToInsert: ReadonlyArray<Commit>,
    lastRetainedCommitRef: string | null
  ) => void

  /** Callback to fire to cherry picking the commit  */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onCherryPick?: (commits: ReadonlyArray<CommitOneLine>) => void

  /** Callback to fire to start reordering commits with the keyboard */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly onKeyboardReorder?: (toReorder: ReadonlyArray<Commit>) => void

  /** Callback to fire to squashing commits  */
  readonly onSquash?: (
    toSquash: ReadonlyArray<Commit>,
    squashOnto: Commit,
    lastRetainedCommitRef: string | null,
    isInvokedByContextMenu: boolean
  ) => void

  /**
   * Optional callback that fires on page scroll in order to allow passing
   * a new scrollTop value up to the parent component for storing.
   */
  readonly onCompareListScrolled?: (scrollTop: number) => void

  /* The scrollTop of the compareList. It is stored to allow for scroll position persistence */
  readonly compareListScrollTop?: number

  /* Whether the repository is local (it has no remotes) */
  readonly isLocalRepository: boolean

  /* Tags that haven't been pushed yet. This is used to show the unpushed indicator */
  readonly tagsToPush?: ReadonlyArray<string>

  /** Whether or not commits in this list can be reordered. */
  readonly reorderingEnabled?: boolean

  /** Whether a multi commit operation is in progress (in particular the
   * conflicts resolution step allows interaction with history) */
  readonly isMultiCommitOperationInProgress?: boolean

  /** Whether assisted execution or retained recovery locks this repository. */
  readonly isAssistedCommitRepositoryLocked?: boolean

  /** Callback to render commit drag element */
  readonly onRenderCommitDragElement?: (
    commit: Commit,
    selectedCommits: ReadonlyArray<Commit>
  ) => void

  /** Callback to remove commit drag element */
  readonly onRemoveCommitDragElement?: () => void

  /** Whether reordering should be enabled on the commit list */
  // eslint-disable-next-line react/no-unused-prop-types -- read via this.props by commit menu controller
  readonly disableReordering?: boolean

  /** Whether squashing should be enabled on the commit list */
  readonly disableSquashing?: boolean

  /** Shas that should be highlighted */
  readonly shasToHighlight?: ReadonlyArray<string>

  readonly accounts: ReadonlyArray<Account>

  readonly preferAbsoluteDates: boolean

  /** This will make the list semantics friendly to screen reader users in browse mode. */
  readonly isInformationalView?: boolean
}

interface ICommitListState {
  /**
   * Aria live message used to guide users through the keyboard reordering
   * process.
   */
  readonly reorderingMessage: string
}

/** A component which displays the list of commits. */
export class CommitList extends React.Component<
  ICommitListProps,
  ICommitListState
> {
  private commitsHash = memoize(makeCommitsHash, arrayEquals)
  private commitIndexBySha = memoizeOne(
    (commitSHAs: ReadonlyArray<string>) =>
      new Map(commitSHAs.map((sha, index) => [sha, index]))
  )

  private containerRef = React.createRef<HTMLDivElement>()
  private listRef = React.createRef<List>()

  // This function is debounced to avoid updating the aria live region too
  // frequently on every key press.
  private updateKeyboardReorderingMessage = debounce(
    (insertionIndexPath: RowIndexPath | null) => {
      const { keyboardReorderData } = this.props

      if (keyboardReorderData === undefined) {
        this.setState({ reorderingMessage: '' })
        return
      }

      const plural = keyboardReorderData.commits.length === 1 ? '' : 's'

      if (insertionIndexPath !== null) {
        const { row } = insertionIndexPath

        const insertionPoint =
          row < this.props.commitSHAs.length
            ? `before commit ${row + 1}`
            : `after commit ${row}`

        this.setState({
          reorderingMessage: `Press Enter to insert the selected commit${plural} ${insertionPoint} or Escape to cancel.`,
        })
        return
      }

      this.setState({
        reorderingMessage: `Use the Up and Down arrow keys to choose a new location for the selected commit${plural}, then press Enter to confirm or Escape to cancel.`,
      })
    },
    500
  )

  public constructor(props: ICommitListProps) {
    super(props)

    this.state = { reorderingMessage: '' }
  }

  public componentDidUpdate(prevProps: ICommitListProps) {
    if (this.props.keyboardReorderData !== prevProps.keyboardReorderData) {
      this.updateKeyboardReorderingMessage(null)
    }
  }

  private getVisibleCommits(): ReadonlyArray<Commit> {
    const commits = new Array<Commit>()
    for (const sha of this.props.commitSHAs) {
      const commitMaybe = this.props.commitLookup.get(sha)
      // this should never be undefined, but just in case
      if (commitMaybe !== undefined) {
        commits.push(commitMaybe)
      }
    }
    return commits
  }

  private isLocalCommit = (sha: string) =>
    this.props.localCommitSHAs.includes(sha)

  private renderCommit = (row: number) => {
    const sha = this.props.commitSHAs[row]
    const commit = this.props.commitLookup.get(sha)

    if (commit == null) {
      if (__DEV__) {
        log.warn(
          `[CommitList]: the commit '${sha}' does not exist in the cache`
        )
      }
      return null
    }

    const isLocal = this.isLocalCommit(commit.sha)
    const unpushedTags = this.getUnpushedTags(commit)

    const showUnpushedIndicator =
      (isLocal || unpushedTags.length > 0) &&
      this.props.isLocalRepository === false

    return (
      <CommitListItem
        key={commit.sha}
        gitHubRepository={this.props.gitHubRepository}
        showUnpushedIndicator={showUnpushedIndicator}
        unpushedIndicatorTitle={this.getUnpushedIndicatorTitle(
          isLocal,
          unpushedTags.length
        )}
        commit={commit}
        emoji={this.props.emoji}
        isDraggable={
          this.props.isMultiCommitOperationInProgress === false &&
          this.props.isAssistedCommitRepositoryLocked !== true &&
          !this.inKeyboardReorderMode
        }
        onSquash={this.onSquash}
        selectedCommits={this.selectedCommits}
        onRenderCommitDragElement={this.onRenderCommitDragElement}
        onRemoveDragElement={this.props.onRemoveCommitDragElement}
        disableSquashing={this.props.disableSquashing}
        accounts={this.props.accounts}
        preferAbsoluteDates={this.props.preferAbsoluteDates}
      />
    )
  }

  private get inKeyboardReorderMode() {
    return this.props.keyboardReorderData !== undefined
  }

  private getLastRetainedCommitRef(indexes: ReadonlyArray<number>) {
    const maxIndex = Math.max(...indexes)
    const lastIndex = this.props.commitSHAs.length - 1
    /* If the commit is the first commit in the branch, you cannot reference it
    using the sha */
    const lastRetainedCommitRef =
      maxIndex !== lastIndex ? `${this.props.commitSHAs[maxIndex]}^` : null
    return lastRetainedCommitRef
  }

  private onSquash = (
    toSquash: ReadonlyArray<Commit>,
    squashOnto: Commit,
    isInvokedByContextMenu: boolean
  ) => {
    const indexes = [...toSquash, squashOnto].map(v =>
      this.props.commitSHAs.findIndex(sha => sha === v.sha)
    )
    this.props.onSquash?.(
      toSquash,
      squashOnto,
      this.getLastRetainedCommitRef(indexes),
      isInvokedByContextMenu
    )
  }

  private onRenderCommitDragElement = (commit: Commit) => {
    this.props.onRenderCommitDragElement?.(commit, this.selectedCommits)
  }

  private getUnpushedIndicatorTitle(
    isLocalCommit: boolean,
    numUnpushedTags: number
  ) {
    if (isLocalCommit) {
      return 'This commit has not been pushed to the remote repository'
    }

    if (numUnpushedTags > 0) {
      return `This commit has ${numUnpushedTags} tag${
        numUnpushedTags > 1 ? 's' : ''
      } to push`
    }

    return undefined
  }

  private get selectedCommits() {
    return this.lookupCommits(this.props.selectedSHAs)
  }

  private getUnpushedTags(commit: Commit) {
    const tagsToPushSet = new Set(this.props.tagsToPush ?? [])
    return commit.tags.filter(tagName => tagsToPushSet.has(tagName))
  }

  private onSelectionChanged = (rows: ReadonlyArray<number>) => {
    const selectedShas = rows.map(r => this.props.commitSHAs[r])
    const selectedCommits = this.lookupCommits(selectedShas)
    this.props.onCommitsSelected?.(selectedCommits, this.isContiguous(rows))
  }

  /**
   * Accepts a sorted array of numbers in descending order. If the numbers ar
   * contiguous order, 4, 3, 2 not 5, 3, 1, returns true.
   *
   * Defined an array of 0 and 1 are considered contiguous.
   */
  private isContiguous(indexes: ReadonlyArray<number>) {
    if (indexes.length <= 1) {
      return true
    }

    const sorted = indexes.toSorted((a, b) => b - a)

    for (let i = 0; i < sorted.length; i++) {
      const current = sorted[i]
      if (i + 1 === sorted.length) {
        continue
      }

      if (current - 1 !== sorted[i + 1]) {
        return false
      }
    }

    return true
  }

  // This is required along with onSelectedRangeChanged in the case of a user
  // paging up/down or using arrow keys up/down.
  private onSelectedRowChanged = (row: number) => {
    const sha = this.props.commitSHAs[row]
    const commit = this.props.commitLookup.get(sha)
    if (commit) {
      this.props.onCommitsSelected?.([commit], true)
    }
  }

  private lookupCommits(
    commitSHAs: ReadonlyArray<string>
  ): ReadonlyArray<Commit> {
    const commits: Commit[] = []
    commitSHAs.forEach(sha => {
      const commit = this.props.commitLookup.get(sha)
      if (commit === undefined) {
        log.warn(
          '[Commit List] - Unable to lookup commit from sha - This should not happen.'
        )
        return
      }
      commits.push(commit)
    })
    return commits
  }

  private onScroll = (scrollTop: number, clientHeight: number) => {
    const numberOfRows = Math.ceil(clientHeight / RowHeight)
    const top = Math.floor(scrollTop / RowHeight)
    const bottom = top + numberOfRows
    this.props.onScroll?.(top, bottom)

    // Pass new scroll value so the scroll position will be remembered (if the callback has been supplied).
    this.props.onCompareListScrolled?.(scrollTop)
  }

  private rowForSHA(sha: string) {
    return this.commitIndexBySha(this.props.commitSHAs).get(sha) ?? -1
  }

  private getRowCustomClassMap = () => {
    const { commitSHAs, shasToHighlight } = this.props
    if (shasToHighlight === undefined || shasToHighlight.length === 0) {
      return undefined
    }

    const rowsForShasNotInDiff = commitSHAs
      .filter(sha => shasToHighlight.includes(sha))
      .map(sha => this.rowForSHA(sha))

    if (rowsForShasNotInDiff.length === 0) {
      return undefined
    }

    const rowClassMap = new Map<string, ReadonlyArray<number>>()
    rowClassMap.set('highlighted', rowsForShasNotInDiff)
    return rowClassMap
  }

  private renderExpandedAuthor(user: IAvatarUser): string | JSX.Element {
    if (!user) {
      return 'Unknown user'
    }

    if (user.name) {
      return (
        <>
          <div>{user.name}</div>
          <div>{user.email}</div>
        </>
      )
    }

    return user.email
  }

  private renderRowFocusTooltip = (indexPath: RowIndexPath | undefined) => {
    if (!indexPath) {
      return null
    }
    const row = indexPath.row
    const sha = this.props.commitSHAs[row]
    const commit = this.props.commitLookup.get(sha)
    if (!commit) {
      return null
    }

    const avatarUsers = getAvatarUsersForCommit(
      this.props.gitHubRepository,
      commit
    )

    const {
      author: { date },
    } = commit

    const absoluteDate = formatDate(date, {
      dateStyle: 'full',
      timeStyle: 'short',
    })

    const authorList = avatarUsers.map((user, i) => {
      return (
        <div className="author" key={i}>
          <div className="label">
            <Avatar accounts={this.props.accounts} user={user} title={null} />
          </div>
          <div>{this.renderExpandedAuthor(user)}</div>
        </div>
      )
    })

    const isLocal = this.isLocalCommit(commit.sha)
    const unpushedTags = this.getUnpushedTags(commit)

    const showUnpushedIndicator =
      (isLocal || unpushedTags.length > 0) &&
      this.props.isLocalRepository === false

    return (
      <div className="commit-list-item-tooltip list-item-tooltip">
        {authorList}
        <div>
          <div className="label">Date: </div>
          {absoluteDate}
        </div>
        {showUnpushedIndicator ? (
          <div>
            <div className="label">
              <span className="unpushed-indicator">
                <Octicon symbol={octicons.arrowUp} />
              </span>
            </div>
            <div>
              {this.getUnpushedIndicatorTitle(isLocal, unpushedTags.length)}
            </div>
          </div>
        ) : null}
      </div>
    )
  }

  public focus() {
    this.listRef.current?.focus()
  }

  public render() {
    const {
      commitSHAs,
      selectedSHAs,
      shasToHighlight,
      emptyListMessage,
      reorderingEnabled,
      isMultiCommitOperationInProgress,
      isAssistedCommitRepositoryLocked,
    } = this.props
    if (commitSHAs.length === 0) {
      return (
        <div className="panel blankslate">
          {emptyListMessage ?? 'No commits to list'}
        </div>
      )
    }

    const classes = classNames({
      'has-highlighted-commits':
        shasToHighlight !== undefined && shasToHighlight.length > 0,
    })

    const selectedRows = selectedSHAs
      .map(sha => this.rowForSHA(sha))
      .filter(r => r !== -1)

    return (
      <div id="commit-list" className={classes} ref={this.containerRef}>
        {this.renderReorderCommitsHint()}
        <List
          ariaLabel="Commits"
          role={this.props.isInformationalView === true ? 'list' : 'listbox'}
          ref={this.listRef}
          rowCount={commitSHAs.length}
          rowHeight={RowHeight}
          selectedRows={selectedRows}
          rowRenderer={this.renderCommit}
          onDropDataInsertion={this.onDropDataInsertion}
          onSelectionChanged={this.onSelectionChanged}
          onSelectedRowChanged={this.onSelectedRowChanged}
          onKeyboardInsertionIndexPathChanged={
            this.onKeyboardInsertionIndexPathChanged
          }
          onCancelKeyboardInsertion={this.props.onCancelKeyboardReorder}
          onConfirmKeyboardInsertion={this.onConfirmKeyboardReorder}
          onRowContextMenu={this.onRowContextMenu}
          selectionMode="multi"
          onScroll={this.onScroll}
          keyboardInsertionData={this.props.keyboardReorderData}
          keyboardInsertionElementRenderer={this.renderKeyboardInsertionElement}
          insertionDragType={
            reorderingEnabled === true &&
            isMultiCommitOperationInProgress === false &&
            isAssistedCommitRepositoryLocked !== true
              ? DragType.Commit
              : undefined
          }
          invalidationProps={{
            commits: this.props.commitSHAs,
            localCommitSHAs: this.props.localCommitSHAs,
            commitLookupHash: this.commitsHash(this.getVisibleCommits()),
            tagsToPush: this.props.tagsToPush,
            shasToHighlight: this.props.shasToHighlight,
            preferAbsoluteDates: this.props.preferAbsoluteDates,
            isAssistedCommitRepositoryLocked,
          }}
          setScrollTop={this.props.compareListScrollTop}
          rowCustomClassNameMap={this.getRowCustomClassMap()}
          renderRowFocusTooltip={this.renderRowFocusTooltip}
        />
        <AriaLiveContainer message={this.state.reorderingMessage} />
      </div>
    )
  }

  private renderReorderCommitsHint = () => {
    if (!this.inKeyboardReorderMode) {
      return null
    }

    const containerWidth = this.containerRef.current?.clientWidth ?? 0
    const reorderCommitsHintTitle = __DARWIN__
      ? 'Reorder Commits'
      : 'Reorder commits'

    return (
      <Popover
        className="reorder-commits-hint-popover"
        anchor={this.containerRef.current}
        anchorOffset={PopoverScreenBorderPadding}
        anchorPosition={PopoverAnchorPosition.Top}
        isDialog={false}
        trapFocus={false}
        style={{
          width: `${containerWidth - 2 * PopoverScreenBorderPadding}px`,
        }}
      >
        <h4>{reorderCommitsHintTitle}</h4>
        <p>
          Use <KeyboardShortcut darwinKeys={['↑']} keys={['↑']} />
          <KeyboardShortcut darwinKeys={['↓']} keys={['↓']} /> to choose a new
          location.
        </p>
        <p>
          Press <KeyboardShortcut darwinKeys={['⏎']} keys={['⏎']} /> to confirm.
        </p>
      </Popover>
    )
  }

  private renderKeyboardInsertionElement = (
    data: KeyboardInsertionData
  ): JSX.Element | null => {
    const { emoji, gitHubRepository } = this.props
    const { commits } = data

    if (commits.length === 0) {
      return null
    }

    switch (data.type) {
      case DragType.Commit:
        return (
          <CommitDragElement
            gitHubRepository={gitHubRepository}
            commit={commits[0]}
            selectedCommits={commits}
            isKeyboardInsertion={true}
            emoji={emoji}
            accounts={this.props.accounts}
          />
        )
      default:
        return assertNever(data.type, `Unknown drag element type: ${data}`)
    }
  }

  private onRowContextMenu = (
    row: number,
    event: React.MouseEvent<HTMLDivElement>
  ) => {
    event.preventDefault()

    if (this.inKeyboardReorderMode) {
      return
    }

    const sha = this.props.commitSHAs[row]
    const commit = this.props.commitLookup.get(sha)
    if (commit === undefined) {
      if (__DEV__) {
        log.warn(
          `[CommitList]: the commit '${sha}' does not exist in the cache`
        )
      }
      return
    }

    showContextualMenu(
      generateCommitContextMenu(
        {
          ...this.props,
          onSquash:
            this.props.onSquash === undefined ? undefined : this.onSquash,
        },
        row,
        commit,
        this.selectedCommits
      )
    )
  }

  private onKeyboardInsertionIndexPathChanged = (indexPath: RowIndexPath) => {
    this.updateKeyboardReorderingMessage(indexPath)
  }

  private onConfirmKeyboardReorder = (
    indexPath: RowIndexPath,
    data: KeyboardInsertionData
  ) => {
    this.onDropDataInsertion(indexPath.row, data)
  }

  private onDropDataInsertion = (row: number, data: DragData) => {
    if (
      this.props.onDropCommitInsertion === undefined ||
      data.type !== DragType.Commit
    ) {
      return
    }

    const plan = planWorkspaceCommitInsertion(
      this.props.commitSHAs,
      row,
      data.commits
        .filter((v): v is Commit => v !== null && v !== undefined)
        .map(c => c.sha)
    )
    if (plan === null) {
      return
    }
    const baseCommit =
      plan.baseSHA === null ? null : this.props.commitLookup.get(plan.baseSHA)
    this.props.onDropCommitInsertion(
      baseCommit ?? null,
      data.commits,
      plan.lastRetainedCommitRef
    )
  }
}

/**
 * Makes a hash of the commit's data that will be shown in a CommitListItem
 */
function commitListItemHash(commit: Commit): string {
  return `${commit.sha} ${commit.tags}`
}

function makeCommitsHash(commits: ReadonlyArray<Commit>): string {
  return commits.map(commitListItemHash).join(' ')
}

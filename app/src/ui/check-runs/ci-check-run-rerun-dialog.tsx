import * as React from 'react'
import { Dialog, DialogContent, DialogFooter } from '../dialog'
import { OkCancelButtonGroup } from '../dialog/ok-cancel-button-group'
import { IRefCheck } from '../../lib/ci-checks/ci-checks'
import { CICheckRunList } from './ci-check-run-list'
import { GitHubRepository } from '../../models/github-repository'
import { Repository } from '../../models/repository'
import { Dispatcher } from '../dispatcher'
import {
  determineCheckRerunnability,
  rerunDialogChecks,
} from '../lib/auxiliary-dialog-actions'
import { Octicon } from '../octicons'
import * as octicons from './../octicons/octicons.generated'
import { encodePathAsUrl } from '../../lib/path'

const BlankSlateImage = encodePathAsUrl(
  __dirname,
  'static/empty-no-pull-requests.svg'
)

interface ICICheckRunRerunDialogProps {
  readonly dispatcher: Dispatcher
  readonly repository: GitHubRepository
  readonly localRepository: Repository

  /** List of all the check runs (some of which are not rerunnable) */
  readonly checkRuns: ReadonlyArray<IRefCheck>

  /** The git reference of the pr */
  readonly prRef: string

  /** Whether to only rerun failed checks */
  readonly failedOnly: boolean

  readonly onDismissed: () => void
}

interface ICICheckRunRerunDialogState {
  readonly loadingCheckSuites: boolean
  readonly loadingRerun: boolean
  readonly rerunnable: ReadonlyArray<IRefCheck>
  readonly nonRerunnable: ReadonlyArray<IRefCheck>
}

/**
 * Dialog that informs the user of which jobs will be rerun
 */
export class CICheckRunRerunDialog extends React.Component<
  ICICheckRunRerunDialogProps,
  ICICheckRunRerunDialogState
> {
  public constructor(props: ICICheckRunRerunDialogProps) {
    super(props)
    this.state = {
      loadingCheckSuites: true,
      loadingRerun: false,
      rerunnable: [],
      nonRerunnable: [],
    }
    this.determineRerunnability()
  }

  private onSubmit = async () => {
    const { dispatcher, repository, localRepository, prRef } = this.props
    this.setState({ loadingRerun: true })
    const succeeded = await rerunDialogChecks(
      dispatcher,
      repository,
      localRepository,
      prRef,
      this.state.rerunnable,
      this.props.failedOnly
    )
    if (!succeeded) {
      this.setState({ loadingRerun: false })
      return
    }
    this.props.onDismissed()
  }

  private determineRerunnability = async () => {
    const { dispatcher, repository, localRepository, checkRuns, failedOnly } =
      this.props
    const { rerunnable, nonRerunnable } = await determineCheckRerunnability(
      dispatcher,
      repository,
      localRepository,
      checkRuns,
      failedOnly
    )
    this.setState({ loadingCheckSuites: false, rerunnable, nonRerunnable })
  }

  private renderRerunnableJobsList = () => {
    if (this.state.rerunnable.length === 0) {
      return null
    }

    return (
      <div className="ci-check-run-list check-run-rerun-list">
        <CICheckRunList
          checkRuns={this.state.rerunnable}
          notExpandable={true}
          isCondensedView={true}
          hasStatusTooltip={true}
        />
      </div>
    )
  }

  private renderRerunDependentsMessage = () => {
    if (this.state.rerunnable.length === 0) {
      return null
    }

    const name =
      this.props.checkRuns.length === 1 ? (
        <strong>{this.props.checkRuns[0].name}</strong>
      ) : (
        'these workflows'
      )
    const dependentAdj = this.props.checkRuns.length === 1 ? 'its' : 'their'

    return (
      <div className="re-run-dependents-message">
        A new attempt of {name} will be started, including all of {dependentAdj}{' '}
        dependents:
      </div>
    )
  }

  private renderRerunWarning = () => {
    if (
      this.state.loadingCheckSuites ||
      this.state.nonRerunnable.length === 0
    ) {
      return null
    }

    const pluralize = `check${this.state.nonRerunnable.length !== 1 ? 's' : ''}`
    const verb = this.state.nonRerunnable.length !== 1 ? 'are' : 'is'
    const warningPrefix =
      this.state.rerunnable.length === 0
        ? `There are no ${
            this.props.failedOnly ? 'failed ' : ''
          }checks that can be re-run`
        : `There ${verb} ${this.state.nonRerunnable.length} ${
            this.props.failedOnly ? 'failed ' : ''
          }${pluralize} that cannot be re-run`
    return (
      <div className="non-re-run-info warning-helper-text">
        <Octicon symbol={octicons.alert} />

        {`${warningPrefix}. A check run cannot be re-run if the check is more than one month old,
          the check or its dependent has not completed, or the check is not configured to be
          re-run.`}
      </div>
    )
  }

  public getTitle = (showDescriptor: boolean = true) => {
    const { checkRuns, failedOnly } = this.props
    const s = checkRuns.length === 1 ? '' : 's'
    const c = __DARWIN__ ? 'C' : 'c'

    let descriptor = ''
    if (showDescriptor && checkRuns.length === 1) {
      descriptor = __DARWIN__ ? 'Single ' : 'single '
    }

    if (showDescriptor && failedOnly) {
      descriptor = __DARWIN__ ? 'Failed ' : 'failed '
    }

    return `Re-run ${descriptor}${c}heck${s}`
  }

  private renderDialogContent = () => {
    if (this.state.loadingCheckSuites && this.props.checkRuns.length > 1) {
      return (
        <div className="loading-rerun-checks">
          <img src={BlankSlateImage} className="blankslate-image" alt="" />
          <div className="title">Please wait</div>
          <div className="call-to-action">
            Determining which checks can be re-run.
          </div>
        </div>
      )
    }

    return (
      <>
        {this.renderRerunDependentsMessage()}
        {this.renderRerunnableJobsList()}
        {this.renderRerunWarning()}
      </>
    )
  }

  public render() {
    return (
      <Dialog
        id="rerun-check-runs"
        title={this.getTitle()}
        onSubmit={this.onSubmit}
        onDismissed={this.props.onDismissed}
        loading={this.state.loadingCheckSuites || this.state.loadingRerun}
      >
        <DialogContent>{this.renderDialogContent()}</DialogContent>
        <DialogFooter>
          <OkCancelButtonGroup
            okButtonText={this.getTitle(false)}
            okButtonDisabled={this.state.rerunnable.length === 0}
          />
        </DialogFooter>
      </Dialog>
    )
  }
}

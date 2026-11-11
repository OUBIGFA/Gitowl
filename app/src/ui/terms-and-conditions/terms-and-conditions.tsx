import * as React from 'react'
import { Dialog, DialogContent, DefaultDialogFooter } from '../dialog'
import { LinkButton } from '../lib/link-button'
import { applicationTerms } from './terms-content'

interface ITermsAndConditionsProps {
  /** A function called when the dialog is dismissed. */
  readonly onDismissed: () => void
}

/** The original legal blocks are shared with the native presentation, including links. */
export class TermsAndConditions extends React.Component<
  ITermsAndConditionsProps,
  {}
> {
  private renderBlock = (
    block: typeof applicationTerms[number],
    index: number
  ) => {
    return React.createElement(
      block.kind,
      { key: index },
      block.segments.map((segment, i) =>
        'uri' in segment ? (
          <LinkButton key={i} uri={segment.uri}>
            {segment.text}
          </LinkButton>
        ) : (
          segment.text
        )
      )
    )
  }

  public render() {
    return (
      <Dialog
        id="terms-and-conditions"
        title="GitHub Open Source Applications Terms and Conditions"
        onSubmit={this.props.onDismissed}
        onDismissed={this.props.onDismissed}
      >
        <DialogContent>
          {applicationTerms
            .filter(block => block.kind !== 'li')
            .map(this.renderBlock)}
          <ol>
            {applicationTerms
              .filter(block => block.kind === 'li')
              .map(this.renderBlock)}
          </ol>
        </DialogContent>
        <DefaultDialogFooter />
      </Dialog>
    )
  }
}

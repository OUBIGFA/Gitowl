package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// The composer stays in the file pane while its diff opens or closes. Its
// stable parent and keys preserve native editing state through resizing.
func (a *app) commitForm(c *ui.Context, tx locales.Messages) {
	width, height := c.Size()
	narrow := a.isChangesWorkspace() && a.diffPanelLayout.width < 420
	if a.core != nil && a.repo != nil && a.notebook.width > 0 {
		rightPane := width - a.nativeRailWidth(c) - 32 - a.notebook.width - 12
		narrow = rightPane < 460
	}
	// Reviewing a file at a short window: the diff is what is being read. The
	// actions wrap to two rows there, so tighten the spacing above them (every
	// label stays) to keep a usable code viewport. Padding is unchanged so Save
	// keeps its exact position when review opens.
	compact := height < 700
	margin, gap := float32(16), float32(8)
	if compact {
		margin, gap = 6, 6
	}
	composerBorder := c.Theme().Background.Mix(c.Theme().Text, 0.14)
	if c.Preferences().HighContrast {
		composerBorder = c.Theme().Border
	}
	ui.Column(c.Key("commit-form:"+a.repo.Path)).Shrink(0).FillWidth().Margin(margin, 0, 0, 0).Padding(12).Gap(gap).
		Border(1, composerBorder).Radius(radiusSurface).Background(c.Theme().Surface).
		OnShortcut(ui.Ctrl, ui.KeyEnter, a.saveVersion).Children(func() {
		// Sources and output language remain visible; they are negotiated and
		// applied from real settings receipts, never invented demonstration data.
		vm := a.nativeCommitView()
		saveAndPush := a.core != nil && (vm == nil || vm.Mode != "copilot") && a.windowPreferences.PushAfterCommit
		scope := tx.IncludedFiles(len(a.included))
		if a.status != nil && len(a.status.Files) > 0 {
			scope = tx.SelectedOfTotal(len(a.included), len(a.status.Files))
		}
		selectionCount := func() {
			// Only the user's file selection changes this caption. Background
			// rule checks must not replace it when the window becomes active.
			shown := scope
			if narrow && a.status != nil && len(a.status.Files) > 0 {
				shown = tx.SelectedOfTotalCompact(len(a.included), len(a.status.Files))
			}
			ui.Text(c.Key("commit-selection-count"), shown).Label(scope).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).FontFeatures("tnum").SingleLine().Tooltip(scope).Role(ui.RoleStatus)
		}
		if vm == nil || vm.Mode != "copilot" {
			if a.core == nil {
				a.generationChoices(c, tx)
			}
			placeholder := tx.CommitSummaryPlaceholder()
			if vm != nil && vm.DefaultSummary != nil {
				placeholder = *vm.DefaultSummary
			}
			input := ui.TextInput(c.Key("commit-summary"), &a.commitSummary).FillWidth().MinHeight(36).Padding(8, 12).FontSize(c.Theme().Rem(15.0 / 15)).
				Placeholder(placeholder).Label(tx.CommitSummary()).Disabled(a.composerLocked()).OnChange(a.generationDraftEdited)
			a.focusCoreCommitMessage(input)
			if a.coreFocusTarget == "focus-changes" && (a.status == nil || len(a.status.Files) == 0) {
				input.Focus()
				a.coreFocusTarget = ""
			}
			var description ui.CollapsibleParts
			inlineHint := vm != nil && vm.SummaryLengthWarning && a.reviewingFile && height < 700
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				description = ui.CollapsibleBase(c.Key("description-disclosure"), &a.descriptionOpen)
				quietSurface(c, description.Trigger).MinHeight(28).Padding(4, 6).Gap(6).AlignItems(ui.Center).AlignSelf(ui.Start).
					Label(tx.CommitDescription()).Children(func() {
					ui.Icon(c, icons["chevron-right"]).FontSize(15).Rotate(90 * description.Progress())
					ui.Text(c, tx.CommitDescription()).FontSize(c.Theme().Rem(14.0 / 15)).NoWrap().TextColor(c.Theme().TextMuted)
					if inlineHint {
						ui.Spacer(c)
						ui.Text(c, nativeDialogLabel(c.Locale(), "commit.summaryHint")).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).SingleLine().MinWidth(0)
					}
				})
				if inlineHint {
					description.Trigger.Description(nativeDialogLabel(c.Locale(), "commit.summaryHint"))
				}
				ui.Spacer(c)
				selectionCount()
				a.nativeCommitOptions(c)
			})
			description.Panel(func() {
				// Give long descriptions more editing space as the window grows.
				// Three lines at the minimum window, about five on large screens:
				// the draft stays a footer to the review, not a second page.
				editorHeight := max(c.Theme().Rem(64.0/15), min(c.Theme().Rem(7), (height-620)/3+64))
				if a.reviewingFile {
					// The diff is what is being read; two lines keep the
					// description at hand without taking its room.
					editorHeight = c.Theme().Rem(52.0 / 15)
				}
				if compact {
					editorHeight = c.Theme().Rem(40.0 / 15)
				}
				ui.TextArea(c.Key("commit-description"), &a.commitDescription).FillWidth().Height(editorHeight).MinHeight(editorHeight).MaxHeight(editorHeight).Padding(10, 12).
					Placeholder(tx.DescriptionPlaceholder()).Label(tx.VersionDescription()).KeepScrollbars(true).Disabled(a.composerLocked()).OnChange(a.generationDraftEdited)
			})
		}
		a.nativeCommitStatus(c)
		a.generationErrors(c, tx)
		if a.saveActionErr != nil {
			ui.Text(c, tx.SaveActionPreferenceFailed()).Role(ui.RoleAlert).TextColor(c.Theme().Danger).Tooltip(a.saveActionErr.Error())
		}
		if a.core == nil && a.status != nil && (a.status.Conflicted || a.status.Operation != nil && *a.status.Operation != "") {
			ui.Text(c, tx.CommitBlocked()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Warning)
		}
		if a.saveErr != nil {
			ui.Text(c, tx.SaveNotConfirmed()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
			ui.Text(c, tx.NoAutomaticRetry()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
			details := ui.CollapsibleBase(c.Key("save-error-disclosure"), &a.saveErrorOpen)
			quietSurface(c, details.Trigger).Height(28).Padding(2, 8).Gap(8).AlignSelf(ui.Start).
				Label(tx.ErrorDetails()).Children(func() {
				ui.Icon(c, icons["chevron-right"]).FontSize(18).Rotate(90 * details.Progress())
				ui.Text(c, tx.ErrorDetails()).FontSize(c.Theme().Rem(14.0 / 15)).NoWrap()
			})
			details.Panel(func() {
				// Hooks can return arbitrarily long diagnostics. A bounded native
				// text area scrolls/selects/copies all of it without losing Save.
				message := a.saveErr.Error()
				field := ui.TextArea(c.Key("save-error-details"), &message).ReadOnly(true).FillWidth().Height(72).MinHeight(72).MaxHeight(72).
					FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Text).Label(tx.FullErrorDetails())
				shown := ui.Local(field, "shown-diagnostic", func() string { return "" })
				if *shown != message {
					// A fresh diagnostic opens at its cause, not at the default
					// end-of-input caret. Later scrolling and copying stay intact.
					field.SetTextSelection(0, 0)
					*shown = message
				}
			})
		}
		if vm != nil && vm.Mode == "copilot" {
			selectionCount()
		}
		ui.Row(c.Key("composer-actions")).FillWidth().Gap(gap).AlignItems(ui.Center).Justify(ui.End).Wrap().Children(func() {
			if a.core != nil && (vm == nil || vm.Mode != "copilot") {
				a.coreGenerationMenu(c, tx)
			}
			ui.Spacer(c)
			ui.Row(c.Key("composer-primary-actions")).Shrink(0).MinWidth(0).AlignItems(ui.Center).Justify(ui.End).Gap(gap).Children(func() {
				if vm == nil || vm.Mode != "copilot" {
					if a.generating {
						quietButton(c.Key("stop-generation"), tx.StopGeneration(), "x").Height(36).OnClick(a.stopGeneration)
					} else {
						label := tx.GenerateMessage()
						if narrow {
							label = tx.GenerateMessageShort()
						}
						ui.Button(c.Key("generate-message"), label).Label(tx.GenerateMessage()).Height(40).Padding(8, 12).FontWeight(500).
							Tooltip(a.generationTooltip(tx)).Disabled(!a.generationActionEnabled()).OnClick(a.requestGeneration)
					}
				}
				saveLabel := tx.SaveVersion()
				if saveAndPush {
					saveLabel = tx.SaveVersionAndPush()
				}
				if vm != nil && vm.Amending && !saveAndPush {
					saveLabel = nativeDialogLabel(c.Locale(), "commit.amendSave")
				}
				if vm != nil && vm.Mode == "copilot" {
					saveLabel = nativeDialogLabel(c.Locale(), "commit.assistedSave")
				}
				if a.savingVersion {
					saveLabel = tx.SavingVersion()
					if saveAndPush {
						saveLabel = tx.SavingVersionAndPush()
					}
				}
				shownSaveLabel := saveLabel
				if narrow && !a.savingVersion && !saveAndPush && (vm == nil || vm.Mode != "copilot" && !vm.Amending) {
					shownSaveLabel = tx.SaveVersionShort()
				}
				saveTooltip, saveDisabled := tx.SaveVersionTooltip(), !a.canSaveVersion()
				if saveAndPush {
					saveTooltip = tx.SaveVersionAndPushTooltip()
					saveDisabled = saveDisabled || a.coreRemotePending || a.coreSnapshot.Selected.RemoteInProgress
				}
				// Save and its advanced options stay together on the right, including
				// at the minimum width when the model selector wraps above them.
				ui.Row(c.Key("commit-save-group")).Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
					var save ui.Element
					if a.core != nil && (vm == nil || vm.Mode != "copilot") {
						// Use MyGo's joined control, including its focus group, arrow
						// keys and menu accessibility. The menu only selects the default;
						// the primary button and shortcut execute it.
						parts := ui.PrimarySplitButton(c.Key("save-version"), shownSaveLabel)
						parts.Group.Height(40).Disabled(!a.canSaveVersion())
						parts.Arrow.Label(tx.SaveOptions()).Tooltip(tx.SaveOptions()).PopoverMenuButton(func(m *ui.Menu) {
							if m.Item(tx.SaveVersion()).Radio(!a.windowPreferences.PushAfterCommit).Chosen() {
								a.selectSaveAction(false)
							}
							if m.Item(tx.SaveVersionAndPush()).Radio(a.windowPreferences.PushAfterCommit).Chosen() {
								a.selectSaveAction(true)
							}
						}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd, Panel: func(panel ui.Element) {
							// Override the toolkit's 180-DIP floor; labels and native
							// choice marks determine this short menu's natural width.
							panel.MinWidth(0)
						}})
						save = parts.Button
					} else {
						save = ui.PrimaryButton(c.Key("save-version"), shownSaveLabel)
					}
					save.Height(40).Padding(8, 14).FontWeight(500).Label(saveLabel).
						Tooltip(saveTooltip + " · " + tx.SaveShortcut() + " · " + tx.IncludedFiles(len(a.included))).Disabled(saveDisabled).OnClick(a.saveVersion)
					if a.coreFocusTarget == "commit-submit" {
						save.Focus()
						a.coreFocusTarget = ""
					}
					if vm != nil && vm.Mode == "copilot" {
						a.nativeCommitOptions(c)
					}
				})
			})
		})
	})
}

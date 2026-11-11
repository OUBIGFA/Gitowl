package main

import (
	"slices"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

type preferenceGroup struct {
	title   string
	fields  []string
	actions []string
}

// Preference groups follow the user's task. They are never split by measured
// height; all pages still share one measured, cached shell and original draft.
func renderPreferencePage(c *ui.Context, popup *backend.KernelPopupPresentation, form *nativeDialogForm, layout preferenceLayout, page string, changed func(backend.KernelDialogField), send func(backend.KernelDialogAction)) map[string]bool {
	var groups []preferenceGroup
	var tail []string
	switch page {
	case "integrations":
		groups = []preferenceGroup{
			{"settings.selectedExternalEditor", []string{"selectedExternalEditor", "useCustomEditor", "customEditorPath", "customEditorArguments", "customEditorBundleID"}, nil},
			{"settings.selectedShell", []string{"selectedShell", "useCustomShell", "customShellPath", "customShellArguments", "customShellBundleID"}, nil},
		}
		tail = []string{"copilotAppPath"}
	case "appearance":
		groups = []preferenceGroup{
			{"settings.group.interface", []string{"native-language", "theme"}, nil},
			{"settings.group.formatting", []string{"dateFormat", "timeFormat", "numberFormat", "preferAbsoluteDates"}, nil},
		}
		tail = []string{"tabSize", "alwaysShowWorktreeList"}
	case "git":
		groups = []preferenceGroup{
			{"settings.group.author", []string{"committerName", "committerEmail", "defaultBranch"}, []string{"editGlobalGitConfig", "unlockGit"}},
			{"settings.group.hooks", []string{"enableGitHookEnv", "cacheGitHookEnv", "selectedGitHookEnvShell"}, nil},
		}
	case "copilot":
		groups = []preferenceGroup{
			{"settings.group.copilotAccount", []string{"account"}, []string{"copilotSettings", "copilotPlans", "copilotFeatures"}},
			{"settings.group.generation", []string{"generationSource", "generationModel", "generationLanguage"}, []string{"customProviders"}},
		}
	case "prompts":
		groups = []preferenceGroup{
			{"settings.group.removal", []string{"confirmRepositoryRemoval", "confirmWorktreeRemoval", "confirmDiscardChanges", "confirmDiscardChangesPermanently", "confirmDiscardStash"}, nil},
			{"settings.group.changes", []string{"confirmCheckoutCommit", "confirmForcePush", "confirmUndoCommit", "askForConfirmationOnCommitFilteredChanges", "confirmCommitMessageOverride", "showCommitLengthWarning"}, nil},
		}
		tail = []string{"uncommittedChangesStrategy"}
	case "advanced":
		groups = []preferenceGroup{
			{"settings.group.authentication", []string{"useWindowsOpenSSH", "useExternalCredentialHelper"}, nil},
			{"settings.group.application", []string{"repositoryIndicatorsEnabled", "optOutOfUsageTracking"}, nil},
		}
	default:
		// Short pages retain a single reading sequence. The known dense pages
		// above define their own columns rather than scattering short controls.
		ids := []string{}
		for _, field := range nativeDialogContentFields(popup) {
			ids = append(ids, field.ID)
		}
		groups = []preferenceGroup{{fields: ids}}
	}
	fields := make(map[string]backend.KernelDialogField)
	for _, field := range nativeDialogContentFields(popup) {
		fields[field.ID] = field
	}
	if page == "appearance" && form.appearanceTop != nil {
		fields["native-language"] = backend.KernelDialogField{ID: "native-language"}
	}
	used, handledActions := make(map[string]bool), make(map[string]bool)
	renderField := func(id, heading string) {
		field, ok := fields[id]
		if !ok {
			return
		}
		used[id] = true
		ui.Column(c.Key(id)).FillWidth().Shrink(0).Children(func() {
			if id == "native-language" {
				form.appearanceTop(c)
			} else if field.Kind == "select" && heading == field.LabelKey {
				// The group heading labels this selector, without repeating the
				// same words above it. Accessibility still names the control.
				renderNativeDialogSelect(c, field, &form.values[id].text, form.busy || field.Disabled).OnChange(func() { changed(field) })
			} else {
				renderNativeDialogField(c, field, form, func() { changed(field) })
			}
		})
	}
	groups = slices.DeleteFunc(groups, func(group preferenceGroup) bool {
		return !slices.ContainsFunc(group.fields, func(id string) bool { _, ok := fields[id]; return ok })
	})
	ui.Column(c.Key("preference-page")).FillWidth().Gap(24).Shrink(0).Children(func() {
		columns := max(1, min(2, layout.columns))
		for start := 0; start < len(groups); start += columns {
			ui.Row(c.Key(start)).FillWidth().Gap(24).Shrink(0).AlignItems(ui.Start).Children(func() {
				for _, group := range groups[start:min(start+columns, len(groups))] {
					ui.Column(c.Key(group.title)).Grow(1).Basis(0).MinWidth(0).Gap(16).Shrink(0).Children(func() {
						if group.title != "" {
							ui.Text(c, nativeDialogLabel(c.Locale(), group.title)).FontSize(c.Theme().Rem(16.0 / 15)).FontWeight(500).Role(ui.RoleHeading).Level(3)
						}
						for _, id := range group.fields {
							renderField(id, group.title)
						}
						if len(group.actions) > 0 {
							ui.Column(c.Key("group-actions")).FillWidth().Gap(4).Children(func() {
								for _, action := range popup.Actions {
									if !slices.Contains(group.actions, action.ID) {
										continue
									}
									handledActions[action.ID] = true
									renderPreferenceTool(c.Key(action.ID), action, form, send)
								}
							})
						}
					})
				}
			})
		}
		// Extra fields are explicit rows below the groups. This also preserves
		// future upstream parameters without silently hiding unknown IDs.
		remaining := []string{}
		for _, id := range tail {
			if _, ok := fields[id]; ok {
				remaining = append(remaining, id)
				used[id] = true
			}
		}
		for _, field := range nativeDialogContentFields(popup) {
			if !used[field.ID] {
				remaining = append(remaining, field.ID)
			}
		}
		if len(remaining) > 0 {
			ui.Column(c.Key("remaining-fields")).FillWidth().Gap(16).Shrink(0).Children(func() {
				ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border).Shrink(0)
				for start := 0; start < len(remaining); start += columns {
					ui.Row(c.Key(start)).FillWidth().Gap(24).Shrink(0).AlignItems(ui.End).Children(func() {
						for _, id := range remaining[start:min(start+columns, len(remaining))] {
							ui.Column(c.Key(id)).Grow(1).Basis(0).MinWidth(0).Children(func() { renderField(id, "") })
						}
					})
				}
			})
		}
		if page == "appearance" && form.appearanceEnd != nil {
			form.appearanceEnd(c)
		}
	})
	return handledActions
}

func renderPreferenceTool(c *ui.Context, action backend.KernelDialogAction, form *nativeDialogForm, send func(backend.KernelDialogAction)) {
	button := quietButton(c, nativeDialogLabel(c.Locale(), action.LabelKey), "").MinHeight(36).AlignSelf(ui.Start).
		Disabled(action.Disabled || send == nil || form.busy || form.validating() || form.navigationPending || form.pendingNavigation != "")
	if action.Destructive {
		button.TextColor(c.Theme().Danger)
	}
	button.OnClick(func() { send(action) })
}

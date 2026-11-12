package main

import (
	"slices"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

func repositoryEntryDialog(popup *backend.KernelPopupPresentation) bool {
	return popup.Type == "CreateRepository" || popup.Type == "CloneRepository" || popup.Type == "AddRepository" || popup.Type == "AddWorktree"
}

// These tasks have an ordered form, not interchangeable settings columns.
// Editors and continuations still use the original dialog draft and Dispatcher.
func renderRepositoryEntryFields(c *ui.Context, popup *backend.KernelPopupPresentation, form *nativeDialogForm, changed func(backend.KernelDialogField), send func(backend.KernelDialogAction)) {
	used := make(map[string]bool)
	field := func(id string) (backend.KernelDialogField, bool) {
		i := slices.IndexFunc(popup.Fields, func(f backend.KernelDialogField) bool { return f.ID == id })
		if i < 0 {
			return backend.KernelDialogField{}, false
		}
		used[id] = true
		return popup.Fields[i], true
	}
	busy := form.busy || form.validating() || send == nil
	button := func(id string, quiet bool) {
		for _, action := range popup.Actions {
			if action.ID != id {
				continue
			}
			label := nativeDialogLabel(c.Locale(), action.LabelKey)
			var control ui.Element
			if quiet {
				control = quietButton(c.Key(id), label, "").MinHeight(32).Padding(4, 8)
			} else {
				control = ui.Button(c.Key(id), label).MinHeight(40).Padding(8, 16)
			}
			control.Shrink(0).Disabled(busy || action.Disabled).OnClick(func() { send(action) })
		}
	}
	render := func(id string) {
		if f, ok := field(id); ok {
			ui.Column(c.Key(id)).FillWidth().Shrink(0).Children(func() {
				renderNativeDialogField(c, f, form, func() { changed(f) })
			})
		}
	}
	path := func() {
		if f, ok := field("path"); ok {
			ui.Field(c.Key("path"), nativeDialogLabel(c.Locale(), f.LabelKey), func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c.Key("input"), &form.values[f.ID].text).MinHeight(40).Grow(1).MinWidth(0).
						Disabled(form.busy || f.Disabled).ReadOnly(f.ReadOnly).Placeholder(f.Placeholder).OnChange(func() { changed(f) })
					button("choose", false)
				})
				nativeRequiredEmptyHint(c, f, form.values[f.ID].text)
			}).Shrink(0)
		}
	}
	pair := func(first, second string) {
		// Wrapped labels may differ in height; align the editable controls.
		ui.Row(c.Key(first + "-" + second)).FillWidth().Gap(16).Shrink(0).AlignItems(ui.End).Children(func() {
			for _, id := range []string{first, second} {
				ui.Column(c.Key(id)).Grow(1).Basis(0).MinWidth(0).Children(func() { render(id) })
			}
		})
	}
	ui.Column(c.Key("repository-entry-fields")).FillWidth().Gap(16).Shrink(0).Children(func() {
		switch popup.Type {
		case "CreateRepository":
			render("name")
			render("description")
			path()
			ui.Box(c.Key("initialization-rule")).FillWidth().Height(1).Background(c.Theme().Border).Shrink(0)
			render("readme")
			pair("gitIgnore", "license")
		case "CloneRepository":
			if f, ok := field("tab"); ok {
				selected := slices.IndexFunc(f.Choices, func(choice backend.KernelDialogChoice) bool { return choice.ID == form.values[f.ID].text })
				tabs := ui.TabsBase(c.Key("source"), &selected, len(f.Choices))
				tabs.List.FillWidth().Gap(4).Padding(4).Radius(8).Background(c.Theme().SurfaceHover).Label(nativeDialogLabel(c.Locale(), f.LabelKey)).Children(func() {
					for i, choice := range f.Choices {
						tab := tabs.Tab(i).MinHeight(36).Grow(1).Basis(0).Padding(6, 12).Radius(6).
							Disabled(form.busy || f.Disabled || choice.Disabled).Label(nativeDialogChoiceLabel(c.Locale(), choice.Label))
						if selected == i {
							tab.Background(c.Theme().Surface).FontWeight(500)
						}
						tab.Children(func() { ui.Text(c, nativeDialogChoiceLabel(c.Locale(), choice.Label)) })
					}
				}).OnChange(func() {
					if selected >= 0 && selected < len(f.Choices) {
						form.values[f.ID].text = f.Choices[selected].ID
						changed(f)
					}
				})
			}
			if f, ok := field("account"); ok {
				if len(f.Choices) == 0 {
					// These controls depend on a signed-in account. Keep their draft
					// values, but show the actionable sign-in state instead of three
					// empty selectors. URL cloning remains available in the other tab.
					field("filter")
					field("repository")
					ui.Column(c.Key("sign-in-section")).FillWidth().Gap(12).Padding(8, 0).Shrink(0).Children(func() {
						ui.Text(c, nativeDialogLabel(c.Locale(), "repositoryDialog.signInToBrowse")).FillWidth().TextColor(c.Theme().TextMuted)
						ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
							button("signIn", false)
							button("signInEnterprise", true)
						})
					})
				} else {
					ui.Column(c.Key("account-section")).FillWidth().Gap(4).Shrink(0).Children(func() {
						ui.Field(c, nativeDialogLabel(c.Locale(), f.LabelKey), func() {
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Column(c.Key("account")).Grow(1).MinWidth(0).Children(func() {
									renderNativeDialogSelect(c, f, &form.values[f.ID].text, form.busy || f.Disabled).OnChange(func() { changed(f) })
								})
								button("refresh", false)
							})
						})
						ui.Row(c.Key("account-actions")).FillWidth().Gap(8).Wrap().Children(func() {
							button("signIn", true)
							button("signInEnterprise", true)
						})
					})
					pair("filter", "repository")
				}
			} else {
				render("url")
			}
			path()
		case "AddRepository":
			path()
		case "AddWorktree":
			render("repositoryName")
			render("repositoryPath")
			pair("name", "branch")
			path()
			render("branches")
		}
		// Warnings and future fields remain visible even when the ordered form
		// gains an upstream parameter. Nothing disappears by positional slicing.
		for _, f := range popup.Fields {
			if !used[f.ID] {
				render(f.ID)
			}
		}
	})
}

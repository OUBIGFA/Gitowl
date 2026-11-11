package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

type nativeDialogValue struct {
	text         string
	check        bool
	image        ui.ImageSource
	imageMedia   *nativeImageDiffMedia
	imageCancel  context.CancelFunc
	imageKey     [32]byte
	imageLoading bool
	imageErr     error
	diffPreview  *preview
	diffState    nativeImageDiffState
	diffKey      [32]byte
	diffErr      error
	dirty        bool
}

// Inputs only. Covered original popups retain their form, removed popups lose
// it. No callbacks, account secrets, Git decisions or second popup stack live
// here. A submitted password editor is remounted to clear native undo history.
type nativeDialogForm struct {
	// Extra native settings share this popup's navigation without adding
	// fields to the original Preferences draft or changing its wire contract.
	sections             []nativeDialogSection
	section              string
	requestedTab         string
	displayTab           string
	pendingNavigation    string
	navigationPending    bool
	appearanceTop        func(*ui.Context)
	appearanceEnd        func(*ui.Context)
	appearanceMeasureKey string
	geometryCache        preferenceGeometryCache
	presentedLayout      preferenceLayout
	expandWidth          float32
	expandHeight         float32
	onRemoved            func()
	settingsWorkArea     func() (float32, float32)
	expandSettings       func(float32, float32)
	renderDiff           func(*ui.Context, backend.KernelDialogField, *nativeDialogValue)
	values               map[string]*nativeDialogValue
	changePending        bool
	changeAction         string
	pendingChanges       map[string]time.Time
	busy                 bool
	accept               bool
	secretEpoch          int
	err                  error
}

type nativeDialogSection struct {
	id, label  string
	measureKey string
	render     func(*ui.Context)
	measure    []func(*ui.Context, ui.Element)
}

func (f *nativeDialogForm) finishReply(automatic bool, err error) {
	if automatic {
		f.changePending, f.changeAction, f.err = false, "", err
		f.navigationPending = false
		// The user owns dirty input until an explicit decision. In particular,
		// a delayed validation echo must not overwrite keys typed after send.
		return
	}
	f.busy, f.err, f.accept = false, err, err == nil
}

func (f *nativeDialogForm) validating() bool { return f.changePending || len(f.pendingChanges) != 0 }

func (f *nativeDialogForm) sync(popup *backend.KernelPopupPresentation) {
	fields := nativeDialogFields(popup)
	if f.values == nil {
		f.values = make(map[string]*nativeDialogValue)
	}
	for action := range f.pendingChanges {
		if !slices.ContainsFunc(fields, func(field backend.KernelDialogField) bool { return field.ChangeAction == action }) {
			delete(f.pendingChanges, action)
		}
	}
	for id, value := range f.values {
		if !slices.ContainsFunc(popup.Fields, func(field backend.KernelDialogField) bool {
			return field.ID == id && (field.Kind == "image" || field.Kind == "diff")
		}) {
			value.closeMedia()
			value.imageKey = [32]byte{}
		}
	}
	for _, field := range fields {
		value := f.values[field.ID]
		if value == nil {
			value = &nativeDialogValue{}
			f.values[field.ID] = value
		}
		if !field.Sensitive && (!value.dirty || f.accept) {
			value.text, value.check = nativeDialogValueText(field), false
			if check, ok := field.Value.(bool); ok {
				value.check = check
			}
			value.dirty = false
		}
		if field.Kind == "select" && value.text != "" && !slices.ContainsFunc(field.Choices, func(choice backend.KernelDialogChoice) bool {
			return choice.ID == value.text && !choice.Disabled
		}) {
			value.text = "" // Invalidated choices never fall back to the first one.
		}
	}
	if popup.Type == "Preferences" && len(popup.LayoutPages) > 0 {
		if tab := f.values["tab"]; tab != nil {
			if f.displayTab == "" || !f.navigationPending && f.pendingNavigation == "" {
				f.displayTab = tab.text
			}
			tab.text = f.displayTab
		}
	}
	f.accept = false
}

// Complete page DTOs share one original draft. This union is only the declared
// input schema; the visible page remains a local presentation choice.
func nativeDialogFields(popup *backend.KernelPopupPresentation) []backend.KernelDialogField {
	fields := slices.Clone(popup.Fields)
	if popup.Type == "Preferences" {
		for _, page := range popup.LayoutPages {
			for _, field := range page.Fields {
				if !slices.ContainsFunc(fields, func(existing backend.KernelDialogField) bool { return existing.ID == field.ID }) {
					fields = append(fields, field)
				}
			}
		}
	}
	return fields
}

func preferencePageView(popup *backend.KernelPopupPresentation, tab string) *backend.KernelPopupPresentation {
	for _, page := range popup.LayoutPages {
		if page.ID != tab {
			continue
		}
		view := *popup
		view.Fields = nil
		for _, field := range popup.Fields {
			if field.ID == "tab" {
				field.Value = tab
				view.Fields = append(view.Fields, field)
			}
		}
		view.Fields = append(view.Fields, page.Fields...)
		view.Actions = slices.Clone(page.Actions)
		for _, action := range popup.Actions {
			if action.ID == "load" || action.ID == "change" || action.ID == "navigate" || nativeDialogFooterAction(action) {
				view.Actions = append(view.Actions, action)
			}
		}
		return &view
	}
	return popup
}

func nativeDialogValueText(field backend.KernelDialogField) string {
	if field.Text != "" {
		return field.Text
	}
	switch value := field.Value.(type) {
	case string:
		return value
	case float64:
		return fmt.Sprint(value)
	case int:
		return fmt.Sprint(value)
	}
	if field.Kind == "list" {
		labels := make([]string, len(field.Choices))
		for i, choice := range field.Choices {
			labels[i] = choice.Label
		}
		return strings.Join(labels, "\n")
	}
	return ""
}

// Operation names are UI state, unlike literal Git branches and paths. Keep
// their original wire values and translate only this explicitly typed display.
func nativeDialogDisplayText(locale string, field backend.KernelDialogField) string {
	text := nativeDialogValueText(field)
	if field.Kind == "readonly" && field.LabelKey == "operation.operation" && slices.Contains([]string{"Merge", "Rebase", "Cherry-pick", "Squash", "Reorder"}, text) {
		return nativeDialogLabel(locale, "operation.kind."+text)
	}
	return text
}

func nativeDialogLabel(locale, key string) string {
	if key == "" {
		return ""
	}
	id := sha256.Sum256([]byte(key))
	return locales.Catalog.Format(locale, fmt.Sprintf("dialogLabel_%x", id[:8]), nil)
}

func nativeDialogChoiceLabel(locale, label string) string {
	if strings.HasPrefix(label, "settings.") || strings.HasPrefix(label, "repositoryDialog.") || strings.HasPrefix(label, "aux.") || strings.HasPrefix(label, "operation.") {
		return nativeDialogLabel(locale, label)
	}
	return label // Technical model, account, branch and repository names are data.
}

func nativeDialogValid(popup *backend.KernelPopupPresentation) bool {
	if popup == nil || !popup.Supported || popup.Layout != "form" || popup.ID == nil || *popup.ID <= 0 || popup.TitleKey == "" {
		return false
	}
	fields, actions := make(map[string]bool), make(map[string]bool)
	for _, field := range nativeDialogFields(popup) {
		if field.ID == "" || field.LabelKey == "" || fields[field.ID] || !slices.Contains([]string{"text", "multiline", "checkbox", "select", "readonly", "list", "progress", "icons", "image", "diff"}, field.Kind) {
			return false
		}
		fields[field.ID] = true
		if field.Kind == "icons" && (field.IconLibrary != "octicons" || !field.ReadOnly || slices.ContainsFunc(field.Choices, func(choice backend.KernelDialogChoice) bool { return nativeOcticons[choice.ID] == nil })) {
			return false
		}
		if field.Kind == "image" && (!field.ReadOnly || field.Image == nil) {
			return false
		}
		if field.Kind == "diff" && (!field.ReadOnly || field.Diff == nil) {
			return false
		}
	}
	for _, action := range popup.Actions {
		if action.ID == "" || action.LabelKey == "" || actions[action.ID] {
			return false
		}
		actions[action.ID] = true
		for _, id := range action.Fields {
			if !fields[id] {
				return false
			}
		}
	}
	return true
}

func nativeDialogReply(popup *backend.KernelPopupPresentation, form *nativeDialogForm, action backend.KernelDialogAction) (backend.KernelPopupReply, bool) {
	if !nativeDialogValid(popup) || form.busy || action.Disabled {
		return backend.KernelPopupReply{}, false
	}
	declared := slices.IndexFunc(popup.Actions, func(candidate backend.KernelDialogAction) bool { return candidate.ID == action.ID })
	if declared < 0 || popup.Actions[declared].Disabled {
		return backend.KernelPopupReply{}, false
	}
	out := backend.KernelPopupReply{PopupID: *popup.ID, Type: popup.Type, Action: action.ID, Values: make(map[string]any)}
	fields := nativeDialogFields(popup)
	for _, id := range popup.Actions[declared].Fields {
		field := fields[slices.IndexFunc(fields, func(field backend.KernelDialogField) bool { return field.ID == id })]
		value := form.values[id]
		if popup.Type == "Preferences" && len(popup.LayoutPages) > 0 && (action.ID == "change" || action.ID == "save") && value != nil && !value.dirty && id != "tab" {
			continue
		}
		if field.ReadOnly || field.Kind == "readonly" || field.Kind == "list" || field.Kind == "progress" || value == nil {
			return backend.KernelPopupReply{}, false
		}
		if field.Kind == "checkbox" {
			out.Values[id] = value.check
		} else {
			out.Values[id] = value.text
		}
	}
	return out, true
}

func renderNativeDialog(c *ui.Context, tx locales.Messages, popup *backend.KernelPopupPresentation, form *nativeDialogForm, reply func(backend.KernelPopupReply), close func(int)) {
	if !nativeDialogValid(popup) {
		renderKernelPopup(c, tx, popup, &kernelPopupForm{}, nil, nil)
		return
	}
	form.sync(popup)
	originalPopup := popup
	if popup.Type == "Preferences" && len(popup.LayoutPages) > 0 {
		popup = preferencePageView(popup, form.displayTab)
	}
	send := func(action backend.KernelDialogAction) {
		if form.validating() || form.navigationPending || form.pendingNavigation != "" {
			return
		}
		out, ok := nativeDialogReply(popup, form, action)
		if !ok || reply == nil {
			return
		}
		sentSecret := false
		for _, field := range popup.Fields {
			if field.Sensitive && slices.Contains(action.Fields, field.ID) {
				form.values[field.ID].text = ""
				sentSecret = true
			}
		}
		if sentSecret {
			form.secretEpoch++
		}
		form.busy, form.err = true, nil
		reply(out)
	}
	dismiss := func() {
		if form.busy || close == nil || !slices.ContainsFunc(popup.Actions, func(action backend.KernelDialogAction) bool {
			return !action.Disabled && (action.ID == "cancel" || action.ID == "close" || action.ID == "done")
		}) {
			return
		}
		for _, value := range form.values {
			value.text = ""
		}
		clear(form.pendingChanges)
		form.pendingNavigation = ""
		form.secretEpoch++
		form.busy, form.err = true, nil
		close(*popup.ID)
	}
	title := nativeDialogLabel(c.Locale(), popup.TitleKey)
	var navigation *backend.KernelDialogField
	if nativeDialogHasNavigation(popup) {
		for i := range popup.Fields {
			if popup.Fields[i].ID == "tab" && popup.Fields[i].Kind == "select" {
				navigation = &popup.Fields[i]
			}
		}
	}
	// Measure actual controls. Repository entry forms retain their task order;
	// settings pages use responsive columns without page scrolling.
	preferences := measurePreferences(c, popup, navigation, form)
	if preferences.measured && (popup.Type != "Preferences" || !slices.ContainsFunc(originalPopup.Actions, func(action backend.KernelDialogAction) bool { return action.ID == "load" })) {
		form.presentedLayout = preferences
	} else if form.presentedLayout.measured {
		// Retain the previous complete layout while a changed form is measured.
		preferences = form.presentedLayout
	}
	// Native category buttons send the same complete draft as the original
	// select. Only presentation changes; validation and cancel still own it.
	changedField := func(field backend.KernelDialogField) {
		form.values[field.ID].dirty = true
		if field.ChangeAction == "" {
			return
		}
		if form.pendingChanges == nil {
			form.pendingChanges = make(map[string]time.Time)
		}
		delay := time.Duration(0)
		if field.Kind == "text" || field.Kind == "multiline" {
			delay = 180 * time.Millisecond
		}
		form.pendingChanges[field.ChangeAction] = c.Now().Add(delay)
	}
	if navigation != nil && form.requestedTab != "" && !form.busy && !form.validating() && !navigation.Disabled {
		if form.values[navigation.ID].text != form.requestedTab {
			form.values[navigation.ID].text = form.requestedTab
			if popup.Type == "Preferences" && len(popup.LayoutPages) > 0 {
				form.displayTab, form.pendingNavigation = form.requestedTab, form.requestedTab
				popup = preferencePageView(originalPopup, form.displayTab)
			} else {
				changedField(*navigation)
			}
		}
		form.requestedTab = ""
	}
	open := true
	panel := ui.DialogBase(c.Key(struct {
		Name string
		ID   int
	}{"native-dialog", *popup.ID}), &open, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Width(preferences.width).Height(preferences.height).Padding(24).Gap(16).Radius(12).
			Border(1, c.Theme().Border).Background(c.Theme().Background).Clip().Label(title)
		if !form.presentedLayout.measured && form.err == nil && (popup.Error == nil || *popup.Error == "") {
			// Cold layout and the original load finish before the first paint.
			// The backdrop still owns modality and Escape; only the panel waits.
			panel.Invisible()
		}
		ui.Row(c).FillWidth().Shrink(0).Children(func() {
			ui.Text(c, title).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500).Role(ui.RoleHeading).Level(1).Grow(1)
			if navigation != nil {
				canClose := slices.ContainsFunc(popup.Actions, func(action backend.KernelDialogAction) bool {
					return !action.Disabled && (action.ID == "cancel" || action.ID == "close" || action.ID == "done")
				})
				iconButton(c.Key("close-preferences"), tx.Close(), "x").Disabled(form.busy || !canClose).OnClick(dismiss)
			}
		})
		ui.Row(c.Key("dialog-body")).Grow(1).MinHeight(0).FillWidth().AlignItems(ui.Stretch).Gap(24).Children(func() {
			if navigation != nil {
				ui.Column(c.Key("dialog-navigation")).Width(preferences.navigation).Shrink(0).FillHeight().Gap(4).Children(func() {
					for _, choice := range navigation.Choices {
						label := nativeDialogChoiceLabel(c.Locale(), choice.Label)
						button := quietButton(c.Key(choice.ID), label, "").MinHeight(36).FillWidth().Justify(ui.Start).
							Disabled(form.busy || navigation.Disabled || choice.Disabled)
						if form.section == "" && form.values[navigation.ID].text == choice.ID {
							button.Background(c.Theme().SurfacePressed).Value(tx.CurrentPage())
						}
						button.OnClick(func() {
							form.section = ""
							if form.values[navigation.ID].text == choice.ID {
								return
							}
							form.values[navigation.ID].text = choice.ID
							if popup.Type == "Preferences" && len(popup.LayoutPages) > 0 {
								form.displayTab, form.pendingNavigation = choice.ID, choice.ID
								popup = preferencePageView(originalPopup, choice.ID)
							} else {
								changedField(*navigation)
							}
						})
					}
					for _, section := range form.sections {
						button := quietButton(c.Key(section.id), section.label, "").MinHeight(36).FillWidth().Justify(ui.Start).Disabled(form.busy)
						if form.section == section.id {
							button.Background(c.Theme().SurfacePressed).Value(tx.CurrentPage())
						}
						button.OnClick(func() { form.section = section.id })
					}
				})
			}
			for _, section := range form.sections {
				if section.id == form.section {
					ui.Column(c.Key(section.id)).Grow(1).MinWidth(0).MinHeight(0).FillHeight().Gap(16).Children(func() { section.render(c) })
					return
				}
			}
			ui.Column(c.Key("dialog-content")).Grow(1).MinWidth(0).MinHeight(0).FillHeight().Gap(16).Children(func() {
				sendAction := send
				if reply == nil {
					sendAction = nil
				}
				renderPreferenceContent(c, popup, form, preferences, changedField, sendAction)
			})
		})
		// Extension sections own their persistence (provider Save or immediate
		// language selection). The original Save would close this view without
		// saving its editor, so expose it only with the draft it actually saves.
		if form.section != "" {
			return
		}
		ui.Row(c.Key("dialog-footer")).Gap(8).Wrap().Justify(ui.End).FillWidth().Shrink(0).Children(func() {
			if form.validating() {
				ui.Text(c, tx.PopupCheckingFields()).Role(ui.RoleStatus).TextColor(c.Theme().TextMuted).Grow(1)
			}
			actions := slices.Clone(popup.Actions)
			slices.SortStableFunc(actions, func(a, b backend.KernelDialogAction) int {
				if a.Primary != b.Primary {
					if a.Primary {
						return 1
					}
					return -1
				}
				return 0
			})
			for _, action := range actions {
				if nativeDialogFooterAction(action) {
					cancelAction := action.ID == "cancel" || action.ID == "close" || action.ID == "done"
					if cancelAction {
						renderNativeDialogButton(c.Key(action.ID), action, form.busy || close == nil, func(backend.KernelDialogAction) { dismiss() })
					} else {
						renderNativeDialogButton(c.Key(action.ID), action, form.busy || form.validating() || form.navigationPending || form.pendingNavigation != "" || reply == nil, send)
					}
				}
			}
		})
	})
	if panel.Dismissed() {
		dismiss()
	}
	if form.busy || form.changePending || form.navigationPending || reply == nil {
		return
	}
	for _, action := range popup.Actions {
		due, pending := form.pendingChanges[action.ID]
		if !pending {
			continue
		}
		if c.Now().Before(due) {
			c.After(due.Sub(c.Now()))
			continue
		}
		out, ok := nativeDialogReply(popup, form, action)
		if !ok {
			continue
		}
		delete(form.pendingChanges, action.ID)
		// Validation owns no modal busy lock. Keep typed dirty values while an
		// older response arrives; its canonical draft is not a new user edit.
		form.changePending, form.changeAction, form.err = true, action.ID, nil
		reply(out)
		return
	}
	if form.pendingNavigation != "" {
		for _, action := range popup.Actions {
			if action.ID != "navigate" || action.Disabled {
				continue
			}
			out, ok := nativeDialogReply(popup, form, action)
			if !ok {
				return
			}
			out.Values["tab"] = form.pendingNavigation
			form.pendingNavigation, form.changeAction, form.navigationPending = "", "navigate", true
			reply(out)
			return
		}
	}
}

func nativeDialogFooterAction(action backend.KernelDialogAction) bool {
	return action.Primary || action.ID == "cancel" || action.ID == "close" || action.ID == "done"
}

func renderNativeDialogButton(c *ui.Context, action backend.KernelDialogAction, busy bool, send func(backend.KernelDialogAction)) {
	label := nativeDialogLabel(c.Locale(), action.LabelKey)
	var button ui.Element
	if action.Primary {
		button = ui.PrimaryButton(c, label)
	} else {
		button = ui.Button(c, label)
	}
	button.MinHeight(40).Padding(8, 16).FontWeight(500).Disabled(busy || action.Disabled)
	if action.Destructive {
		button.TextColor(c.Theme().Danger)
	}
	button.OnClick(func() { send(action) })
}

func renderNativeDialogField(c *ui.Context, field backend.KernelDialogField, form *nativeDialogForm, changed func()) {
	label := nativeDialogLabel(c.Locale(), field.LabelKey)
	value, disabled := form.values[field.ID], field.Disabled || form.busy
	if field.Kind == "checkbox" {
		ui.Checkbox(c, &value.check, label).FillWidth().Disabled(disabled).OnChange(changed)
		return
	}
	if field.Kind == "progress" {
		progress := -1.0
		if v, ok := field.Value.(float64); ok {
			progress = v
		}
		ui.Progress(c, progress).Label(label)
		return
	}
	ui.Field(c, label, func() {
		switch field.Kind {
		case "diff":
			if form.renderDiff != nil {
				form.renderDiff(c, field, value)
			}
		case "image":
			// Loading and decoded media reserve the same bounded area, so a
			// late decode cannot displace the surrounding form parameters.
			ui.Column(c).FillWidth().Height(192).Children(func() {
				if value.imageMedia != nil && value.imageMedia.player != nil {
					// Video already owns reduced-motion and ended playback. Calling
					// Play every build would rewind a naturally finished animation.
					ui.Video(c, value.imageMedia.player).FillWidth().Height(192).Fit(ui.Contain).Label(label)
				} else if value.image != nil {
					ui.Image(c, value.image).FillWidth().Height(192).Fit(ui.Contain).Label(label)
				} else {
					text := locales.In(c.Locale()).LoadingDiff()
					if value.imageErr != nil {
						text = locales.In(c.Locale()).DiffUnrenderable()
					}
					ui.Text(c, text).Role(ui.RoleStatus)
				}
			})
		case "icons":
			size := float32(24)
			if number, ok := field.Value.(float64); ok && number > 0 {
				size = float32(number)
			}
			_, viewHeight := c.Size()
			// The icon catalogue is collection data, not form parameters.
			ui.Scroll(c).FillWidth().MaxHeight(viewHeight / 3).Children(func() {
				ui.Row(c).FillWidth().Wrap().Gap(12).Children(func() {
					for _, choice := range field.Choices {
						ui.Column(c.Key(choice.ID)).Width(max(112, size+24)).Gap(8).Padding(8).AlignItems(ui.Center).Tooltip(choice.Label).Children(func() {
							ui.Icon(c, nativeOcticons[choice.ID]).FontSize(size).Label(choice.Label)
							ui.Text(c, choice.Label).FontSize(c.Theme().Rem(14.0 / 15)).SingleLine().FillWidth().TextAlign(ui.Center)
						})
					}
				})
			})
		case "select":
			renderNativeDialogSelect(c, field, &value.text, disabled).OnChange(changed)
		case "readonly", "list":
			text := nativeDialogDisplayText(c.Locale(), field)
			input := ui.TextArea(c, &text).ReadOnly(true).FillWidth().Lines(1, 6).Label(label).Disabled(field.Disabled)
			if field.Kind == "readonly" && field.Height == 0 && !strings.ContainsAny(text, "\r\n") {
				// Short metadata remains selectable without looking like another
				// editable card. Long diagnostics/lists keep their bounded editor.
				input.Lines(1, 2).MinHeight(0).Padding(4, 0).BorderWidth(0).Background(c.Theme().Background)
			}
		case "multiline":
			height := field.Height
			if height <= 0 {
				height = 120
			}
			ui.TextArea(c, &value.text).Height(min(height, 256)).FillWidth().ReadOnly(field.ReadOnly).Disabled(disabled).OnChange(changed)
		default:
			inputContext := c.Key("text")
			if field.Sensitive {
				inputContext = c.Key(form.secretEpoch)
			}
			input := ui.TextInput(inputContext, &value.text).MinHeight(40).FillWidth().ReadOnly(field.ReadOnly).Disabled(disabled).Placeholder(field.Placeholder).OnChange(changed)
			if field.Sensitive {
				input.Password()
			}
		}
	}).Shrink(0)
}

func renderNativeDialogSelect(c *ui.Context, field backend.KernelDialogField, value *string, disabled bool) ui.Element {
	label := nativeDialogLabel(c.Locale(), field.LabelKey)
	shown := ""
	for _, choice := range field.Choices {
		if choice.ID == *value {
			shown = nativeDialogChoiceLabel(c.Locale(), choice.Label)
		}
	}
	selectBox := ui.SelectBase(c, value)
	trigger := selectBox.Trigger.MinHeight(40).MinWidth(0).FillWidth().Padding(8, 10).Gap(8).Radius(8).
		Border(1, c.Theme().Border).Background(c.Theme().Surface).Label(label).Value(shown).Disabled(disabled)
	trigger.Children(func() {
		ui.Text(c, shown).SingleLine().Grow(1).MinWidth(0)
		ui.Icon(c, icons["chevron-down"]).FontSize(16).Shrink(0).TextColor(c.Theme().TextMuted)
	})
	selectBox.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border)
		ui.Scroll(c).Height(min(280, float32(len(field.Choices)*42))).FillWidth().Gap(2).Children(func() {
			for _, choice := range field.Choices {
				text := nativeDialogChoiceLabel(c.Locale(), choice.Label)
				item := selectBox.Item(choice.ID).MinHeight(40).Padding(8, 10).Radius(6).Label(text).Disabled(choice.Disabled)
				if item.Highlighted() {
					item.Background(c.Theme().SurfacePressed)
				}
				item.Children(func() { ui.Text(c, text).SingleLine().Grow(1).MinWidth(0) })
			}
		})
	})
	return trigger
}

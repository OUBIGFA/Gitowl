package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

type preferenceItem struct {
	field  backend.KernelDialogField
	height float32
}

type preferenceLayout struct {
	width, height, navigation, columnWidth float32
	columns                                int
	measured                               bool
}

// Only numeric geometry survives a popup. Drafts and control handles remain
// owned by their original form; a small cache bounds window-resize variants.
type preferenceGeometryCache map[string]preferenceLayout

func preferenceGeometryKey(c *ui.Context, popup *backend.KernelPopupPresentation, navigation *backend.KernelDialogField, form *nativeDialogForm, width, height float32) string {
	h := sha256.New()
	fmt.Fprint(h, width, height, c.Zoom(), c.Locale(), c.Theme().Font, c.Theme().FontSize, c.Theme().Spacing, c.Theme().Radius, popup.Type, popup.TitleKey, popup.Description, popup.DescriptionKey, form.busy, form.err, form.appearanceMeasureKey)
	if popup.Error != nil {
		fmt.Fprint(h, *popup.Error)
	}
	encoder := json.NewEncoder(h)
	if navigation != nil {
		for _, choice := range navigation.Choices {
			_ = encoder.Encode([]string{choice.ID, choice.Label})
		}
	}
	pages := popup.LayoutPages
	if len(pages) == 0 {
		pages = []backend.KernelDialogLayoutPage{{Fields: popup.Fields, Actions: popup.Actions}}
	}
	for _, page := range pages {
		fmt.Fprint(h, page.ID)
		for _, field := range page.Fields {
			if field.ID == "tab" && nativeDialogHasNavigation(popup) {
				continue
			}
			text := ""
			if field.Kind == "readonly" || field.Kind == "list" {
				text = nativeDialogDisplayText(c.Locale(), field)
			} else if field.Kind == "icons" {
				text = fmt.Sprint(field.Value)
			}
			// Fixed-height editors do not change size when their value changes.
			_ = encoder.Encode([]any{field.ID, field.Kind, field.LabelKey, field.Placeholder, field.Height, field.Required, field.ReadOnly, text})
			for _, choice := range field.Choices {
				if repositoryEntryDialog(popup) {
					break // Repository/account names are data, not dialog width.
				}
				_ = encoder.Encode([]string{choice.ID, choice.Label})
			}
		}
		for _, action := range page.Actions {
			if action.ID != "load" && action.ID != "change" && action.ID != "navigate" {
				_ = encoder.Encode([]any{action.ID, action.LabelKey, action.Primary})
			}
		}
	}
	for _, section := range form.sections {
		_ = encoder.Encode([]string{section.id, section.label, section.measureKey})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func preferenceTextSize(c *ui.Context, width float32, text string, size float32) (float32, float32) {
	return c.MeasureText(width, ui.Span{Text: text, Size: size, Font: c.Theme().Font, Weight: 500})
}

// Measure the same labels and controls used by the production field renderer.
// Panel size is derived from all original pages, never a per-page fixed size.
func preferenceItems(c *ui.Context, fields []backend.KernelDialogField, page string, width float32, form *nativeDialogForm) []preferenceItem {
	items := []preferenceItem{}
	if page == "appearance" && form.appearanceTop != nil {
		_, label := preferenceTextSize(c, width, locales.In(c.Locale()).InterfaceLanguage(), c.Theme().FontSize)
		items = append(items, preferenceItem{field: backend.KernelDialogField{ID: "native-language"}, height: label + 6 + 40})
	}
	// Keep editor and shell controls together, in the original semantic groups.
	if page == "integrations" {
		ordered := make([]backend.KernelDialogField, 0, len(fields))
		for _, id := range []string{"selectedExternalEditor", "useCustomEditor", "customEditorPath", "customEditorArguments", "customEditorBundleID", "selectedShell", "useCustomShell", "customShellPath", "customShellArguments", "customShellBundleID", "copilotAppPath"} {
			for _, field := range fields {
				if field.ID == id {
					ordered = append(ordered, field)
				}
			}
		}
		for _, field := range fields {
			if !slices.ContainsFunc(ordered, func(item backend.KernelDialogField) bool { return item.ID == field.ID }) {
				ordered = append(ordered, field)
			}
		}
		fields = ordered
	}
	for _, field := range fields {
		labelWidth := width
		if field.Kind == "checkbox" {
			labelWidth -= 28
		}
		_, label := preferenceTextSize(c, max(1, labelWidth), nativeDialogLabel(c.Locale(), field.LabelKey), c.Theme().FontSize)
		h := label + 6 + 40
		switch field.Kind {
		case "checkbox":
			h = max(24, label) + 8
		case "progress":
			h = 24
		case "readonly", "list":
			_, textHeight := preferenceTextSize(c, width-16, nativeDialogDisplayText(c.Locale(), field), c.Theme().FontSize)
			h = label + 6 + max(24, textHeight+8)
		case "multiline":
			h = label + 6 + max(120, field.Height)
		}
		items = append(items, preferenceItem{field: field, height: h})
	}
	if page == "appearance" && form.appearanceEnd != nil {
		_, h := preferenceTextSize(c, width, locales.In(c.Locale()).ProductVersion("0.0.1")+"   "+locales.In(c.Locale()).BuildNumber(BuildNumber), c.Theme().FontSize)
		items = append(items, preferenceItem{field: backend.KernelDialogField{ID: "native-version"}, height: h})
	}
	return items
}

// Partition consecutive fields into balanced columns, retaining keyboard order.
func preferenceColumns(items []preferenceItem, columns int) ([][]preferenceItem, float32) {
	if len(items) == 0 {
		return nil, 0
	}
	groups := 0
	for i, item := range items {
		if i == 0 || !(strings.HasPrefix(item.field.ID, "customEditor") || strings.HasPrefix(item.field.ID, "customShell") || strings.HasPrefix(item.field.ID, "useCustom")) {
			groups++
		}
	}
	columns = min(columns, groups)
	prefix := make([]float32, len(items)+1)
	for i, item := range items {
		prefix[i+1] = prefix[i] + item.height + 16
	}
	dp := make([][]float32, columns+1)
	cut := make([][]int, columns+1)
	for col := range dp {
		dp[col], cut[col] = make([]float32, len(items)+1), make([]int, len(items)+1)
		for i := 1; i <= len(items); i++ {
			dp[col][i] = float32(math.Inf(1))
		}
	}
	for col := 1; col <= columns; col++ {
		for end := col; end <= len(items); end++ {
			for start := col - 1; start < end; start++ {
				// A custom integration's switch, executable and arguments belong
				// to the same column as its editor/shell selector.
				if start > 0 && (strings.HasPrefix(items[start].field.ID, "customEditor") || strings.HasPrefix(items[start].field.ID, "customShell") || strings.HasPrefix(items[start].field.ID, "useCustom")) {
					continue
				}
				cost := max(dp[col-1][start], prefix[end]-prefix[start]-16)
				if cost < dp[col][end] {
					dp[col][end], cut[col][end] = cost, start
				}
			}
		}
	}
	out := make([][]preferenceItem, columns)
	end := len(items)
	for col := columns; col > 0; col-- {
		start := cut[col][end]
		out[col-1] = items[start:end]
		end = start
	}
	return out, dp[columns][len(items)]
}

func measurePreferences(c *ui.Context, popup *backend.KernelPopupPresentation, navigation *backend.KernelDialogField, form *nativeDialogForm) preferenceLayout {
	viewWidth, viewHeight := c.Size()
	layout := measurePreferencesWithin(c.Key("current-window"), popup, navigation, form, viewWidth, viewHeight)
	if layout.measured && (layout.width+24 > viewWidth || layout.height+24 > viewHeight) && form.settingsWorkArea != nil && form.expandSettings != nil {
		maxWidth, maxHeight := form.settingsWorkArea()
		maxWidth, maxHeight = maxWidth/c.Zoom(), maxHeight/c.Zoom()
		// Reflow against this display's work area before enlarging the host.
		// The requested size comes from content, including text scaling.
		if maxWidth > viewWidth || maxHeight > viewHeight {
			larger := measurePreferencesWithin(c.Key("display-work-area"), popup, navigation, form, maxWidth, maxHeight)
			layout.measured = false
			if larger.measured {
				layout = larger
				width, height := min(maxWidth, max(viewWidth, layout.width+24))*c.Zoom(), min(maxHeight, max(viewHeight, layout.height+24))*c.Zoom()
				if form.expandWidth != width || form.expandHeight != height {
					form.expandWidth, form.expandHeight = width, height
					form.expandSettings(width, height)
				}
				layout.measured = viewWidth*c.Zoom()+1 >= width && viewHeight*c.Zoom()+1 >= height
			}
		}
	}
	return layout
}

func measurePreferencesWithin(c *ui.Context, popup *backend.KernelPopupPresentation, navigation *backend.KernelDialogField, form *nativeDialogForm, viewWidth, viewHeight float32) preferenceLayout {
	contentKey := preferenceGeometryKey(c, popup, navigation, form, viewWidth, viewHeight)
	if cached, ok := form.geometryCache[contentKey]; ok {
		return cached
	}
	availableWidth, availableHeight := viewWidth-24, viewHeight-24
	navWidth, navCount := float32(0), 0
	if navigation != nil {
		for _, choice := range navigation.Choices {
			w, _ := preferenceTextSize(c, 0, nativeDialogChoiceLabel(c.Locale(), choice.Label), c.Theme().FontSize)
			navWidth, navCount = max(navWidth, w+20+settingsNavigationIconWidth(popup.Type, choice.ID)), navCount+1
		}
	}
	for _, section := range form.sections {
		w, _ := preferenceTextSize(c, 0, section.label, c.Theme().FontSize)
		navWidth, navCount = max(navWidth, w+20+settingsNavigationIconWidth(popup.Type, section.id)), navCount+1
	}
	pages := popup.LayoutPages
	if len(pages) == 0 {
		pages = []backend.KernelDialogLayoutPage{{Fields: nativeDialogContentFields(popup), Actions: popup.Actions}}
	}
	preferredColumn := float32(0)
	for _, page := range pages {
		for _, field := range page.Fields {
			w, _ := preferenceTextSize(c, 0, nativeDialogLabel(c.Locale(), field.LabelKey), c.Theme().FontSize)
			preferredColumn = max(preferredColumn, w+28)
			for _, choice := range field.Choices {
				if repositoryEntryDialog(popup) {
					break // Long repository/account names stay inside their controls.
				}
				w, _ = preferenceTextSize(c, 0, nativeDialogChoiceLabel(c.Locale(), choice.Label), c.Theme().FontSize)
				preferredColumn = max(preferredColumn, w+48)
			}
		}
	}
	// Labels describe fields; their length must not cap the space for editing
	// paths and arguments. Use a readable line length at the active text size,
	// then constrain it to the available window. The panel has no fixed size.
	preferredColumn = max(preferredColumn, c.Theme().Rem(26))
	if repositoryEntryDialog(popup) {
		preferredColumn = max(preferredColumn, c.Theme().Rem(36))
	}
	if popup.Type != "Preferences" {
		// Original dialogs may request more reading space for previews. Treat
		// that as a preference, still bounded by the current responsive columns.
		preferredColumn = max(preferredColumn, popup.Width-48-navWidth)
	}
	_, titleHeight := preferenceTextSize(c, 0, nativeDialogLabel(c.Locale(), popup.TitleKey), c.Theme().Rem(22.0/15))
	_, lineHeight := preferenceTextSize(c, 0, "Ag", c.Theme().FontSize)
	navHeight := max(float32(36), lineHeight+12)
	chrome := float32(48+32) + max(float32(40), lineHeight+16) + max(36, titleHeight)
	navGap := float32(0)
	if navWidth > 0 {
		navGap = 24
	}
	contentWidth := max(1, availableWidth-48-navWidth-navGap)
	result := preferenceLayout{navigation: navWidth}
	bestScore := math.Inf(1)
	allMeasured := true
	maxColumns := max(1, int(contentWidth/c.Theme().Rem(12)))
	if popup.Type == "Preferences" {
		maxColumns = min(2, maxColumns)
	}
	if repositoryEntryDialog(popup) {
		maxColumns = 1
	}
	for cols := 1; cols <= maxColumns; cols++ {
		columnWidth := min(preferredColumn, (contentWidth-float32(cols-1)*24)/float32(cols))
		if columnWidth < c.Theme().Rem(12) && cols > 1 {
			break
		}
		bodyHeight := float32(navCount)*navHeight + float32(max(0, navCount-1)*4)
		for _, page := range pages {
			_, fieldHeight := preferenceColumns(preferenceItems(c, page.Fields, page.ID, columnWidth, form), cols)
			// Page heading and spacing are shared. Action rows reserve their
			// measured labels rather than assuming every page has one button.
			toolsWidth := float32(0)
			for _, action := range page.Actions {
				w, _ := preferenceTextSize(c, 0, nativeDialogLabel(c.Locale(), action.LabelKey), c.Theme().FontSize)
				toolsWidth += w + 32
			}
			toolsHeight := float32(0)
			if toolsWidth > 0 {
				toolsHeight = float32(math.Ceil(float64(toolsWidth/(columnWidth*float32(cols))))) * 48
			}
			bodyHeight = max(bodyHeight, titleHeight+16+fieldHeight+toolsHeight)
		}
		// Invisible MyGo controls retain intrinsic layout but cannot draw,
		// receive input or enter accessibility. Measure every real page using
		// the production renderer; estimates above only bootstrap its first frame.
		actualHeight, measured := float32(navCount)*navHeight+float32(max(0, navCount-1)*4), true
		candidate := preferenceLayout{columnWidth: columnWidth, columns: cols}
		pageWidth := columnWidth*float32(cols) + float32(cols-1)*24
		for _, page := range pages {
			sample := *popup
			sample.Fields = page.Fields
			if popup.Type == "Preferences" {
				sample.Fields = append([]backend.KernelDialogField{{ID: "tab", Value: page.ID}}, page.Fields...)
			} else if navigation != nil {
				sample.Fields = append([]backend.KernelDialogField{*navigation}, page.Fields...)
			}
			sample.Actions = page.Actions
			draft := nativeDialogForm{appearanceTop: form.appearanceTop, appearanceEnd: form.appearanceEnd, renderDiff: form.renderDiff, err: form.err, busy: form.busy}
			draft.sync(&sample)
			probe := ui.Column(c.Key(fmt.Sprintf("measure-preferences-%g-%g-%d-%s-%s-%g-%s", viewWidth, viewHeight, cols, page.ID, c.Locale(), c.Theme().FontSize, contentKey))).Absolute().Top(0).Left(0).Width(pageWidth).MinHeight(1).Gap(16).Invisible().Shrink(0)
			probe.Children(func() {
				renderPreferenceContent(c, &sample, &draft, candidate, func(backend.KernelDialogField) {}, func(backend.KernelDialogAction) {})
			})
			bounds := probe.LayoutBounds()
			if bounds.H <= 0 || math.Abs(float64(bounds.W-pageWidth)) > 1 {
				measured = false
				c.Invalidate()
			} else {
				actualHeight = max(actualHeight, bounds.H)
			}
		}
		for _, section := range form.sections {
			for index, measure := range section.measure {
				probe := ui.Column(c.Key(fmt.Sprintf("measure-preferences-%g-%g-%d-%s-%d-%s-%g-%s", viewWidth, viewHeight, cols, section.id, index, c.Locale(), c.Theme().FontSize, section.measureKey))).Absolute().Top(0).Left(0).Width(pageWidth).Gap(16).Invisible().Shrink(0)
				probe.Children(func() { measure(c, probe) })
				bounds := probe.LayoutBounds()
				if bounds.H <= 0 || math.Abs(float64(bounds.W-pageWidth)) > 1 {
					measured = false
					c.Invalidate()
				} else {
					actualHeight = max(actualHeight, bounds.H)
				}
			}
		}
		if measured {
			bodyHeight = actualHeight
		}
		allMeasured = allMeasured && measured
		candidate = preferenceLayout{navigation: navWidth, columnWidth: columnWidth, columns: cols, measured: measured, width: 48 + navWidth + navGap + columnWidth*float32(cols) + float32(cols-1)*24, height: chrome + bodyHeight}
		// Choose the arrangement that uses the available width and height most
		// evenly. A tall window must not force a narrow, towering single column.
		score := max(float64(candidate.width/availableWidth), float64(candidate.height/availableHeight))
		if score < bestScore {
			result, bestScore = candidate, score
		}
	}
	result.measured = allMeasured
	if allMeasured {
		if form.geometryCache == nil {
			form.geometryCache = make(preferenceGeometryCache)
		}
		if len(form.geometryCache) >= 8 {
			clear(form.geometryCache)
		}
		form.geometryCache[contentKey] = result
	}
	return result
}

func renderPreferenceFields(c *ui.Context, popup *backend.KernelPopupPresentation, form *nativeDialogForm, layout preferenceLayout, changed func(backend.KernelDialogField)) {
	page := ""
	if i := slices.IndexFunc(popup.Fields, func(field backend.KernelDialogField) bool { return field.ID == "tab" }); i >= 0 && nativeDialogHasNavigation(popup) {
		page, _ = popup.Fields[i].Value.(string)
	}
	columns, _ := preferenceColumns(preferenceItems(c, nativeDialogContentFields(popup), page, layout.columnWidth, form), layout.columns)
	ui.Row(c.Key("dialog-fields")).FillWidth().Gap(24).Shrink(0).AlignItems(ui.Start).Children(func() {
		for index, column := range columns {
			ui.Column(c.Key(index)).Width(layout.columnWidth).Gap(16).Shrink(0).Children(func() {
				for _, item := range column {
					field := item.field
					ui.Column(c.Key(field.ID)).FillWidth().Shrink(0).Children(func() {
						switch field.ID {
						case "native-language":
							form.appearanceTop(c)
						case "native-version":
							form.appearanceEnd(c)
						default:
							renderNativeDialogField(c, field, form, func() { changed(field) })
						}
					})
				}
			})
		}
	})
}

// Both visible pages and their inert size probes use this renderer. Errors and
// wrapped tool buttons therefore take exactly the same room during measurement.
func renderPreferenceContent(c *ui.Context, popup *backend.KernelPopupPresentation, form *nativeDialogForm, layout preferenceLayout, changed func(backend.KernelDialogField), send func(backend.KernelDialogAction)) {
	page := ""
	for _, field := range popup.Fields {
		if field.ID == "tab" && nativeDialogHasNavigation(popup) {
			page, _ = field.Value.(string)
		}
	}
	if page != "" {
		settingsTitle(c, nativeDialogChoiceLabel(c.Locale(), "settings.option."+page))
	}
	if popup.DescriptionKey != "" {
		ui.Text(c, nativeDialogLabel(c.Locale(), popup.DescriptionKey)).Shrink(0)
	}
	if popup.Description != "" {
		ui.Text(c, popup.Description).Shrink(0)
	}
	var handledActions map[string]bool
	if popup.Type == "Preferences" {
		handledActions = renderPreferencePage(c, popup, form, layout, page, changed, send)
	} else if repositoryEntryDialog(popup) {
		renderRepositoryEntryFields(c, popup, form, changed, send)
	} else {
		renderPreferenceFields(c, popup, form, layout, changed)
	}
	tools := []backend.KernelDialogAction{}
	for _, action := range popup.Actions {
		if !handledActions[action.ID] && nativeDialogToolAction(popup, action) {
			tools = append(tools, action)
		}
	}
	if len(tools) != 0 {
		ui.Row(c.Key("dialog-tools")).Gap(8).Wrap().FillWidth().Shrink(0).Children(func() {
			for _, action := range tools {
				if popup.Type == "Preferences" {
					renderPreferenceTool(c.Key(action.ID), action, form, send)
				} else {
					renderNativeDialogButton(c.Key(action.ID), action, send == nil || form.busy || form.validating() || form.navigationPending || form.pendingNavigation != "", send)
				}
			}
		})
	}
	if popup.Error != nil && *popup.Error != "" && !form.validating() {
		message := nativeDialogChoiceLabel(c.Locale(), *popup.Error)
		if popup.Type == "Preferences" {
			ui.Text(c, message).FillWidth().Shrink(0).TextColor(c.Theme().Danger).Role(ui.RoleAlert)
		} else {
			// Diagnostics can be arbitrarily long. Keep their own selectable
			// viewer bounded without turning the parameters into a scrolling page.
			ui.TextArea(c.Key("dialog-error"), &message).ReadOnly(true).FillWidth().Lines(1, 4).Shrink(0).TextColor(c.Theme().Danger).Role(ui.RoleAlert).Label(locales.In(c.Locale()).FullErrorDetails())
		}
	}
	if form.err != nil && !form.validating() {
		ui.Text(c, locales.In(c.Locale()).PopupReplyFailed()).FillWidth().Shrink(0).TextColor(c.Theme().Danger).Role(ui.RoleAlert)
	}
	if form.busy {
		ui.Progress(c, -1).Label(locales.In(c.Locale()).PopupWorking())
	}
}

// Repository clone's "tab" is an ordinary source selector. Only settings
// categories move into the navigation rail; other parameters stay in the form.
func nativeDialogHasNavigation(popup *backend.KernelPopupPresentation) bool {
	return popup.Type == "Preferences" || popup.Type == "RepositorySettings"
}

func nativeDialogContentFields(popup *backend.KernelPopupPresentation) []backend.KernelDialogField {
	if !nativeDialogHasNavigation(popup) {
		return popup.Fields
	}
	return slices.DeleteFunc(slices.Clone(popup.Fields), func(field backend.KernelDialogField) bool { return field.ID == "tab" })
}

func nativeDialogToolAction(popup *backend.KernelPopupPresentation, action backend.KernelDialogAction) bool {
	if repositoryEntryDialog(popup) {
		// Tools belong to their input group. Recovery remains a declared
		// continuation but is shown only alongside the matching original error.
		switch action.ID {
		case "choose", "refresh", "signIn", "signInEnterprise":
			return false
		case "trust":
			return popup.Error != nil && *popup.Error == "repositoryDialog.error.unsafeRepository"
		case "createHere":
			return popup.Error != nil && *popup.Error == "repositoryDialog.error.notRepository"
		case "addInstead":
			return popup.Error != nil && *popup.Error == "repositoryDialog.error.existingRepository"
		}
	}
	if nativeDialogFooterAction(action) || nativeDialogHasNavigation(popup) && (action.ID == "change" || action.ID == "navigate") || slices.ContainsFunc(popup.Fields, func(field backend.KernelDialogField) bool { return field.ChangeAction == action.ID }) {
		return false
	}
	// Automatic loading is not an extra setup step. Its retry is available
	// when the original controller reports a failure.
	return action.ID != "load" || popup.Error != nil && *popup.Error != ""
}

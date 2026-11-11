package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func dialogFixture() *backend.KernelPopupPresentation {
	id := 100
	return &backend.KernelPopupPresentation{
		ID: &id, Type: "EditCopilotBYOKProvider", Supported: true, Layout: "form", TitleKey: "settings.title.EditCopilotBYOKProvider",
		Fields: []backend.KernelDialogField{
			{ID: "name", LabelKey: "settings.name", Kind: "text", Value: "Development gateway"},
			{ID: "format", LabelKey: "settings.format", Kind: "select", Value: "openai-responses", Choices: []backend.KernelDialogChoice{{ID: "openai-responses", Label: "OpenAI Responses"}, {ID: "anthropic-messages", Label: "Anthropic Messages"}}},
			{ID: "secret", LabelKey: "settings.secret", Kind: "text", Sensitive: true},
			{ID: "reference", LabelKey: "settings.account", Kind: "readonly", Text: "fixture-account"},
		},
		Actions: []backend.KernelDialogAction{
			{ID: "save", LabelKey: "settings.save", Primary: true, Fields: []string{"name", "format", "secret"}},
			{ID: "cancel", LabelKey: "settings.cancel"},
		},
	}
}

func dialogTester(popup *backend.KernelPopupPresentation, form *nativeDialogForm, locale string, width, height int, replies *[]backend.KernelPopupReply, closes *[]int) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		renderNativeDialog(c, locales.In(c.Locale()), popup, form, func(reply backend.KernelPopupReply) { *replies = append(*replies, reply) }, func(id int) { *closes = append(*closes, id) })
	}, width, height)
	tt.SetLocale(locale)
	tt.Advance(time.Second)
	return tt
}

func TestRepositoryDialogShowsEveryParameterWithoutFormScrolling(t *testing.T) {
	popup := dialogFixture()
	popup.Type, popup.TitleKey = "CreateRepository", "repositoryDialog.CreateRepository"
	popup.Fields = []backend.KernelDialogField{
		{ID: "name", LabelKey: "repositoryDialog.name", Kind: "text"},
		{ID: "path", LabelKey: "repositoryDialog.path", Kind: "text"},
		{ID: "description", LabelKey: "repositoryDialog.description", Kind: "text"},
		{ID: "readme", LabelKey: "repositoryDialog.readme", Kind: "checkbox"},
		{ID: "gitIgnore", LabelKey: "repositoryDialog.gitIgnore", Kind: "select", Choices: []backend.KernelDialogChoice{{ID: "None", Label: "—"}}},
		{ID: "license", LabelKey: "repositoryDialog.license", Kind: "select", Choices: []backend.KernelDialogChoice{{ID: "None", Label: "—"}}},
	}
	popup.Actions = []backend.KernelDialogAction{
		{ID: "addInstead", LabelKey: "repositoryDialog.addInstead"},
		{ID: "choose", LabelKey: "repositoryDialog.choose"},
		{ID: "confirm", LabelKey: "repositoryDialog.createRepository", Primary: true},
		{ID: "cancel", LabelKey: "repositoryDialog.cancel"},
	}
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			var replies []backend.KernelPopupReply
			var closes []int
			tt := dialogTester(popup, &nativeDialogForm{}, locale, 1180, 900, &replies, &closes)
			for _, size := range [][2]int{{1180, 900}, {900, 620}} {
				tt.SetSize(size[0], size[1])
				tt.Frame()
				if _, found := tt.AccessByRole(ui.RoleScroll, ""); found {
					t.Fatal("repository parameters still require form scrolling")
				}
				for _, field := range popup.Fields {
					visibleControl(t, tt, nativeDialogLabel(locale, field.LabelKey), size[0], size[1])
				}
				for _, action := range popup.Actions {
					if action.ID == "addInstead" {
						if tt.HasText(nativeDialogLabel(locale, action.LabelKey)) {
							t.Fatal("normal creation offers an unrelated recovery action")
						}
						continue
					}
					info, found := tt.AccessByRole(ui.RoleButton, nativeDialogLabel(locale, action.LabelKey))
					if !found || info.Offscreen || info.Bounds.H < 40 || info.Bounds.Y+info.Bounds.H > float32(size[1]) {
						t.Fatalf("dialog clips %s at %v: %+v", action.ID, size, info)
					}
				}
				path, _ := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel(locale, "repositoryDialog.path"))
				choose, _ := tt.AccessByRole(ui.RoleButton, nativeDialogLabel(locale, "repositoryDialog.choose"))
				if choose.Bounds.X < path.Bounds.X+path.Bounds.W || choose.Bounds.Y != path.Bounds.Y {
					t.Fatalf("path chooser is detached from its input: path=%+v choose=%+v", path, choose)
				}
				confirm, _ := tt.AccessByRole(ui.RoleButton, nativeDialogLabel(locale, "repositoryDialog.createRepository"))
				cancel, _ := tt.AccessByRole(ui.RoleButton, nativeDialogLabel(locale, "repositoryDialog.cancel"))
				if confirm.Bounds.X < cancel.Bounds.X+cancel.Bounds.W {
					t.Fatal("primary action is not the trailing footer action")
				}
			}
		})
	}
}

func TestRepositoryRecoveryAppearsOnlyForItsOriginalError(t *testing.T) {
	for _, test := range []struct{ kind, action, errorKey string }{
		{"AddRepository", "createHere", "repositoryDialog.error.notRepository"},
		{"AddRepository", "trust", "repositoryDialog.error.unsafeRepository"},
		{"CreateRepository", "addInstead", "repositoryDialog.error.existingRepository"},
	} {
		t.Run(test.action, func(t *testing.T) {
			popup := dialogFixture()
			popup.Type, popup.TitleKey = test.kind, "repositoryDialog."+test.kind
			popup.Fields = []backend.KernelDialogField{{ID: "path", Kind: "text", LabelKey: "repositoryDialog.path", Value: "fixture"}}
			popup.Actions = []backend.KernelDialogAction{{ID: test.action, LabelKey: "repositoryDialog." + test.action, Fields: []string{"path"}}, {ID: "cancel", LabelKey: "repositoryDialog.cancel"}}
			var replies []backend.KernelPopupReply
			var closes []int
			form := &nativeDialogForm{}
			tt := dialogTester(popup, form, "en", 900, 620, &replies, &closes)
			label := nativeDialogLabel("en", "repositoryDialog."+test.action)
			if tt.HasText(label) {
				t.Fatal("recovery action shown without an error")
			}
			popup.Error = &test.errorKey
			tt.Advance(time.Second)
			tt.Frame()
			if err := tt.Click(label); err != nil {
				t.Fatalf("%v; texts=%q layout=%+v", err, tt.Texts(), form.presentedLayout)
			}
			if len(replies) != 1 || replies[0].Action != test.action || replies[0].Values["path"] != "fixture" {
				t.Fatalf("recovery lost original continuation or input: %+v", replies)
			}
		})
	}
}

func TestCloneWithoutAccountsOffersSignInWithoutEmptySelectors(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			popup := dialogFixture()
			popup.Type, popup.TitleKey = "CloneRepository", "repositoryDialog.CloneRepository"
			popup.Fields = []backend.KernelDialogField{
				{ID: "account", LabelKey: "repositoryDialog.account", Kind: "select"},
				{ID: "filter", LabelKey: "repositoryDialog.filter", Kind: "text"},
				{ID: "repository", LabelKey: "repositoryDialog.repository", Kind: "select"},
				{ID: "path", LabelKey: "repositoryDialog.path", Kind: "text", Value: "fixture"},
			}
			popup.Actions = []backend.KernelDialogAction{
				{ID: "signIn", LabelKey: "repositoryDialog.signIn"},
				{ID: "signInEnterprise", LabelKey: "repositoryDialog.signInEnterprise"},
				{ID: "cancel", LabelKey: "repositoryDialog.cancel"},
			}
			var replies []backend.KernelPopupReply
			var closes []int
			tt := dialogTester(popup, &nativeDialogForm{}, locale, 900, 620, &replies, &closes)
			for _, field := range popup.Fields[:3] {
				if tt.HasText(nativeDialogLabel(locale, field.LabelKey)) {
					t.Fatalf("unsigned clone shows an unusable %s field", field.ID)
				}
			}
			visibleControl(t, tt, nativeDialogLabel(locale, "repositoryDialog.path"), 900, 620)
			if err := tt.Click(nativeDialogLabel(locale, "repositoryDialog.signIn")); err != nil {
				t.Fatal(err)
			}
			if len(replies) != 1 || replies[0].Action != "signIn" {
				t.Fatalf("sign-in did not use the original dialog action: %+v", replies)
			}
		})
	}
}

func TestPreferencesNavigationKeepsCategoriesVisibleAndPreservesOriginalDraft(t *testing.T) {
	popup := dialogFixture()
	popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
	popup.Fields = []backend.KernelDialogField{
		{ID: "tab", LabelKey: "settings.tab", Kind: "select", Value: "accounts", ChangeAction: "navigate", Choices: []backend.KernelDialogChoice{{ID: "accounts", Label: "settings.option.accounts"}, {ID: "appearance", Label: "settings.option.appearance"}, {ID: "git", Label: "settings.option.git"}}},
		{ID: "name", LabelKey: "settings.name", Kind: "text", Value: "Existing draft"},
	}
	popup.LayoutPages = []backend.KernelDialogLayoutPage{{ID: "accounts", Fields: popup.Fields[1:]}, {ID: "appearance"}, {ID: "git"}}
	popup.Actions = []backend.KernelDialogAction{{ID: "navigate", LabelKey: "settings.tab", Fields: []string{"tab"}}, {ID: "change", LabelKey: "settings.save", Fields: []string{"tab", "name"}}, {ID: "save", LabelKey: "settings.save", Primary: true, Fields: []string{"tab", "name"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
	var replies []backend.KernelPopupReply
	var closes []int
	form := &nativeDialogForm{}
	tt := dialogTester(popup, form, "en", 900, 620, &replies, &closes)
	for _, choice := range popup.Fields[0].Choices {
		visibleControl(t, tt, nativeDialogChoiceLabel("en", choice.Label), 900, 620)
	}
	if err := tt.Click(nativeDialogChoiceLabel("en", "settings.option.appearance")); err != nil {
		t.Fatal(err)
	}
	tt.Advance(200 * time.Millisecond)
	if len(replies) != 1 || replies[0].Action != "navigate" || replies[0].Values["tab"] != "appearance" || len(replies[0].Values) != 1 || form.values["name"].text != "Existing draft" {
		t.Fatalf("category navigation lost the original draft: %+v", replies)
	}
	if form.busy {
		t.Fatal("category selection locked the whole preference form")
	}
}

func TestPreferencesExtraSectionKeepsOriginalDraftAndCancel(t *testing.T) {
	popup := dialogFixture()
	popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
	popup.Fields = append([]backend.KernelDialogField{{ID: "tab", LabelKey: "settings.tab", Kind: "select", Value: "accounts", Choices: []backend.KernelDialogChoice{{ID: "accounts", Label: "settings.option.accounts"}}}}, popup.Fields...)
	form := &nativeDialogForm{sections: []nativeDialogSection{{id: "providers", label: "Custom providers", render: func(c *ui.Context) { ui.Text(c, "Provider editor") }}}}
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 900, 620, &replies, &closes)
	if err := tt.Click(nativeDialogLabel("en", "settings.name")); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Unsaved original draft")
	if err := tt.Click("Custom providers"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Provider editor") || tt.HasText(nativeDialogLabel("en", "settings.secret")) || len(replies) != 0 {
		t.Fatal("extra section did not replace content independently of the original draft")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, nativeDialogLabel("en", "settings.save")); ok {
		t.Fatal("provider section exposes an unrelated preference save action")
	}
	if err := tt.Click(nativeDialogChoiceLabel("en", "settings.option.accounts")); err != nil {
		t.Fatal(err)
	}
	if form.values["name"].text != "Unsaved original draft" {
		t.Fatal("category switch discarded original input")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, nativeDialogLabel("en", "settings.save")); !ok {
		t.Fatal("returning to original preferences lost its save action")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(time.Second)
	if len(closes) != 1 || closes[0] != *popup.ID || len(replies) != 0 {
		t.Fatal("Escape did not preserve original cancellation")
	}
}

func TestNativeDialogSendsOnlyDeclaredValuesAndClearsPasswordUndo(t *testing.T) {
	popup, form := dialogFixture(), &nativeDialogForm{}
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "zh-Hans", 900, 620, &replies, &closes)
	name, secret := nativeDialogLabel("zh-Hans", "settings.name"), nativeDialogLabel("zh-Hans", "settings.secret")
	if err := tt.Click(name); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("重命名后的供应商")
	if err := tt.Click(secret); err != nil {
		t.Fatal(err)
	}
	tt.Type("WRITE-ONLY-SECRET-SENTINEL")
	data, err := json.Marshal(popup)
	if err != nil || strings.Contains(string(data), "SENTINEL") {
		t.Fatal("UI secret entered a projected snapshot")
	}
	if err := tt.Click(nativeDialogLabel("zh-Hans", "settings.save")); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Values["name"] != "重命名后的供应商" || replies[0].Values["secret"] != "WRITE-ONLY-SECRET-SENTINEL" || len(replies[0].Values) != 3 {
		t.Fatalf("wrong allow-listed native form reply: %+v", replies)
	}
	if !form.busy || form.values["secret"].text != "" || len(closes) != 0 {
		t.Fatal("submit hid the original dialog or retained a password")
	}
	form.busy = false // A rejected original reply permits correction, not replay.
	tt.Frame()
	if err := tt.Click(secret); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if form.values["secret"].text != "" {
		t.Fatal("native undo restored an already submitted credential")
	}
}

func TestNativeDialogSelectionInvalidationNeverChoosesAnotherModel(t *testing.T) {
	popup, form := dialogFixture(), &nativeDialogForm{}
	popup.Fields[1].Choices = append(popup.Fields[1].Choices, backend.KernelDialogChoice{ID: "retired", Label: "Retired model", Disabled: true})
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 900, 620, &replies, &closes)
	trigger, ok := tt.AccessByRole(ui.RolePopUpButton, nativeDialogLabel("en", "settings.format"))
	if !ok || trigger.Bounds.W <= 0 || trigger.Bounds.H <= 0 {
		t.Fatalf("native protocol trigger missing: %+v", trigger)
	}
	tt.ClickAt(trigger.Bounds.X+trigger.Bounds.W/2, trigger.Bounds.Y+trigger.Bounds.H/2)
	access, ok := tt.AccessByRole(ui.RoleOption, "Retired model")
	if !ok || !access.Disabled {
		t.Fatalf("unavailable choice is actionable: %+v / %v", access, ok)
	}
	tt.Key(0, ui.KeyEscape)
	popup.Fields[1].Choices = []backend.KernelDialogChoice{{ID: "anthropic-messages", Label: "Anthropic Messages"}}
	tt.Frame()
	if form.values["format"].text != "" || len(replies) != 0 {
		t.Fatal("removed selection silently fell back or wrote a new selection")
	}
}

func TestNativeDialogCoveredFormsRetainInputsOnlyWhileOriginalPopupExists(t *testing.T) {
	popup := dialogFixture()
	form := &nativeDialogForm{}
	form.sync(popup)
	form.values["name"].text, form.values["name"].dirty = "Retained input", true
	a := newApp(nil, nil)
	defer a.close()
	a.coreDialogForms = map[int]*nativeDialogForm{*popup.ID: form}
	coverID := 101
	a.pruneNativeDialogForms(backend.KernelSnapshot{Popups: []backend.KernelPopup{{ID: popup.ID, Type: popup.Type}, {ID: &coverID, Type: "Error"}}})
	form.sync(popup)
	if form.values["name"].text != "Retained input" {
		t.Fatal("a covered form lost its local unsent edit")
	}
	a.pruneNativeDialogForms(backend.KernelSnapshot{})
	if len(a.coreDialogForms) != 0 || form.values["name"].text != "" {
		t.Fatal("removed original popup retained an editable native form")
	}
}

func TestNativeUnsubmittedSecretSurvivesOrdinaryFormActions(t *testing.T) {
	popup, form := dialogFixture(), &nativeDialogForm{}
	popup.Actions = append(popup.Actions, backend.KernelDialogAction{ID: "change", LabelKey: "settings.change", Fields: []string{"name", "format"}})
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 900, 620, &replies, &closes)
	secret := nativeDialogLabel("en", "settings.secret")
	if err := tt.Click(secret); err != nil {
		t.Fatal(err)
	}
	tt.Type("REPLACEMENT-SECRET-SENTINEL")
	if err := tt.Click(nativeDialogLabel("en", "settings.change")); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Values["secret"] != nil || form.values["secret"].text != "REPLACEMENT-SECRET-SENTINEL" {
		t.Fatal("ordinary form action discarded or transmitted an unsent credential")
	}
	form.busy, form.accept = false, true
	tt.Frame()
	if err := tt.Click(nativeDialogLabel("en", "settings.save")); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 || replies[1].Values["secret"] != "REPLACEMENT-SECRET-SENTINEL" || form.values["secret"].text != "" {
		t.Fatal("save did not transmit exactly the replacement credential and then clear it")
	}
}

func TestNativeDialogLayoutLanguageAndKeyboardMatrix(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			for _, size := range []struct{ width, height int }{{900, 620}, {1180, 760}} {
				t.Run(fmt.Sprintf("%s/%v/%dx%d", tag, dark, size.width, size.height), func(t *testing.T) {
					popup, form := dialogFixture(), &nativeDialogForm{}
					popup.Fields[0].Value = strings.Repeat("Long provider / 长供应商 / ", 6)
					var replies []backend.KernelPopupReply
					var closes []int
					tt := dialogTester(popup, form, tag, size.width, size.height, &replies, &closes)
					tt.SetDark(dark)
					for _, key := range []string{popup.TitleKey, "settings.name", "settings.format", "settings.secret", "settings.save", "settings.cancel"} {
						label := nativeDialogLabel(tag, key)
						if label == "" || label == key || strings.HasPrefix(label, "dialogLabel_") {
							t.Fatalf("missing active-language text: %s -> %q", key, label)
						}
						visibleControl(t, tt, label, size.width, size.height)
					}
					for _, other := range []string{"en", "zh-Hans", "zh-Hant"} {
						if other != tag && tt.HasText(nativeDialogLabel(other, popup.TitleKey)) {
							t.Fatal("native dialog mixes interface languages")
						}
					}
					tt.Key(0, ui.KeyTab)
					tt.Key(0, ui.KeyEscape)
					tt.Advance(time.Second)
					if len(closes) != 1 || closes[0] != *popup.ID || len(replies) != 0 {
						t.Fatalf("Escape did not request original cancellation: busy=%v closes=%v replies=%v menu=%v texts=%q", form.busy, closes, replies, tt.Menu(), tt.Texts())
					}
					// Optional ignored artifacts are real production primitive renders,
					// not desktop or kernel integration evidence.
					if dir := os.Getenv("GITOWL_NATIVE_UI_ARTIFACTS"); dir != "" {
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
						name := fmt.Sprintf("dialog-%s-%v-%dx%d", tag, dark, size.width, size.height)
						file, err := os.Create(filepath.Join(dir, name+".png"))
						if err != nil {
							t.Fatal(err)
						}
						if err = png.Encode(file, tt.Image()); err != nil {
							file.Close()
							t.Fatal(err)
						}
						if err = file.Close(); err != nil {
							t.Fatal(err)
						}
						data, err := json.MarshalIndent(map[string]any{"renderer": "MyGo ui.NewTester / renderNativeDialog", "locale": tag, "dark": dark, "width": size.width, "height": size.height, "texts": tt.Texts()}, "", "  ")
						if err != nil {
							t.Fatal(err)
						}
						if err = os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

// Choice-backed lists carry original branch/tag labels, while operation lists
// already carry text. Both must remain readable without altering their values.
func TestNativeDialogReadsChoiceBackedLists(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
	}{
		{"", "main\nrelease/中文"}, {"original commit summary", "original commit summary"},
	} {
		field := backend.KernelDialogField{ID: "branches", Kind: "list", LabelKey: "repositoryDialog.branches", Text: tc.text,
			Choices: []backend.KernelDialogChoice{{ID: "refs/heads/main", Label: "main"}, {ID: "refs/heads/release/中文", Label: "release/中文"}}}
		popup := dialogFixture()
		popup.Fields = []backend.KernelDialogField{field}
		form := &nativeDialogForm{}
		form.sync(popup)
		tt := ui.NewTester(func(c *ui.Context) {
			setWorkspaceTheme(c)
			ui.Column(c).Width(600).Padding(24).Children(func() { renderNativeDialogField(c, field, form, func() {}) })
		}, 1180, 900)
		tt.Advance(time.Second)
		actual, ok := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel("en", field.LabelKey))
		if !ok || actual.Value != tc.want || !actual.ReadOnly {
			t.Fatalf("list content = %+v, want %q", actual, tc.want)
		}
		if field.Choices[1].Label != "release/中文" {
			t.Fatal("rendering changed technical label")
		}
	}
}

func TestOperationDisplayNamesDoNotTranslateGitData(t *testing.T) {
	operation := backend.KernelDialogField{Kind: "readonly", LabelKey: "operation.operation", Text: "Merge"}
	branch := backend.KernelDialogField{Kind: "readonly", LabelKey: "operation.targetBranch", Text: "Merge"}
	for _, tc := range []struct{ locale, want string }{{"en", "Merge"}, {"zh-Hans", "合并"}, {"zh-Hant", "合併"}} {
		if got := nativeDialogDisplayText(tc.locale, operation); got != tc.want {
			t.Fatalf("%s operation label = %q", tc.locale, got)
		}
		if nativeDialogDisplayText(tc.locale, branch) != "Merge" || nativeDialogValueText(operation) != "Merge" {
			t.Fatal("display localization changed Git data or the wire value")
		}
	}
}

func TestWorktreeDialogKeepsPathChooserWithItsInput(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			popup := dialogFixture()
			popup.Type, popup.TitleKey = "AddWorktree", "repositoryDialog.AddWorktree"
			popup.Fields = []backend.KernelDialogField{
				{ID: "repositoryName", LabelKey: "repositoryDialog.repositoryName", Kind: "readonly", Text: "Fixture"},
				{ID: "repositoryPath", LabelKey: "repositoryDialog.repositoryPath", Kind: "readonly", Text: "C:/projects/fixture"},
				{ID: "name", LabelKey: "repositoryDialog.worktreeName", Kind: "text", Value: "review"},
				{ID: "path", LabelKey: "repositoryDialog.path", Kind: "text", Value: "C:/projects"},
				{ID: "branch", LabelKey: "repositoryDialog.branch", Kind: "text", Value: "review"},
				{ID: "branches", LabelKey: "repositoryDialog.branches", Kind: "list", Choices: []backend.KernelDialogChoice{{ID: "main", Label: "main"}, {ID: "topic", Label: "topic"}}},
			}
			popup.Actions = []backend.KernelDialogAction{
				{ID: "choose", LabelKey: "repositoryDialog.choose", Fields: []string{"name", "path", "branch"}},
				{ID: "confirm", LabelKey: "repositoryDialog.createWorktree", Primary: true},
				{ID: "cancel", LabelKey: "repositoryDialog.cancel"},
			}
			var replies []backend.KernelPopupReply
			var closes []int
			tt := dialogTester(popup, &nativeDialogForm{}, locale, 1180, 900, &replies, &closes)
			for _, size := range [][2]int{{1180, 900}, {900, 620}} {
				tt.SetSize(size[0], size[1])
				tt.Frame()
				tt.Advance(time.Second)
				path, _ := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel(locale, "repositoryDialog.path"))
				choose, _ := tt.AccessByRole(ui.RoleButton, nativeDialogLabel(locale, "repositoryDialog.choose"))
				if choose.Bounds.X < path.Bounds.X+path.Bounds.W || choose.Bounds.Y != path.Bounds.Y {
					t.Fatalf("detached chooser: path=%+v choose=%+v", path.Bounds, choose.Bounds)
				}
				name, _ := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel(locale, "repositoryDialog.worktreeName"))
				branch, _ := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel(locale, "repositoryDialog.branch"))
				if name.Bounds.Y != branch.Bounds.Y {
					t.Fatalf("paired inputs lost alignment when labels wrap: name=%+v branch=%+v", name.Bounds, branch.Bounds)
				}
				for _, field := range popup.Fields {
					visibleControl(t, tt, nativeDialogLabel(locale, field.LabelKey), size[0], size[1])
				}
			}
			if err := tt.Click(nativeDialogLabel(locale, "repositoryDialog.choose")); err != nil {
				t.Fatal(err)
			}
			if len(replies) != 1 || replies[0].Action != "choose" || replies[0].Values["path"] != "C:/projects" {
				t.Fatalf("chooser did not retain original reply: %+v", replies)
			}
		})
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestProviderNavigationPreservesNonSecretDraft(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.generationConfig = &backend.GenerationSettings{Providers: []backend.Provider{{ProviderSettings: backend.ProviderSettings{ID: "existing", Name: "Original", Format: "openai-responses", AuthKind: "apiKey"}}}}
	for _, id := range []string{"", "existing"} {
		a.editProvider(id)
		a.providerEdit.Name, a.providerEdit.BaseURL, a.providerEdit.secret = "Edited name", "https://example.invalid/v1", "unsent-secret"
		a.closeSettings()
		a.editProvider(id)
		if a.providerEdit.Name != "Edited name" || a.providerEdit.BaseURL != "https://example.invalid/v1" || a.providerEdit.secret != "" {
			t.Fatalf("navigation lost non-secret draft or retained a credential: id=%q", id)
		}
	}
}

func TestModelRowsKeepScaledTextSeparated(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	models := []backend.ProviderModel{{ID: "first-model-id", Name: "First model", Available: true}, {ID: "second-model-id", Name: "Second model", Available: true}}
	var list ui.ListState
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() { a.modelCatalogView(c, locales.In(c.Locale()), models, &list, func(string, bool) {}) })
	}, 900, 620)
	tt.SetLocale("en")
	tt.SetPreferences(ui.Preferences{TextScale: 1.5})
	tt.Advance(time.Second)
	first, one := tt.AccessByRole(ui.RoleText, "first-model-id")
	second, two := tt.AccessByRole(ui.RoleText, "Second model")
	if !one || !two || second.Bounds.Y-first.Bounds.Y-first.Bounds.H < 8 {
		t.Fatalf("scaled model rows overlap: %+v / %+v", first, second)
	}
}

// The list's overlay scrollbar sits on its right edge; trailing status text
// must stay clear of it so the last word is never painted over.
func TestModelStatusStaysClearOfTheScrollbar(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	models := make([]backend.ProviderModel, 40)
	for i := range models {
		models[i] = backend.ProviderModel{ID: fmt.Sprintf("model-%02d", i), Name: fmt.Sprintf("Model %02d", i), Available: true}
	}
	var list ui.ListState
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() { a.modelCatalogView(c, locales.In(c.Locale()), models, &list, func(string, bool) {}) })
	}, 900, 620)
	tt.SetLocale("en")
	tt.Advance(time.Second)
	status, ok := tt.AccessByRole(ui.RoleText, "Not enabled")
	if !ok {
		t.Fatal("model status is missing")
	}
	if right := status.Bounds.X + status.Bounds.W; float32(900)-right < 12 {
		t.Fatalf("status reaches the scrollbar lane: ends at %v of 900", right)
	}
}

func TestProviderSaveAcknowledgesOnlySubmittedDraftAfterNavigation(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, newer := range []bool{false, true} {
			t.Run(fmt.Sprintf("existing=%v/newer=%v", existing, newer), func(t *testing.T) {
				h := newGenerationHarness(t)
				settings := generationFixture(false)
				h.configure(t, settings)
				id := ""
				if existing {
					id = settings.Providers[1].ID
				}
				h.a.editProvider(id)
				h.a.providerEdit.Name, h.a.providerEdit.BaseURL, h.a.providerEdit.AuthKind = " Submitted source ", " http://localhost:11434 ", "none"
				h.a.saveProviderSettings()
				r := h.generationRequest(t, "providers.save")
				h.a.clearProviderEditor()
				if newer {
					h.a.editProvider(id)
					h.a.providerEdit.Name = "Newer edit"
					h.a.clearProviderEditor()
				}
				saved := backend.Provider{ProviderSettings: r.provider}
				if !existing {
					saved.ID = "created-source"
					settings.Providers = append(settings.Providers, saved)
				} else {
					settings.Providers[1] = saved
				}
				r.reply <- readReply{value: settings}
				h.apply(t)
				h.a.editProvider(id)
				want := ""
				if existing {
					want = "Submitted source"
				}
				if newer {
					want = "Newer edit"
				}
				if h.a.providerEdit.Name != want {
					t.Fatalf("save receipt restored wrong draft: got %q, want %q", h.a.providerEdit.Name, want)
				}
			})
		}
	}
}

type generationUIRequest struct {
	method                    string
	ctx                       context.Context
	provider                  backend.ProviderSettings
	secret                    *string
	providerID, modelID, name string
	accountID, flowID, input  string
	language                  string
	selection                 *backend.ModelSelection
	enabled, consent          bool
	id                        int
	paths                     []string
	reply                     chan readReply
}

// This double only supplies labelled configuration/UI-response fixtures. It
// never implements an SDK, auth, storage or successful Git commit. Live source
// connectivity and OAuth completion cannot be established by these tests.
type generationUIBackend struct {
	*controlledBackend
	calls chan *generationUIRequest
}

var _ backend.GenerationBackend = (*generationUIBackend)(nil)

func generationUIResult[T any](b *generationUIBackend, r *generationUIRequest) (T, error) {
	var zero T
	r.reply = make(chan readReply, 1)
	select {
	case b.calls <- r:
	case <-b.stop:
		return zero, context.Canceled
	}
	// Intentionally allow a late reply after cancellation to exercise the
	// production model's project/model/input revision checks.
	select {
	case reply := <-r.reply:
		if reply.err != nil {
			return zero, reply.err
		}
		return reply.value.(T), nil
	case <-b.stop:
		return zero, context.Canceled
	}
}

func (b *generationUIBackend) GenerationSettings(ctx context.Context) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "generation.settings", ctx: ctx})
}
func (b *generationUIBackend) SaveProvider(ctx context.Context, p backend.ProviderSettings, secret *string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "providers.save", ctx: ctx, provider: p, secret: secret})
}
func (b *generationUIBackend) RenameProvider(ctx context.Context, id, name string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "providers.rename", ctx: ctx, providerID: id, name: name})
}
func (b *generationUIBackend) DeleteProvider(ctx context.Context, id string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "providers.delete", ctx: ctx, providerID: id})
}
func (b *generationUIBackend) RefreshProviderModels(ctx context.Context, id string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "providers.models.refresh", ctx: ctx, providerID: id})
}
func (b *generationUIBackend) SetProviderModelEnabled(ctx context.Context, id, model string, enabled bool) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "providers.models.setEnabled", ctx: ctx, providerID: id, modelID: model, enabled: enabled})
}
func (b *generationUIBackend) AddProviderModel(ctx context.Context, id, model, name string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "providers.models.add", ctx: ctx, providerID: id, modelID: model, name: name})
}
func (b *generationUIBackend) SetGenerationModel(ctx context.Context, selection *backend.ModelSelection) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "generation.select", ctx: ctx, selection: selection})
}
func (b *generationUIBackend) SetGenerationLanguage(ctx context.Context, value string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "generation.language", ctx: ctx, language: value})
}
func (b *generationUIBackend) GenerateMessage(ctx context.Context, id int, paths []string, consent bool) (backend.GeneratedMessage, error) {
	return generationUIResult[backend.GeneratedMessage](b, &generationUIRequest{method: "repository.generateMessage", ctx: ctx, id: id, paths: paths, consent: consent})
}
func (b *generationUIBackend) CopilotStatus(ctx context.Context) (backend.CopilotStatus, error) {
	return generationUIResult[backend.CopilotStatus](b, &generationUIRequest{method: "copilot.status", ctx: ctx})
}
func (b *generationUIBackend) CopilotAccounts(ctx context.Context) ([]backend.CopilotAccount, error) {
	return generationUIResult[[]backend.CopilotAccount](b, &generationUIRequest{method: "copilot.accounts", ctx: ctx})
}
func (b *generationUIBackend) BeginCopilotLogin(ctx context.Context) (backend.CopilotLoginFlow, error) {
	return generationUIResult[backend.CopilotLoginFlow](b, &generationUIRequest{method: "copilot.login.begin", ctx: ctx})
}
func (b *generationUIBackend) AdvanceCopilotLogin(ctx context.Context, id, input string) (backend.CopilotLoginStep, error) {
	return generationUIResult[backend.CopilotLoginStep](b, &generationUIRequest{method: "copilot.login.advance", ctx: ctx, flowID: id, input: input})
}
func (b *generationUIBackend) CancelCopilotLogin(ctx context.Context, id string) error {
	_, err := generationUIResult[struct{}](b, &generationUIRequest{method: "copilot.login.cancel", ctx: ctx, flowID: id})
	return err
}
func (b *generationUIBackend) LogoutCopilot(ctx context.Context, id string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "copilot.logout", ctx: ctx, accountID: id})
}
func (b *generationUIBackend) RefreshCopilotModels(ctx context.Context, id string) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "copilot.models.refresh", ctx: ctx, accountID: id})
}
func (b *generationUIBackend) SetCopilotModelEnabled(ctx context.Context, id, model string, enabled bool) (backend.GenerationSettings, error) {
	return generationUIResult[backend.GenerationSettings](b, &generationUIRequest{method: "copilot.models.setEnabled", ctx: ctx, accountID: id, modelID: model, enabled: enabled})
}

type generationHarness struct {
	*viewHarness
	g       *generationUIBackend
	pending []*generationUIRequest
}

func newGenerationHarness(t *testing.T) *generationHarness {
	t.Helper()
	b := &controlledBackend{requests: make(chan *readRequest, 32), stop: make(chan struct{})}
	g := &generationUIBackend{controlledBackend: b, calls: make(chan *generationUIRequest, 32)}
	a := newApp(g, nil)
	updates := make(chan func(), 128)
	a.update = func(fn func()) { updates <- fn }
	h := &generationHarness{viewHarness: &viewHarness{a: a, b: b, updates: updates}, g: g}
	h.tt = ui.NewTester(a.view, 1180, 760)
	h.tt.SetLocale("en")
	t.Cleanup(func() { a.close(); close(b.stop) })
	return h
}

func (h *generationHarness) generationRequest(t *testing.T, method string) *generationUIRequest {
	t.Helper()
	for i, request := range h.pending {
		if request.method == method {
			h.pending = append(h.pending[:i], h.pending[i+1:]...)
			return request
		}
	}
	for {
		request := waitEvent(t, h.g.calls)
		if request.method == method {
			return request
		}
		h.pending = append(h.pending, request)
	}
}

func (h *generationHarness) generationAnswer(t *testing.T, method string, value any, err error) *generationUIRequest {
	t.Helper()
	r := h.generationRequest(t, method)
	r.reply <- readReply{value: value, err: err}
	h.apply(t)
	return r
}

func (h *generationHarness) configure(t *testing.T, settings backend.GenerationSettings) {
	t.Helper()
	h.a.connect("")
	h.answer(t, "info", backend.Info{Protocol: 1, Version: "ui-fixture", Capabilities: []string{"repository.commit", "repository.generateMessage"}}, nil)
	h.generationAnswer(t, "generation.settings", settings, nil)
}

func generationFixture(selected bool) backend.GenerationSettings {
	first := backend.ModelSelection{Kind: "custom", ProviderID: "fixture-provider-a", ModelID: "enabled"}
	second := backend.ModelSelection{Kind: "custom", ProviderID: "fixture-provider-b", ModelID: "other"}
	settings := backend.GenerationSettings{
		Providers: []backend.Provider{
			{ProviderSettings: backend.ProviderSettings{ID: first.ProviderID, Name: "Fixture source A", Format: "openai-responses", BaseURL: "https://source-a.example.invalid/v1", AuthKind: "apiKey", RequestTimeoutSeconds: 60},
				Models: []backend.ProviderModel{{ID: "enabled", Name: "Enabled fixture model", Enabled: true, Available: true}, {ID: "unchecked", Name: "Unchecked fixture model", Available: true}, {ID: "expired", Name: "Expired fixture model", Enabled: true}}},
			{ProviderSettings: backend.ProviderSettings{ID: second.ProviderID, Name: "Fixture source B", Format: "ollama", BaseURL: "http://localhost:11434", AuthKind: "none", RequestTimeoutSeconds: 60},
				Models: []backend.ProviderModel{{ID: "other", Name: "Other fixture model", Enabled: true, Available: true}}},
		},
		CopilotCatalogs: []backend.CopilotCatalog{},
		Models:          []backend.ModelOption{{Selection: first, SourceName: "Fixture source A", ModelName: "Enabled fixture model"}, {Selection: second, SourceName: "Fixture source B", ModelName: "Other fixture model"}},
		Language:        "en",
	}
	if selected {
		settings.Selection = &first
	}
	return settings
}

func copyGenerationFixture(t *testing.T, settings backend.GenerationSettings) backend.GenerationSettings {
	t.Helper()
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var copy backend.GenerationSettings
	if err = json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func (h *generationHarness) draft(t *testing.T) {
	t.Helper()
	h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}, {Path: "other.txt", Status: "Modified"}})
	h.answer(t, "diff", patch("first.txt", "before", "after"), nil)
	h.click(t, "Include first.txt")
	typeDraft(t, h.viewHarness, locales.In("en"), "Original draft", "Original description")
}

func (h *generationHarness) noGenerationCalls(t *testing.T) {
	t.Helper()
	if len(h.pending) != 0 {
		t.Fatalf("unexpected pending generation calls: %+v", h.pending)
	}
	select {
	case r := <-h.g.calls:
		t.Fatalf("unexpected generation/settings call: %s", r.method)
	default:
	}
}

func TestGenerationRequiresHandshakeAndOptionalBackend(t *testing.T) {
	h := newGenerationHarness(t)
	enableCommitCapability(t, h.viewHarness)
	h.draft(t)
	h.click(t, "Generate message")
	if h.a.generationCapable || h.a.generating || !h.tt.HasText("This service does not support message generation yet.") {
		t.Fatal("unadvertised generation was presented as available")
	}
	h.noGenerationCalls(t)
	readOnly := newViewHarness(t)
	readOnly.a.connect("")
	readOnly.answer(t, "info", backend.Info{Protocol: 1, Version: "ui-fixture", Capabilities: []string{"repository.commit", "repository.generateMessage"}}, nil)
	if readOnly.a.generationCapable {
		t.Fatal("the old repository double was forced to masquerade as a generation backend")
	}
}

func TestGenerationServiceConsentLocksInputsAndStopDiscardsLateReply(t *testing.T) {
	h := newGenerationHarness(t)
	settings := generationFixture(true)
	h.configure(t, settings)
	h.draft(t)
	h.tt.SetSize(900, 620)
	h.click(t, "Generate message")
	r := h.generationRequest(t, "repository.generateMessage")
	if r.consent || r.id != 1 || !reflect.DeepEqual(r.paths, []string{"first.txt"}) || !h.a.generating || h.a.canSaveVersion() {
		t.Fatalf("unchecked authorization/selection or unlocked write: %+v", r)
	}
	h.click(t, "Version summary")
	h.tt.Type("must not edit while generating")
	h.click(t, "Include other.txt")
	h.click(t, "Save version")
	if h.a.commitSummary != "Original draft" || h.a.included["other.txt"] || h.a.savingVersion {
		t.Fatal("generation did not lock draft, selection and Save")
	}
	r.reply <- readReply{err: &backend.RPCError{Code: -32020, Message: "Confirm sending selected changes"}}
	h.apply(t)
	if h.a.generating || !h.a.generationConsentOpen || !h.tt.HasText("Fixture source A") || !h.tt.HasText("Only fills the draft. Does not commit or push.") {
		t.Fatal("unauthorized service receipt did not open an honest native confirmation")
	}
	h.click(t, "Cancel")
	h.noGenerationCalls(t)
	h.click(t, "Generate message")
	h.generationAnswer(t, "repository.generateMessage", nil, &backend.RPCError{Code: -32020, Message: "Confirm sending selected changes"})
	h.click(t, "Send and generate")
	r = h.generationRequest(t, "repository.generateMessage")
	if !r.consent || r.ctx.Err() != nil {
		t.Fatal("only explicit confirmation may send consent:true")
	}
	visibleControl(t, h.tt, "Stop generating", 900, 620)
	visibleControl(t, h.tt, "Save version", 900, 620)
	h.click(t, "Stop generating")
	if r.ctx.Err() == nil || h.a.generating || !h.a.canSaveVersion() {
		t.Fatal("Stop did not cancel the request and unlock the original draft")
	}
	r.reply <- readReply{value: backend.GeneratedMessage{Title: "Late UI fixture reply", Selection: *settings.Selection, Language: "en"}}
	h.apply(t)
	if h.a.commitSummary != "Original draft" || h.a.commitDescription != "Original description" || !h.tt.HasText(locales.In("en").GenerationStopped()) {
		t.Fatal("a cancelled late reply replaced the draft")
	}
	h.noGenerationCalls(t)
}

func TestAuthorizedUIReplyFixtureOnlyFillsReviewableDraft(t *testing.T) {
	h := newGenerationHarness(t)
	settings := generationFixture(true)
	h.configure(t, settings)
	h.draft(t)
	h.click(t, "Generate message")
	r := h.generationAnswer(t, "repository.generateMessage", backend.GeneratedMessage{
		Title: "UI reply fixture title", Description: "UI reply fixture context", Selection: *settings.Selection, Language: "en",
	}, nil)
	if r.consent || h.a.generationConsentOpen || h.a.commitSummary != "UI reply fixture title" || h.a.commitDescription != "UI reply fixture context" || !h.a.descriptionOpen || !h.a.included["first.txt"] {
		t.Fatal("an already authorized source did not fill only the UI draft")
	}
	if h.a.savingVersion || h.a.lastSavedSHA != "" {
		t.Fatal("applying a draft fixture manufactured a Git commit")
	}
	select {
	case request := <-h.b.requests:
		t.Fatalf("generation initiated a repository action: %s", request.method)
	default:
	}
	h.click(t, "Generate message")
	next := h.generationRequest(t, "repository.generateMessage")
	if next.consent {
		t.Fatal("a subsequent one-click request must use service-owned persistent authorization")
	}
}

func TestGenerationRepliesCannotCrossProjectModelOrEditedDraft(t *testing.T) {
	for _, kind := range []string{"project", "model", "draft", "language", "destination"} {
		t.Run(kind, func(t *testing.T) {
			h := newGenerationHarness(t)
			settings := generationFixture(true)
			h.configure(t, settings)
			h.draft(t)
			h.click(t, "Generate message")
			old := h.generationRequest(t, "repository.generateMessage")
			expected := "Original draft"
			switch kind {
			case "project":
				h.a.openRepository("C:/repos/second")
				h.answer(t, "open", backend.Repository{ID: 2, Path: "C:/repos/second", Name: "second"}, nil)
				h.answer(t, "status", backend.Status{Files: []backend.File{}}, nil)
				expected = ""
			case "model":
				selection := settings.Models[1].Selection
				h.a.chooseGenerationModel(&selection)
				receipt := copyGenerationFixture(t, settings)
				receipt.Selection = &selection
				r := h.generationAnswer(t, "generation.select", receipt, nil)
				if *r.selection != selection || old.ctx.Err() == nil {
					t.Fatal("model change did not use the exact receipt identity/cancel the old request")
				}
			case "draft":
				// A UI-owned draft mutation can be queued alongside a reply.
				// Compare input bytes as well as explicit edit revisions.
				h.a.commitSummary = "Edited elsewhere on the UI thread"
				expected = h.a.commitSummary
			case "language":
				receipt := copyGenerationFixture(t, settings)
				receipt.Language = "zh-Hans"
				h.a.generationConfig = &receipt
			case "destination":
				receipt := copyGenerationFixture(t, settings)
				receipt.Providers[0].BaseURL = "https://different.example.invalid/v1"
				h.a.generationConfig = &receipt
			}
			old.reply <- readReply{value: backend.GeneratedMessage{Title: "Stale UI fixture", Selection: *settings.Selection, Language: "en"}}
			h.apply(t)
			if h.a.commitSummary != expected || h.a.generating {
				t.Fatal("a stale result overwrote another project/model/input draft")
			}
		})
	}
}

func TestGenerationRevokedModelRefreshesSettingsWithoutFallbackOrRetry(t *testing.T) {
	h := newGenerationHarness(t)
	settings := generationFixture(true)
	h.configure(t, settings)
	h.draft(t)
	h.click(t, "Generate message")
	h.generationAnswer(t, "repository.generateMessage", nil, &backend.RPCError{Code: -32022, Message: "Choose an enabled model"})
	receipt := copyGenerationFixture(t, settings)
	receipt.Providers[0].Models[0].Available = false
	receipt.Selection, receipt.Models = nil, receipt.Models[1:]
	h.generationAnswer(t, "generation.settings", receipt, nil)
	if h.a.generationConfig.Selection != nil || len(generationOptions(h.a.generationConfig)) != 1 || h.a.commitSummary != "Original draft" || h.a.generating {
		t.Fatal("revoked model did not preserve the draft and require reselection")
	}
	h.noGenerationCalls(t)
}

func TestGenerationFailureDetailsAreCopyableAndBoundedAtMinimumSize(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(tag, func(t *testing.T) {
			h := newGenerationHarness(t)
			h.configure(t, generationFixture(true))
			h.draft(t)
			h.tt.SetLocale(tag)
			h.tt.SetSize(900, 620)
			tx := locales.In(tag)
			h.click(t, tx.GenerateMessage())
			message := "Safe provider diagnostic\n" + strings.Repeat("Safe operation detail\n", 500)
			h.generationAnswer(t, "repository.generateMessage", nil, errors.New(message))
			if !h.tt.HasText(tx.GenerationFailed()) || !h.tt.HasText(tx.GenerationNoRetry()) || h.a.commitSummary != "Original draft" || h.a.commitDescription != "Original description" {
				t.Fatal("failure lost the draft, mixed languages or omitted retry guidance")
			}
			h.click(t, tx.GenerationErrorDetails())
			h.tt.Advance(250 * time.Millisecond)
			r := visibleControl(t, h.tt, tx.GenerationFullErrorDetails(), 900, 620)
			if r.H != 64 {
				t.Fatal("generation diagnostics are not bounded")
			}
			visibleControl(t, h.tt, tx.GenerateMessage(), 900, 620)
			visibleControl(t, h.tt, tx.SaveVersion(), 900, 620)
			h.click(t, tx.GenerationFullErrorDetails())
			h.tt.Key(ui.Cmd, ui.KeyA)
			h.tt.Key(ui.Cmd, ui.KeyC)
			if h.tt.Clipboard() != message {
				t.Fatal("the complete safe diagnostic is not selectable/copyable")
			}
			h.noGenerationCalls(t)
		})
	}
}

func TestGenerationOutputLanguageIsIndependentAndValidated(t *testing.T) {
	h := newGenerationHarness(t)
	settings := generationFixture(true)
	h.configure(t, settings)
	h.draft(t)
	h.tt.SetLocale("zh-Hans")
	var interfaceChanges []string
	h.a.setLocale = func(value string) { interfaceChanges = append(interfaceChanges, value) }
	tx := locales.In("zh-Hans")
	h.click(t, "English")
	h.click(t, tx.CustomGenerationLanguage())
	h.click(t, tx.GenerationLanguageTag())
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("fr-ca")
	h.click(t, tx.ApplyLanguage())
	receipt := copyGenerationFixture(t, settings)
	receipt.Language = "fr-CA"
	r := h.generationAnswer(t, "generation.language", receipt, nil)
	if r.language != "fr-CA" || h.a.generationConfig.Language != "fr-CA" || len(interfaceChanges) != 0 || !h.tt.HasText(tx.SaveVersion()) || h.tt.HasText("Save version") {
		t.Fatal("output language altered the interface or ignored the service receipt")
	}
	h.click(t, tx.GenerationLanguageTag())
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("en_US")
	h.click(t, tx.ApplyLanguage())
	if !h.tt.HasText(tx.GenerationLanguageInvalid()) || h.a.generationConfig.Language != "fr-CA" {
		t.Fatal("invalid BCP47 input changed the selected output language")
	}
	h.noGenerationCalls(t)
}

func TestGenerationBCP47Tags(t *testing.T) {
	for input, expected := range map[string]string{"zh-Hans": "zh-Hans", "en": "en", "fr-ca": "fr-CA", "ja-JP": "ja-JP", "en-US-u-hc-h23": "en-US-u-hc-h23"} {
		if actual, err := canonicalGenerationLanguage(input); err != nil || actual != expected {
			t.Errorf("%q: %q / %v", input, actual, err)
		}
	}
	for _, invalid := range []string{"", "en_US", "en--US", "123", "en\nUS", "English language"} {
		if _, err := canonicalGenerationLanguage(invalid); err == nil {
			t.Errorf("accepted invalid BCP47 tag %q", invalid)
		}
	}
}

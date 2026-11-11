package main

import (
	"encoding/json"
	"errors"
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

// Callbacks are just a record of boundary decisions. No service, account network,
// Git executable, credential store or global Git config is used by these tests.
type onboardingDecision struct {
	action, step, name, email string
	useAccount                bool
	id                        int
}

type onboardingHarness struct {
	tt        *ui.Tester
	state     *backend.KernelOnboarding
	signIn    *backend.KernelSignIn
	form      kernelOnboardingForm
	decisions []onboardingDecision
}

func newOnboardingHarness(locale string, state *backend.KernelOnboarding, signIn *backend.KernelSignIn) *onboardingHarness {
	h := &onboardingHarness{state: state, signIn: signIn}
	h.tt = ui.NewTester(func(c *ui.Context) {
		renderKernelOnboarding(c, locales.In(c.Locale()), h.state, h.signIn, &h.form, kernelOnboardingActions{
			Advance: func(step string) {
				h.decisions = append(h.decisions, onboardingDecision{action: "advance", step: step})
			},
			Configure: func(name, email string, useAccount bool) {
				h.decisions = append(h.decisions, onboardingDecision{action: "configure", name: name, email: email, useAccount: useAccount})
			},
			DeleteLock: func() { h.decisions = append(h.decisions, onboardingDecision{action: "deleteLock"}) },
			Endpoint: func(value string) {
				h.decisions = append(h.decisions, onboardingDecision{action: "endpoint", email: value})
			},
			Browser:     func() { h.decisions = append(h.decisions, onboardingDecision{action: "browser"}) },
			ResetSignIn: func() { h.decisions = append(h.decisions, onboardingDecision{action: "resetSignIn"}) },
		})
	}, 900, 620)
	h.tt.SetLocale(locale)
	h.tt.Advance(time.Second)
	return h
}

func onboardingClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func onboardingInput(t *testing.T, tt *ui.Tester, label, value string) {
	t.Helper()
	onboardingClick(t, tt, label)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(value)
}

func onboardingPointer(value string) *string { return &value }

func TestKernelOnboardingStartUsesOriginalStepsWithoutDuplicateBrowserRequest(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, choice := range []string{"skip", "dotcom", "enterprise"} {
			t.Run(locale+"/"+choice, func(t *testing.T) {
				h := newOnboardingHarness(locale, &backend.KernelOnboarding{Step: "Start"}, nil)
				tx := locales.In(locale)
				label, step := tx.OnboardingSkip(), "ConfigureGit"
				switch choice {
				case "dotcom":
					label, step = tx.OnboardingDotcom(), "SignInToDotComWithBrowser"
				case "enterprise":
					label, step = tx.OnboardingEnterprise(), "SignInToEnterprise"
				}
				if len(h.decisions) != 0 || !h.tt.HasText(tx.OnboardingTitle()) {
					t.Fatal("render dispatched an action or failed to show localized Start")
				}
				if _, ok := h.tt.AccessByRole(ui.RoleDialog, ""); ok {
					t.Fatal("first-run setup was placed in a modal")
				}
				onboardingClick(t, h.tt, label)
				if len(h.decisions) != 1 || h.decisions[0] != (onboardingDecision{action: "advance", step: step}) {
					t.Fatalf("wrong core step or duplicate browser authentication: %+v", h.decisions)
				}
				if h.form.step != "Start" || !h.tt.HasText(tx.OnboardingTitle()) || !h.form.busy {
					t.Fatal("the view advanced without a core DTO")
				}
				// An RPC reply is not navigation; the parent clears UI status and
				// then renders the snapshot. Only the new DTO changes the page.
				h.form.busy = false
				h.state = &backend.KernelOnboarding{Step: step}
				h.tt.Frame()
				if h.form.step != step || h.form.busy {
					t.Fatal("core step did not reset temporary request status")
				}
			})
		}
	}
}

func TestKernelOnboardingConfigureKeepsManualDraftAndEmptyHelperSemantics(t *testing.T) {
	tx := locales.In("zh-Hans")
	state := &backend.KernelOnboarding{Step: "ConfigureGit", GlobalName: onboardingPointer("Existing author"), GlobalEmail: onboardingPointer("old@example.invalid")}
	h := newOnboardingHarness("zh-Hans", state, nil)
	before, _ := json.Marshal(state)
	onboardingInput(t, h.tt, tx.OnboardingName(), "Reviewed author")
	onboardingInput(t, h.tt, tx.OnboardingEmail(), "reviewed@example.invalid")
	onboardingClick(t, h.tt, tx.OnboardingFinish())
	want := onboardingDecision{action: "configure", name: "Reviewed author", email: "reviewed@example.invalid"}
	if len(h.decisions) != 1 || h.decisions[0] != want || h.form.step != "ConfigureGit" {
		t.Fatalf("author input did not reach the original helper boundary: %+v", h.decisions)
	}
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Fatal("manual fields mutated the global Git DTO")
	}
	// A successful same-step reply clears busy in the parent, not by guessing
	// from Loading. Blank input must remain blank and remain submittable.
	h.form.busy = false
	h.tt.Frame()
	for _, label := range []string{tx.OnboardingName(), tx.OnboardingEmail()} {
		onboardingClick(t, h.tt, label)
		h.tt.Key(ui.Cmd, ui.KeyA)
		h.tt.Key(0, ui.KeyBackspace)
	}
	finish, _ := h.tt.AccessByRole(ui.RoleButton, tx.OnboardingFinish())
	if finish.Disabled || h.form.manualName != "" || h.form.manualEmail != "" {
		t.Fatal("empty manual input was blocked or refilled from unchanged defaults")
	}
	onboardingClick(t, h.tt, tx.OnboardingFinish())
	if len(h.decisions) != 2 || h.decisions[1] != (onboardingDecision{action: "configure"}) {
		t.Fatalf("empty fields did not preserve the upstream conditional-write contract: %+v", h.decisions)
	}
}

func TestKernelOnboardingAccountProjectionIsReadOnlyAndEmailSelectable(t *testing.T) {
	for _, enterprise := range []bool{false, true} {
		t.Run(map[bool]string{false: "dotcom", true: "enterprise"}[enterprise], func(t *testing.T) {
			tx := locales.In("en")
			account := &backend.KernelOnboardingAccount{Name: "Account author", PreferredEmail: "private@example.invalid", Emails: []string{"public@example.invalid", "private@example.invalid"}, Enterprise: enterprise}
			state := &backend.KernelOnboarding{Step: "ConfigureGit", Account: account}
			h := newOnboardingHarness("en", state, nil)
			before, _ := json.Marshal(state)
			field, ok := h.tt.AccessByRole(ui.RoleTextField, tx.OnboardingName())
			if !ok || !field.ReadOnly || field.Value != account.Name || !h.form.useAccount || h.form.accountEmail != account.PreferredEmail {
				t.Fatalf("account name or preferred email was not projected correctly: %+v", field)
			}
			onboardingInput(t, h.tt, tx.OnboardingName(), "not-an-account-rename")
			onboardingClick(t, h.tt, tx.OnboardingEmail())
			h.tt.Key(0, ui.KeySpace) // a field-label click focuses, but does not open, the native select
			onboardingClick(t, h.tt, "public@example.invalid")
			onboardingClick(t, h.tt, tx.OnboardingFinish())
			if len(h.decisions) != 1 || h.decisions[0] != (onboardingDecision{action: "configure", name: account.Name, email: account.Emails[0], useAccount: true}) {
				t.Fatalf("the view invented account identity instead of using the selected DTO email: %+v", h.decisions)
			}
			after, _ := json.Marshal(state)
			if string(before) != string(after) {
				t.Fatal("account projection was mutated by input")
			}
			h.form.busy = false
			h.tt.Frame()
			onboardingClick(t, h.tt, tx.OnboardingManual())
			field, _ = h.tt.AccessByRole(ui.RoleTextField, tx.OnboardingName())
			if field.ReadOnly || field.Value != account.Name {
				t.Fatal("manual radio did not expose the original manual defaults")
			}
			onboardingInput(t, h.tt, tx.OnboardingName(), "Manual author")
			onboardingInput(t, h.tt, tx.OnboardingEmail(), "manual@example.invalid")
			if !h.tt.HasText(tx.OnboardingEmailWarning()) {
				t.Fatal("manual email missing from account lost its warning")
			}
			// A refreshed account may reset the account selection but cannot
			// overwrite an already entered manual name/email.
			h.state.Account = &backend.KernelOnboardingAccount{Name: "New account", PreferredEmail: "new@example.invalid", Emails: []string{"new@example.invalid"}}
			h.tt.Frame()
			if !h.form.useAccount || h.form.accountEmail != "new@example.invalid" || h.form.manualName != "Manual author" || h.form.manualEmail != "manual@example.invalid" {
				t.Fatal("account refresh retained an invalid selection or overwrote the manual draft")
			}
		})
	}
}

func TestKernelOnboardingLockDeletionRequiresExplicitConsentAndSendsNoPath(t *testing.T) {
	tx := locales.In("zh-Hans")
	state := &backend.KernelOnboarding{Step: "ConfigureGit", LockFilePath: onboardingPointer("fixture/config.lock")}
	h := newOnboardingHarness("zh-Hans", state, nil)
	deletion, ok := h.tt.AccessByRole(ui.RoleButton, tx.OnboardingDeleteLock())
	if !ok || !deletion.Disabled || len(h.decisions) != 0 {
		t.Fatal("a lock was deleted or deletion enabled without explicit consent")
	}
	onboardingClick(t, h.tt, tx.OnboardingLockConfirm())
	deletion, _ = h.tt.AccessByRole(ui.RoleButton, tx.OnboardingDeleteLock())
	if deletion.Disabled {
		t.Fatal("explicit confirmation did not enable deletion")
	}
	onboardingClick(t, h.tt, tx.OnboardingDeleteLock())
	if len(h.decisions) != 1 || h.decisions[0] != (onboardingDecision{action: "deleteLock"}) || h.form.confirmLockDeletion || !h.form.busy {
		t.Fatalf("deletion included a path, retried Finish, or retained consent: %+v", h.decisions)
	}
	if state.LockFilePath == nil || !h.tt.HasText(tx.OnboardingLockNotice()) {
		t.Fatal("the view hid the kernel's lock optimistically")
	}
	h.form.busy, h.form.err = false, errors.New("fixture unlink failed")
	h.tt.Frame()
	if !h.tt.HasText(tx.OnboardingRequestFailed()) {
		t.Fatal("RPC failure was hidden")
	}
	// Consent applies only to the current lock projection, not another path.
	onboardingClick(t, h.tt, tx.OnboardingLockConfirm())
	h.state.LockFilePath = onboardingPointer("fixture/other-config.lock")
	h.tt.Frame()
	if h.form.confirmLockDeletion {
		t.Fatal("new core lock retained consent for the old lock")
	}
	h.state.LockFilePath = nil
	h.form.err = nil
	h.tt.Frame()
	if h.tt.HasText(tx.OnboardingLockNotice()) || len(h.decisions) != 1 {
		t.Fatal("kernel lock resolution triggered an automatic write or remained visible")
	}
}

func TestKernelOnboardingCancelAndCoreStepReset(t *testing.T) {
	tx := locales.In("en")
	h := newOnboardingHarness("en", &backend.KernelOnboarding{Step: "SignInToDotComWithBrowser"}, &backend.KernelSignIn{Kind: "Authentication", Loading: true})
	onboardingClick(t, h.tt, tx.Cancel())
	if len(h.decisions) != 1 || h.decisions[0] != (onboardingDecision{action: "advance", step: "Start"}) {
		t.Fatalf("Welcome cancel must only advance to Start, never reset late OAuth state: %+v", h.decisions)
	}
	h.state = &backend.KernelOnboarding{Step: "Start"}
	h.signIn = &backend.KernelSignIn{Kind: "Success"} // a late original OAuth result
	h.tt.Frame()
	if len(h.decisions) != 1 || !h.tt.HasText(tx.OnboardingTitle()) {
		t.Fatal("the view advanced or cleared SignIn on a late result after cancellation")
	}
	h.state, h.signIn = &backend.KernelOnboarding{Step: "ConfigureGit"}, nil
	h.tt.Frame()
	onboardingInput(t, h.tt, tx.OnboardingName(), "Discard on core step replacement")
	onboardingClick(t, h.tt, tx.Cancel())
	if h.decisions[1] != (onboardingDecision{action: "advance", step: "Start"}) {
		t.Fatal("Configure cancel did not request Start")
	}
	h.state = &backend.KernelOnboarding{Step: "Start"}
	h.tt.Frame()
	h.state = &backend.KernelOnboarding{Step: "ConfigureGit"}
	h.tt.Frame()
	onboardingClick(t, h.tt, tx.OnboardingName())
	h.tt.Key(ui.Cmd, ui.KeyZ)
	if h.form.manualName != "" || h.form.busy || h.form.err != nil {
		t.Fatal("a new core step restored old input / undo history / request status")
	}
	h.state = nil
	h.tt.Frame()
	if h.tt.HasText(tx.OnboardingConfigureTitle()) || h.form.step != "" {
		t.Fatal("endWelcome DTO did not remove/reset the full page")
	}
}

type signInHarness struct {
	tt        *ui.Tester
	id        int
	state     *backend.KernelSignIn
	form      kernelSignInForm
	decisions []onboardingDecision
}

func newSignInHarness(locale string, state *backend.KernelSignIn) *signInHarness {
	h := &signInHarness{id: 23, state: state}
	h.tt = ui.NewTester(func(c *ui.Context) {
		// A real native control behind the dialog makes focus protection
		// observable rather than merely asserting that we called DialogBase.
		ui.Button(c.Key("behind"), "behind-signin").Size(200, 44)
		renderKernelSignInDialog(c, locales.In(c.Locale()), h.id, h.state, &h.form,
			func(value string) {
				h.decisions = append(h.decisions, onboardingDecision{action: "endpoint", email: value})
			},
			func() { h.decisions = append(h.decisions, onboardingDecision{action: "browser"}) },
			func(id int) { h.decisions = append(h.decisions, onboardingDecision{action: "cancel", id: id}) })
	}, 900, 620)
	h.tt.SetLocale(locale)
	h.tt.Advance(time.Second)
	return h
}

func TestKernelSignInEndpointLoadingCanCancelAndRetainsCoreAuthority(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			tx := locales.In(locale)
			h := newSignInHarness(locale, &backend.KernelSignIn{Kind: "EndpointEntry"})
			if _, found := h.tt.AccessByRole(ui.RoleScroll, ""); found {
				t.Fatal("sign-in parameters still sit in a scrolling page")
			}
			if !h.tt.Focused(tx.SignInEndpoint()) || !h.tt.HasText(tx.SignInEndpointHint()) {
				t.Fatal("endpoint step did not focus/show its localized native field")
			}
			button, _ := h.tt.AccessByRole(ui.RoleButton, tx.SignInContinue())
			if !button.Disabled {
				t.Fatal("an empty endpoint is submittable")
			}
			onboardingInput(t, h.tt, tx.SignInEndpoint(), "https://ghes.example.invalid:8443")
			onboardingClick(t, h.tt, tx.SignInContinue())
			if len(h.decisions) != 1 || h.decisions[0] != (onboardingDecision{action: "endpoint", email: "https://ghes.example.invalid:8443"}) || h.form.kind != "EndpointEntry" {
				t.Fatalf("endpoint was normalized or advanced locally: %+v", h.decisions)
			}
			h.form.busy = false // parent's RPC completion
			h.state.Loading = true
			h.tt.Frame()
			button, _ = h.tt.AccessByRole(ui.RoleButton, tx.SignInContinue())
			cancel, _ := h.tt.AccessByRole(ui.RoleButton, tx.Cancel())
			if !button.Disabled || cancel.Disabled || !h.tt.HasText(tx.OnboardingWorking()) {
				t.Fatal("loading did not disable submit while preserving Cancel")
			}
			onboardingClick(t, h.tt, tx.Cancel())
			if len(h.decisions) != 2 || h.decisions[1] != (onboardingDecision{action: "cancel", id: h.id}) || !h.tt.HasText(tx.SignInEndpoint()) {
				t.Fatal("loading dismissal did not send only the popup ID or hid before core state")
			}
		})
	}
}

func TestKernelSignInBrowserWarningExpectedLoginAndSuccessAreCoreDriven(t *testing.T) {
	tx := locales.In("zh-Hans")
	state := &backend.KernelSignIn{Kind: "Authentication", Endpoint: onboardingPointer("https://ghes.example.invalid:8443/api/v3"), ExpectedLogin: onboardingPointer("fixture-login"), UnrecognizedEnterpriseServer: true}
	h := newSignInHarness("zh-Hans", state)
	before, _ := json.Marshal(state)
	if !h.tt.HasText(tx.SignInUnrecognizedServer()) || !h.tt.HasText(tx.SignInExpectedLogin("fixture-login")) || !h.tt.Focused(tx.SignInBrowser()) {
		t.Fatal("unrecognized-server warning / expected-login / browser focus is missing")
	}
	if _, ok := h.tt.AccessByRole(ui.RoleAlertDialog, tx.SignInBrowserTitle()); !ok {
		t.Fatal("unrecognized GHES did not protect focus as an alert dialog")
	}
	if _, ok := h.tt.AccessByRole(ui.RoleTextField, tx.PopupPassword()); ok {
		t.Fatal("GitHub sign-in requested a password")
	}
	for i := 0; i < 6; i++ {
		h.tt.Key(0, ui.KeyTab)
		if h.tt.Focused("behind-signin") {
			t.Fatal("Tab escaped the SignIn dialog")
		}
	}
	onboardingClick(t, h.tt, tx.SignInBrowser())
	if len(h.decisions) != 1 || h.decisions[0] != (onboardingDecision{action: "browser"}) || len(h.tt.OpenedURLs()) != 0 {
		t.Fatal("the view opened an OAuth URL or sent something other than the original browser action")
	}
	onboardingClick(t, h.tt, tx.Cancel()) // pending browser RPC still cancelable
	if len(h.decisions) != 2 || h.decisions[1].action != "cancel" {
		t.Fatal("pending browser authentication blocked cancellation")
	}
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Fatal("the view mutated SignInStore DTO")
	}
	h.state = &backend.KernelSignIn{Kind: "Success"}
	h.tt.Frame()
	if h.tt.HasText(tx.SignInBrowser()) || len(h.decisions) != 2 || h.form.busy || h.form.err != nil {
		t.Fatal("Success sent a pretend close/advance action or left the sign-in dialog visible")
	}
}

func TestKernelSignInStepAndPopupIDResetDraftAndFocus(t *testing.T) {
	tx := locales.In("en")
	h := newSignInHarness("en", &backend.KernelSignIn{Kind: "EndpointEntry"})
	onboardingInput(t, h.tt, tx.SignInEndpoint(), "https://old.example.invalid")
	h.form.busy, h.form.err = true, errors.New("old reply")
	h.id++
	h.tt.Frame()
	if h.form.endpoint != "" || h.form.busy || h.form.err != nil || !h.tt.Focused(tx.SignInEndpoint()) {
		t.Fatal("new core popup ID retained the old endpoint/status/focus")
	}
	onboardingInput(t, h.tt, tx.SignInEndpoint(), "https://enterprise.example.invalid")
	h.tt.Key(0, ui.KeyEnter)
	if len(h.decisions) != 1 || h.decisions[0].action != "endpoint" {
		t.Fatal("Enter from the native endpoint input did not submit")
	}
	h.state = &backend.KernelSignIn{Kind: "Authentication", Endpoint: onboardingPointer("https://enterprise.example.invalid/api/v3")}
	h.tt.Frame()
	if h.form.endpoint != "" || h.form.busy || !h.tt.Focused(tx.SignInBrowser()) {
		t.Fatal("core SignIn step replacement did not reset draft/status/focus")
	}
	h.tt.Key(0, ui.KeyEscape)
	if len(h.decisions) != 2 || h.decisions[1] != (onboardingDecision{action: "cancel", id: h.id}) {
		t.Fatal("Escape did not use the original popup dismissal")
	}
}

func TestKernelOnboardingLateDefaultsAndSignInErrors(t *testing.T) {
	tx := locales.In("en")
	h := newOnboardingHarness("en", &backend.KernelOnboarding{Step: "ConfigureGit", Loading: true}, nil)
	h.state = &backend.KernelOnboarding{Step: "ConfigureGit", GlobalName: onboardingPointer("Late author"), GlobalEmail: onboardingPointer("late@example.invalid")}
	h.tt.Frame()
	if h.form.manualName != "Late author" || h.form.manualEmail != "late@example.invalid" {
		t.Fatal("late core global defaults did not populate the empty draft")
	}
	s := newSignInHarness("en", &backend.KernelSignIn{Kind: "EndpointEntry"})
	onboardingInput(t, s.tt, tx.SignInEndpoint(), "invalid-server-input")
	s.state.Error = onboardingPointer("fixture validation diagnostic")
	s.tt.Frame()
	field, _ := s.tt.AccessByRole(ui.RoleTextField, tx.SignInEndpoint())
	if field.Value != "invalid-server-input" || !s.tt.HasText(tx.SignInFailed()) {
		t.Fatal("same-step core error lost input or its recovery message")
	}
	s.form.err = errors.New("fixture transport diagnostic")
	s.tt.Frame()
	if !s.tt.HasText(tx.OnboardingRequestFailed()) || len(s.decisions) != 0 {
		t.Fatal("transport error was hidden or retried automatically")
	}
}

func TestKernelWelcomeEnterpriseEntryAndBrowserUseOnlyOriginalActions(t *testing.T) {
	tx := locales.In("zh-Hans")
	h := newOnboardingHarness("zh-Hans", &backend.KernelOnboarding{Step: "SignInToEnterprise"}, &backend.KernelSignIn{Kind: "EndpointEntry"})
	onboardingInput(t, h.tt, tx.SignInEndpoint(), "https://enterprise.example.invalid")
	h.tt.Key(0, ui.KeyEnter)
	if len(h.decisions) != 1 || h.decisions[0] != (onboardingDecision{action: "endpoint", email: "https://enterprise.example.invalid"}) {
		t.Fatalf("Welcome endpoint did not use the original SignIn action: %+v", h.decisions)
	}
	h.signIn = &backend.KernelSignIn{Kind: "Authentication", Endpoint: onboardingPointer("https://enterprise.example.invalid/api/v3")}
	h.tt.Frame()
	onboardingClick(t, h.tt, tx.SignInBrowser())
	if len(h.decisions) != 2 || h.decisions[1] != (onboardingDecision{action: "browser"}) || len(h.tt.OpenedURLs()) != 0 {
		t.Fatal("Welcome did not forward a browser-only action to the original store")
	}
	onboardingClick(t, h.tt, tx.Cancel())
	if len(h.decisions) != 3 || h.decisions[2] != (onboardingDecision{action: "advance", step: "Start"}) {
		t.Fatal("Enterprise cancel reset the original store instead of only advancing Start")
	}
	button, _ := h.tt.AccessByRole(ui.RoleButton, tx.SignInBrowser())
	if !button.Disabled {
		t.Fatal("a pending Welcome navigation left browser authentication actionable")
	}
	// Success is not a local Configure action. The original presenter decides
	// whether an OAuth result advances this wizard or arrives after cancel.
	h.signIn = &backend.KernelSignIn{Kind: "Success"}
	h.tt.Frame()
	if len(h.decisions) != 3 || h.form.step != "SignInToEnterprise" {
		t.Fatal("Success advanced the wizard in the view")
	}
	h.state = &backend.KernelOnboarding{Step: "ConfigureGit"}
	h.tt.Frame()
	if !h.tt.HasText(tx.OnboardingConfigureTitle()) || h.form.signIn.busy || h.form.busy {
		t.Fatal("the original Configure DTO did not replace the sign-in view/status")
	}
}

func TestKernelSignInLongDiagnosticsRemainKeyboardReachableAndCopyable(t *testing.T) {
	tx := locales.In("en")
	diagnostic := strings.Repeat("Original fixture diagnostic line\n", 80)
	h := newSignInHarness("en", &backend.KernelSignIn{Kind: "Authentication", Endpoint: onboardingPointer("https://enterprise.example.invalid/api/v3"), UnrecognizedEnterpriseServer: true, Error: &diagnostic})
	for i := 0; i < 8; i++ {
		field, ok := h.tt.AccessByRole(ui.RoleTextField, tx.FullErrorDetails())
		if ok && field.Focused {
			break
		}
		h.tt.Key(0, ui.KeyTab)
	}
	field, ok := h.tt.AccessByRole(ui.RoleTextField, tx.FullErrorDetails())
	if !ok || !field.Focused || !field.ReadOnly || field.Value != diagnostic || field.Bounds.H <= 0 {
		t.Fatalf("bounded diagnostics are not keyboard-reachable intact: %+v", field)
	}
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Command("copy")
	if h.tt.Clipboard() != diagnostic || len(h.decisions) != 0 {
		t.Fatal("copying a long original diagnostic lost content or sent an action")
	}
}

// Opt-in production-view raster evidence uses only published DTO fixtures.
// Artifacts never go into locales or the index; no second/demo renderer exists.
func onboardingSnapshot(t *testing.T, tt *ui.Tester, name, locale string, dark bool) {
	t.Helper()
	if os.Getenv("GITOWL_ONBOARDING_SNAPSHOTS") != "1" {
		return
	}
	out := filepath.Join("..", "..", "_temp", "kernel-reuse", "onboarding")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	tt.Move(-10, -10)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	f, err := os.Create(filepath.Join(out, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, tt.Image())
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("snapshot encode=%v close=%v", err, closeErr)
	}
	data, _ := json.MarshalIndent(map[string]any{"renderer": "MyGo ui.Tester, production onboarding_view.go", "input": "backend.KernelOnboarding / KernelSignIn fixture; no network or Git", "locale": locale, "dark": dark, "widthDIP": 900, "heightDIP": 620, "state": name}, "", "  ")
	if err := os.WriteFile(filepath.Join(out, name+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKernelOnboardingMinimumWindowLocalesAndThemes(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			t.Run(locale+map[bool]string{false: "/light", true: "/dark"}[dark], func(t *testing.T) {
				tx := locales.In(locale)
				cases := []struct {
					name   string
					state  *backend.KernelOnboarding
					signIn *backend.KernelSignIn
					labels []string
				}{
					{"start", &backend.KernelOnboarding{Step: "Start"}, nil, []string{tx.OnboardingDotcom(), tx.OnboardingEnterprise(), tx.OnboardingSkip(), tx.OnboardingTerms()}},
					{"configure", &backend.KernelOnboarding{Step: "ConfigureGit", Account: &backend.KernelOnboardingAccount{Name: "Fixture account", PreferredEmail: "fixture@example.invalid", Emails: []string{"fixture@example.invalid"}}}, nil, []string{tx.OnboardingName(), tx.OnboardingEmail(), tx.OnboardingAuthorPreview(), tx.OnboardingFinish(), tx.Cancel()}},
					{"lock", &backend.KernelOnboarding{Step: "ConfigureGit", LockFilePath: onboardingPointer("fixture/config.lock")}, nil, []string{tx.OnboardingLockConfirm(), tx.OnboardingDeleteLock(), tx.OnboardingFinish(), tx.Cancel()}},
					{"enterprise", &backend.KernelOnboarding{Step: "SignInToEnterprise"}, &backend.KernelSignIn{Kind: "EndpointEntry"}, []string{tx.SignInEndpoint(), tx.SignInContinue(), tx.Cancel()}},
					{"browser", &backend.KernelOnboarding{Step: "SignInToDotComWithBrowser"}, &backend.KernelSignIn{Kind: "Authentication", Loading: true}, []string{tx.SignInBrowser(), tx.Cancel()}},
				}
				for _, test := range cases {
					h := newOnboardingHarness(locale, test.state, test.signIn)
					h.tt.SetDark(dark)
					for _, label := range test.labels {
						r, ok := h.tt.Find(label)
						if !ok || r.W <= 0 || r.H <= 0 || r.Y+r.H > 620 || r.X+r.W > 900 {
							t.Fatalf("%s control %q is clipped at 900x620: %+v", test.name, label, r)
						}
					}
					if strings.HasPrefix(locale, "zh") && h.tt.HasText(locales.In("en").OnboardingSkip()) {
						t.Fatal("English UI leaked into Chinese setup")
					}
					onboardingSnapshot(t, h.tt, test.name+"-"+locale+map[bool]string{false: "-light", true: "-dark"}[dark], locale, dark)
				}
				long := newSignInHarness(locale, &backend.KernelSignIn{Kind: "Authentication", Endpoint: onboardingPointer("https://ghes.example.invalid:8443/api/v3"), ExpectedLogin: onboardingPointer(strings.Repeat("fixture-user-", 10)), UnrecognizedEnterpriseServer: true, Error: onboardingPointer(strings.Repeat("Original diagnostic details\n", 40))})
				long.tt.SetDark(dark)
				for _, label := range []string{tx.SignInBrowser(), tx.Cancel()} {
					button, ok := long.tt.AccessByRole(ui.RoleButton, label)
					if !ok || button.Bounds.H < 40 || button.Bounds.Y+button.Bounds.H > 620 {
						t.Fatalf("long dialog diagnostic pushed decision %q out of view: %+v", label, button)
					}
				}
				onboardingSnapshot(t, long.tt, "signin-warning-"+locale+map[bool]string{false: "-light", true: "-dark"}[dark], locale, dark)
			})
		}
	}
}

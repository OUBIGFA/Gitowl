package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

var (
	errCopilotAuthorizationURL = errors.New("The SDK did not return an official HTTPS github.com authorization URL. No browser was opened.")
	errCopilotSecureStorage    = errors.New("Copilot sign-in was cancelled because secure credential storage is unavailable. Gitowl does not allow plaintext token storage.")
	errCopilotInput            = errors.New("This sign-in step is not supported. Complete GitHub authentication in the browser; do not enter a password or token in Gitowl.")
	errCopilotLoginStep        = errors.New("The SDK returned an unsupported sign-in state.")
	errCopilotBrowser          = errors.New("Could not open the GitHub authorization page in the default browser.")
)

type copilotLoginState struct {
	copilotStatus     *backend.CopilotStatus
	copilotAccounts   []backend.CopilotAccount
	copilotLoading    bool
	copilotErr        error
	copilotErrorOpen  bool
	copilotLoadSeq    uint64
	copilotLoadCancel context.CancelFunc

	loginFlowID    string
	loginStep      *backend.CopilotLoginStep
	loginBusy      bool
	loginErr       error
	loginErrorOpen bool
	loginNotice    string
	loginSeq       uint64
	loginContext   context.Context
	loginCancel    context.CancelFunc
	loginNeedsPoll bool
	loginPollAt    time.Time
	loginOpenedURL string
	openExternal   func(string) error
}

// The URL is SDK-owned, but opening it is a native trust boundary. Never open a
// generic HTTPS URL, lookalike host, arbitrary GitHub path or embedded userinfo.
func officialCopilotAuthorizationURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "github.com") &&
		u.User == nil && u.Opaque == "" && u.Fragment == "" && u.RawPath == "" &&
		(u.Path == "/login/oauth/authorize" || u.Path == "/login/device")
}

func (a *app) loadCopilotAccounts() {
	if a.core != nil {
		return
	}
	if !a.ready || !a.generationCapable || a.copilotLoading {
		return
	}
	a.copilotLoadSeq++
	seq := a.copilotLoadSeq
	a.copilotLoading, a.copilotErr = true, nil
	ctx, cancel := context.WithCancel(a.ctx)
	a.copilotLoadCancel = cancel
	b, update := a.generation, a.update
	go func() {
		defer cancel()
		status, err := b.CopilotStatus(ctx)
		var accounts []backend.CopilotAccount
		if err == nil {
			accounts, err = b.CopilotAccounts(ctx)
		}
		update(func() {
			if a.closed || seq != a.copilotLoadSeq {
				return
			}
			a.copilotLoading, a.copilotLoadCancel, a.copilotErr = false, nil, err
			if err == nil {
				a.copilotStatus, a.copilotAccounts = &status, accounts
				if a.copilotAccountID != "" {
					present := false
					for _, account := range accounts {
						present = present || account.SelectionID == a.copilotAccountID
					}
					if !present {
						a.copilotAccountID, a.copilotModelsView = "", false
					}
				}
			}
		})
	}()
}

func (a *app) beginCopilotLogin() {
	if a.core != nil {
		core, ctx := a.core, a.ctx
		a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.SignIn(ctx, nil) })
		return
	} // Keep the research login gateway out of production kernel launches.
	if a.configurationLocked() || a.copilotLoading || a.loginBusy || a.loginFlowID != "" {
		return
	}
	a.loginSeq++
	seq := a.loginSeq
	a.loginBusy, a.loginErr, a.loginNotice, a.loginOpenedURL = true, nil, "", ""
	a.loginContext, a.loginCancel = context.WithCancel(a.ctx)
	ctx, b, update := a.loginContext, a.generation, a.update
	go func() {
		flow, err := b.BeginCopilotLogin(ctx)
		update(func() {
			if a.closed || seq != a.loginSeq {
				if flow.FlowID != "" {
					a.discardCopilotLogin(flow.FlowID)
				}
				return
			}
			a.loginBusy, a.loginErr = false, err
			if err != nil {
				a.finishCopilotLogin()
				return
			}
			if flow.FlowID == "" {
				a.loginErr = errCopilotLoginStep
				a.finishCopilotLogin()
				return
			}
			a.loginFlowID = flow.FlowID
			a.acceptCopilotLoginStep(flow.Step)
		})
	}()
}

func (a *app) openCopilotAuthorization() {
	if a.loginStep == nil || a.loginStep.Kind != "open-url" || a.loginBusy || a.loginFlowID == "" {
		return
	}
	value := a.loginStep.URL
	if !officialCopilotAuthorizationURL(value) {
		a.loginErr = errCopilotAuthorizationURL
		a.cancelCopilotLogin()
		return
	}
	if a.openExternal == nil {
		a.loginErr = errCopilotBrowser
		return
	}
	a.loginBusy, a.loginErr = true, nil
	seq, open, update := a.loginSeq, a.openExternal, a.update
	go func() {
		err := open(value)
		update(func() {
			if a.closed || seq != a.loginSeq {
				return
			}
			a.loginBusy = false
			if err != nil {
				// A shell error can include the full OAuth URL. Keep a fixed
				// diagnostic rather than copying URL/state into error details.
				a.loginErr = errCopilotBrowser
				return
			}
			a.loginOpenedURL = value
			a.advanceCopilotLogin("")
		})
	}()
}

// Account choice is restricted to the SDK's pending roster, not free-form
// input or a default/ambient account.
func (a *app) selectCopilotLoginAccount(id string) {
	if a.loginStep == nil || a.loginStep.Kind != "completed" || a.loginStep.Result == nil || a.loginStep.Result.Status != "needs-account-selection" {
		return
	}
	for _, account := range a.loginStep.Result.Accounts {
		if account.SelectionID == id {
			a.advanceCopilotLogin(id)
			return
		}
	}
}

func (a *app) advanceCopilotLogin(input string) {
	if !a.ready || a.loginFlowID == "" || a.loginBusy {
		return
	}
	a.loginBusy, a.loginNeedsPoll = true, false
	seq, id, ctx, b, update := a.loginSeq, a.loginFlowID, a.loginContext, a.generation, a.update
	go func() {
		step, err := b.AdvanceCopilotLogin(ctx, id, input)
		update(func() {
			if a.closed || seq != a.loginSeq {
				return
			}
			a.loginBusy, a.loginErr = false, err
			if err != nil {
				a.cancelCopilotLogin()
				return
			}
			a.acceptCopilotLoginStep(step)
		})
	}()
}

func (a *app) acceptCopilotLoginStep(step backend.CopilotLoginStep) {
	a.loginStep, a.loginPollAt, a.loginNeedsPoll = &step, time.Time{}, false
	switch step.Kind {
	case "open-url":
		if !officialCopilotAuthorizationURL(step.URL) {
			// Do not retain an unsafe URL in any visible/copyable component.
			a.loginErr = errCopilotAuthorizationURL
			a.cancelCopilotLogin()
		} else if a.loginOpenedURL == step.URL {
			a.loginNeedsPoll = true
		} else {
			a.openCopilotAuthorization()
		}
	case "awaiting", "needs-interaction":
		a.loginNeedsPoll = true
	case "input-required":
		// The official github.com flow needs no credentials in the UI. A
		// generic SDK prompt must not turn into a password/token collector.
		a.loginErr = errCopilotInput
		a.cancelCopilotLogin()
	case "completed":
		if step.Result == nil {
			a.loginErr = errCopilotLoginStep
			a.cancelCopilotLogin()
			return
		}
		switch step.Result.Status {
		case "needs-plaintext-consent":
			a.loginErr = errCopilotSecureStorage
			a.cancelCopilotLogin() // Refuse, never advance with an approval.
		case "needs-account-selection":
			// Wait for an exact SDK-returned selectionId; never pick index 0.
			if len(step.Result.Accounts) == 0 {
				a.loginErr = errCopilotLoginStep
				a.cancelCopilotLogin()
			}
		case "completed":
			a.finishCopilotLogin()
			a.loginNotice = "completed"
			a.loadGenerationSettings()
			a.loadCopilotAccounts()
		case "declined":
			a.finishCopilotLogin()
			a.loginNotice = "cancelled"
		default:
			a.loginErr = errCopilotLoginStep
			a.cancelCopilotLogin()
		}
	case "error":
		a.loginErr = errors.New(step.Message)
		a.cancelCopilotLogin()
	default:
		a.loginErr = errCopilotLoginStep
		a.cancelCopilotLogin()
	}
}

func (a *app) finishCopilotLogin() {
	if a.loginCancel != nil {
		a.loginCancel()
	}
	a.loginFlowID, a.loginBusy, a.loginNeedsPoll, a.loginCancel = "", false, false, nil
}

func (a *app) discardCopilotLogin(id string) {
	b, update := a.generation, a.update
	go func() {
		// A late begin may allocate a flow after its waiter was cancelled.
		// Explicitly retire that SDK resource, even though its UI is gone.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := b.CancelCopilotLogin(ctx, id)
		update(func() {
			if !a.closed && err != nil {
				a.copilotErr = err
			}
		})
	}()
}

func (a *app) cancelCopilotLogin() {
	if a.loginFlowID == "" && !a.loginBusy {
		return
	}
	id := a.loginFlowID
	a.loginSeq++
	seq := a.loginSeq
	a.finishCopilotLogin()
	a.loginStep, a.loginNotice = nil, "cancelled"
	if id == "" || !a.ready {
		return
	}
	b, update := a.generation, a.update
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		err := b.CancelCopilotLogin(ctx, id)
		update(func() {
			if !a.closed && seq == a.loginSeq && err != nil {
				a.loginErr = errors.Join(a.loginErr, err)
			}
		})
	}()
}

func (a *app) pollCopilotLogin(c *ui.Context) {
	if a.core != nil {
		return
	}
	if !a.settingsModal || a.settingsPage != settingsCopilot || !a.loginNeedsPoll || a.loginBusy || a.loginFlowID == "" {
		return
	}
	if a.loginPollAt.IsZero() {
		a.loginPollAt = c.Now().Add(time.Second)
	}
	if remaining := a.loginPollAt.Sub(c.Now()); remaining > 0 {
		c.After(remaining)
	} else {
		a.advanceCopilotLogin("")
	}
}

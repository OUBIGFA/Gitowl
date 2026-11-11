package main

import (
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// RPC replies unlock only the form that initiated them. Original snapshot
// changes own navigation and sign-in success; no native account is created.
func (a *app) runOnboardingRequest(call func() (backend.KernelResult, error)) {
	welcomeSession, signInSession := a.coreOnboardingForm.session, a.coreSignInForm.session
	embeddedSession := a.coreOnboardingForm.signIn.session
	a.runCoreTask(call, func(_ backend.KernelResult, err error) {
		if a.coreOnboardingForm.session == welcomeSession {
			a.coreOnboardingForm.busy, a.coreOnboardingForm.canceling, a.coreOnboardingForm.err = false, false, err
		}
		if a.coreOnboardingForm.signIn.session == embeddedSession {
			a.coreOnboardingForm.signIn.busy, a.coreOnboardingForm.signIn.canceling, a.coreOnboardingForm.signIn.err = false, false, err
		}
		if a.coreSignInForm.session == signInSession {
			a.coreSignInForm.busy, a.coreSignInForm.canceling, a.coreSignInForm.err = false, false, err
		}
	})
}

func (a *app) onboardingActions() kernelOnboardingActions {
	core, ctx := a.core, a.ctx
	return kernelOnboardingActions{
		Advance: func(step string) {
			a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.AdvanceWelcome(ctx, step) })
		},
		Configure: func(name, email string, useAccount bool) {
			a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.ConfigureWelcomeAuthor(ctx, name, email, useAccount) })
		},
		DeleteLock: func() {
			a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.UnlockWelcomeGitConfig(ctx) })
		},
		Endpoint:    a.setCoreSignInEndpoint,
		Browser:     a.authenticateCoreUsingBrowser,
		ResetSignIn: func() { a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.ResetSignIn(ctx) }) },
	}
}

func (a *app) setCoreSignInEndpoint(endpoint string) {
	core, ctx := a.core, a.ctx
	a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.SetSignInEndpoint(ctx, endpoint) })
}

func (a *app) authenticateCoreUsingBrowser() {
	core, ctx := a.core, a.ctx
	a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.AuthenticateUsingBrowser(ctx) })
}

func (a *app) cancelCoreSignInDialog(id int) {
	core, ctx := a.core, a.ctx
	a.runOnboardingRequest(func() (backend.KernelResult, error) { return core.ClosePopup(ctx, id) })
}

func (a *app) coreAuthenticationView(c *ui.Context, tx locales.Messages) {
	if a.coreSnapshot == nil {
		return
	}
	popup := a.coreSnapshot.CurrentPopup
	if popup != nil && popup.Type == "SignIn" && popup.ID != nil {
		renderKernelSignInDialog(c, tx, *popup.ID, a.coreSnapshot.SignIn, &a.coreSignInForm, a.setCoreSignInEndpoint, a.authenticateCoreUsingBrowser, a.cancelCoreSignInDialog)
		return
	}
	if popup != nil && popup.Layout == "form" && popup.ID != nil {
		form := a.nativeDialogFormFor(popup)
		form.settingsWorkArea, form.expandSettings = a.settingsWorkArea, a.expandSettings
		if popup.Type == "Preferences" {
			form.appearanceTop = func(c *ui.Context) { a.interfaceLanguageView(c, tx) }
			form.appearanceEnd = func(c *ui.Context) { a.interfaceVersionView(c, tx) }
			form.appearanceMeasureKey = fmt.Sprint(a.windowPreferenceErr)
			form.sections = []nativeDialogSection{
				{id: "providers", label: tx.CustomProviders(), measureKey: a.providerMeasureVariant(), measure: a.providerMeasurements(tx), render: func(c *ui.Context) { a.providerSectionView(c, tx) }},
			}
			form.onRemoved = a.clearProviderEditor
			if a.coreSettingsSection != "" {
				if a.coreSettingsSection == "appearance" {
					form.section, form.requestedTab = "", "appearance"
				} else {
					form.section = a.coreSettingsSection
				}
				a.coreSettingsSection = ""
			}
		}
		renderNativeDialog(c, tx, popup, form, a.replyCorePopup, a.closeCorePopup)
		if popup.Type == "Preferences" {
			// Provider confirmations cover the settings shell, never sit behind it.
			a.providerDeleteDialog(c, tx)
			a.manualModelDialog(c, tx)
		}
		return
	}
	renderKernelPopup(c, tx, popup, &a.corePopupForm, a.replyCorePopup, a.closeCorePopup)
}

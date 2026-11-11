package main

import (
	"fmt"
	"slices"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// kernelOnboardingActions forwards explicit decisions to the original Welcome
// presenter / Dispatcher. Advance(dotcom) already starts browser authentication;
// Browser is only a subsequent SignIn request. No Git or account lives here.
type kernelOnboardingActions struct {
	Advance     func(step string)
	Configure   func(name, email string, useAccount bool)
	DeleteLock  func()
	Endpoint    func(string)
	Browser     func()
	ResetSignIn func()
}

// Forms own drafts and UI request status only. The parent clears busy/canceling
// and sets err after each RPC, including successful replies with the same DTO
// step. A new core step resets the draft; never advance on an optimistic reply.
type kernelOnboardingForm struct {
	step                            string
	session                         uint64
	manualName, manualEmail         string
	accountEmail, accountKey        string
	globalName, globalEmail         string // last input defaults, not Git configuration
	useAccount, confirmLockDeletion bool
	lockKey                         string // invalidates consent when the presented lock changes
	busy, canceling                 bool
	err                             error
	signIn                          kernelSignInForm
}

type kernelSignInForm struct {
	popupID         int
	kind            string
	session         uint64
	endpoint        string
	busy, canceling bool
	err             error
}

// renderKernelOnboarding is a full-page first-run view. Core snapshots alone
// determine its presence and step; it cannot end Welcome, persist identity, or
// acquire credentials. The root entry should render this instead of workbench.
func renderKernelOnboarding(c *ui.Context, tx locales.Messages, state *backend.KernelOnboarding, signIn *backend.KernelSignIn, form *kernelOnboardingForm, actions kernelOnboardingActions) {
	if state == nil {
		*form = kernelOnboardingForm{session: form.session + 1}
		return
	}
	if form.step != state.Step {
		*form = kernelOnboardingForm{step: state.Step, session: form.session + 1}
	}
	if state.Step == "ConfigureGit" {
		form.syncAuthorDefaults(state)
	}
	colors := setWorkspaceTheme(c)
	width, _ := c.Size()
	padding := float32(40)
	if width < 1100 {
		padding = 24
	}
	advance := func(step string) {
		if form.busy || state.Loading || actions.Advance == nil {
			return
		}
		form.busy, form.err = true, nil
		actions.Advance(step)
	}
	cancel := func() {
		if form.canceling || actions.Advance == nil {
			return
		}
		form.busy, form.canceling, form.err = true, true, nil
		// Welcome's original cancellation only changes the wizard step. Do
		// not reset SignInStore: a late OAuth result still belongs to the core.
		actions.Advance("Start")
	}
	ui.Row(c.Key("kernel-onboarding")).Fill().AlignItems(ui.Stretch).FontWeight(400).Children(func() {
		ui.Column(c.Key("setup-rail")).Width(projectRailWidth).FillHeight().Shrink(0).Background(colors.rail).Children(func() {
			bar := c.TitleBar()
			ui.Row(c).Height(max(48, bar.Height)).Padding(0, 20, 0, 20+bar.Left).DragWindow().Children(func() {
				ui.Text(c, tx.AppName()).FontSize(c.Theme().Rem(17.0 / 15)).FontWeight(500)
			})
			ui.Column(c).Padding(32, 20).Gap(16).Label(tx.OnboardingSteps()).Children(func() {
				for i, label := range []string{tx.OnboardingAccountStep(), tx.OnboardingAuthorStep()} {
					text := ui.Text(c, label).TextColor(c.Theme().TextMuted)
					if (i == 1) == (state.Step == "ConfigureGit") {
						text.TextColor(c.Theme().Text).FontWeight(500).Current(ui.CurrentStep)
					}
				}
			})
			ui.Spacer(c)
			ui.Text(c, tx.OnboardingLocalHint()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).Padding(24)
		})
		ui.Column(c.Key(form.session)).Grow(1).MinWidth(0).FillHeight().Children(func() {
			bar := c.TitleBar()
			ui.Row(c).Height(max(48, bar.Height)).Shrink(0).Padding(0, bar.Right+padding, 0, padding).DragWindow()
			ui.Column(c).Grow(1).MinHeight(0).FillWidth().MaxWidth(880).AlignSelf(ui.Center).Padding(24, padding).Gap(20).Children(func() {
				switch state.Step {
				case "Start":
					onboardingHeading(c, tx.OnboardingTitle())
					ui.Scroll(c.Key("start-body")).Grow(1).MinHeight(0).FillWidth().Gap(24).Children(func() {
						ui.Text(c, tx.OnboardingIntro()).TextColor(c.Theme().TextMuted).Shrink(0)
						ui.Row(c).Gap(12).Wrap().FillWidth().Shrink(0).Children(func() {
							ui.PrimaryButton(c.Key("dotcom"), tx.OnboardingDotcom()).Height(44).Padding(8, 16).FontWeight(500).AutoFocus().
								Disabled(form.busy || state.Loading || actions.Advance == nil).OnClick(func() { advance("SignInToDotComWithBrowser") })
							ui.Button(c.Key("enterprise"), tx.OnboardingEnterprise()).Height(44).Padding(8, 16).FontWeight(500).
								Disabled(form.busy || state.Loading || actions.Advance == nil).OnClick(func() { advance("SignInToEnterprise") })
						})
						ui.Text(c, tx.OnboardingBrowserOnly()).Shrink(0)
						ui.Link(c, tx.OnboardingCreateAccount(), "https://github.com/join?source=github-desktop").AlignSelf(ui.Start)
						ui.Button(c.Key("skip"), tx.OnboardingSkip()).Height(40).Padding(8, 16).AlignSelf(ui.Start).
							Disabled(form.busy || state.Loading || actions.Advance == nil).OnClick(func() { advance("ConfigureGit") })
						onboardingRequestStatus(c, tx, state.Loading || form.busy, form.err)
					})
					ui.Column(c.Key("setup-footer")).Gap(8).Shrink(0).BorderWidth(1, 0, 0, 0).BorderColor(colors.hairline).Padding(16, 0, 0, 0).Children(func() {
						ui.Text(c, tx.OnboardingTermsNotice()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
						ui.Row(c).Gap(16).Wrap().Children(func() {
							ui.Link(c, tx.OnboardingTerms(), "https://github.com/site/terms").FontSize(c.Theme().Rem(14.0 / 15))
							ui.Link(c, tx.OnboardingPrivacy(), "https://github.com/site/privacy").FontSize(c.Theme().Rem(14.0 / 15))
							ui.Link(c, tx.OnboardingMetrics(), "https://github.com/desktop/desktop/blob/development/docs/process/usage-data.md").FontSize(c.Theme().Rem(14.0 / 15))
						})
					})
				case "ConfigureGit":
					onboardingHeading(c, tx.OnboardingConfigureTitle())
					onboardingAuthor(c, tx, state, form, actions, cancel)
				case "SignInToDotComWithBrowser", "SignInToEnterprise":
					title := tx.SignInBrowserTitle()
					if state.Step == "SignInToEnterprise" {
						title = tx.OnboardingEnterpriseTitle()
					}
					onboardingHeading(c, title)
					syncKernelSignInForm(&form.signIn, 0, signIn)
					endpoint, browser := actions.Endpoint, actions.Browser
					if state.Loading || form.busy {
						endpoint, browser = nil, nil
					}
					ui.Scroll(c.Key("signin-body")).Grow(1).MinHeight(0).FillWidth().Gap(20).KeepScrollbars(true).Padding(0, 8, 0, 0).Children(func() {
						kernelSignInBody(c, tx, signIn, &form.signIn, endpoint)
						onboardingRequestStatus(c, tx, state.Loading || form.busy, form.err)
					})
					kernelSignInButtons(c, tx, signIn, &form.signIn, endpoint, browser, cancel, form.canceling || actions.Advance == nil)
				default:
					onboardingHeading(c, tx.KernelFrontendIncomplete())
					ui.Text(c, tx.OnboardingUnknownStep()).Role(ui.RoleAlert)
					onboardingValue(c, tx.OnboardingSteps(), state.Step)
				}
			})
		})
	})
}

func (form *kernelOnboardingForm) syncAuthorDefaults(state *backend.KernelOnboarding) {
	name, email := "", ""
	if state.GlobalName != nil {
		name = *state.GlobalName
	}
	if state.GlobalEmail != nil {
		email = *state.GlobalEmail
	}
	// Match ConfigureGitUser's late global/account defaults without replacing
	// an existing manual draft. Clearing a field does not refill it every frame.
	if name != form.globalName && form.manualName == "" {
		form.manualName = name
	}
	if email != form.globalEmail && form.manualEmail == "" {
		form.manualEmail = email
	}
	form.globalName, form.globalEmail = name, email
	key := ""
	if account := state.Account; account != nil {
		key = fmt.Sprintf("%q/%q/%q/%t", account.Name, account.PreferredEmail, account.Emails, account.Enterprise)
		if key != form.accountKey {
			form.useAccount, form.accountEmail = true, account.PreferredEmail
			if form.manualName == "" {
				form.manualName = account.Name
			}
			if form.manualEmail == "" {
				form.manualEmail = account.PreferredEmail
			}
		}
	} else {
		form.useAccount, form.accountEmail = false, ""
	}
	form.accountKey = key
	lock := ""
	if state.LockFilePath != nil {
		lock = *state.LockFilePath
	}
	if lock != form.lockKey {
		form.confirmLockDeletion = false
		form.lockKey = lock
	}
}

func onboardingAuthor(c *ui.Context, tx locales.Messages, state *backend.KernelOnboarding, form *kernelOnboardingForm, actions kernelOnboardingActions, cancel func()) {
	blocked := state.Loading || form.busy
	save := func() {
		if blocked || actions.Configure == nil {
			return
		}
		name, email := form.manualName, form.manualEmail
		if form.useAccount {
			if state.Account == nil || !slices.Contains(state.Account.Emails, form.accountEmail) {
				return
			}
			name, email = state.Account.Name, form.accountEmail
		}
		form.busy, form.err = true, nil
		actions.Configure(name, email, form.useAccount)
	}
	ui.Scroll(c.Key("author-body")).Grow(1).MinHeight(0).FillWidth().Gap(16).KeepScrollbars(true).Padding(0, 8, 0, 0).Children(func() {
		ui.Text(c, tx.OnboardingAuthorNotice()).TextColor(c.Theme().TextMuted).Shrink(0)
		if state.Account != nil {
			label := tx.OnboardingUseAccount()
			if state.Account.Enterprise {
				label = tx.OnboardingUseEnterpriseAccount()
			}
			ui.RadioGroup(c.Key("author-source"), func() {
				ui.Radio(c.Key("account"), &form.useAccount, true, label).Disabled(blocked)
				ui.Radio(c.Key("manual"), &form.useAccount, false, tx.OnboardingManual()).Disabled(blocked)
			}).Gap(12).FillWidth().Shrink(0).Label(tx.OnboardingAuthorSource())
		}
		if state.LockFilePath != nil {
			ui.Column(c.Key("config-lock")).Gap(12).FillWidth().Shrink(0).Children(func() {
				ui.Text(c, tx.OnboardingLockNotice()).TextColor(c.Theme().Danger).Role(ui.RoleAlert)
				onboardingValue(c, tx.OnboardingLockPath(), *state.LockFilePath)
				ui.Checkbox(c.Key(form.lockKey), &form.confirmLockDeletion, tx.OnboardingLockConfirm()).Disabled(blocked).AutoFocus()
				ui.Button(c.Key("delete-lock"), tx.OnboardingDeleteLock()).Height(40).Padding(8, 16).AlignSelf(ui.Start).
					Disabled(blocked || !form.confirmLockDeletion || actions.DeleteLock == nil).OnClick(func() {
					if form.busy || !form.confirmLockDeletion || actions.DeleteLock == nil {
						return
					}
					form.busy, form.err, form.confirmLockDeletion = true, nil, false
					// Only the action crosses the boundary. The path is diagnostic
					// text, never an argument to unlink or a wire command here.
					actions.DeleteLock()
				})
			})
		}
		ui.Field(c.Key("name-field"), tx.OnboardingName(), func() {
			var input ui.Element
			if form.useAccount && state.Account != nil {
				// Bind a presentation copy: a read-only input must not mutate DTO
				// defaults or the manual draft, including through editor undo.
				name := state.Account.Name
				input = ui.TextInput(c.Key("account-name"), &name).ReadOnly(true).Height(40).FillWidth().Disabled(blocked)
			} else {
				input = ui.TextInput(c.Key("manual-name"), &form.manualName).Height(40).FillWidth().Disabled(blocked).
					Placeholder(tx.OnboardingNamePlaceholder()).OnSubmit(save)
			}
			if state.LockFilePath == nil {
				input.AutoFocus()
			}
		}).Shrink(0)
		ui.Field(c.Key("email-field"), tx.OnboardingEmail(), func() {
			if form.useAccount && state.Account != nil {
				ui.Select(c.Key("account-email"), &form.accountEmail, state.Account.Emails).Height(40).FillWidth().Disabled(blocked)
			} else {
				ui.TextInput(c.Key("manual-email"), &form.manualEmail).Height(40).FillWidth().Disabled(blocked).
					Placeholder("your-email@example.com").OnSubmit(save)
			}
		}).Shrink(0)
		if !form.useAccount && state.Account != nil && form.manualEmail != "" && !slices.Contains(state.Account.Emails, form.manualEmail) {
			ui.Text(c, tx.OnboardingEmailWarning()).TextColor(c.Theme().Warning).Shrink(0)
		}
		ui.Column(c.Key("author-preview")).Gap(8).FillWidth().Shrink(0).Padding(16, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
			ui.Text(c, tx.OnboardingExample()).FontWeight(500)
			name, email := form.manualName, form.manualEmail
			if form.useAccount && state.Account != nil {
				name, email = state.Account.Name, form.accountEmail
			}
			ui.Text(c, tx.OnboardingExampleSummary())
			author := name + " <" + email + ">"
			ui.TextArea(c, &author).ReadOnly(true).FillWidth().Lines(1, 2).MinHeight(0).Padding(0).
				BorderWidth(0).Background(c.Theme().Background).Label(tx.OnboardingAuthorPreview())
		})
		onboardingRequestStatus(c, tx, blocked, form.err)
	})
	ui.Row(c.Key("author-actions")).Gap(12).Wrap().FillWidth().Shrink(0).Children(func() {
		ui.PrimaryButton(c.Key("finish"), tx.OnboardingFinish()).Height(44).Padding(8, 20).FontWeight(500).
			Disabled(blocked || actions.Configure == nil || form.useAccount && (state.Account == nil || !slices.Contains(state.Account.Emails, form.accountEmail))).OnClick(save)
		ui.Button(c.Key("cancel"), tx.Cancel()).Height(44).Padding(8, 16).Disabled(form.canceling || actions.Advance == nil).OnClick(cancel)
	})
}

// renderKernelSignInDialog is the protected-focus PopupType.SignIn surface.
// Success renders nothing; only the original SignIn state closes the popup.
// cancel receives the popup ID, never credentials or an authorization URL.
func renderKernelSignInDialog(c *ui.Context, tx locales.Messages, popupID int, state *backend.KernelSignIn, form *kernelSignInForm, endpoint func(string), browser func(), cancel func(int)) {
	syncKernelSignInForm(form, popupID, state)
	if state == nil || state.Kind == "Success" {
		return
	}
	setWorkspaceTheme(c)
	title := tx.SignInTitle()
	if state.Kind == "Authentication" {
		title = tx.SignInBrowserTitle()
	}
	dismiss := func() {
		if form.canceling || cancel == nil {
			return
		}
		form.busy, form.canceling, form.err = true, true, nil
		cancel(popupID)
	}
	open := true // the core, not local dismissal, owns the dialog's presence
	panel := ui.DialogBase(c.Key(struct {
		name string
		id   int
	}{"kernel-signin", popupID}), &open, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Width(640).MaxWidthPercent(94).Padding(24).Gap(20).
			Radius(12).Border(1, c.Theme().Border).Background(c.Theme().Background).Clip().Label(title).FontWeight(400)
		if state.UnrecognizedEnterpriseServer {
			panel.Role(ui.RoleAlertDialog)
		}
		onboardingHeading(c, title)
		// A core step change remounts content and refocuses its first control,
		// as the original SignIn dialog does, without a local popup stack.
		ui.Column(c.Key(form.session)).Grow(1).MinHeight(0).FillWidth().Gap(20).Children(func() {
			ui.Column(c.Key("signin-body")).FillWidth().Gap(20).Shrink(0).Children(func() {
				kernelSignInBody(c, tx, state, form, endpoint)
			})
			kernelSignInButtons(c, tx, state, form, endpoint, browser, dismiss, form.canceling || cancel == nil)
		})
	})
	if panel.Dismissed() {
		dismiss()
	}
}

func syncKernelSignInForm(form *kernelSignInForm, id int, state *backend.KernelSignIn) {
	kind := ""
	if state != nil {
		kind = state.Kind
	}
	if form.popupID != id || form.kind != kind || state == nil {
		*form = kernelSignInForm{popupID: id, kind: kind, session: form.session + 1}
	}
}

func kernelSignInBody(c *ui.Context, tx locales.Messages, state *backend.KernelSignIn, form *kernelSignInForm, endpoint func(string)) {
	if state == nil || state.Kind == "Success" {
		ui.Text(c, tx.OnboardingWorking()).Role(ui.RoleStatus).Shrink(0)
		return
	}
	switch state.Kind {
	case "EndpointEntry":
		ui.Text(c, tx.SignInEndpointHint()).TextColor(c.Theme().TextMuted).Shrink(0)
		ui.Field(c.Key("endpoint-field"), tx.SignInEndpoint(), func() {
			ui.TextInput(c.Key(form.session), &form.endpoint).Height(40).FillWidth().AutoFocus().
				Placeholder("https://example.ghe.com").Disabled(state.Loading || form.busy || endpoint == nil).
				OnSubmit(func() { submitKernelSignIn(state, form, endpoint, nil) })
		}).Shrink(0)
	case "Authentication":
		if state.UnrecognizedEnterpriseServer {
			ui.Text(c, tx.SignInUnrecognizedServer()).TextColor(c.Theme().Warning).Role(ui.RoleAlert).Shrink(0)
		}
		if state.Endpoint != nil {
			onboardingValue(c, tx.SignInServer(), *state.Endpoint)
		}
		if state.ExpectedLogin != nil {
			ui.Text(c, tx.SignInExpectedLogin(*state.ExpectedLogin)).Shrink(0)
		}
		ui.Text(c, tx.SignInBrowserHint()).Shrink(0)
	default:
		ui.Text(c, tx.SignInUnknownStep()).Role(ui.RoleAlert).Shrink(0)
		onboardingValue(c, tx.OnboardingSteps(), state.Kind)
	}
	if state.Error != nil {
		ui.Text(c, tx.SignInFailed()).Role(ui.RoleAlert).TextColor(c.Theme().Danger).Shrink(0)
		onboardingValue(c, tx.FullErrorDetails(), *state.Error)
	}
	onboardingRequestStatus(c, tx, state.Loading || form.busy, form.err)
}

func submitKernelSignIn(state *backend.KernelSignIn, form *kernelSignInForm, endpoint func(string), browser func()) {
	if state == nil || state.Loading || form.busy {
		return
	}
	switch state.Kind {
	case "EndpointEntry":
		if form.endpoint == "" || endpoint == nil {
			return
		}
		form.busy, form.err = true, nil
		endpoint(form.endpoint) // normalization and validation stay in SignInStore
	case "Authentication":
		if browser != nil {
			form.busy, form.err = true, nil
			browser()
		}
	}
}

func kernelSignInButtons(c *ui.Context, tx locales.Messages, state *backend.KernelSignIn, form *kernelSignInForm, endpoint func(string), browser func(), cancel func(), cancelBlocked bool) {
	submit := func() { submitKernelSignIn(state, form, endpoint, browser) }
	ui.Row(c.Key(struct {
		name    string
		session uint64
	}{"signin-actions", form.session})).Gap(12).Wrap().FillWidth().Shrink(0).Children(func() {
		if state != nil && (state.Kind == "EndpointEntry" || state.Kind == "Authentication") {
			label, unavailable := tx.SignInContinue(), endpoint == nil || form.endpoint == ""
			if state.Kind == "Authentication" {
				label, unavailable = tx.SignInBrowser(), browser == nil
			}
			button := ui.PrimaryButton(c.Key("submit"), label).Height(44).Padding(8, 20).FontWeight(500).
				Disabled(state.Loading || form.busy || unavailable).OnClick(submit)
			if state.Kind == "Authentication" {
				button.AutoFocus()
			}
			c.OnWindowShortcut(0, ui.KeyEnter, submit)
		}
		// Loading must never trap a user in authentication. Cancel goes to
		// the original store/popup continuation even while submit is pending.
		ui.Button(c.Key("cancel"), tx.Cancel()).Height(44).Padding(8, 16).Disabled(cancelBlocked).OnClick(cancel)
	})
}

func onboardingHeading(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(c.Theme().Rem(28.0 / 15)).FontWeight(500).Role(ui.RoleHeading).Level(1).Shrink(0)
}

func onboardingValue(c *ui.Context, label, value string) {
	ui.Field(c, label, func() {
		ui.TextArea(c, &value).ReadOnly(true).FillWidth().Lines(1, 3).MinHeight(0).Padding(4, 0).
			BorderWidth(0).Background(c.Theme().Background)
	}).Shrink(0)
}

func onboardingRequestStatus(c *ui.Context, tx locales.Messages, busy bool, err error) {
	if busy {
		ui.Text(c, tx.OnboardingWorking()).Role(ui.RoleStatus).Busy(true).Shrink(0)
	}
	if err != nil {
		ui.Text(c, tx.OnboardingRequestFailed()).Role(ui.RoleAlert).TextColor(c.Theme().Danger).Shrink(0)
		onboardingValue(c, tx.FullErrorDetails(), err.Error())
	}
}

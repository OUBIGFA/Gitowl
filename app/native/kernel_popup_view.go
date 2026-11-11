package main

import (
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// kernelPopupForm is one prompt's temporary input, never kernel state. The
// caller clears busy / sets err when a reply fails, and replaces the popup only
// from the kernel's next snapshot. Secrets leave this form before dispatch.
// No continuation, credential store, repository, or credential-bearing Account
// lives here; account is only the user's allowed endpoint/login selection.
type kernelPopupForm struct {
	popupID      int
	username     string
	secret       string
	storeSecret  bool
	dontAskAgain bool
	account      backend.KernelAccountIdentity
	busy         bool
	err          error
}

// renderKernelPopup presents the bounded original Desktop popup contract.
// Only explicit controls send replies; dismissals use the original close
// continuation. Unsupported or malformed presentations remain blocking.
func renderKernelPopup(c *ui.Context, tx locales.Messages, popup *backend.KernelPopupPresentation, form *kernelPopupForm, reply func(backend.KernelPopupReply), close func(int)) {
	if popup == nil {
		*form = kernelPopupForm{}
		return
	}
	id := 0
	if popup.ID != nil {
		id = *popup.ID
	}
	if form.popupID != id {
		*form = kernelPopupForm{popupID: id}
		if popup.Type == "ConfirmForcePush" {
			form.dontAskAgain = !popup.AskForConfirmationOnForcePush
		}
	}
	valid := kernelPopupValid(popup)
	if !valid {
		// An unsupported view must not retain a previous prompt's input.
		form.secret, form.username = "", ""
		form.account = backend.KernelAccountIdentity{}
	}
	if popup.SelectedAccount != nil && form.account == (backend.KernelAccountIdentity{}) && kernelPopupAccountAllowed(popup, *popup.SelectedAccount) {
		form.account = *popup.SelectedAccount
	}
	if form.account != (backend.KernelAccountIdentity{}) && !kernelPopupAccountAllowed(popup, form.account) {
		// A refreshed allow-list invalidates the choice; never choose its first
		// account as a fallback, including the repository's assigned account.
		form.account = backend.KernelAccountIdentity{}
	}
	send := func(out backend.KernelPopupReply) {
		if !valid || form.popupID != id || form.busy || reply == nil {
			return
		}
		out.PopupID, out.Type = id, popup.Type
		form.secret, form.busy, form.err = "", true, nil
		reply(out)
	}
	cancel := func() {
		if !valid || form.popupID != id || form.busy || close == nil {
			return
		}
		form.secret, form.username = "", ""
		form.account = backend.KernelAccountIdentity{}
		form.busy, form.err = true, nil
		close(id)
	}
	title := kernelPopupTitle(tx, popup)
	if !valid {
		title = tx.KernelFrontendIncomplete()
	}
	// The kernel owns whether the dialog exists. Do not hide it optimistically
	// while an asynchronous continuation is unresolved, or keep a local stack.
	open, backdropClicked := true, false
	panel := ui.DialogBase(c.Key(struct {
		name string
		id   int
	}{"kernel-popup", id}), &open, func(back, panel ui.Element) {
		backdropClicked = back.Clicked()
		back.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Width(640).MaxWidthPercent(94).
			Padding(24).Gap(16).Radius(12).Border(1, c.Theme().Border).
			Background(c.Theme().Background).Clip().Label(title)
		ui.Text(c, title).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500).Role(ui.RoleHeading).Level(1).Shrink(0)
		// Size the prompt from its parameters. Only diagnostics and account
		// collections own scrolling; the form and decision buttons stay visible.
		ui.Column(c.Key("popup-body")).FillWidth().Gap(16).Shrink(0).Children(func() {
			if !valid {
				message := tx.PopupInvalid()
				if !popup.Supported || !kernelPopupKnown(popup.Type) {
					message = tx.PopupUnsupported()
				}
				ui.Text(c, message).Role(ui.RoleAlert).Shrink(0)
				kernelPopupDiagnostic(c.Key("unsupported-type"), tx.FullErrorDetails(), popup.Type, 96)
				return
			}
			kernelPopupBody(c, tx, popup, form, send)
			if form.busy {
				ui.Text(c, tx.PopupWorking()).Role(ui.RoleStatus).Busy(true).Shrink(0)
			}
			if form.err != nil {
				ui.Text(c, tx.PopupReplyFailed()).Role(ui.RoleAlert).TextColor(c.Theme().Danger).Shrink(0)
				kernelPopupDiagnostic(c.Key("reply-error"), tx.FullErrorDetails(), form.err.Error(), 96)
			}
		})
		if valid {
			kernelPopupActions(c, tx, popup, form, send, cancel, reply == nil, close == nil)
		}
	})
	if panel.Dismissed() {
		// Upstream SSH prompts cannot be dismissed by the backdrop. Escape is
		// still a real rejection/cancel; a busy or unsupported prompt stays put.
		ssh := popup.Type == "AddSSHHost" || popup.Type == "SSHKeyPassphrase" || popup.Type == "SSHUserPassword"
		if !ssh || !backdropClicked {
			cancel()
		}
	}
}

func kernelPopupBody(c *ui.Context, tx locales.Messages, popup *backend.KernelPopupPresentation, form *kernelPopupForm, send func(backend.KernelPopupReply)) {
	switch popup.Type {
	case "AddSSHHost":
		ui.Text(c, tx.PopupSSHHostNotice()).Shrink(0)
		kernelPopupDetail(c.Key("host"), tx.PopupHost(), popup.Host)
		kernelPopupDetail(c.Key("ip"), tx.PopupIP(), popup.IP)
		kernelPopupDetail(c.Key("key-type"), tx.PopupKeyType(), popup.KeyType)
		kernelPopupDetail(c.Key("fingerprint"), tx.PopupFingerprint(), popup.Fingerprint)
	case "SSHKeyPassphrase", "SSHUserPassword":
		label, remember := tx.PopupPassphrase(), tx.PopupRememberPassphrase()
		if popup.Type == "SSHKeyPassphrase" {
			kernelPopupDetail(c.Key("key-path"), tx.PopupKeyPath(), popup.KeyPath)
		} else {
			label, remember = tx.PopupPassword(), tx.PopupRememberPassword()
			kernelPopupDetail(c.Key("ssh-username"), tx.PopupUsername(), *popup.Username)
		}
		if kernelPopupSecretInput(c, label, form, true).Submitted() && kernelPopupSingleLine(form.secret) && !form.busy {
			send(kernelPopupCredentialReply(popup, form))
		}
		ui.Checkbox(c.Key("remember-secret"), &form.storeSecret, remember).Disabled(form.busy)
	case "GenericGitAuthentication":
		ui.Text(c, tx.PopupAuthenticationNotice()).Shrink(0)
		kernelPopupDetail(c.Key("remote"), tx.PopupRemoteURL(), popup.RemoteURL)
		if popup.Username == nil {
			ui.Field(c.Key("username-field"), tx.PopupUsername(), func() {
				ui.TextInput(c.Key("username"), &form.username).Height(40).FillWidth().
					AutoFocus().Disabled(form.busy).OnPaste(kernelPopupRejectPaste)
			})
		} else {
			// nil alone means editable. Bind a local presentation string, never
			// the DTO's pointer or the user's draft, even for a fixed empty name.
			username := *popup.Username
			ui.Field(c.Key("username-field"), tx.PopupUsername(), func() {
				ui.TextInput(c.Key("fixed-username"), &username).Height(40).FillWidth().ReadOnly(true).
					Description(tx.PopupUsernameFixed())
			})
		}
		if kernelPopupSecretInput(c, tx.PopupPassword(), form, popup.Username != nil).Submitted() && kernelPopupCredentialsReady(popup, form) && !form.busy {
			send(kernelPopupCredentialReply(popup, form))
		}
		ui.Text(c, tx.PopupAuthenticationTokenHint()).Shrink(0)
		ui.Link(c, tx.PopupAuthenticationHelp(), "https://github.com/desktop/desktop/tree/development/docs/integrations").AlignSelf(ui.Start).Disabled(form.busy)
	case "HookFailed":
		ui.Text(c, tx.PopupHookFailedNotice(popup.HookName)).Shrink(0)
		kernelPopupDiagnostic(c.Key("hook-output"), tx.PopupTerminalOutput(), popup.TerminalOutput, 256)
	case "GenerateCommitMessageOverrideWarning":
		ui.Text(c, tx.PopupGenerationOverrideNotice()).Shrink(0)
		if popup.ShowCopilotInstructionsTip {
			ui.Text(c, tx.PopupCopilotInstructionsTip()).Shrink(0)
			ui.Link(c, tx.PopupCopilotInstructions(), "https://gh.io/desktop-copilot-custom-instructions").AlignSelf(ui.Start).Disabled(form.busy)
		}
		ui.Checkbox(c.Key("dont-ask"), &form.dontAskAgain, tx.PopupDontAskAgain()).Disabled(form.busy)
	case "GenerateCommitMessageDisclaimer":
		ui.Text(c, tx.PopupGenerationDisclaimerNotice()).Shrink(0)
		if popup.GenerationSource != "custom" {
			ui.Link(c, tx.PopupCopilotLearnMore(), "https://gh.io/copilot-for-desktop-transparency").AlignSelf(ui.Start).Disabled(form.busy)
		}
	case "RepositoryAccount":
		ui.Text(c, tx.PopupRepositoryAccountNotice(popup.RepositoryName)).Shrink(0)
		kernelPopupDetail(c.Key("endpoint"), tx.PopupEndpoint(), popup.Endpoint)
		if popup.AssignedLogin != nil {
			kernelPopupDetail(c.Key("assigned-login"), tx.PopupAssignedLogin(), *popup.AssignedLogin)
		}
		if popup.RequiresSignIn {
			ui.Text(c, tx.PopupAccountSignInRequired()).Shrink(0)
			ui.Button(c.Key("choose-another-account"), tx.PopupChooseAnotherAccount()).Disabled(form.busy).
				OnClick(func() { send(backend.KernelPopupReply{Action: "choose-another"}) })
			return
		}
		ui.Button(c.Key("sign-in-another-account"), tx.PopupSignInAnotherAccount()).Disabled(form.busy).
			OnClick(func() { send(backend.KernelPopupReply{Action: "sign-in"}) })
		count := 0
		_, viewHeight := c.Size()
		ui.Scroll(c.Key("account-list")).MaxHeight(viewHeight / 3).FillWidth().Shrink(0).Children(func() {
			ui.RadioGroup(c.Key("accounts"), func() {
				for _, account := range popup.Accounts {
					if !kernelPopupAccountAllowed(popup, account) {
						continue
					}
					count++
					ui.Radio(c.Key(account), &form.account, account, account.Login).Description(account.Endpoint).
						Padding(8, 0).Disabled(form.busy)
				}
			}).Label(tx.PopupChooseAccount()).Gap(4).FillWidth().Shrink(0)
		})
		if count == 0 {
			ui.Text(c, tx.PopupNoAccounts()).Role(ui.RoleStatus).Shrink(0)
		}
	case "ConfirmForcePush":
		ui.Text(c, tx.PopupForcePushNotice()).Shrink(0)
		kernelPopupDetail(c.Key("upstream"), tx.PopupUpstreamBranch(), popup.UpstreamBranch)
		ui.Checkbox(c.Key("dont-ask"), &form.dontAskAgain, tx.PopupDontAskAgain()).Disabled(form.busy)
	case "PushNeedsPull":
		ui.Text(c, tx.PopupPushNeedsFetchNotice()).Shrink(0)
	case "Error":
		ui.Text(c, tx.PopupErrorNotice()).Shrink(0)
		if popup.RetryCloneName != nil {
			kernelPopupDetail(c.Key("clone-name"), tx.PopupCloneName(), *popup.RetryCloneName)
		}
		kernelPopupDiagnostic(c.Key("error-output"), tx.FullErrorDetails(), kernelPopupErrorOutput(popup), 232)
	}
}

// Each secret editor is keyed by popup (its parent) and busy. Clearing a string
// alone retains MyGo's undo history; dispatch remounts an empty disabled editor
// so failure/cancel/new prompt cannot restore a previously submitted secret.
func kernelPopupSecretInput(c *ui.Context, label string, form *kernelPopupForm, autofocus bool) ui.Element {
	var input ui.Element
	ui.Field(c.Key("secret-field"), label, func() {
		input = ui.TextInput(c.Key(form.busy), &form.secret).Password().Height(40).FillWidth().
			Disabled(form.busy).OnPaste(kernelPopupRejectPaste)
		if autofocus {
			input.AutoFocus()
		}
	})
	return input
}

func kernelPopupDetail(c *ui.Context, label, value string) {
	ui.Field(c, label, func() {
		ui.TextArea(c, &value).ReadOnly(true).FillWidth().Lines(1, 3).MinHeight(0).
			Padding(4, 0).BorderWidth(0).Background(c.Theme().Background)
	}).Shrink(0)
}

func kernelPopupDiagnostic(c *ui.Context, label, value string, height float32) {
	ui.TextArea(c, &value).ReadOnly(true).Height(height).MinHeight(0).FillWidth().Shrink(0).
		Font("monospace").KeepScrollbars(true).Label(label)
}

func kernelPopupActions(c *ui.Context, tx locales.Messages, popup *backend.KernelPopupPresentation, form *kernelPopupForm, send func(backend.KernelPopupReply), cancel func(), replyUnavailable, closeUnavailable bool) {
	if popup.Type == "Error" {
		for index, content := range append([]backend.KernelErrorContent{popup.Content}, popup.Causes...) {
			if content.ActionText == "" || content.ActionURL == "" {
				continue
			}
			label := content.ActionText
			if label == "Open GitHub Copilot settings" {
				label = locales.Catalog.Format(c.Locale(), "popupOpenGitHubCopilotSettings", nil)
			}
			// Empty href retains native link keyboard semantics, while the
			// opaque action resolves and opens the original error URL in Desktop.
			if ui.Link(c.Key(index), label, "").Disabled(form.busy || replyUnavailable).Clicked() {
				position := index
				send(backend.KernelPopupReply{Action: "openAction", ContentIndex: &position})
			}
		}
	}
	ui.Row(c.Key("popup-actions")).Gap(8).Wrap().Justify(ui.End).FillWidth().Shrink(0).Children(func() {
		blocked := form.busy || replyUnavailable
		if popup.Type != "AddSSHHost" && popup.Type != "HookFailed" && popup.Type != "Error" {
			ui.Button(c.Key("cancel"), tx.Cancel()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(form.busy || closeUnavailable).OnClick(cancel)
		}
		switch popup.Type {
		case "AddSSHHost":
			ui.Button(c.Key("reject-host"), tx.PopupRejectHost()).Height(40).Padding(8, 16).FontWeight(500).AutoFocus().
				Disabled(blocked).OnClick(func() { no := false; send(backend.KernelPopupReply{Action: "submit", AddHost: &no}) })
			ui.PrimaryButton(c.Key("trust-host"), tx.PopupTrustHost()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(blocked).OnClick(func() { yes := true; send(backend.KernelPopupReply{Action: "submit", AddHost: &yes}) })
		case "SSHKeyPassphrase", "SSHUserPassword", "GenericGitAuthentication":
			ui.PrimaryButton(c.Key("authenticate"), tx.PopupAuthenticate()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(blocked || !kernelPopupCredentialsReady(popup, form)).OnClick(func() { send(kernelPopupCredentialReply(popup, form)) })
		case "HookFailed":
			for _, choice := range []string{"abort", "ignore"} {
				if !slices.Contains(popup.Choices, choice) {
					continue
				}
				label := tx.PopupAbort()
				if choice == "ignore" {
					label = tx.PopupIgnoreHook()
				}
				button := ui.Button(c.Key(choice), label).Height(40).Padding(8, 16).FontWeight(500).Disabled(blocked)
				if choice == "abort" {
					button.AutoFocus()
				}
				button.OnClick(func() { result := choice; send(backend.KernelPopupReply{Action: "submit", Result: &result}) })
			}
		case "GenerateCommitMessageOverrideWarning":
			ui.PrimaryButton(c.Key("generate-override"), tx.PopupOverrideAndGenerate()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(blocked).OnClick(func() {
				confirm := !form.dontAskAgain
				send(backend.KernelPopupReply{Action: "submit", ConfirmCommitMessageOverride: &confirm})
			})
		case "GenerateCommitMessageDisclaimer":
			ui.PrimaryButton(c.Key("generate-disclaimer"), tx.PopupUnderstandAndGenerate()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(blocked).OnClick(func() { send(backend.KernelPopupReply{Action: "submit"}) })
		case "RepositoryAccount":
			if popup.RequiresSignIn {
				ui.PrimaryButton(c.Key("sign-in-assigned-account"), tx.SignInTitle()).Height(40).Padding(8, 16).Disabled(blocked).
					OnClick(func() { send(backend.KernelPopupReply{Action: "sign-in"}) })
				return
			}
			ui.PrimaryButton(c.Key("use-account"), tx.PopupUseAccount()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(blocked || !kernelPopupAccountAllowed(popup, form.account)).OnClick(func() {
				account := form.account
				send(backend.KernelPopupReply{Action: "submit", Account: &account})
			})
		case "ConfirmForcePush":
			ui.PrimaryButton(c.Key("force-push"), tx.PopupForcePush()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(blocked).OnClick(func() {
				ask := !form.dontAskAgain
				send(backend.KernelPopupReply{Action: "submit", AskForConfirmationOnForcePush: &ask})
			})
		case "PushNeedsPull":
			// Its original continuation is fetch(UserInitiatedTask), not pull or
			// a local retry strategy. No repository/Git operation runs in the view.
			ui.PrimaryButton(c.Key("fetch"), tx.FetchUpdates()).Height(40).Padding(8, 16).FontWeight(500).
				Disabled(blocked).OnClick(func() { send(backend.KernelPopupReply{Action: "submit"}) })
		case "Error":
			for _, choice := range []string{"close", "retry", "openPreferences"} {
				if !slices.Contains(popup.Choices, choice) {
					continue
				}
				label := tx.Close()
				switch choice {
				case "retry":
					label = tx.PopupRetry()
				case "openPreferences":
					label = tx.PopupOpenPreferences()
				}
				ui.Button(c.Key(choice), label).Height(40).Padding(8, 16).FontWeight(500).Disabled(blocked).
					OnClick(func() { send(backend.KernelPopupReply{Action: choice}) })
			}
		}
	})
}

func kernelPopupSingleLine(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\x00\r\n")
}

func kernelPopupRejectPaste(value string) bool { return strings.ContainsAny(value, "\x00\r\n") }

func kernelPopupCredentialsReady(popup *backend.KernelPopupPresentation, form *kernelPopupForm) bool {
	if !kernelPopupSingleLine(form.secret) {
		return false
	}
	if popup.Type != "GenericGitAuthentication" {
		return true
	}
	username := form.username
	if popup.Username != nil {
		username = *popup.Username
	}
	return kernelPopupSingleLine(username)
}

func kernelPopupCredentialReply(popup *backend.KernelPopupPresentation, form *kernelPopupForm) backend.KernelPopupReply {
	// All pointers refer to one-shot local values, never to the cleared form
	// or snapshot fields. The wire transports them; the view never saves them.
	secret, store := form.secret, form.storeSecret
	out := backend.KernelPopupReply{Action: "submit"}
	switch popup.Type {
	case "SSHKeyPassphrase":
		out.Passphrase, out.StorePassphrase = &secret, &store
	case "SSHUserPassword":
		out.Password, out.StorePassword = &secret, &store
	case "GenericGitAuthentication":
		username := form.username
		if popup.Username != nil {
			username = *popup.Username
		}
		out.Username, out.Password = &username, &secret
	}
	return out
}

func kernelPopupAccountAllowed(popup *backend.KernelPopupPresentation, account backend.KernelAccountIdentity) bool {
	return account.Endpoint == popup.Endpoint && kernelPopupSingleLine(account.Endpoint) && kernelPopupSingleLine(account.Login) && slices.Contains(popup.Accounts, account)
}

func kernelPopupKnown(kind string) bool {
	switch kind {
	case "AddSSHHost", "SSHKeyPassphrase", "SSHUserPassword", "GenericGitAuthentication", "HookFailed", "GenerateCommitMessageOverrideWarning", "GenerateCommitMessageDisclaimer", "RepositoryAccount", "ConfirmForcePush", "PushNeedsPull", "Error":
		return true
	}
	return false
}

func kernelPopupValid(popup *backend.KernelPopupPresentation) bool {
	if !popup.Supported || popup.ID == nil || *popup.ID <= 0 || !kernelPopupKnown(popup.Type) {
		return false
	}
	switch popup.Type {
	case "AddSSHHost":
		return kernelPopupSingleLine(popup.Host) && kernelPopupSingleLine(popup.IP) && kernelPopupSingleLine(popup.KeyType) && kernelPopupSingleLine(popup.Fingerprint)
	case "SSHKeyPassphrase":
		return kernelPopupSingleLine(popup.KeyPath)
	case "SSHUserPassword":
		return popup.Username != nil && kernelPopupSingleLine(*popup.Username)
	case "GenericGitAuthentication":
		return kernelPopupSingleLine(popup.RemoteURL)
	case "RepositoryAccount":
		return kernelPopupSingleLine(popup.Endpoint) && popup.RepositoryName != ""
	case "ConfirmForcePush":
		return kernelPopupSingleLine(popup.UpstreamBranch)
	case "HookFailed", "Error":
		allowed := []string{"abort", "ignore"}
		if popup.Type == "Error" {
			allowed = []string{"close", "retry", "openPreferences"}
		}
		if len(popup.Choices) == 0 {
			return false
		}
		for i, choice := range popup.Choices {
			if !slices.Contains(allowed, choice) || slices.Contains(popup.Choices[:i], choice) {
				return false
			}
		}
	}
	return true
}

func kernelPopupTitle(tx locales.Messages, popup *backend.KernelPopupPresentation) string {
	switch popup.Type {
	case "AddSSHHost":
		return tx.PopupSSHHostTitle()
	case "SSHKeyPassphrase":
		return tx.PopupSSHKeyPassphraseTitle()
	case "SSHUserPassword":
		return tx.PopupSSHUserPasswordTitle()
	case "GenericGitAuthentication":
		return tx.PopupAuthenticationTitle()
	case "HookFailed":
		return tx.PopupHookFailedTitle()
	case "GenerateCommitMessageOverrideWarning":
		return tx.PopupGenerationOverrideTitle()
	case "GenerateCommitMessageDisclaimer":
		return tx.GenerateMessage()
	case "RepositoryAccount":
		return tx.PopupRepositoryAccountTitle()
	case "ConfirmForcePush":
		return tx.PopupForcePushTitle()
	case "PushNeedsPull":
		return tx.PopupPushNeedsFetchTitle()
	case "Error":
		// Titles projected by the verified original presenter are labels, so
		// translate them. Unknown future titles stay in the diagnostics below.
		switch popup.Title {
		case "File size limit exceeded":
			return tx.PopupFileSizeErrorTitle()
		case "Clone failed":
			return tx.PopupCloneErrorTitle()
		case "Failed to push":
			return tx.PopupPushErrorTitle()
		case "Failed creating repository":
			return tx.PopupCreateRepositoryErrorTitle()
		case "Commit failed":
			return tx.PopupCommitErrorTitle()
		case "Quota exceeded":
			return tx.PopupQuotaErrorTitle()
		case "Session quota exceeded":
			return tx.PopupSessionQuotaErrorTitle()
		case "Copilot billing not configured":
			return tx.PopupBillingNotConfiguredErrorTitle()
		case "Copilot billing issue":
			return tx.PopupBillingErrorTitle()
		}
	}
	return tx.PopupErrorTitle()
}

func kernelPopupErrorOutput(popup *backend.KernelPopupPresentation) string {
	// Original English/server output is diagnostic content, not UI labels.
	// Preserve messages, causes, file names and ancillary details for copying.
	parts := []string{popup.Title}
	for _, content := range append([]backend.KernelErrorContent{popup.Content}, popup.Causes...) {
		parts = append(parts, content.Message)
		parts = append(parts, content.OversizedFiles...)
		parts = append(parts, content.RetryAfterMessage, content.ActionText, content.ActionURL)
	}
	parts = slices.DeleteFunc(parts, func(part string) bool { return part == "" })
	return strings.Join(parts, "\n\n")
}

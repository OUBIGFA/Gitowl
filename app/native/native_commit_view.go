package main

import (
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

type nativeCommitUI struct {
	commitOptionsOpen     bool
	coAuthorQuery         string
	coAuthorChoice        string
	coAuthorOwner         string
	coAuthorSearchID      uint64
	coAuthorSearchPending bool
	coAuthorSearchAt      time.Time
	coAuthorAdding        bool
	coreCommitNotice      string
	coreCommitCount       int
}

func (a *app) nativeCommitView() *backend.KernelCommitView {
	if a.coreSnapshot == nil || a.coreSnapshot.Selected == nil {
		return nil
	}
	return a.coreSnapshot.Selected.CommitView
}

func (a *app) commitPreference(call func() (backend.KernelResult, error)) {
	if a.core == nil || a.composerLocked() {
		return
	}
	a.queueCore(nativeOperation{call: call, complete: func(_ backend.KernelResult, err error) { a.saveErr = err }})
}

// The heading's one options button protects the central composer from an
// always-open bank of advanced Git controls. All values come from AppStore.
func (a *app) nativeCommitOptions(c *ui.Context) {
	vm := a.nativeCommitView()
	if vm == nil {
		return
	}
	label := func(key string) string { return nativeDialogLabel(c.Locale(), "commit."+key) }
	button := iconButton(c.Key("commit-options"), label("options"), "settings").Disabled(a.composerLocked())
	button.OnClick(func() { a.commitOptionsOpen = !a.commitOptionsOpen })
	if vm.Mode == "copilot" {
		a.focusCoreCommitMessage(button)
	}
	core, ctx := a.core, a.ctx
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	ui.Popover(c.Key("commit-options-popover"), button, &a.commitOptionsOpen, func() {
		ui.Text(c, label("options")).FontSize(c.Theme().Rem(18.0 / 15)).FontWeight(500)
		if vm.Author != nil {
			ui.Text(c, vm.Author.Name+" <"+vm.Author.Email+">").SingleLine().Tooltip(vm.Author.Email).TextColor(c.Theme().TextMuted)
		}
		if vm.AssistedAvailable {
			mode := vm.Mode
			workspaceSelect(c.Key("commit-mode"), &mode, label("mode"), "", []nativeChoice[string]{{"manual", label("manual")}, {"copilot", label("assisted")}}, a.composerLocked()).FillWidth().
				OnChange(func() {
					a.commitPreference(func() (backend.KernelResult, error) { return core.SetCommitMode(ctx, ref, mode) })
				})
		}
		if vm.Mode == "manual" {
			amending := vm.Amending
			ui.Checkbox(c.Key("amend"), &amending, label("amend")).Disabled(a.composerLocked() || !vm.CanAmend && !vm.Amending).
				OnChange(func() {
					a.commitPreference(func() (backend.KernelResult, error) { return core.AmendCommit(ctx, ref, amending) })
				})
		}
		if vm.ShowSkipHooks {
			skip := vm.SkipHooks
			ui.Checkbox(c.Key("skip-hooks"), &skip, label("skipHooks")).Disabled(a.composerLocked()).OnChange(func() {
				a.commitPreference(func() (backend.KernelResult, error) {
					return core.SetCommitOptions(ctx, ref, backend.KernelCommitOptions{SkipHooks: &skip})
				})
			})
		}
		signOff, empty := vm.SignOff, vm.AllowEmpty
		ui.Checkbox(c.Key("sign-off"), &signOff, label("signOff")).Disabled(a.composerLocked()).OnChange(func() {
			a.commitPreference(func() (backend.KernelResult, error) {
				return core.SetCommitOptions(ctx, ref, backend.KernelCommitOptions{SignOff: &signOff})
			})
		})
		ui.Checkbox(c.Key("allow-empty"), &empty, label("allowEmpty")).Disabled(a.composerLocked()).OnChange(func() {
			a.commitPreference(func() (backend.KernelResult, error) {
				return core.SetCommitOptions(ctx, ref, backend.KernelCommitOptions{AllowEmpty: &empty})
			})
		})
		if vm.Mode == "copilot" {
			push := vm.PushAfterAssisted
			ui.Checkbox(c.Key("push-assisted"), &push, label("pushAfter")).Disabled(a.composerLocked()).OnChange(func() {
				a.commitPreference(func() (backend.KernelResult, error) { return core.PushAfterAssistedCommit(ctx, ref, push) })
			})
			ui.Text(c, label("pushNotice")).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		}
		if vm.CoAuthorsAvailable {
			shown := vm.ShowCoAuthors
			ui.Checkbox(c.Key("show-coauthors"), &shown, label("coAuthors")).Disabled(a.composerLocked()).OnChange(func() {
				a.commitPreference(func() (backend.KernelResult, error) { return core.ShowCoAuthors(ctx, ref, shown) })
			})
			if vm.ShowCoAuthors {
				a.nativeCoAuthorEditor(c.Key("coauthors"), vm)
			}
		}
	}).Width(420).MaxWidthPercent(94).MaxHeightPercent(80).Padding(20).Gap(12)
}

func (a *app) nativeCoAuthorEditor(c *ui.Context, vm *backend.KernelCommitView) {
	label := func(key string) string { return nativeDialogLabel(c.Locale(), "commit."+key) }
	core, ctx := a.core, a.ctx
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	if a.coAuthorOwner != ref.Path {
		a.coAuthorOwner, a.coAuthorQuery, a.coAuthorChoice = ref.Path, "", ""
	}
	ui.Field(c, label("findAuthor"), func() {
		input := ui.TextInput(c, &a.coAuthorQuery).FillWidth().Height(36).Disabled(a.composerLocked())
		input.OnChange(func() {
			a.coAuthorSearchID++
			a.coAuthorSearchPending = true
			a.coAuthorSearchAt = c.Now().Add(200 * time.Millisecond)
		})
		if a.coAuthorSearchPending {
			if delay := a.coAuthorSearchAt.Sub(c.Now()); delay > 0 {
				c.After(delay)
			} else {
				a.coAuthorSearchPending = false
				query := a.coAuthorQuery
				a.runCoreTask(func() (backend.KernelResult, error) { return core.SearchCoAuthors(ctx, ref, query) }, nil)
			}
		}
	})
	if vm.AuthorSearching {
		ui.Text(c, label("searching")).Role(ui.RoleStatus).TextColor(c.Theme().TextMuted)
	}
	choices := []nativeChoice[string]{}
	for _, hit := range vm.AuthorHits {
		choices = append(choices, nativeChoice[string]{hit.ID, "@" + hit.Username + " · " + hit.Name})
	}
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		workspaceSelect(c.Key("author-choice"), &a.coAuthorChoice, label("chooseAuthor"), label("chooseAuthor"), choices, a.composerLocked() || a.coAuthorAdding).Grow(1).MinWidth(0)
		quietButton(c.Key("add-author"), label("addAuthor"), "plus").Disabled(a.coAuthorChoice == "" || a.composerLocked() || a.coAuthorAdding).OnClick(func() {
			id := a.coAuthorChoice
			a.coAuthorAdding = true
			a.runCoreTask(func() (backend.KernelResult, error) { return core.AddCoAuthor(ctx, ref, id) }, func(_ backend.KernelResult, err error) {
				a.coAuthorAdding, a.saveErr = false, err
				if err == nil {
					a.coAuthorQuery, a.coAuthorChoice = "", ""
				}
			})
		})
	})
	ui.Scroll(c.Key("author-list")).FillWidth().MaxHeight(160).Gap(4).Children(func() {
		for _, author := range vm.CoAuthors {
			text := author.Name + " <" + author.Email + ">"
			if author.Kind != "known" && author.Username != nil {
				text = "@" + *author.Username + " · " + label("unresolved")
			}
			ui.Row(c.Key(author.ID)).FillWidth().Gap(8).Children(func() {
				ui.Text(c, text).SingleLine().Tooltip(text).Grow(1).MinWidth(0)
				iconButton(c, label("removeAuthor"), "x").Disabled(a.composerLocked()).OnClick(func() {
					a.commitPreference(func() (backend.KernelResult, error) { return core.RemoveCoAuthor(ctx, ref, author.ID) })
				})
			})
		}
	})
}

func (a *app) nativeCommitStatus(c *ui.Context) {
	vm := a.nativeCommitView()
	if vm == nil {
		return
	}
	label := func(key string) string { return nativeDialogLabel(c.Locale(), "commit."+key) }
	if vm.RulesPending {
		ui.Text(c, label("checkingRules")).FontSize(c.Theme().Rem(14.0 / 15)).Role(ui.RoleStatus).TextColor(c.Theme().TextMuted)
	}
	if vm.RulesBlocked {
		ui.Text(c, label("rulesBlocked")).FontSize(c.Theme().Rem(14.0 / 15)).Role(ui.RoleAlert).TextColor(c.Theme().Danger)
	}
	_, height := c.Size()
	if vm.SummaryLengthWarning && !(a.reviewingFile && height < 700) {
		ui.Text(c, label("summaryHint")).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	}
	if vm.Mode != "copilot" {
		return
	}
	ui.Text(c, label("assistedNotice")).TextColor(c.Theme().TextMuted)
	if vm.Assisted.Kind != "idle" {
		text := label("phase." + vm.Assisted.Kind)
		if vm.Assisted.Title != "" {
			text += " · " + vm.Assisted.Title
		}
		ui.Text(c, text).Role(ui.RoleStatus)
	}
	if vm.Assisted.RunID == nil {
		return
	}
	core, ctx, id := a.core, a.ctx, *vm.Assisted.RunID
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	ui.Row(c).Gap(8).Wrap().Children(func() {
		if vm.Assisted.CanCancel {
			quietButton(c, label("cancelAssisted"), "x").OnClick(func() {
				a.runCoreTask(func() (backend.KernelResult, error) { return core.CancelAssistedCommits(ctx, ref, id) }, nil)
			})
		}
		if vm.Assisted.Retry != nil {
			kind := "recovery"
			if *vm.Assisted.Retry == "push" {
				kind = "push"
			}
			quietButton(c, label("retryAssisted"), "refresh-cw").OnClick(func() {
				a.runCoreTask(func() (backend.KernelResult, error) { return core.RetryAssistedCommits(ctx, ref, id, kind) }, a.acceptAssistedReceipt)
			})
		}
		if vm.Assisted.CanDismiss {
			quietButton(c, label("dismissAssisted"), "").OnClick(func() {
				a.runCoreTask(func() (backend.KernelResult, error) { return core.DismissAssistedCommitError(ctx, ref, id) }, nil)
			})
		}
		if vm.Assisted.Kind == "error" || vm.Assisted.Kind == "push-error" {
			quietButton(c, label("details"), "file-text").OnClick(func() {
				a.runCoreTask(func() (backend.KernelResult, error) { return core.InspectAssistedCommitError(ctx, ref, id) }, nil)
			})
		}
	})
}

func (a *app) saveCoreAssistedVersion() {
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	core, ctx := a.core, a.ctx
	a.coreCommitPending, a.savingVersion, a.saveErr = true, true, nil
	a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) { return core.CreateAssistedCommits(ctx, ref) }, complete: func(result backend.KernelResult, err error) {
		a.coreCommitPending, a.savingVersion = false, false
		a.acceptAssistedReceipt(result, err)
	}})
}
func (a *app) acceptAssistedReceipt(result backend.KernelResult, err error) {
	a.saveErr = err
	if err != nil || result.Assisted == nil {
		return
	}
	a.coreCommitNotice, a.coreCommitCount = result.Assisted.Kind, len(result.Assisted.Commits)
	if (result.Assisted.Kind == "local-ready" || result.Assisted.Kind == "pushed") && result.Assisted.HeadSHA != nil {
		a.lastSavedSHA = *result.Assisted.HeadSHA
	}
}

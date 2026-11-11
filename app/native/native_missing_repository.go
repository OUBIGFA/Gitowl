package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

func (a *app) missingRepositoryView(c *ui.Context) bool {
	if a.coreSnapshot == nil || a.coreSnapshot.MissingRepository == nil {
		return false
	}
	missing := a.coreSnapshot.MissingRepository
	label := func(key string) string { return nativeDialogLabel(c.Locale(), "missingRepository."+key) }
	ui.Column(c.Key("missing-repository")).Grow(1).MinHeight(0).FillWidth().Gap(16).Padding(24).Children(func() {
		ui.Text(c, missing.Repository.Name).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500).Role(ui.RoleHeading).Level(1)
		if missing.UnsafePath != nil {
			ui.Text(c, label("unsafe")).TextColor(c.Theme().Warning)
			path := *missing.UnsafePath
			ui.TextArea(c.Key("unsafe-path"), &path).ReadOnly(true).FillWidth().Lines(1, 3).Label(label("path"))
			ui.Text(c, label("trustNotice"))
		} else {
			ui.Text(c, label("missing"))
			path := missing.Repository.Path
			ui.TextArea(c.Key("last-path"), &path).ReadOnly(true).FillWidth().Lines(1, 3).Label(label("path"))
		}
		if missing.Checking {
			ui.Progress(c, -1).Label(label("checking"))
		}
		if missing.Error != nil {
			showError(c, *missing.Error)
		}
		ui.Row(c.Key("missing-actions")).Wrap().Gap(8).Children(func() {
			busy := missing.Busy || a.coreMissingPending || !a.ready
			button := func(action, text string, primary bool) {
				var item ui.Element
				if primary {
					item = ui.PrimaryButton(c.Key(action), label(text))
				} else {
					item = ui.Button(c.Key(action), label(text))
				}
				item.Height(40).Disabled(busy || (missing.Checking && action != "remove")).OnClick(func() { a.recoverMissingRepository(action) })
			}
			if missing.UnsafePath != nil {
				button("trust", "trust", true)
			} else {
				button("locate", "locate", true)
				if missing.CanClone {
					button("clone", "clone", false)
				}
				button("check", "check", false)
			}
			button("remove", "remove", false)
		})
	})
	return true
}

func (a *app) recoverMissingRepository(action string) {
	if a.core == nil || a.coreSnapshot == nil || a.coreSnapshot.MissingRepository == nil || a.coreMissingPending || !a.ready {
		return
	}
	missing := a.coreSnapshot.MissingRepository
	if missing.Busy || (missing.Checking && action != "remove") {
		return
	}
	core, ctx, reference := a.core, a.ctx, missing.Repository.KernelRepositoryReference
	a.coreMissingPending = true
	a.runCoreTask(func() (backend.KernelResult, error) {
		return core.RecoverMissingRepository(ctx, reference, action)
	}, func(_ backend.KernelResult, err error) {
		a.coreMissingPending = false
		if a.coreSnapshot.MissingRepository != nil && a.coreSnapshot.MissingRepository.Repository.KernelRepositoryReference == reference {
			a.openErr = err
		}
	})
}

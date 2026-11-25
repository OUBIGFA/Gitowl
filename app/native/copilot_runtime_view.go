package main

import (
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

func (a *app) cancelCopilotRuntime() {
	go func() {
		if err := a.core.PlatformEvent(a.ctx, "copilot-runtime-cancel"); err != nil {
			a.update(func() {
				if !a.closed {
					a.openErr = err
				}
			})
		}
	}()
}

// Keep first-use setup in the existing notice area: repository actions stay
// available and the component download has its own cancellation control.
func renderCopilotRuntimeNotice(c *ui.Context, state *backend.KernelCopilotRuntime, cancel func()) {
	if state == nil {
		return
	}
	label := func(key string) string { return workspaceLabel(c.Locale(), "copilotRuntime."+key) }
	ui.Column(c.Key("copilot-runtime")).FillWidth().Shrink(0).Gap(6).Margin(0, 0, 12, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			text := label(state.Phase)
			if state.Phase == "downloading" && state.Total > 0 {
				text += fmt.Sprintf(" · %.1f / %.1f MiB", float64(state.Received)/(1<<20), float64(state.Total)/(1<<20))
			}
			ui.Text(c, text).FillWidth().MinWidth(0).Role(ui.RoleStatus)
			action := label("cancel")
			if state.Phase == "error" {
				action = label("dismiss")
			}
			ui.Button(c, action).Shrink(0).OnClick(cancel)
		})
		if state.Phase == "error" {
			showError(c, state.Error)
			ui.Text(c, label("retry")).FillWidth().TextColor(c.Theme().TextMuted)
		} else {
			progress := -1.0
			if state.Phase == "downloading" && state.Total > 0 {
				progress = min(1, float64(state.Received)/float64(state.Total))
			}
			ui.Progress(c, progress).FillWidth().Label(label(state.Phase))
		}
	})
}

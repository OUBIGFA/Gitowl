package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// PR previews own their media separately from the workspace selection. A late
// decode cannot attach to a closed popup or to its newly selected file.
func (a *app) prepareNativeDialogDiff(popup *backend.KernelPopupPresentation, form *nativeDialogForm, field backend.KernelDialogField) {
	value := form.values[field.ID]
	data, err := json.Marshal(struct {
		Diff *backend.Diff
		Path string
	}{field.Diff, field.DiffPath})
	if err != nil {
		value.diffErr = err
		return
	}
	key := sha256.Sum256(data)
	value.diffState.setNativeImageDiffType(nativeImageDiffType(field.ImageDiffType))
	if value.diffKey == key {
		return
	}
	value.closeMedia()
	value.diffKey = key
	if field.Diff.Kind != "image" {
		value.diffPreview, value.diffErr = preparePreview(*field.Diff, field.DiffPath)
		return
	}
	s := &value.diffState
	s.imageLoading, s.imageSwipe, s.imageOpacity = true, 0, 1
	ctx, cancel := context.WithCancel(a.ctx)
	s.imageCancel = cancel
	id, diff, update, core := *popup.ID, *field.Diff, a.update, a.core
	go func() {
		prepared, err := prepareNativeImageDiffWithKernel(ctx, diff, core)
		update(func() {
			if a.closed || ctx.Err() != nil || a.coreDialogForms[id] != form || value.diffKey != key {
				prepared.close()
				return
			}
			s.imagePrepared, s.imageError, s.imageLoading = prepared, err, false
		})
	}()
}

func (a *app) renderNativeDialogDiff(c *ui.Context, field backend.KernelDialogField, value *nativeDialogValue) {
	tx := locales.In(c.Locale())
	ui.Column(c.Key("pr-diff")).FillWidth().Height(320).MinWidth(0).Clip().Children(func() {
		if value.diffErr != nil {
			emptyState(c, tx.DiffUnrenderable())
			return
		}
		if field.Diff.Kind == "image" {
			var onMode func(nativeImageDiffType)
			if a.core != nil && a.ready {
				onMode = func(mode nativeImageDiffType) {
					core, ctx := a.core, a.ctx
					a.runCoreTask(func() (backend.KernelResult, error) { return core.ChangeImageDiffType(ctx, int(mode)) }, func(_ backend.KernelResult, err error) { a.openErr = err })
				}
			}
			a.nativeImageDiffView(c, &value.diffState, onMode)
			return
		}
		p := value.diffPreview
		if p == nil {
			emptyState(c, tx.LoadingDiff())
			return
		}
		switch p.kind {
		case "text", "large-text":
			if len(p.patches) == 0 {
				if p.kind == "large-text" {
					emptyState(c, tx.DiffLarge())
				} else {
					emptyState(c, tx.DiffEmpty())
				}
				return
			}
			for i, patch := range p.patches {
				patch.state.Layout = editor.DiffUnified
				if field.SideBySide {
					patch.state.Layout = editor.DiffSplit
				}
				patch.state.Wrap = a.wrap
				if a.coreSnapshot != nil && a.coreSnapshot.Appearance != nil {
					patch.state.TabSize = a.coreSnapshot.Appearance.TabSize
				}
				patch.state.Font = ui.Font{Family: workspaceCodeFont(), Size: c.Theme().FontSize}
				patch.state.Theme = workspaceDiffTheme(c.Theme())
				patch.state.RenderSeparator = workspaceDiffSeparator
				editor.DiffView(c.Key(i), patch.state).Grow(1).MinHeight(0).FillWidth().Label(tx.DiffTitle())
			}
		case "submodule":
			a.nativeSubmoduleView(c, tx, p.submodule, false)
		case "binary":
			emptyState(c, tx.DiffBinary())
		default:
			emptyState(c, tx.DiffUnrenderable())
		}
	})
}

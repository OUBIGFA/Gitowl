package main

import (
	"context"
	"crypto/sha256"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

func (a *app) nativeDialogFormFor(popup *backend.KernelPopupPresentation) *nativeDialogForm {
	if a.coreDialogForms == nil {
		a.coreDialogForms = make(map[int]*nativeDialogForm)
	}
	form := a.coreDialogForms[*popup.ID]
	if form == nil {
		if a.preferencesGeometry == nil {
			a.preferencesGeometry = make(preferenceGeometryCache)
		}
		form = &nativeDialogForm{renderDiff: a.renderNativeDialogDiff, geometryCache: a.preferencesGeometry}
		a.coreDialogForms[*popup.ID] = form
	}
	return form
}

func (value *nativeDialogValue) closeMedia() {
	if value.imageCancel != nil {
		value.imageCancel()
		value.imageCancel = nil
	}
	if value.imageMedia != nil && value.imageMedia.player != nil {
		value.imageMedia.player.Close()
	}
	value.imageMedia, value.image = nil, nil
	value.imageKey, value.imageLoading, value.imageErr = [32]byte{}, false, nil
	value.diffState.clearNativeImageDiff()
	value.diffPreview, value.diffErr, value.diffKey = nil, nil, [32]byte{}
}

// Decode media once as the original snapshot arrives, outside the view pass.
// A removed/changed popup cannot acquire an old decoder's bitmap or UI update.
func (a *app) prepareNativeDialogMedia(snapshot backend.KernelSnapshot) {
	popup := snapshot.CurrentPopup
	if popup == nil || popup.Layout != "form" || popup.ID == nil {
		return
	}
	form := a.nativeDialogFormFor(popup)
	form.sync(popup)
	for _, field := range popup.Fields {
		if field.Kind == "diff" && field.Diff != nil {
			a.prepareNativeDialogDiff(popup, form, field)
			continue
		}
		if field.Kind != "image" || field.Image == nil {
			continue
		}
		value := form.values[field.ID]
		hash := sha256.Sum256(field.Image.Contents)
		if field.Image.MediaID != "" {
			hash = sha256.Sum256([]byte(field.Image.MediaID))
		}
		if value.imageKey == hash {
			continue
		}
		value.closeMedia()
		value.imageKey, value.imageLoading, value.imageErr = hash, true, nil
		ctx, cancel := context.WithCancel(a.ctx)
		value.imageCancel = cancel
		blob, id, update, core := field.Image, *popup.ID, a.update, a.core
		go func() {
			var image *nativeImageDiffMedia
			var err error
			if blob.MediaID != "" {
				image, err = prepareNativeKernelImageMedia(ctx, core, blob)
			} else {
				image, err = prepareNativeImageDiffMedia(ctx, blob)
			}
			update(func() {
				if a.closed || ctx.Err() != nil || a.coreDialogForms[id] != form || value.imageKey != hash {
					if image != nil && image.player != nil {
						image.player.Close()
					}
					return
				}
				value.imageMedia, value.imageErr, value.imageLoading = image, err, false
				if image != nil {
					value.image = image.source
				}
			})
		}()
	}
}

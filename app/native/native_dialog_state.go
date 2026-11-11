package main

import "github.com/OUBIGFA/Gitowl/app/native/internal/backend"

func (a *app) pruneNativeDialogForms(snapshot backend.KernelSnapshot) {
	present := make(map[int]bool, len(snapshot.Popups))
	for _, popup := range snapshot.Popups {
		if popup.ID != nil {
			present[*popup.ID] = true
		}
	}
	for id, form := range a.coreDialogForms {
		if !present[id] {
			if form.onRemoved != nil {
				form.onRemoved()
			}
			for _, value := range form.values {
				value.text = ""
				value.closeMedia()
			}
			delete(a.coreDialogForms, id)
		}
	}
}

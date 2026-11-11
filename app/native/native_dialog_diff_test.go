package main

import (
	"context"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui/editor"
)

// A PR preview must use a read-only diff field, so additions/removals remain
// distinguishable and the original side-by-side preference remains effective.
func TestNativeDialogAcceptsReadOnlyDiffAndRejectsEditablePayload(t *testing.T) {
	patch := "@@ -1 +1 @@\n-before\n+after\n"
	popup := dialogFixture()
	popup.Fields = []backend.KernelDialogField{{ID: "diff", LabelKey: "aux.diff", Kind: "diff", ReadOnly: true, DiffPath: "file.txt", SideBySide: true, Diff: &backend.Diff{Kind: "text", Text: &patch}}}
	popup.Actions = nil
	if !nativeDialogValid(popup) {
		t.Fatal("original PR diff is rejected by the native dialog")
	}
	a := &app{ctx: context.Background(), update: func(f func()) { f() }}
	a.prepareNativeDialogMedia(backend.KernelSnapshot{CurrentPopup: popup})
	form := a.nativeDialogFormFor(popup)
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 1000, 720, &replies, &closes)
	if box, ok := tt.Find(locales.In("en").DiffTitle()); !ok || box.H < 100 {
		t.Fatalf("PR patch editor missing or clipped: %v %v %q", box, ok, tt.Texts())
	}
	if form.values["diff"].diffPreview.patches[0].state.Layout != editor.DiffSplit {
		t.Fatal("PR ignored side-by-side preference")
	}
	popup.Fields[0].SideBySide = false
	tt.Frame()
	if form.values["diff"].diffPreview.patches[0].state.Layout != editor.DiffUnified {
		t.Fatal("PR layout did not follow the original option")
	}
	popup.Fields[0].ReadOnly = false
	if nativeDialogValid(popup) {
		t.Fatal("editable diff accepted as dialog input")
	}
	popup.Fields[0].ReadOnly = true
	popup.Fields[0].Diff = nil
	if nativeDialogValid(popup) {
		t.Fatal("missing diff accepted")
	}
}

func TestNativeDialogImageDiffFollowsOptionsAndRejectsClosedDecode(t *testing.T) {
	updates := make(chan func(), 8)
	a := &app{ctx: context.Background(), update: func(f func()) { updates <- f }}
	popup := dialogFixture()
	diff := nativeImageTestDiff(t, 4, 3)
	popup.Fields = []backend.KernelDialogField{{ID: "diff", LabelKey: "aux.diff", Kind: "diff", ReadOnly: true, Diff: &diff, ImageDiffType: int(nativeImageOnionSkin)}}
	popup.Actions = nil
	snapshot := backend.KernelSnapshot{CurrentPopup: popup, Popups: []backend.KernelPopup{{ID: popup.ID}}}
	a.prepareNativeDialogMedia(snapshot)
	form := a.nativeDialogFormFor(popup)
	value := form.values["diff"]
	flush := func() {
		t.Helper()
		select {
		case f := <-updates:
			f()
		case <-time.After(5 * time.Second):
			t.Fatal("PR media decode did not finish")
		}
	}
	flush()
	if value.diffState.imageOpacity != 1 { t.Fatal("PR lost the original initial opacity") }
	if value.diffState.imageError != nil || value.diffState.imagePrepared == nil {
		t.Fatalf("PR image not decoded: %v", value.diffState.imageError)
	}
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 1000, 720, &replies, &closes)
	if !tt.HasText(nativeImageLabel("en", "opacity")) {
		t.Fatal("PR image ignored original onion-skin option")
	}
	popup.Fields[0].ImageDiffType = int(nativeImageSwipe)
	a.prepareNativeDialogMedia(snapshot)
	tt.Frame()
	if !tt.HasText(nativeImageLabel("en", "swipePosition")) {
		t.Fatal("PR image did not follow changed mode")
	}
	// Decode another file, then close its popup before the UI callback runs.
	diff2 := nativeImageTestDiff(t, 7, 5)
	popup.Fields[0].Diff = &diff2
	a.prepareNativeDialogMedia(snapshot)
	a.pruneNativeDialogForms(backend.KernelSnapshot{})
	flush()
	if value.diffState.imagePrepared != nil || value.diffState.imageCancel != nil {
		t.Fatal("closed PR acquired a stale image or retained its worker")
	}
}

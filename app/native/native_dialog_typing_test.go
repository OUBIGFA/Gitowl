package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func TestNativeDialogDebouncesTypingWithoutBusyLockOrStaleOverwrite(t *testing.T) {
	popup, form := dialogFixture(), &nativeDialogForm{}
	popup.Fields[0].ChangeAction = "change"
	popup.Actions = append(popup.Actions, backend.KernelDialogAction{ID: "change", LabelKey: "settings.change", Fields: []string{"name", "format"}})
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 900, 620, &replies, &closes)
	focus := func(id, label string) {
		t.Helper()
		f, ok := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel("en", label))
		if !ok {
			t.Fatal(id)
		}
		tt.ClickAt(f.Bounds.X+f.Bounds.W/2, f.Bounds.Y+f.Bounds.H/2)
	}
	focus("secret", "settings.secret")
	tt.Type("unsent-secret")
	focus("name", "settings.name")
	tt.Key(ui.Cmd, ui.KeyA)
	for _, r := range "abc" {
		tt.Type(string(r))
		tt.Advance(30 * time.Millisecond)
	}
	if len(replies) != 0 {
		t.Fatal("validation was sent for each keystroke")
	}
	tt.Advance(200 * time.Millisecond)
	if len(replies) != 1 || !form.changePending || form.busy {
		t.Fatal("pure validation acquired the operation busy lock")
	}
	if _, present := replies[0].Values["secret"]; present {
		t.Fatal("validation submitted an unrelated secret")
	}
	for _, r := range "def" {
		tt.Type(string(r))
		tt.Advance(30 * time.Millisecond)
	}
	field, _ := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel("en", "settings.name"))
	cancel, _ := tt.AccessByRole(ui.RoleButton, nativeDialogLabel("en", "settings.cancel"))
	save, _ := tt.AccessByRole(ui.RoleButton, nativeDialogLabel("en", "settings.save"))
	if field.Disabled || !field.Focused || field.Value != "abcdef" || cancel.Disabled || !save.Disabled {
		t.Fatalf("held validation lost editing/cancel/latest-validation gate: %+v", field)
	}
	popup.Fields[0].Value = "abc" // Actual old server echo arrives after more keys.
	form.finishReply(true, nil)
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	field, _ = tt.AccessByRole(ui.RoleTextField, nativeDialogLabel("en", "settings.name"))
	if field.Value != "abcdef" || len(replies) != 2 || replies[1].Values["name"] != "abcdef" || form.values["secret"].text != "unsent-secret" {
		t.Fatal("old validation overwrote newer input or unsent secret")
	}
	tt.Key(0, ui.KeyEscape)
	if len(closes) != 1 || !form.busy {
		t.Fatal("Escape could not cancel while validation was held")
	}
	form.finishReply(true, nil)
	tt.Advance(time.Second)
	if !form.busy || len(replies) != 2 {
		t.Fatal("late validation released or replayed the canceled form")
	}
}

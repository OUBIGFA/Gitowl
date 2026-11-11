package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func popupTester(t *testing.T, popup *backend.KernelPopupPresentation, locale string) (*ui.Tester, *kernelPopupForm, *[]backend.KernelPopupReply, *[]int) {
	t.Helper()
	form := &kernelPopupForm{}
	replies, closes := &[]backend.KernelPopupReply{}, &[]int{}
	tt := ui.NewTester(func(c *ui.Context) {
		renderKernelPopup(c, locales.In(c.Locale()), popup, form, func(reply backend.KernelPopupReply) { *replies = append(*replies, reply) }, func(id int) { *closes = append(*closes, id) })
	}, 900, 620)
	tt.SetLocale(locale)
	tt.Advance(time.Second)
	return tt, form, replies, closes
}

func TestNativeSSHHostRequiresAnExplicitDecision(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		for _, trust := range []bool{false, true} {
			t.Run(locale+map[bool]string{false: "-reject", true: "-trust"}[trust], func(t *testing.T) {
				id := 4
				popup := &backend.KernelPopupPresentation{ID: &id, Type: "AddSSHHost", Supported: true, Host: "git.example.invalid", IP: "192.0.2.10", KeyType: "ed25519", Fingerprint: "SHA256:fixture"}
				tt, _, replies, _ := popupTester(t, popup, locale)
				tx := locales.In(locale)
				label := tx.PopupRejectHost()
				if trust {
					label = tx.PopupTrustHost()
				}
				if len(*replies) != 0 {
					t.Fatal("the host was trusted without a user action")
				}
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				if len(*replies) != 1 || (*replies)[0].AddHost == nil || *(*replies)[0].AddHost != trust || (*replies)[0].PopupID != id {
					t.Fatalf("wrong original host decision: %+v", *replies)
				}
			})
		}
	}
}

func TestNativeSSHSecretClearsOnCancelWithoutEnteringSnapshots(t *testing.T) {
	id := 7
	popup := &backend.KernelPopupPresentation{ID: &id, Type: "SSHKeyPassphrase", Supported: true, KeyPath: "fixture/id_ed25519"}
	tt, form, replies, closes := popupTester(t, popup, "zh-Hans")
	tx := locales.In("zh-Hans")
	if err := tt.Click(tx.PopupPassphrase()); err != nil {
		t.Fatal(err)
	}
	tt.Type("UI-SECRET-SENTINEL")
	if form.secret != "UI-SECRET-SENTINEL" {
		t.Fatal("the actual native password input did not retain the temporary entry")
	}
	data, err := json.Marshal(popup)
	if err != nil || strings.Contains(string(data), "UI-SECRET-SENTINEL") {
		t.Fatal("a password entered the original view DTO")
	}
	if err := tt.Click(tx.Cancel()); err != nil {
		t.Fatal(err)
	}
	if form.secret != "" || len(*replies) != 0 || len(*closes) != 1 || (*closes)[0] != id {
		t.Fatal("cancel did not clear the entry and request the original cancel continuation")
	}
}

func TestNativeGenericAuthenticationKeepsTheOriginalFixedUsername(t *testing.T) {
	id, username := 8, "locked-user"
	popup := &backend.KernelPopupPresentation{ID: &id, Type: "GenericGitAuthentication", Supported: true, RemoteURL: "https://git.example.invalid", Username: &username}
	tt, _, replies, _ := popupTester(t, popup, "en")
	tx := locales.In("en")
	field, ok := tt.AccessByRole(ui.RoleTextField, tx.PopupUsername())
	if !ok || !field.ReadOnly || field.Value != username {
		t.Fatalf("the kernel's fixed username is editable or lost: %+v", field)
	}
	if err := tt.Click(tx.PopupPassword()); err != nil {
		t.Fatal(err)
	}
	tt.Type("secret")
	if err := tt.Click(tx.PopupAuthenticate()); err != nil {
		t.Fatal(err)
	}
	if len(*replies) != 1 || (*replies)[0].Username == nil || *(*replies)[0].Username != username || (*replies)[0].Password == nil || *(*replies)[0].Password != "secret" {
		t.Fatalf("wrong generic authentication reply: %+v", *replies)
	}
}

func TestNativeAuthenticationParametersNeverRequirePageScrolling(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		id := 8
		popup := &backend.KernelPopupPresentation{ID: &id, Type: "GenericGitAuthentication", Supported: true, RemoteURL: "https://git.example.invalid/owner/project"}
		tt, _, _, _ := popupTester(t, popup, locale)
		if _, found := tt.AccessByRole(ui.RoleScroll, ""); found {
			t.Fatalf("%s: authentication parameters still sit in a scrolling page", locale)
		}
		tx := locales.In(locale)
		for _, label := range []string{tx.PopupUsername(), tx.PopupPassword(), tx.PopupAuthenticate(), tx.Cancel()} {
			visibleControl(t, tt, label, 900, 620)
		}
	}
}

func TestNativeAccountCollectionKeepsDecisionsVisible(t *testing.T) {
	id := 9
	popup := &backend.KernelPopupPresentation{ID: &id, Type: "RepositoryAccount", Supported: true, Endpoint: "https://api.github.com", RepositoryName: "owner/project"}
	for i := 0; i < 50; i++ {
		popup.Accounts = append(popup.Accounts, backend.KernelAccountIdentity{Endpoint: popup.Endpoint, Login: fmt.Sprintf("account-%d", i)})
	}
	tt, _, _, _ := popupTester(t, popup, "en")
	for _, label := range []string{locales.In("en").PopupUseAccount(), locales.In("en").Cancel()} {
		visibleControl(t, tt, label, 900, 620)
	}
}

func TestNativeUnsupportedPopupCannotSendAPretendOutcome(t *testing.T) {
	id := 9
	popup := &backend.KernelPopupPresentation{ID: &id, Type: "UnknownAuthors", Supported: false}
	tt, form, replies, closes := popupTester(t, popup, "zh-Hans")
	if !tt.HasText(locales.In("zh-Hans").PopupUnsupported()) {
		t.Fatal("the unsupported original dialog was hidden")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(time.Second)
	if len(*replies) != 0 || len(*closes) != 0 || form.secret != "" {
		t.Fatal("an unsupported original continuation was fabricated")
	}
}

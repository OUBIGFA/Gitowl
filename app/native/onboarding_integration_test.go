package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// Unlike the established repository-flow tests, this starts the prepared
// production kernel in a brand-new profile with no has-shown-welcome-flow seed.
func TestOriginalKernelFreshProfileCompletesNativeWelcome(t *testing.T) {
	runtime := os.Getenv("GITOWL_KERNEL_FIRST_RUN_RUNTIME")
	if runtime == "" {
		t.Skip("set GITOWL_KERNEL_FIRST_RUN_RUNTIME to the verified production desktop-kernel directory")
	}
	for _, locale := range []string{"en", "zh-Hans"} {
		t.Run(locale, func(t *testing.T) {
			scratch, err := filepath.Abs("../../_temp/native-first-run")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(scratch, 0o755); err != nil {
				t.Fatal(err)
			}
			work, err := os.MkdirTemp(scratch, "case-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(work)
			global := filepath.Join(work, "global-config")
			t.Setenv("GIT_CONFIG_GLOBAL", global)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("TEST_ENV", "1")
			entry := filepath.Join(runtime, "app", "main.js")
			updates := make(chan func(), 1000)
			a := newCoreApp(nil, nil)
			a.update = func(fn func()) { updates <- fn }
			core, err := backend.StartKernel(backend.KernelConfig{Electron: filepath.Join(runtime, "electron.exe"), Entry: entry, UserData: filepath.Join(work, "profile")}, func(event backend.KernelEvent) {
				updates <- func() { a.receiveCoreEvent(event) }
			})
			if err != nil {
				t.Fatal(err)
			}
			a.core = core
			defer func() {
				a.close()
				if err := core.Close(); err != nil {
					t.Error(err)
				}
			}()
			tt := ui.NewTester(a.view, 900, 620)
			tt.SetLocale(locale)
			tx := locales.In(locale)
			await := func(done func() bool) {
				t.Helper()
				deadline := time.NewTimer(30 * time.Second)
				defer deadline.Stop()
				for !done() {
					select {
					case fn := <-updates:
						fn()
						tt.Frame()
					case <-core.Done():
						t.Fatalf("fresh production kernel disconnected: %v", core.Err())
					case <-deadline.C:
						t.Fatalf("native Welcome did not settle: %+v", a.coreSnapshot)
					}
				}
				tt.Frame()
				tt.Advance(time.Second)
			}
			a.connect("")
			await(func() bool { return a.corePending == 0 && a.coreSnapshot != nil && a.coreSnapshot.Onboarding != nil })
			if !a.coreSnapshot.Welcome || a.ready || a.coreSnapshot.Onboarding.Step != "Start" {
				t.Fatal("a fresh user profile bypassed first-run setup")
			}
			if !tt.HasText(tx.OnboardingTitle()) {
				t.Fatal("the actual MyGo Welcome page was not shown")
			}
			if err := tt.Click(tx.OnboardingSkip()); err != nil {
				t.Fatal(err)
			}
			await(func() bool {
				return a.corePending == 0 && a.coreSnapshot.Onboarding.Step == "ConfigureGit" && !a.coreSnapshot.Onboarding.Loading
			})
			if err := tt.Click(tx.OnboardingName()); err != nil {
				t.Fatal(err)
			}
			tt.Type("Native First Run")
			if err := tt.Click(tx.OnboardingEmail()); err != nil {
				t.Fatal(err)
			}
			tt.Type("first-run@example.invalid")
			if err := tt.Click(tx.OnboardingFinish()); err != nil {
				t.Fatal(err)
			}
			await(func() bool { return a.corePending == 0 && !a.coreSnapshot.Welcome && a.ready })
			if a.coreSnapshot.Onboarding != nil {
				t.Fatal("the native UI kept an independent completed Welcome state")
			}
			git := filepath.Join(runtime, "app", "git", "cmd", "git.exe")
			for key, value := range map[string]string{"user.name": "Native First Run", "user.email": "first-run@example.invalid"} {
				cmd := exec.Command(git, "config", "--file", global, "--get", key)
				out, err := cmd.CombinedOutput()
				if err != nil || strings.TrimSpace(string(out)) != value {
					t.Fatalf("the original config save was not used: %s %v %s", key, err, out)
				}
			}
		})
	}
}

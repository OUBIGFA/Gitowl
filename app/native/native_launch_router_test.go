package main

import (
	"path/filepath"
	"testing"
)

func TestNativeLaunchRouterDeliversOAuthOnce(t *testing.T) {
	for _, scheme := range []string{"gitowl-auth", "x-github-desktop-dev-auth", "x-github-desktop-auth", "x-github-client"} {
		callback := scheme + "://oauth?code=a%2Bb&state=x%2Fy"
		for _, automatic := range []bool{true, false} {
			for _, beforeReady := range []bool{true, false} {
				var calls []nativeLaunchOptions
				router := nativeLaunchRouter{dispatch: func(options nativeLaunchOptions) { calls = append(calls, options) }}
				if automatic {
					router.automaticSchemes = []string{scheme}
				}
				if !beforeReady {
					router.start(nativeLaunchOptions{})
				}
				// MyGo delivers only tracked schemes after OnSecondInstance.
				if err := router.secondInstance([]string{callback}, t.TempDir()); err != nil {
					t.Fatal(err)
				}
				if automatic {
					router.openURL(callback)
				}
				if beforeReady {
					if len(calls) != 0 {
						t.Fatal("dispatched before the kernel exists")
					}
					router.start(nativeLaunchOptions{})
				}
				if len(calls) != 1 || calls[0].protocolURL != callback {
					t.Fatalf("duplicate/lost OAuth: auto=%v calls=%+v", automatic, calls)
				}
			}
			var initial []nativeLaunchOptions
			router := nativeLaunchRouter{dispatch: func(options nativeLaunchOptions) { initial = append(initial, options) }}
			if automatic {
				router.automaticSchemes = []string{scheme}
			}
			options, err := parseNativeLaunchOptions([]string{"--protocol-launcher", callback}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			router.start(options)
			if automatic {
				router.openURL(callback)
			}
			router.start(options) // start is idempotent; OAuth content is not deduplicated.
			if len(initial) != 1 || initial[0].protocolURL != callback {
				t.Fatalf("initial callback: auto=%v calls=%+v", automatic, initial)
			}
		}
	}
}

func TestNativeLaunchRouterKeepsCLIPathBranchAndOrder(t *testing.T) {
	var calls []nativeLaunchOptions
	router := nativeLaunchRouter{dispatch: func(options nativeLaunchOptions) { calls = append(calls, options) }}
	dir := t.TempDir()
	if err := router.secondInstance([]string{"repo with spaces"}, dir); err != nil {
		t.Fatal(err)
	}
	if err := router.secondInstance([]string{"--cli-clone=https://github.com/example/project", "--cli-branch=feature/one"}, dir); err != nil {
		t.Fatal(err)
	}
	router.start(nativeLaunchOptions{openPath: filepath.Join(dir, "initial")})
	if len(calls) != 3 || calls[0].openPath != filepath.Join(dir, "initial") || calls[1].openPath != filepath.Join(dir, "repo with spaces") || calls[2].branch != "feature/one" {
		t.Fatalf("launch queue: %+v", calls)
	}
}

func TestNativeProtocolLaunchCannotSmuggleCLIAction(t *testing.T) {
	options, err := parseNativeLaunchOptions([]string{"--protocol-launcher", "--cli-open=do-not-open", "--cli-clone=do-not-clone", "gitowl-auth://oauth?code=a&state=b"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if options.openPath != "" || options.cloneURL != "" || options.branch != "" {
		t.Fatalf("smuggled CLI launch: %+v", options)
	}
	for _, args := range [][]string{{"--protocol-launcher"}, {"--cli-open"}, {"one", "two"}} {
		if _, err := parseNativeLaunchOptions(args, t.TempDir()); err == nil {
			t.Fatalf("accepted invalid launch: %q", args)
		}
	}
}

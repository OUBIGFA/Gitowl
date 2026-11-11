package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNativeOAuthRegistersOnlyTheOriginalCompiledCallback(t *testing.T) {
	for _, test := range []struct{ metadata, expected string }{
		{"", upstreamDevelopmentOAuthScheme},
		{`{"oauthCallbackScheme":"x-github-desktop-dev-auth"}`, upstreamDevelopmentOAuthScheme},
		{`{"oauthCallbackScheme":"x-github-desktop-auth"}`, upstreamConfiguredOAuthScheme},
	} {
		dir := t.TempDir()
		if test.metadata != "" {
			if err := os.WriteFile(filepath.Join(dir, "native-launch.json"), []byte(test.metadata), 0600); err != nil {
				t.Fatal(err)
			}
		}
		// Registry boundary only. Never alter the real user's installed handlers.
		handlers := map[string]string{upstreamConfiguredOAuthScheme: "Installed Desktop", upstreamDevelopmentOAuthScheme: "Installed Desktop Dev", "x-github-client": "Installed Desktop"}
		var calls []string
		scheme, err := registerNativeOAuthCallback(filepath.Join(dir, "main.js"), func(scheme string) error {
			calls = append(calls, scheme)
			handlers[scheme] = "Gitowl"
			return nil
		})
		if err != nil || scheme != test.expected || !reflect.DeepEqual(calls, []string{test.expected}) {
			t.Fatalf("registration: scheme=%q calls=%q err=%v", scheme, calls, err)
		}
		if test.expected == upstreamDevelopmentOAuthScheme && handlers[upstreamConfiguredOAuthScheme] != "Installed Desktop" {
			t.Fatal("default dev OAuth overwrote stable Desktop handler")
		}
		if test.expected == upstreamConfiguredOAuthScheme && handlers[upstreamDevelopmentOAuthScheme] != "Installed Desktop Dev" {
			t.Fatal("configured OAuth overwrote unrelated dev handler")
		}
		if handlers["x-github-client"] != "Installed Desktop" {
			t.Fatal("raw URL support must not claim Desktop repository link handler")
		}
	}
}

func TestNativeOAuthMetadataAndRegistrationFailuresAreReported(t *testing.T) {
	dir := t.TempDir()
	for _, data := range []string{`not JSON`, `{"oauthCallbackScheme":"https"}`} {
		if err := os.WriteFile(filepath.Join(dir, "native-launch.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := registerNativeOAuthCallback(filepath.Join(dir, "main.js"), func(string) error { t.Fatal("invalid metadata reached OS registration"); return nil })
		if err == nil {
			t.Fatal("invalid callback metadata was ignored")
		}
	}
	if err := os.Remove(filepath.Join(dir, "native-launch.json")); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("registration refused")
	scheme, err := registerNativeOAuthCallback(filepath.Join(dir, "main.js"), func(string) error { return failure })
	if scheme != upstreamDevelopmentOAuthScheme || !errors.Is(err, failure) {
		t.Fatalf("registration failure lost: %q %v", scheme, err)
	}
}

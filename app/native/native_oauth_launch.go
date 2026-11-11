package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const upstreamDevelopmentOAuthScheme = "x-github-desktop-dev-auth"
const upstreamConfiguredOAuthScheme = "x-github-desktop-auth"

// This is launch metadata from the original compiler, not a second OAuth
// implementation. An unpackaged development kernel uses the upstream default
// development callback. Packaged kernels record the actual __DEV_SECRETS__
// choice beside main.js; no client secret or user token is copied here.
func registerNativeOAuthCallback(entry string, register func(string) error) (string, error) {
	scheme := upstreamDevelopmentOAuthScheme
	data, err := os.ReadFile(filepath.Join(filepath.Dir(entry), "native-launch.json"))
	if err == nil {
		var config struct {
			OAuthCallbackScheme string `json:"oauthCallbackScheme"`
		}
		if err := json.Unmarshal(data, &config); err != nil {
			return "", fmt.Errorf("read original OAuth callback metadata: %w", err)
		}
		scheme = config.OAuthCallbackScheme
		if scheme != upstreamDevelopmentOAuthScheme && scheme != upstreamConfiguredOAuthScheme {
			return "", errors.New("unrecognized original OAuth callback scheme")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read original OAuth callback metadata: %w", err)
	}
	// A default development client must never claim the installed Desktop's
	// stable handler. Register only the callback this kernel actually uses.
	// MyGo tracks this scheme for argument delivery even if OS registration fails.
	return scheme, register(scheme)
}

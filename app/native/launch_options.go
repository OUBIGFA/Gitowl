package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type nativeLaunchOptions struct {
	entry, electron, profile                string
	openPath, cloneURL, branch, protocolURL string
}

// Only launcher arguments are decoded here. Git/clone/checkout/OAuth semantics
// are forwarded to the original Desktop Dispatcher and parseAppURL.
func parseNativeLaunchOptions(args []string, workingDirectory string) (nativeLaunchOptions, error) {
	var options nativeLaunchOptions
	flags := flag.NewFlagSet("gitowl", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.entry, "kernel", "", "original Desktop app entry")
	flags.StringVar(&options.electron, "electron", "", "original Electron executable")
	flags.StringVar(&options.profile, "user-data", "", "isolated development profile")
	flags.StringVar(&options.openPath, "cli-open", "", "original open-repository action")
	flags.StringVar(&options.cloneURL, "cli-clone", "", "original clone-url action")
	flags.StringVar(&options.branch, "cli-branch", "", "original clone branch")
	protocol := flags.Bool("protocol-launcher", false, "URL scheme launch")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	positional := flags.Args()
	if *protocol {
		if len(positional) != 1 {
			return options, fmt.Errorf("a protocol launch requires exactly one application URL")
		}
		options.protocolURL = positional[0]
		options.openPath, options.cloneURL, options.branch = "", "", ""
	} else {
		if len(positional) > 1 {
			return options, fmt.Errorf("only one repository path is allowed")
		}
		if len(positional) == 1 {
			value := positional[0]
			if supportedApplicationURL(value) {
				options.protocolURL = value
			} else if options.openPath == "" {
				options.openPath = value
			}
		}
	}
	if options.openPath != "" && !filepath.IsAbs(options.openPath) {
		options.openPath = filepath.Join(workingDirectory, options.openPath)
	}
	return options, nil
}

// Accept original Desktop deep links without making this app their global OS
// handler. Only the selected OAuth callback is registered in main.go.
func supportedApplicationURL(raw string) bool {
	scheme, _, ok := strings.Cut(strings.ToLower(raw), "://")
	if !ok {
		return false
	}
	switch scheme {
	case "gitowl", "gitowl-auth", "x-github-client", "x-github-desktop-dev-auth", "x-github-desktop-auth", "github-windows", "github-mac":
		return true
	default:
		return false
	}
}

// MyGo supplies argv[1:], unlike Electron. Keep the first forwarded argument.
func parseSecondInstanceArguments(args []string, workingDirectory string) (nativeLaunchOptions, error) {
	options, err := parseNativeLaunchOptions(args, workingDirectory)
	if err != nil {
		return options, fmt.Errorf("parse second-instance launch: %w", err)
	}
	return options, nil
}

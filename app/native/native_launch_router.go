package main

import (
	"slices"
	"strings"
)

// MyGo calls OnSecondInstance and then delivers URLs to OnOpenURL (v0.8.1
// single_instance.go/deeplink.go). This coordinator owns timing only: business
// actions still go through routeCoreLaunch and the original Dispatcher.
type nativeLaunchRouter struct {
	dispatch func(nativeLaunchOptions)
	// MyGo delivers only packaged or RegisterURLScheme-tracked schemes.
	// Other accepted raw Desktop URLs must be forwarded explicitly once.
	automaticSchemes []string
	started          bool
	pending          []nativeLaunchOptions
}

func (r *nativeLaunchRouter) route(options nativeLaunchOptions) {
	if !r.started {
		r.pending = append(r.pending, options)
		return
	}
	r.dispatch(options)
}

func (r *nativeLaunchRouter) secondInstance(arguments []string, workingDirectory string) error {
	options, err := parseSecondInstanceArguments(arguments, workingDirectory)
	if err != nil {
		return err
	}
	if r.explicitlyDelivered(options) {
		r.route(options)
	}
	return nil
}

func (r *nativeLaunchRouter) explicitlyDelivered(options nativeLaunchOptions) bool {
	if options.protocolURL != "" {
		scheme, _, _ := strings.Cut(strings.ToLower(options.protocolURL), ":")
		return !slices.Contains(r.automaticSchemes, scheme)
	}
	return options.openPath != "" || options.cloneURL != ""
}

func (r *nativeLaunchRouter) openURL(url string) {
	r.route(nativeLaunchOptions{protocolURL: url})
}

func (r *nativeLaunchRouter) start(initial nativeLaunchOptions) {
	if r.started {
		return
	}
	r.started = true
	if r.explicitlyDelivered(initial) {
		r.dispatch(initial)
	}
	for _, options := range r.pending {
		r.dispatch(options)
	}
	r.pending = nil
}

package main

import (
	"errors"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

// routeCoreLaunch forwards OS and CLI launches to the original Desktop
// Dispatcher. MyGo's second-instance arguments exclude argv[0], so callers must
// preserve every item and let the shared option parser identify URLs/flags.
func (a *app) routeCoreLaunch(options nativeLaunchOptions) {
	if a.core == nil {
		a.openErr = errors.New("the original Desktop kernel is not running")
		return
	}
	if options.protocolURL != "" {
		if !supportedApplicationURL(options.protocolURL) {
			a.openErr = errors.New("unsupported application URL")
			return
		}
		core, ctx := a.core, a.ctx
		a.queueCore(nativeOperation{
			call: func() (backend.KernelResult, error) {
				return core.DispatchApplicationURL(ctx, options.protocolURL)
			},
			complete: func(_ backend.KernelResult, err error) { a.openErr = err },
		})
		return
	}
	if options.cloneURL != "" {
		core, ctx, url, branch := a.core, a.ctx, options.cloneURL, options.branch
		a.queueCore(nativeOperation{
			call: func() (backend.KernelResult, error) {
				return core.CloneFromCLI(ctx, url, branch)
			},
			complete: func(_ backend.KernelResult, err error) { a.openErr = err },
		})
		return
	}
	if options.openPath != "" {
		core, ctx, path := a.core, a.ctx, options.openPath
		a.queueCore(nativeOperation{
			call: func() (backend.KernelResult, error) {
				return core.OpenFromCLI(ctx, path)
			},
			complete: func(_ backend.KernelResult, err error) { a.openErr = err },
		})
	}
}

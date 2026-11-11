//go:build e2e

package main

// The official native UI automation bridge is test-only. Release builds must
// never expose its socket or a generic script/desktop-control surface.
import _ "github.com/egoist/mygo/plugins/automation"

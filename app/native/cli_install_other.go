//go:build !windows

package main

import "errors"

func installNativeCLI(remove bool) error {
	return errors.New("native CLI registration is provided by the platform package")
}

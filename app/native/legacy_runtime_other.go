//go:build !windows

package main

import "path/filepath"

func physicalDirectoryPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

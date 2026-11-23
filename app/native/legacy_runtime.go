package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// Cleanup runs after the first backend snapshot. A failure belongs to the
// upgrade and must not replace the result of opening a repository.
func (a *app) recycleLegacyRuntime(resources, userData string) {
	path, err := legacyRuntimeDirectory(resources, userData)
	if err == nil && path != "" {
		err = mygo.Shell.TrashItem(path)
	}
	if err != nil {
		a.update(func() {
			if !a.closed {
				a.legacyCleanupErr = err
			}
		})
	}
}

// Only the old shipped runtime directory is eligible. User-data paths and
// unexpected directory layouts are never moved by the upgrade cleanup.
func legacyRuntimeDirectory(resources, userData string) (string, error) {
	root, err := physicalDirectoryPath(resources)
	if err != nil {
		return "", err
	}
	old := filepath.Join(root, "desktop-kernel")
	info, err := os.Lstat(old)
	if os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	// A junction in a parent is fine, but never recycle a link at the target.
	if !info.IsDir() {
		return "", errors.New("legacy runtime is not an ordinary directory; files were preserved")
	}
	data, err := physicalDirectoryPath(userData)
	if err != nil {
		return "", err
	}
	for _, pair := range [][2]string{{old, data}, {data, old}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", errors.New("legacy runtime overlaps application data")
		}
	}
	for _, file := range []string{"electron.exe", filepath.Join("app", "main.js")} {
		info, err := os.Stat(filepath.Join(old, file))
		if err != nil || !info.Mode().IsRegular() {
			return "", errors.New("unexpected legacy runtime layout; files were preserved")
		}
	}
	return old, nil
}

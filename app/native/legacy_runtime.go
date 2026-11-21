package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Only the old shipped runtime directory is eligible. User-data paths and
// unexpected directory layouts are never moved by the upgrade cleanup.
func legacyRuntimeDirectory(resources, userData string) (string, error) {
	root, err := filepath.EvalSymlinks(resources)
	if err != nil {
		return "", err
	}
	old := filepath.Join(root, "desktop-kernel")
	if _, err := os.Stat(old); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(old)
	if err != nil {
		return "", err
	}
	if resolved != old {
		return "", errors.New("legacy runtime path is redirected; files were preserved")
	}
	data, err := filepath.EvalSymlinks(userData)
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

package main

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// Ask Windows for the directory reached by the open handle. Walking path
// components with EvalSymlinks does not reliably resolve directory junctions.
func physicalDirectoryPath(path string) (string, error) {
	dir, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	buf := make([]uint16, 260)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(dir.Fd()), &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", &os.PathError{Op: "resolve directory", Path: path, Err: err}
		}
		if n >= uint32(len(buf)) {
			buf = make([]uint16, n+1)
			continue
		}
		// The shell takes ordinary drive and UNC paths for recycling.
		final := windows.UTF16ToString(buf[:n])
		if share, ok := strings.CutPrefix(final, `\\?\UNC\`); ok {
			return `\\` + share, nil
		}
		if drive, ok := strings.CutPrefix(final, `\\?\`); ok && len(drive) >= 2 && drive[1] == ':' {
			return drive, nil
		}
		return final, nil
	}
}

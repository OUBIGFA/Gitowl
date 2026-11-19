package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
)

var errNotebookLink = errors.New("editor.link")

// Document links are untrusted content. Relative files stay in this project;
// only browser and email URLs are sent to the operating system's URL handler.
func (a *app) openNotebookLink(d *notebookDocument, raw string) {
	u, err := url.Parse(raw)
	if err != nil || strings.IndexFunc(raw, unicode.IsControl) >= 0 || u.User != nil {
		a.notebook.err = errNotebookLink
		return
	}
	if u.Scheme == "" && u.Host == "" {
		if u.Path == "" || filepath.IsAbs(u.Path) || a.repo == nil || a.repo.Path != d.root {
			a.notebook.err = errNotebookLink
			return
		}
		a.openNote(filepath.Join(filepath.Dir(d.name), filepath.FromSlash(u.Path)))
		return
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	scheme := strings.ToLower(u.Scheme)
	if !((scheme == "https" || scheme == "http") && u.Hostname() != "" || scheme == "mailto" && u.Opaque != "") || a.openExternal == nil {
		a.notebook.err = errNotebookLink
		return
	}
	a.notebook.err = a.openExternal(u.String())
}

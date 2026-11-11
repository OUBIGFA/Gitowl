package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type patchPreview struct {
	file  editor.FilePatch
	state *editor.DiffState
}

// preview contains decoded data, never build-pass Elements or Contexts.
type preview struct {
	kind      string
	patches   []patchPreview
	previous  ui.ImageSource
	current   ui.ImageSource
	before    *backend.Blob
	after     *backend.Blob
	submodule *nativeSubmoduleDiff
}

func preparePreview(diff backend.Diff, path string) (*preview, error) {
	p := &preview{kind: diff.Kind, before: diff.Previous, after: diff.Current}
	switch diff.Kind {
	case "text", "large-text":
		if diff.Text == nil {
			if diff.Kind == "large-text" {
				return p, nil
			}
			return nil, errors.New("text diff has no patch")
		}
		files, err := editor.ParsePatch(*diff.Text)
		if err != nil {
			return nil, fmt.Errorf("parse patch: %w", err)
		}
		if len(files) == 0 && *diff.Text != "" {
			return nil, errors.New("text diff is not a unified patch")
		}
		// Preserve every patch (e.g. index and working-tree parts); taking only
		// the first would silently hide actual changes returned by the service.
		for _, file := range files {
			if file.OldName == "" && file.NewName == "" {
				// Upstream DiffParser.text can contain hunks without file headers.
				// The requested path supplies syntax/identity, not fabricated lines.
				file.OldName, file.NewName = path, path
			}
			state := &editor.DiffState{}
			state.SetPatch(file)
			p.patches = append(p.patches, patchPreview{file, state})
		}
	case "image":
		if diff.Previous == nil && diff.Current == nil {
			return nil, errors.New("image diff has no image contents")
		}
		for _, side := range []struct {
			blob *backend.Blob
			to   *ui.ImageSource
		}{{diff.Previous, &p.previous}, {diff.Current, &p.current}} {
			if side.blob != nil {
				image, err := ui.DecodeImage(side.blob.Contents)
				if err != nil {
					return nil, fmt.Errorf("decode image: %w", err)
				}
				*side.to = image
			}
		}
	case "submodule":
		if len(diff.Submodule) != 0 && string(diff.Submodule) != "null" {
			if err := json.Unmarshal(diff.Submodule, &p.submodule); err != nil {
				return nil, fmt.Errorf("submodule details: %w", err)
			}
		}
	case "binary", "unrenderable":
	default:
		return nil, fmt.Errorf("unknown diff kind %q", diff.Kind)
	}
	return p, nil
}

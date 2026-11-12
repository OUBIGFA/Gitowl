package main

import (
	"embed"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The unmodified Lucide source assets and their licenses/checksums are registered
// in third_party/native-icons.json. MyGo owns SVG layout and rendering.
//
//go:embed icons/*.svg
var iconFiles embed.FS

var icons = func() map[string]*ui.SVG {
	entries, err := iconFiles.ReadDir("icons")
	if err != nil {
		panic(err)
	}
	result := make(map[string]*ui.SVG, len(entries))
	for _, entry := range entries {
		data, err := iconFiles.ReadFile("icons/" + entry.Name())
		if err != nil {
			panic(err)
		}
		result[strings.TrimSuffix(entry.Name(), ".svg")] = ui.MustParseSVG(data)
	}
	return result
}()

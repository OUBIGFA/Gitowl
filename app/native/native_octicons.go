package main

import (
	"embed"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Exact @primer/octicons files, recorded by tools/record_octicons.py. The
// original development preview uses these symbols, not textual substitutes.
//
//go:embed octicons/*.svg
var octiconFiles embed.FS

// The full upstream catalogue is only shown in its dedicated dialog. Parsing
// it before the first window allocated thousands of unused SVG objects.
var nativeOcticons = sync.OnceValue(func() map[string]*ui.SVG {
	icons := make(map[string]*ui.SVG)
	files, err := octiconFiles.ReadDir("octicons")
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		data, err := octiconFiles.ReadFile("octicons/" + file.Name())
		if err != nil {
			panic(err)
		}
		icons[strings.TrimSuffix(file.Name(), ".svg")] = ui.MustParseSVG(data)
	}
	return icons
})

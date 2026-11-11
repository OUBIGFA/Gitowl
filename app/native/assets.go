package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The packaging config refers to the existing source logo directly. Only a
// packaged resource copy is used in production, never a duplicate source asset.
func loadLogo(resources string) (ui.ImageSource, error) {
	data, err := loadLogoPNG(resources)
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmap(data)
}

func loadLogoPNG(resources string) ([]byte, error) {
	paths := []string{filepath.Join(resources, "logo.png")}
	if mygo.IsDev() {
		paths = append(paths,
			filepath.Join("..", "static", "logos", "gitowl", "logo.png"),
			filepath.Join("app", "static", "logos", "gitowl", "logo.png"))
	}
	var missing error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			missing = errors.Join(missing, err)
			continue
		}
		_, err = ui.DecodeBitmap(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return data, nil
	}
	return nil, missing
}

package main

import (
	"errors"
	"fmt"
	"math"
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
	return loadBrandPNG(resources, "logo.png")
}

// The Windows notification area uses 16 DIPs, independently of the main
// window's size/monitor. Pick its prefiltered pixel size at the taskbar's
// scale instead of asking the shell to shrink the 992-pixel original.
func trayIconSize(scale float64) int {
	wanted := int(math.Round(16 * scale))
	for _, size := range []int{16, 20, 24, 28, 32, 40, 48, 64, 96, 128, 192, 256} {
		if size >= wanted {
			return size
		}
	}
	return 256
}

func loadTrayPNG(resources string, scale float64) ([]byte, error) {
	return loadBrandPNG(resources, filepath.Join("icons", fmt.Sprintf("%d.png", trayIconSize(scale))))
}

func loadBrandPNG(resources, name string) ([]byte, error) {
	paths := []string{filepath.Join(resources, name)}
	if mygo.IsDev() {
		paths = append(paths,
			filepath.Join("..", "static", "logos", "gitowl", name),
			filepath.Join("app", "static", "logos", "gitowl", name))
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

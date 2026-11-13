// Generate Gitowl's size-specific brand assets from the registered original.
// Run from the repository with npm run generate:native-icons. No platform
// drawing API or new dependency is needed; x/image is already locked by MyGo.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

func main() {
	check := flag.Bool("check", false, "verify generated assets without writing")
	flag.Parse()
	if err := generate(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(check bool) error {
	// go -C app/native keeps the generator on the same locked Go module.
	root := filepath.Join("..", "..")
	const source = "app/static/logos/gitowl/logo.png"
	const directory = "app/static/logos/gitowl/icons/"
	data, err := os.ReadFile(filepath.Join(root, source))
	if err != nil {
		return err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if img.Bounds().Dx() != img.Bounds().Dy() {
		return fmt.Errorf("the registered logo must be square")
	}
	type asset struct {
		Path   string `json:"path"`
		Size   int    `json:"size,omitempty"`
		SHA256 string `json:"sha256"`
	}
	var assets []asset
	write := func(name string, size int, content []byte) error {
		path := filepath.Join(root, filepath.FromSlash(name))
		assets = append(assets, asset{name, size, fmt.Sprintf("%x", sha256.Sum256(content))})
		if check {
			existing, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(existing, content) {
				return fmt.Errorf("generated icon is stale: %s", name)
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, content, 0o644)
	}
	// Tray/taskbar sizes cover common 100–400% Windows scales. Larger
	// images serve Explorer, launchers and high-resolution app surfaces.
	sizes := []int{16, 20, 24, 28, 32, 40, 48, 64, 96, 128, 192, 256, 512, 1024}
	var icoImages [][]byte
	var icoSizes []int
	for _, size := range sizes {
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		// Resample premultiplied alpha directly from the original each time:
		// a chain of resized icons blurs edges, especially at 16/20 pixels.
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, dst); err != nil {
			return err
		}
		if err := write(fmt.Sprintf("%s%d.png", directory, size), size, encoded.Bytes()); err != nil {
			return err
		}
		if size <= 256 {
			icoSizes, icoImages = append(icoSizes, size), append(icoImages, encoded.Bytes())
		}
	}
	// Windows' standard ICO directory selects the matching PNG frame. The
	// app executable/installer also receive MyGo's native multi-size PE icons
	// from this same original through mygo.json's icon setting.
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(icoImages))})
	offset := 6 + 16*len(icoImages)
	for i, content := range icoImages {
		dim := byte(icoSizes[i]) // 0 encodes 256 in an ICO directory.
		ico.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&ico, binary.LittleEndian, [2]uint16{1, 32})
		_ = binary.Write(&ico, binary.LittleEndian, [2]uint32{uint32(len(content)), uint32(offset)})
		offset += len(content)
	}
	for _, content := range icoImages {
		ico.Write(content)
	}
	if err := write(directory+"gitowl.ico", 0, ico.Bytes()); err != nil {
		return err
	}
	manifest := struct {
		Source       string  `json:"source"`
		SourceSHA256 string  `json:"sourceSha256"`
		Generator    string  `json:"generator"`
		Resampling   string  `json:"resampling"`
		Assets       []asset `json:"assets"`
	}{source, fmt.Sprintf("%x", sha256.Sum256(data)), "tools/generate-native-icons/main.go", "Catmull-Rom with premultiplied alpha; every size derives directly from the original", assets}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := write("references/native-brand-icons.json", 0, append(encoded, '\n')); err != nil {
		return err
	}
	fmt.Printf("Gitowl brand icons: %d PNG sizes and one multi-size ICO (check=%v)\n", len(sizes), check)
	return nil
}

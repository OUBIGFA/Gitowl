package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
	"image"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type nativeImage struct {
	mu     sync.Mutex
	data   []byte
	mime   string
	frames []nativeFrame
}
type nativeFrame struct {
	width, height int
	duration      int64
	repetitions   int
}
type nativeMedia struct {
	mu         sync.Mutex
	images     map[string]*nativeImage
	executable string
}

func (m *nativeMedia) retain(data []byte, mime string) *Blob {
	hash := sha256.Sum256(data)
	id := fmt.Sprintf("%x", hash[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.images == nil {
		m.images = make(map[string]*nativeImage)
	}
	m.images[id] = &nativeImage{data: bytes.Clone(data), mime: mime}
	return &Blob{MediaID: id, MediaType: mime, Bytes: len(data)}
}
func (m *nativeMedia) clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.images = make(map[string]*nativeImage)
}

// ImageMagick's official native executable replaces its Node/WASM host. Decode
// only the requested frame; no persistent decoder process or full-animation RGBA
// cache survives a preview. The current two compressed sides own the byte budget.
func (m *nativeMedia) frame(ctx context.Context, id string, index int) (*image.NRGBA, KernelMediaChunk, error) {
	m.mu.Lock()
	entry := m.images[id]
	m.mu.Unlock()
	if entry == nil {
		return nil, KernelMediaChunk{}, errors.New("image preview expired")
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if animatedPNG(entry.data) {
		return decodeAPNGFrame(entry.data, index)
	}
	if entry.mime == "image/svg+xml" {
		if err := validateNativeSVG(entry.data); err != nil {
			return nil, KernelMediaChunk{}, err
		}
	}
	coder := map[string]string{"image/png": "PNG", "image/jpeg": "JPEG", "image/gif": "GIF", "image/webp": "WEBP", "image/avif": "AVIF", "image/x-icon": "ICO", "image/vnd-ms.dds": "DDS", "image/svg+xml": "SVG", "image/bmp": "BMP", "image/tiff": "TIFF"}[entry.mime]
	if coder == "" {
		return nil, KernelMediaChunk{}, errors.New("unsupported image type")
	}
	source := coder + ":-"
	run := func(limit int, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, m.executable, args...)
		// Windows canonicalizes the stdin name '-' before checking policy.
		// Keep that synthetic path inside the shipped decoder directory.
		cmd.Dir = filepath.Dir(m.executable)
		hideConsole(cmd)
		cmd.Stdin = bytes.NewReader(entry.data)
		cmd.Env = append(os.Environ(), "MAGICK_CONFIGURE_PATH="+filepath.Dir(m.executable), "MAGICK_THREAD_LIMIT=1", "MAGICK_MEMORY_LIMIT=128MiB", "MAGICK_MAP_LIMIT=0", "MAGICK_DISK_LIMIT=0", "MAGICK_TIME_LIMIT=30")
		stderr := nativeLimitedOutput{limit: 64 << 10}
		output := nativeLimitedOutput{limit: limit}
		cmd.Stderr = &stderr
		cmd.Stdout = &output
		err := cmd.Run()
		if err != nil {
			return nil, fmt.Errorf("image decoding failed: %s", strings.TrimSpace(stderr.String()))
		}
		return output.Bytes(), nil
	}
	if entry.frames == nil {
		metadata, err := run(8<<20, source, "-coalesce", "-auto-orient", "json:-")
		if err != nil {
			return nil, KernelMediaChunk{}, err
		}
		var decoded []struct {
			Image struct {
				Geometry   struct{ Width, Height int }
				Delay      string
				Iterations int
			}
		}
		if err := json.Unmarshal(metadata, &decoded); err != nil {
			return nil, KernelMediaChunk{}, errors.New("invalid image metadata")
		}
		for _, image := range decoded {
			w, h := image.Image.Geometry.Width, image.Image.Geometry.Height
			if w <= 0 || h <= 0 || w > 32768 || h > 32768 || int64(w)*int64(h)*4 > 256<<20 || len(decoded) > 10000 {
				return nil, KernelMediaChunk{}, errors.New("image exceeds preview limits")
			}
			duration := int64(100000)
			if image.Image.Delay != "" {
				ticks, rate, ok := strings.Cut(image.Image.Delay, "x")
				if !ok {
					return nil, KernelMediaChunk{}, errors.New("invalid animation delay")
				}
				numerator, e1 := strconv.ParseInt(ticks, 10, 64)
				denominator, e2 := strconv.ParseInt(rate, 10, 64)
				if e1 != nil || e2 != nil || denominator <= 0 {
					return nil, KernelMediaChunk{}, errors.New("invalid animation delay")
				}
				duration = max(1, numerator*1000000/denominator)
			}
			repeats := 0
			if len(decoded) > 1 {
				repeats = image.Image.Iterations - 1
			}
			entry.frames = append(entry.frames, nativeFrame{w, h, duration, repeats})
		}
	}
	if index < 0 || index >= len(entry.frames) {
		return nil, KernelMediaChunk{}, errors.New("image frame is outside the animation")
	}
	frame := entry.frames[index]
	args := []string{source, "-coalesce", "-auto-orient"}
	if len(entry.frames) > 1 {
		args = append(args, "-delete", "0-"+strconv.Itoa(index-1))
		if index == 0 {
			args = args[:len(args)-2]
		}
		if index < len(entry.frames)-1 {
			args = append(args, "-delete", "1--1")
		}
	}
	args = append(args, "-alpha", "on", "-depth", "8", "rgba:-")
	pixels, err := run(frame.width*frame.height*4, args...)
	if err != nil {
		return nil, KernelMediaChunk{}, err
	}
	if len(pixels) != frame.width*frame.height*4 {
		return nil, KernelMediaChunk{}, errors.New("image decoder returned an invalid frame length")
	}
	metadata := KernelMediaChunk{Total: len(pixels), Width: frame.width, Height: frame.height, Duration: frame.duration, Frames: len(entry.frames), Repetitions: frame.repetitions}
	return &image.NRGBA{Pix: pixels, Stride: frame.width * 4, Rect: image.Rect(0, 0, frame.width, frame.height)}, metadata, nil
}

type nativeLimitedOutput struct {
	bytes.Buffer
	limit int
}

// Repository SVG is a standalone preview. Reject active/embedded documents and
// external references before a decoder can silently replace them with blanks.
// The decoder policy separately denies filesystem/network/delegate access.
var svgURL = regexp.MustCompile(`(?i)url\s*\(([^)]*)\)`)

func validateNativeSVG(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("invalid SVG document")
		}
		switch node := token.(type) {
		case xml.ProcInst:
			if node.Target != "xml" {
				return errors.New("SVG processing instructions are not supported")
			}
		case xml.Directive:
			return errors.New("SVG document declarations are not supported")
		case xml.StartElement:
			switch strings.ToLower(node.Name.Local) {
			case "image", "script", "foreignobject", "style":
				return errors.New("SVG embedded or external content is not supported")
			}
			for _, attribute := range node.Attr {
				name, value := strings.ToLower(attribute.Name.Local), strings.TrimSpace(attribute.Value)
				if name == "href" && !strings.HasPrefix(value, "#") {
					return errors.New("SVG external references are not supported")
				}
				for _, match := range svgURL.FindAllStringSubmatch(value, -1) {
					if !strings.HasPrefix(strings.Trim(strings.TrimSpace(match[1]), "\"'"), "#") {
						return errors.New("SVG external styles are not supported")
					}
				}
				if strings.HasPrefix(name, "on") || strings.ContainsAny(value, "\\@") {
					return errors.New("SVG active or external content is not supported")
				}
			}
		}
	}
}

func (b *nativeLimitedOutput) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.Len() {
		return 0, errors.New("decoder output exceeds preview limits")
	}
	return b.Buffer.Write(data)
}

func imageMIME(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	known := map[string]string{".svg": "image/svg+xml", ".avif": "image/avif", ".dds": "image/vnd-ms.dds", ".ico": "image/x-icon", ".apng": "image/png"}
	if mime := known[ext]; mime != "" {
		return mime
	}
	mime := http.DetectContentType(data)
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	return ""
}

func (n *NativeOwner) imageDiff(ctx context.Context, path, commit string) (*Diff, error) {
	previousPath := path
	files := n.state.Selected.Files
	if commit != "" {
		files = n.state.Selected.HistoryFiles
	}
	for _, file := range files {
		if file.Path == path && file.OldPath != nil {
			previousPath = *file.OldPath
			break
		}
	}
	var current, previous []byte
	var err error
	if commit != "" {
		current, err = n.current.Blob(ctx, commit, path, 64<<20)
		if err != nil && !errors.Is(err, gitcli.ErrObjectMissing) {
			return nil, err
		}
		parents, parentErr := n.current.Run(ctx, nil, "rev-list", "--parents", "-n", "1", commit, "--")
		if parentErr != nil {
			return nil, parentErr
		}
		if ids := strings.Fields(string(parents)); len(ids) > 1 {
			previous, err = n.current.Blob(ctx, ids[1], previousPath, 64<<20)
			if err != nil && !errors.Is(err, gitcli.ErrObjectMissing) {
				return nil, err
			}
		}
	} else {
		current, err = readPreviewFile(filepath.Join(n.current.Path, filepath.FromSlash(path)), 64<<20)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if n.status.Head != "" {
			previous, err = n.current.Blob(ctx, n.status.Head, previousPath, 64<<20)
			if err != nil && !errors.Is(err, gitcli.ErrObjectMissing) {
				return nil, err
			}
		}
	}
	if len(current)+len(previous) > 64<<20 {
		return nil, errors.New("image preview exceeds the compressed content limit")
	}
	mime := imageMIME(path, current)
	if mime == "" {
		mime = imageMIME(path, previous)
	}
	if mime == "" {
		return nil, nil
	}
	n.media.clear()
	diff := &Diff{Kind: "image"}
	if len(current) > 0 {
		diff.Current = n.media.retain(current, mime)
	}
	if len(previous) > 0 {
		diff.Previous = n.media.retain(previous, mime)
	}
	return diff, nil
}

// Never return a truncated file as a valid image, and never allocate an
// unbounded working copy before deciding whether it fits the preview budget.
func readPreviewFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, gitcli.ErrOutputLimit
	}
	return data, nil
}

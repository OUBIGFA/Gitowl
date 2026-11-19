package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	rich "github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/editor"
	"github.com/egoist/mygo/ui/editor"
)

var (
	errNotebookPath     = errors.New("editor.path")
	errNotebookBinary   = errors.New("editor.binary")
	errNotebookConflict = errors.New("editor.conflict")
	errNotebookTooLarge = errors.New("editor.tooLarge")
)

// Bound both editor buffers and per-file search reads. The same limit is
// stated in the editor error and content-search UI in every language.
const notebookTextLimit = 8 << 20

func readNotebookFile(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotebookPath
	}
	if info.Size() > notebookTextLimit {
		return nil, errNotebookTooLarge
	}
	// Stat is only an early rejection; the file may grow while it is read.
	data, err := io.ReadAll(io.LimitReader(f, notebookTextLimit+1))
	if len(data) > notebookTextLimit {
		return nil, errNotebookTooLarge
	}
	return data, err
}

type notebookDocument struct {
	imageContext     context.Context
	cancelImages     context.CancelFunc
	webImages        map[string]rich.ImageResource
	root, name       string
	original         []byte
	code             editor.State
	rich             *rich.Editor
	markdown         bool
	richBase         string
	richRevision     uint64
	observedRevision uint64
	modified         bool
	encoding         notebookEncoding
	crlf             bool
}

// Only regular files in the chosen working tree are editable. Use rooted I/O
// at the actual access point, including when a worktree is moved between views.
func notebookFileRoot(project, name string) (*os.Root, error) {
	if !filepath.IsLocal(name) || name == "." {
		return nil, errNotebookPath
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		return nil, err
	}
	prefix := ""
	for _, part := range strings.Split(filepath.Clean(name), string(os.PathSeparator)) {
		prefix = filepath.Join(prefix, part)
		if strings.EqualFold(part, ".git") || strings.Contains(part, ":") {
			root.Close()
			return nil, errNotebookPath
		}
		info, err := root.Lstat(prefix)
		if err != nil {
			root.Close()
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, errNotebookPath
		}
	}
	return root, nil
}

func openNotebookDocument(project, name string) (*notebookDocument, error) {
	root, err := notebookFileRoot(project, name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := readNotebookFile(root, name)
	if err != nil {
		return nil, err
	}
	text, encoding, err := decodeNotebookText(data)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(name))
	d := &notebookDocument{root: project, name: name, original: data, encoding: encoding, crlf: strings.Contains(text, "\r\n"), markdown: ext == ".md" || ext == ".markdown"}
	if d.markdown {
		d.setRich(text)
	} else {
		d.setCode(text)
	}
	d.observedRevision = d.revision()
	return d, nil
}

func (d *notebookDocument) setRich(text string) {
	d.rich = rich.New(text)
	d.richBase = text
	d.richRevision = d.rich.Revision()
	d.code = editor.State{}
}
func (d *notebookDocument) setCode(text string) {
	b := editor.NewBuffer(text)
	b.SetLanguage(editor.LanguageFor(d.name))
	d.code = editor.State{Buffer: b}
	d.rich = nil
}
func (d *notebookDocument) revision() uint64 {
	if d.rich != nil {
		return d.rich.Revision()
	}
	return d.code.Buffer.Version()
}
func (d *notebookDocument) text() string {
	if d.rich != nil {
		if d.rich.Revision() == d.richRevision {
			return d.richBase
		}
		return rich.ExportMarkdown(d.rich.Committed())
	}
	var buf bytes.Buffer
	_, _ = d.code.Buffer.WriteTo(&buf)
	return buf.String()
}
func (d *notebookDocument) bytes() []byte {
	text := d.text()
	if d.crlf && d.rich != nil {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	}
	return d.encoding.encode(text)
}
func (d *notebookDocument) dirty() bool {
	if revision := d.revision(); revision != d.observedRevision {
		d.modified = !bytes.Equal(d.bytes(), d.original)
		d.observedRevision = revision
	}
	return d.modified
}
func (d *notebookDocument) switchMode() {
	if !d.markdown {
		return
	}
	dirty := d.dirty()
	text := d.text()
	if d.rich == nil {
		d.setRich(text)
	} else {
		d.setCode(text)
	}
	d.observedRevision = d.revision()
	d.modified = dirty
}
func (d *notebookDocument) save() error {
	if !d.dirty() {
		return nil
	}
	root, err := notebookFileRoot(d.root, d.name)
	if err != nil {
		return err
	}
	defer root.Close()
	data := d.bytes()
	info, err := root.Stat(d.name)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(d.name), ".gitowl-"+rand.Text()+".tmp")
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	current, err := readNotebookFile(root, d.name)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, d.original) {
		return fmt.Errorf("%w: %s", errNotebookConflict, d.name)
	}
	if err = root.Rename(tmp, d.name); err != nil {
		return err
	}
	d.original = data
	d.modified = false
	d.observedRevision = d.revision()
	return nil
}

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	rich "github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/editor"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestNotebookEditsPreserveOtherBlocksAndExternalChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.md")
	original := "\ufeff# Notes\r\n\r\n*keep this*\r\n\r\nchange me\r\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := openNotebookDocument(root, "notes.md")
	if err != nil || doc.rich == nil {
		t.Fatalf("open Markdown: %v", err)
	}
	if doc.dirty() {
		t.Fatal("newly opened note is dirty")
	}
	doc.switchMode()
	doc.code.Buffer.Set(strings.Replace(doc.code.Buffer.String(), "change me", "changed", 1))
	doc.switchMode()
	if err := doc.save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != strings.Replace(original, "change me", "changed", 1) {
		t.Fatalf("saved bytes: %q, %v", b, err)
	}
	doc.switchMode()
	doc.code.Buffer.Set("second edit")
	if err := os.WriteFile(path, []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := doc.save(); err == nil {
		t.Fatal("overwrote an external edit")
	}
	b, _ = os.ReadFile(path)
	if string(b) != "external edit" || !doc.dirty() {
		t.Fatal("disk change or unsaved buffer lost")
	}
}

func TestNotebookRejectsGitMetadataAndBinaryFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte{0, 1}, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", ".git/config", "bin"} {
		if _, err := openNotebookDocument(root, name); err == nil {
			t.Errorf("opened %s", name)
		}
	}
}

func TestNotebookLargeTextDoesNotEnterEditorOrContentSearch(t *testing.T) {
	root := t.TempDir()
	large := []byte("needle" + strings.Repeat("x", 8<<20))
	if err := os.WriteFile(filepath.Join(root, "large.txt"), large, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("needle"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openNotebookDocument(root, "large.txt"); err == nil {
		t.Error("an oversized file entered the interactive editor")
	}
	hits, err := searchNotebook(t.Context(), root, "text", "needle")
	if err != nil || !slices.Equal(hits, []string{"small.txt"}) {
		t.Errorf("bounded content search: %v, %v", hits, err)
	}
	// The size limit must not hide files from filename search.
	hits, err = searchNotebook(t.Context(), root, "files", "large")
	if err != nil || !slices.Equal(hits, []string{"large.txt"}) {
		t.Errorf("filename search: %v, %v", hits, err)
	}
}

func TestNotebookLinkUsesNativeBrowser(t *testing.T) {
	a := designEvidenceApp(t, time.Now(), "changes")
	a.repo.Path = t.TempDir()
	if err := os.WriteFile(filepath.Join(a.repo.Path, "links.md"), []byte("Visit [our site](https://example.com/path?q=1)."), 0600); err != nil {
		t.Fatal(err)
	}
	var opened string
	a.openExternal = func(url string) error { opened = url; return nil }
	a.openNote("links.md")
	tt := designEvidenceTester(a, 1180, 760, "en", false)
	a.notes().current.rich.SetSelection(8, 8)
	a.notes().current.rich.EditLink()
	tt.Frame()
	if err := tt.Click("Open link"); err != nil {
		t.Fatal(err)
	}
	if opened != "https://example.com/path?q=1" {
		t.Fatalf("link did not reach the native browser: %q", opened)
	}
}

func TestNotebookLinksStayWithinFileAndURLBoundaries(t *testing.T) {
	a := designEvidenceApp(t, time.Now(), "changes")
	a.repo.Path = t.TempDir()
	if err := os.WriteFile(filepath.Join(a.repo.Path, "linked note.md"), []byte("linked note"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &notebookDocument{root: a.repo.Path, name: "notes/source.md"}
	var opened []string
	a.openExternal = func(url string) error { opened = append(opened, url); return nil }
	a.openNotebookLink(d, "../linked%20note.md")
	if a.notebook.err != nil || a.notes().current.name != "linked note.md" {
		t.Fatalf("relative project link: %v", a.notebook.err)
	}
	for _, raw := range []string{"file:///C:/Windows/system32/cmd.exe", "javascript:alert(1)", "gitowl://open", "../../outside.txt", "https://user:password@example.com", "https://example.com/\x00"} {
		a.openNotebookLink(d, raw)
		if a.notebook.err == nil || len(opened) != 0 {
			t.Fatalf("unsafe link was accepted: %q", raw)
		}
	}
	for _, raw := range []string{"https://example.com", "mailto:reader@example.com", "//example.com/path"} {
		a.openNotebookLink(d, raw)
		if a.notebook.err != nil {
			t.Fatalf("safe link %q: %v", raw, a.notebook.err)
		}
	}
	if len(opened) != 3 || opened[2] != "https://example.com/path" {
		t.Fatalf("browser requests: %v", opened)
	}
}

func TestNotebookTreeSortControls(t *testing.T) {
	a := designEvidenceApp(t, time.Now(), "changes")
	a.repo.Path = t.TempDir()
	root := a.repo.Path
	if err := os.Mkdir(filepath.Join(root, "notes"), 0700); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"a.txt", "z.txt", "b.txt", "notes/c.txt"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(1000+i), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	tt := designEvidenceTester(a, 900, 620, "en", true)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("notes")
	check := func(files ...string) {
		t.Helper()
		want := append([]string{"notes", filepath.Join("notes", "c.txt")}, files...)
		var names []string
		for _, row := range a.notes().rows {
			names = append(names, row.name)
		}
		if !slices.Equal(names, want) {
			t.Fatalf("tree %v, want %v", names, want)
		}
	}
	check("a.txt", "b.txt", "z.txt")
	click("Modified")
	check("b.txt", "z.txt", "a.txt")
	click(locales.Catalog.Format("en", "editorDescending", nil))
	check("a.txt", "z.txt", "b.txt")
	click("Name")
	click(locales.Catalog.Format("en", "editorAscending", nil))
	check("z.txt", "b.txt", "a.txt")
}

func TestNotebookWebImageLoadsAndRetriesInEditor(t *testing.T) {
	data := nativeImageTestGIF(t, []int{4, 12}, 0).Contents
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/image.gif" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/gif")
		w.Write(data)
	}))
	defer server.Close()
	src := server.URL + "/image.gif"
	a := designEvidenceApp(t, time.Now(), "changes")
	a.repo.Path = t.TempDir()
	updates := make(chan func(), 8)
	a.update = func(fn func()) { updates <- fn }
	name := "A long notebook file with multiple headings and an embedded image.md"
	if err := os.WriteFile(filepath.Join(a.repo.Path, name), []byte("# Review\n\n![picture]("+src+")\n\n## Details\n\nA second section."), 0600); err != nil {
		t.Fatal(err)
	}
	a.openNote(name)
	tt := designEvidenceTester(a, 1180, 760, "en", false)
	advance := func() {
		t.Helper()
		select {
		case fn := <-updates:
			fn()
			tt.Frame()
		case <-time.After(4 * time.Second):
			t.Fatal("no image completion")
		}
	}
	advance()
	if calls.Load() != 0 {
		t.Fatal("unopened intranet link was contacted automatically")
	}
	if err := tt.Click("Retry"); err != nil {
		t.Fatal(err)
	}
	advance()
	image := a.notes().current.webImages[src]
	if image.Bitmap == nil || len(image.Frames) != 2 || image.Delays[1] != 120*time.Millisecond || image.Local {
		t.Fatalf("invalid GIF result: %+v", image)
	}
	if _, ok := tt.Find("View image: image.gif"); !ok {
		t.Fatalf("decoded image missing from the rendered editor")
	}
	tt.Frame()
	if calls.Load() != 1 {
		t.Fatalf("duplicate requests: %d", calls.Load())
	}
	a.notes().close(a.notes().current)
}

func TestNotebookImageFailureStates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			w.Header().Set("Content-Length", fmt.Sprint(notebookImageLimit+1))
		case "/broken":
			fmt.Fprint(w, "not an image")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for path, want := range map[string]rich.ImageProblem{"/missing": rich.ImageRemote, "/large": rich.ImageTooLarge, "/broken": rich.ImageUnsupported} {
		if got := fetchNotebookImage(t.Context(), server.Client(), server.URL+path); got.Problem != want || got.Bitmap != nil {
			t.Errorf("%s: %+v", path, got)
		}
	}
}

func TestNotebookSearchLayout(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			a := designEvidenceApp(t, time.Now(), "changes")
			a.repo.Path = t.TempDir()
			doc := "项目文档－编辑工作台在小窗口中的长文件名与键盘导航检查.md"
			if err := os.WriteFile(filepath.Join(a.repo.Path, doc), []byte("# Report\n\n## Notes\n\nArticle text."), 0600); err != nil {
				t.Fatal(err)
			}
			a.openNote(doc)
			n := &a.notebook
			n.searchOpen = true
			n.search = notebookSearch{mode: "files", root: a.repo.Path, results: []string{".gitignore", doc, "settings/plugins.json"}}
			tt := designEvidenceTester(a, 900, 620, locale, dark)
			if r, ok := tt.Find(".gitignore"); !ok || r.W < 350 {
				t.Fatalf("result row bounds: %v", r)
			}
			tt.Move(0, 0)
			saveDesignEvidence(t, tt, fmt.Sprintf("quick-open-%s-%t.png", locale, dark))
			n.search.mode = "text"
			tt.Frame()
			if !tt.HasText(locales.Catalog.Format(locale, "editorSearchLimit", nil)) {
				t.Fatal("content search did not state its file-size boundary")
			}
			visibleControl(t, tt, locales.Catalog.Format(locale, "editorClose", nil), 900, 620)
			saveDesignEvidence(t, tt, fmt.Sprintf("content-search-%s-%t.png", locale, dark))
			n.search.mode = "files"
			tt.Frame()
			tt.Key(0, ui.KeyDown)
			if n.search.active != 1 {
				t.Fatal("Down did not select the next search result")
			}
			tt.Key(0, ui.KeyEnter)
			if n.searchOpen || n.err != nil {
				t.Fatalf("Enter did not open the chosen file: %v", n.err)
			}
			n.searchOpen = true
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			if n.searchOpen {
				t.Fatal("Escape did not close quick open")
			}
			saveDesignEvidence(t, tt, fmt.Sprintf("long-tab-%s-%t.png", locale, dark))
		}
	}
}

func TestNotebookWorkspaceOpensFilesAndSwitchesModes(t *testing.T) {
	type scenario struct {
		width, height int
		locale        string
		dark          bool
	}
	var scenarios []scenario
	for _, size := range [][2]int{{900, 620}, {1180, 760}} {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			for _, dark := range []bool{false, true} {
				scenarios = append(scenarios, scenario{size[0], size[1], locale, dark})
			}
		}
	}
	for _, tc := range scenarios {
		t.Run(fmt.Sprintf("%s/%d/dark=%t", tc.locale, tc.width, tc.dark), func(t *testing.T) {
			a := designEvidenceApp(t, time.Now(), "changes")
			root := t.TempDir()
			a.repo.Path = root
			if err := os.Mkdir(filepath.Join(root, "notes"), 0700); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{"notes/Guide.md": "# Project notes\n\n## Working files\n\nThe editor opens Markdown and source files in separate tabs.\n\n- Review the draft\n- Save the working file\n\n| Item | State |\n| --- | --- |\n| Guide | Draft |\n", "main.go": "package main\n\nfunc main() {}\n", "related.md": "[[Guide]]"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			tt := designEvidenceTester(a, tc.width, tc.height, tc.locale, tc.dark)
			if err := tt.Click("notes"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("Guide.md"); err != nil {
				t.Fatal(err)
			}
			if a.notes().current == nil || a.notes().current.rich == nil {
				t.Fatal("Markdown did not open in writing mode")
			}
			visibleControl(t, tt, locales.Catalog.Format(tc.locale, "editorSave", nil), tc.width, tc.height)
			tt.Move(2, 2)
			tt.Advance(time.Second)
			saveDesignEvidence(t, tt, fmt.Sprintf("notebook-%s-%d-dark-%t.png", tc.locale, tc.width, tc.dark))
			if err := tt.Click(locales.Catalog.Format(tc.locale, "editorCode", nil)); err != nil {
				t.Fatal(err)
			}
			if a.notes().current.code.Buffer == nil {
				t.Fatal("source mode did not open")
			}
			if err := tt.Click("main.go"); err != nil {
				t.Fatal(err)
			}
			if a.notes().current.name != "main.go" || len(a.notes().docs) != 2 {
				t.Fatal("file tree did not retain both tabs")
			}
			hits, err := searchNotebook(context.Background(), root, "backlinks", "Guide")
			if err != nil || len(hits) != 1 || hits[0] != "related.md" {
				t.Fatalf("backlinks: %v %v", hits, err)
			}
		})
	}
}

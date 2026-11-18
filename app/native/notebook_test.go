package main

import (
	"context"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestNotebookWorkspaceOpensFilesAndSwitchesModes(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		locale        string
		dark          bool
	}{{1180, 760, "en", false}, {900, 620, "zh-Hans", true}, {1180, 760, "zh-Hant", false}} {
		t.Run(tc.locale, func(t *testing.T) {
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
			saveDesignEvidence(t, tt, "notebook-"+tc.locale+".png")
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

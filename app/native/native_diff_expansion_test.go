package main

import (
	"bufio"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestNativeDiffHunkControlsUseOriginalExpansionDirections(t *testing.T) {
	patch := "@@ -20,3 +20,3 @@ first\n before\n-old\n+new\n after\n@@ -80,3 +80,3 @@ second\n before2\n-old2\n+new2\n after2\n"
	for _, words := range []struct{ locale, up, down string }{
		{"en", "Show context above", "Show context below"},
		{"zh-Hans", "显示上方上下文", "显示下方上下文"},
		{"zh-Hant", "顯示上方上下文", "顯示下方上下文"},
	} {
		for _, action := range []struct {
			header, direction string
			index             int
		}{
			{"@@ -20,3 +20,3 @@ first", "up", 0},
			{"@@ -80,3 +80,3 @@ second", "up", 1},
			{"@@ -80,3 +80,3 @@ second", "down", 1},
			{"", "down", 2},
		} {
			t.Run(words.locale+"/"+action.header+"/"+action.direction, func(t *testing.T) {
				a := designEvidenceApp(t, time.Now(), "review")
				var err error
				a.preview, err = preparePreview(backend.Diff{Kind: "text", Text: &patch}, "file.txt")
				if err != nil {
					t.Fatal(err)
				}
				textDiff := &backend.KernelWorkspaceTextDiff{}
				if err = json.Unmarshal([]byte("{\"fileID\":\"file\",\"identity\":\"original-diff\",\"prepared\":true,\"canExpand\":true,\"hunks\":[{\"header\":\"@@ -20,3 +20,3 @@ first\",\"expansion\":\"Up\"},{\"header\":\"@@ -80,3 +80,3 @@ second\",\"expansion\":\"Both\"},{\"header\":\"\",\"expansion\":\"Down\"}]}"), textDiff); err != nil {
					t.Fatal(err)
				}
				ref := a.coreSnapshot.Selected.Repository.KernelRepositoryReference
				a.coreSnapshot.Workspace = &backend.KernelWorkspace{Repository: &ref, TextDiff: textDiff}
				stdout, output := io.Pipe()
				input, stdin := io.Pipe()
				a.core = backend.NewKernel(stdout, stdin, nil)
				t.Cleanup(func() { a.close(); input.Close(); stdin.Close(); stdout.Close(); output.Close() })
				tester := ui.NewTester(func(c *ui.Context) {
					setWorkspaceTheme(c)
					ui.Column(c).Fill().Children(func() { a.previewView(c, locales.In(c.Locale()), 1000) })
				}, 1000, 500)
				tester.SetLocale(words.locale)
				tester.SetDark(action.index == 1)
				tester.Advance(time.Second)
				if action.index == 0 {
					saveDesignEvidence(t, tester, "context-"+words.locale+"-light.png")
				} else if action.index == 1 && action.direction == "up" {
					saveDesignEvidence(t, tester, "context-"+words.locale+"-dark.png")
				}
				label := words.up
				if action.direction == "down" {
					label = words.down
				}
				if action.header != "" {
					label += " · " + action.header
				}
				if err = tester.Click(label); err != nil {
					t.Fatal(err)
				}
				if len(a.coreQueue) != 1 {
					t.Fatal("context expansion did not dispatch exactly one original action")
				}
				tester.Frame()
				button, found := tester.AccessByRole(ui.RoleButton, label)
				if !found || !button.Disabled {
					t.Fatal("pending context expansion allowed a second click")
				}
				operation := <-a.coreQueue
				go operation.call()
				requests := make(chan []byte, 1)
				go func() { data, _ := bufio.NewReader(input).ReadBytes('\n'); requests <- data }()
				select {
				case data := <-requests:
					var request struct {
						Params struct {
							Name   string
							Action struct {
								Type, FileID, Identity, Direction string
								Hunk                              int
							}
						}
					}
					if err = json.Unmarshal(data, &request); err != nil {
						t.Fatal(err)
					}
					got := request.Params.Action
					if request.Params.Name != "workspace.action" || got.Type != "diff.expandHunk" || got.FileID != "file" || got.Identity != "original-diff" || got.Hunk != action.index || got.Direction != action.direction {
						t.Fatalf("expansion lost the original patch handle: %s", data)
					}
				case <-time.After(time.Second):
					t.Fatal("context expansion did not reach the original kernel")
				}
			})
		}
	}
}

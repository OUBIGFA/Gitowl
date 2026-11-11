package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// Use the wire DTO so this checks the presentation boundary without duplicating
// the original kernel's alias/fuzzy ranking implementation.
func projectSearchSnapshot(t *testing.T, revision int64, text string, paths []string) backend.KernelSnapshot {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"revision": revision,
		"repositories": []map[string]any{
			{"id": 1, "path": "C:/alpha", "name": "alpha", "alias": "Release Notes", "kind": "repository"},
			{"id": 2, "path": "C:/beta", "name": "beta", "kind": "repository"},
		},
		"repositoryFilter": map[string]any{"text": text, "paths": paths},
	})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot backend.KernelSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func projectSearchTester(t *testing.T) (*app, *ui.Tester) {
	t.Helper()
	a := newApp(nil, nil)
	t.Cleanup(a.close)
	// Hold outgoing operations at the I/O boundary to exercise delayed replies.
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 32)
	a.applyCoreSnapshot(projectSearchSnapshot(t, 1, "", []string{"C:/alpha", "C:/beta"}))
	tt := ui.NewTester(func(c *ui.Context) {
		colors := setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() {
			a.coreProjectList(c, locales.In("en"), colors)
		})
	}, 232, 620)
	tt.Advance(time.Second)
	return a, tt
}

func TestProjectSearchUsesOriginalResultsAndKeepsNewestInput(t *testing.T) {
	a, tt := projectSearchTester(t)
	if err := tt.Click("Search projects"); err != nil {
		t.Fatal(err)
	}
	tt.Type("r")
	tt.Type("ln")
	if len(a.coreQueue) != 2 {
		t.Fatalf("search did not forward edits: %d", len(a.coreQueue))
	}
	first, last := <-a.coreQueue, <-a.coreQueue
	old := projectSearchSnapshot(t, 2, "r", []string{"C:/beta", "C:/alpha"})
	first.complete(backend.KernelResult{Snapshot: old}, nil)
	a.applyCoreSnapshot(old)
	tt.Frame()
	field, ok := tt.AccessByRole(ui.RoleTextField, "Search projects")
	if !ok || field.Value != "rln" {
		t.Fatalf("late response replaced input: %+v", field)
	}
	latest := projectSearchSnapshot(t, 3, "rln", []string{"C:/alpha"})
	last.complete(backend.KernelResult{Snapshot: latest}, nil)
	a.applyCoreSnapshot(latest)
	tt.Frame()
	if !tt.HasText("Release Notes") || tt.HasText("beta") {
		t.Fatal("rail ignored the original filtered paths")
	}
	tt.Key(0, ui.KeyEscape)
	field, _ = tt.AccessByRole(ui.RoleTextField, "Search projects")
	if field.Value != "" || len(a.coreQueue) != 1 {
		t.Fatal("Escape did not clear the original filter")
	}
	clear := <-a.coreQueue
	empty := projectSearchSnapshot(t, 4, "", []string{"C:/alpha", "C:/beta"})
	clear.complete(backend.KernelResult{Snapshot: empty}, nil)
	a.applyCoreSnapshot(empty)
	tt.Frame()
	if !tt.HasText("beta") {
		t.Fatal("clearing search did not restore projects")
	}
	a.applyCoreSnapshot(projectSearchSnapshot(t, 5, "missing", []string{}))
	tt.Frame()
	if !tt.HasText("No matching projects") || tt.HasText("Release Notes") || tt.HasText("beta") {
		t.Fatal("empty result did not replace the visible list")
	}
}

func TestProjectSearchMenuFocusAndKeyboardSelection(t *testing.T) {
	a, tt := projectSearchTester(t)
	snapshot := projectSearchSnapshot(t, 2, "", []string{"C:/beta", "C:/alpha"})
	snapshot.Workspace = &backend.KernelWorkspace{Navigation: "repositories"}
	a.applyCoreSnapshot(snapshot)
	tt.Frame()
	field, ok := tt.AccessByRole(ui.RoleTextField, "Search projects")
	if !ok || !field.Focused {
		t.Fatal("original repository-list request did not focus search")
	}
	if len(a.coreQueue) != 1 {
		t.Fatal("original foldout was not acknowledged exactly once")
	}
	<-a.coreQueue
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if len(a.coreQueue) != 1 {
		t.Fatal("keyboard selection did not request original repository selection")
	}
}

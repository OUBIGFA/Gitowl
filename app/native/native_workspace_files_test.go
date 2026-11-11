package main

import (
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestWorkspaceResizePresentationUsesFiveDIPsAndWindowBounds(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	tt := ui.NewTester(func(c *ui.Context) { ui.Box(c).Width(a.nativeRailWidth(c)).FillHeight() }, 900, 620)
	tt.Frame()
	if a.workspaceRailCurrent != 232 {
		t.Fatalf("default project rail = %v", a.workspaceRailCurrent)
	}
	a.workspaceFocus = "files"
	if !a.workspacePresentation("increase-active-resizable-width") {
		t.Fatal("focused file list ignored original resize intent")
	}
	tt.Frame()
	if a.workspaceRailCurrent != 237 {
		t.Fatalf("resize did not use original 5 DIP step: %v", a.workspaceRailCurrent)
	}
	for i := 0; i < 40; i++ {
		a.workspacePresentation("increase-active-resizable-width")
		tt.Frame()
	}
	if a.workspaceRailCurrent != 280 {
		t.Fatalf("minimum window lost central room: %v", a.workspaceRailCurrent)
	}
	a.workspacePresentation("decrease-active-resizable-width")
	tt.Frame()
	if a.workspaceRailCurrent != 275 {
		t.Fatalf("decreasing a clamped rail must visibly resize immediately: %v", a.workspaceRailCurrent)
	}
	a.workspaceFocus = "diff"
	a.workspaceDiffWidth = 640
	diff := &editor.DiffState{Layout: editor.DiffSplit}
	a.preview = &preview{patches: []patchPreview{{state: diff}}}
	if !a.workspacePresentation("increase-active-resizable-width") || diff.SplitPercent != 50+float32(500)/640 {
		t.Fatalf("diff split did not resize five DIPs: %v", diff.SplitPercent)
	}
	a.workspaceFocus = ""
	if a.workspacePresentation("select-all") {
		t.Fatal("text selection must remain with MyGo/native editing role")
	}
}

func TestWorkspaceFileClickReviewsButModifiersAndArrowsOnlySelect(t *testing.T) {
	state := &nativeWorkspaceState{workspaceFileCursor: -1}
	list := &ui.ListState{}
	files := []backend.KernelFile{{ID: "a", Path: "a.txt", Status: "Modified"}, {ID: "b", Path: "b.txt", Status: "Modified"}, {ID: "c", Path: "c.txt", Status: "Modified"}, {ID: "d", Path: "d.txt", Status: "Modified"}}
	var selected, reviews []string
	included := 0
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Padding(24).Children(func() {
			renderWorkspaceChangesList(c, state, list, files, selected, "fixture", true, false, workspaceChangesCallbacks{
				checkState:  func(string) ui.CheckState { return ui.CheckOff },
				include:     func(string, bool) { included++ },
				selectFiles: func(ids []string) { selected = slices.Clone(ids) },
				review:      func(path string) { reviews = append(reviews, path) },
			})
		})
	}, 900, 620)
	tt.Advance(time.Second)
	if err := tt.Click("a.txt"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reviews, []string{"a.txt"}) || !slices.Equal(selected, []string{"a"}) {
		t.Fatalf("ordinary click must select and enter focused review: selected=%v review=%v", selected, reviews)
	}
	reviews = nil
	if err := tt.ClickWith(ui.Cmd, "c.txt"); err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 0 || !slices.Equal(selected, []string{"a", "c"}) {
		t.Fatalf("Ctrl/Cmd click must only extend native selection: selected=%v review=%v", selected, reviews)
	}
	if err := tt.ClickWith(ui.Shift, "d.txt"); err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 0 || !slices.Contains(selected, "c") || !slices.Contains(selected, "d") {
		t.Fatalf("Shift click must only select a native range: selected=%v review=%v", selected, reviews)
	}
	tt.Key(0, ui.KeyUp)
	if len(reviews) != 0 || !slices.Equal(selected, []string{"c"}) {
		t.Fatalf("arrow key opened a file instead of selecting: selected=%v review=%v", selected, reviews)
	}
	tt.Key(0, ui.KeyEnter)
	if !slices.Equal(reviews, []string{"c.txt"}) {
		t.Fatalf("Enter did not review focused selection: %v", reviews)
	}
	reviews = nil
	if err := tt.Click(locales.In("en").IncludeFile("b.txt")); err != nil {
		t.Fatal(err)
	}
	if included != 1 || len(reviews) != 0 {
		t.Fatal("inclusion checkbox accidentally entered review")
	}
	if err := tt.Click(locales.In("en").OpenFile("b.txt")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reviews, []string{"b.txt"}) {
		t.Fatalf("explicit review control did not open file: %v", reviews)
	}
}

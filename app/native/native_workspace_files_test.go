package main

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestWorkspaceFileScrollbarStaysOutsideSelectedRows(t *testing.T) {
	for _, width := range []int{280, 700} {
		for _, dark := range []bool{false, true} {
			for _, scale := range []float32{1, 1.25} {
				t.Run(fmt.Sprintf("width=%d/dark=%v/scale=%v", width, dark, scale), func(t *testing.T) {
					state := &nativeWorkspaceState{workspaceFileCursor: -1}
					list := &ui.ListState{}
					files := make([]backend.KernelFile, 30)
					for i := range files {
						files[i] = backend.KernelFile{ID: fmt.Sprint(i), Path: fmt.Sprintf("long-directory/文件-%02d-with-a-long-name.txt", i), Status: "Modified"}
					}
					selected := []string{files[0].ID}
					var trackWidth float32
					tt := ui.NewTester(func(c *ui.Context) {
						setWorkspaceTheme(c)
						trackWidth = c.Theme().ScrollbarStyle.Resolve(c.Theme()).TrackWidth
						ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
							renderWorkspaceChangesList(c, state, list, files, selected, "fixture", true, false, workspaceChangesCallbacks{
								checkState: func(string) ui.CheckState { return ui.CheckOn },
								include:    func(string, bool) {}, selectFiles: func(ids []string) { selected = ids }, review: func(string) {},
							})
						})
					}, width, 300)
					tt.SetDark(dark)
					tt.SetScale(scale)
					tt.Advance(time.Second)
					bounds := visibleControl(t, tt, locales.In("en").ChangedFiles(), width, 300)
					row, ok := tt.AccessByRole(ui.RoleListItem, files[0].Path)
					if !ok {
						t.Fatal("selected file row missing")
					}
					if row.Bounds.X+row.Bounds.W > bounds.X+bounds.W-trackWidth {
						t.Fatalf("selected row extends under the scrollbar track: row=%+v list=%+v track=%v", row.Bounds, bounds, trackWidth)
					}
					// The whole selection surface must end before the native track,
					// including while the thumb grows during a drag.
					x, y := bounds.X+bounds.W-trackWidth/2, row.Bounds.Y+row.Bounds.H/2
					tt.Move(x, y)
					tt.Advance(time.Second)
					image := tt.Image()
					selectedPixel := image.RGBAAt(int((row.Bounds.X+row.Bounds.W-2)*scale), int(y*scale))
					for column := int((bounds.X + bounds.W - trackWidth) * scale); column < int((bounds.X+bounds.W)*scale); column++ {
						if image.RGBAAt(column, int(y*scale)) == selectedPixel {
							t.Fatal("selection background reaches the scrollbar track")
						}
					}
					saveDesignEvidence(t, tt, fmt.Sprintf("file-scrollbar-%d-dark-%v-scale-%v.png", width, dark, scale))
					tt.Press(x, y)
					tt.Move(x, y+100)
					tt.Release(x, y+100)
					if moved, found := tt.AccessByRole(ui.RoleListItem, files[0].Path); found && moved.Bounds.Y >= row.Bounds.Y {
						t.Fatal("reserved scrollbar no longer scrolls the list")
					}
				})
			}
		}
	}
}

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

func TestWorkspaceFileClickAndNavigationOnlySelectUntilExplicitReview(t *testing.T) {
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
	if len(reviews) != 0 || !slices.Equal(selected, []string{"a"}) {
		t.Fatalf("ordinary click must only select: selected=%v review=%v", selected, reviews)
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

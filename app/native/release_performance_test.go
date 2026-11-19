package main

import (
	"fmt"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

// Exercise the production projection as well as the virtualized list. A list
// benchmark alone misses work performed before MyGo chooses its visible rows.
func BenchmarkWorkspaceChangedFilesFrames(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			a := newApp(nil, nil)
			defer a.close()
			a.core, a.ready = &backend.Kernel{}, true
			files := make([]backend.KernelFile, count)
			ids := make([]string, count)
			for i := range files {
				ids[i] = fmt.Sprintf("file-%05d", i)
				files[i] = backend.KernelFile{ID: ids[i], Path: ids[i] + ".txt", Status: "Modified", Inclusion: "all"}
			}
			a.coreSnapshot = &backend.KernelSnapshot{
				Selected:  &backend.KernelSelected{Files: files},
				Workspace: &backend.KernelWorkspace{VisibleFileIDs: ids},
			}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Children(func() { a.workspaceChangesList(c) })
			}, 500, 620)
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tt.Frame()
			}
		})
	}
}

func BenchmarkWorkspaceAllFilesCheckbox(b *testing.B) {
	a := newApp(nil, nil)
	defer a.close()
	a.status = &backend.Status{}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{}}
	for i := 0; i < 10000; i++ {
		path := fmt.Sprintf("file-%05d.txt", i)
		a.status.Files = append(a.status.Files, backend.File{Path: path})
		a.coreSnapshot.Selected.Files = append(a.coreSnapshot.Selected.Files, backend.KernelFile{Path: path, Inclusion: "all"})
		a.included[path] = true
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if a.allFileCheckState() != ui.CheckOn {
			b.Fatal("lost full inclusion")
		}
	}
}

func TestAllFilesCheckboxPreservesOptimisticAndPartialSelection(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.status = &backend.Status{Files: []backend.File{{Path: "first"}, {Path: "second"}}}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{Files: []backend.KernelFile{
		{Path: "first", Inclusion: "all"}, {Path: "second", Inclusion: "all"},
		{Path: "stale", Inclusion: "partial"},
	}}}
	for _, tc := range []struct {
		first, second bool
		partial       string
		want          ui.CheckState
	}{
		{false, false, "all", ui.CheckOff},
		{true, true, "all", ui.CheckOn},
		{true, false, "all", ui.CheckMixed},
		{true, true, "partial", ui.CheckMixed},
		{false, false, "partial", ui.CheckMixed},
	} {
		a.included["first"], a.included["second"] = tc.first, tc.second
		a.coreSnapshot.Selected.Files[0].Inclusion = tc.partial
		if got := a.allFileCheckState(); got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
}

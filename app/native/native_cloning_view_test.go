package main

import (
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestCloningViewPreservesOriginalProgressAndDescription(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	description := strings.Repeat("Receiving objects from the original Git process. ", 12)
	a.coreSnapshot = &backend.KernelSnapshot{CloningRepository: &backend.KernelCloningRepository{Repository: backend.KernelRepository{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 9, Path: "fixture/cloning"}, Name: "Project in progress", Kind: "cloning"}}}
	a.coreSnapshot.CloningRepository.Progress.Description = &description
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() { a.noProject(c, locales.In(c.Locale())) })
	}, 900, 620)
	tt.SetLocale("en")
	for _, progress := range []float64{0, 0.45, 1} {
		a.coreSnapshot.CloningRepository.Progress.Value = progress
		tt.Advance(time.Second)
		if tt.HasText(locales.In("en").NoRepository()) || tt.HasText(locales.In("en").ChooseProject()) {
			t.Fatal("clone was presented as no project")
		}
		if !tt.HasText(locales.In("en").CloningProject("Project in progress")) {
			t.Fatal("clone title missing")
		}
		status, ok := tt.AccessByRole(ui.RoleStatus, description)
		if !ok || status.Bounds.Y+status.Bounds.H > 620 || status.Bounds.H <= 0 {
			t.Fatalf("clone description is clipped: %+v", status)
		}
	}
}

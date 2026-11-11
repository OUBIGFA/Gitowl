package main

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

func enableCommitCapability(t *testing.T, h *viewHarness) {
	t.Helper()
	h.a.connect("")
	h.answer(t, "info", backend.Info{Protocol: 1, Version: "0.0.1", Capabilities: []string{"repository.open", "repository.status", "repository.history", "repository.changeset", "repository.diff", "repository.commit"}}, nil)
}

func TestNoCommitCapabilityRemainsReadOnly(t *testing.T) {
	h := newViewHarness(t)
	h.a.connect("")
	h.answer(t, "info", backend.Info{Protocol: 1, Version: "0.0.1", Capabilities: []string{"repository.open", "repository.status"}}, nil)
	h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}})
	h.answer(t, "diff", patch("first.txt", "old", "new"), nil)
	h.a.commitSummary = "not permitted"
	h.a.included["first.txt"] = true
	h.a.saveVersion()
	if h.a.commitCapable || h.a.savingVersion || h.tt.HasText("Save version") || !h.tt.HasText("Read-only project browser") {
		t.Fatalf("read-only service exposed a write: %q", h.tt.Texts())
	}
	select {
	case request := <-h.b.requests:
		t.Fatalf("read-only service received %s", request.method)
	default:
	}
}

func TestNativeCommitFailureKeepsDraftAndDoesNotRetry(t *testing.T) {
	h := newViewHarness(t)
	enableCommitCapability(t, h)
	h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}, {Path: "second.txt", Status: "Untracked"}})
	h.answer(t, "diff", patch("first.txt", "old", "new"), nil)
	h.click(t, "Include first.txt")
	h.click(t, "Version summary")
	h.tt.Type("Save current work")
	if !h.a.descriptionOpen {
		h.click(t, "Description (optional)")
	}
	h.tt.Advance(250 * time.Millisecond)
	h.click(t, "Version description")
	h.tt.Type("Description\nsecond line")
	if !h.a.canSaveVersion() {
		t.Fatalf("valid draft disabled: summary=%q paths=%q", h.a.commitSummary, h.a.includedPaths())
	}
	h.click(t, "Save version")
	r := h.request(t, "commit")
	if !reflect.DeepEqual(r.paths, []string{"first.txt"}) || r.summary != "Save current work" || r.description != "Description\nsecond line" || r.id != 1 {
		t.Fatalf("whole-file commit request: %+v", r)
	}
	h.click(t, "Open project…")
	h.click(t, "Save version")
	h.a.openRepository("C:/repos/other")
	h.a.saveVersion()
	if !h.a.savingVersion || h.a.openModal || h.a.repo.ID != 1 || !h.tt.HasText("Saving local version…") {
		t.Fatalf("write did not lock repository/repeated action: %q", h.tt.Texts())
	}
	r.reply <- readReply{err: errors.New("pre-commit hook rejected changes")}
	h.apply(t)
	if h.a.savingVersion || h.a.saveErr == nil || h.a.commitSummary != "Save current work" || h.a.commitDescription != "Description\nsecond line" || !h.a.included["first.txt"] {
		t.Fatalf("failed write lost draft: summary=%q description=%q selected=%v error=%v", h.a.commitSummary, h.a.commitDescription, h.a.included, h.a.saveErr)
	}
	if !h.tt.HasText("Not retried. Check history before trying again.") {
		t.Fatalf("failure omitted recovery guidance: %q", h.tt.Texts())
	}
	select {
	case unexpected := <-h.b.requests:
		t.Fatalf("failed write retried or refreshed: %s", unexpected.method)
	default:
	}
}

func TestWholeCollectionCheckboxSelectionAndMinimumWindow(t *testing.T) {
	h := newViewHarness(t)
	enableCommitCapability(t, h)
	files := make([]backend.File, 10000)
	for i := range files {
		files[i] = backend.File{Path: fmt.Sprintf("file-%05d.txt", i), Status: "Modified"}
	}
	h.a.repo, h.a.status = &backend.Repository{ID: 1, Name: "example"}, &backend.Status{Files: files}
	h.tt.SetSize(900, 620)
	h.click(t, "Select all files")
	if len(h.a.included) != len(files) || !h.a.included["file-09999.txt"] || len(h.a.includedPaths()) != len(files) {
		t.Fatalf("select-all covered only visible rows: selected=%d", len(h.a.included))
	}
	h.click(t, "Select all files")
	if len(h.a.included) != 0 {
		t.Fatal("unselect-all left files included")
	}
	button, found := h.tt.Find("Save version")
	if !found || button.X < projectRailWidth || button.X+button.W > 900 || button.Y+button.H > 620 || button.H != 40 {
		t.Fatalf("native commit form outside minimum window: %+v / %v", button, found)
	}
	_, last := h.a.changesList.list().Visible()
	if last > 100 {
		t.Fatalf("checkbox list stopped virtualizing: last=%d", last)
	}
}

func TestCommitBlockedByConflictOperationAndUnconfirmedStatus(t *testing.T) {
	h := newViewHarness(t)
	enableCommitCapability(t, h)
	h.a.repo = &backend.Repository{ID: 1, Name: "example"}
	h.a.status = &backend.Status{Files: []backend.File{{Path: "file.txt", Status: "Modified"}}}
	h.a.included["file.txt"], h.a.commitSummary = true, "summary"
	operation := "CherryPick"
	for _, test := range []struct {
		conflict bool
		op       *string
		loading  bool
		err      error
	}{
		{conflict: true}, {op: &operation}, {loading: true}, {err: errors.New("status failed")},
	} {
		h.a.status.Conflicted, h.a.status.Operation, h.a.loadingStatus, h.a.statusErr = test.conflict, test.op, test.loading, test.err
		h.a.saveVersion()
		if h.a.canSaveVersion() || h.a.savingVersion {
			t.Fatalf("unsafe commit enabled: %+v", test)
		}
	}
	select {
	case request := <-h.b.requests:
		t.Fatalf("unsafe commit sent %s", request.method)
	default:
	}
}

func TestSettingsShowsProductVersionAndInjectedBuild(t *testing.T) {
	old := BuildNumber
	BuildNumber = "12345"
	t.Cleanup(func() { BuildNumber = old })
	h := newViewHarness(t)
	h.click(t, "Settings")
	if !h.tt.HasText("Version: 0.0.1") || !h.tt.HasText("Build: 12345") {
		t.Fatalf("version/build missing: %q", h.tt.Texts())
	}
	h.tt.SetLocale("zh-Hans")
	if !h.tt.HasText("版本：0.0.1") || !h.tt.HasText("构建号：12345") || h.tt.HasText("Build: 12345") {
		t.Fatalf("version/build language mixed: %q", h.tt.Texts())
	}
}

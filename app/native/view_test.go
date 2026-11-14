package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type readReply struct {
	value any
	err   error
}

type readRequest struct {
	method      string
	id          int
	path        string
	commit      string
	skip        int
	limit       int
	paths       []string
	summary     string
	description string
	reply       chan readReply
}

// controlledBackend delays actual production-model reads to test selection
// races. It contains no view, patch parsing, or startup demonstration data.
type controlledBackend struct {
	requests chan *readRequest
	stop     chan struct{}
}

func readResult[T any](b *controlledBackend, request *readRequest) (T, error) {
	request.reply = make(chan readReply, 1)
	var zero T
	select {
	case b.requests <- request:
	case <-b.stop:
		return zero, context.Canceled
	}
	// Deliberately finish even after context cancellation: a service may
	// already be reading Git. Production must still reject the stale result.
	select {
	case r := <-request.reply:
		if r.err != nil {
			return zero, r.err
		}
		return r.value.(T), nil
	case <-b.stop:
		return zero, context.Canceled
	}
}

func (b *controlledBackend) Info(context.Context) (backend.Info, error) {
	return readResult[backend.Info](b, &readRequest{method: "info"})
}
func (b *controlledBackend) Open(_ context.Context, path string) (backend.Repository, error) {
	return readResult[backend.Repository](b, &readRequest{method: "open", path: path})
}
func (b *controlledBackend) Status(_ context.Context, id int) (backend.Status, error) {
	return readResult[backend.Status](b, &readRequest{method: "status", id: id})
}
func (b *controlledBackend) History(_ context.Context, id, skip, limit int) ([]backend.Commit, error) {
	return readResult[[]backend.Commit](b, &readRequest{method: "history", id: id, skip: skip, limit: limit})
}
func (b *controlledBackend) Changeset(_ context.Context, id int, commit string) (backend.Changeset, error) {
	return readResult[backend.Changeset](b, &readRequest{method: "changeset", id: id, commit: commit})
}
func (b *controlledBackend) Diff(_ context.Context, id int, path, commit string) (backend.Diff, error) {
	return readResult[backend.Diff](b, &readRequest{method: "diff", id: id, path: path, commit: commit})
}

func (b *controlledBackend) Commit(_ context.Context, id int, paths []string, summary, description string) (backend.CommitResult, error) {
	r := &readRequest{method: "commit", id: id, paths: paths, summary: summary, description: description, reply: make(chan readReply, 1)}
	select {
	case b.requests <- r:
	case <-b.stop:
		return backend.CommitResult{}, context.Canceled
	}
	select {
	case reply := <-r.reply:
		if reply.err != nil {
			return backend.CommitResult{}, reply.err
		}
		return backend.CommitResult{}, errors.New("commit confirmation requires the real Node service, not a UI test double")
	case <-b.stop:
		return backend.CommitResult{}, context.Canceled
	}
}

type viewHarness struct {
	a       *app
	tt      *ui.Tester
	b       *controlledBackend
	updates chan func()
	pending []*readRequest
}

func waitEvent[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Minute):
		t.Fatal("blocked waiting for UI/backend event")
		var zero T
		return zero
	}
}

func newViewHarness(t *testing.T) *viewHarness {
	t.Helper()
	logo, err := loadLogo("resources")
	if err != nil {
		t.Fatal(err)
	}
	b := &controlledBackend{requests: make(chan *readRequest, 32), stop: make(chan struct{})}
	a := newApp(b, logo)
	updates := make(chan func(), 128)
	a.update = func(fn func()) { updates <- fn }
	h := &viewHarness{a: a, b: b, updates: updates}
	h.tt = ui.NewTester(a.view, 1180, 760)
	h.tt.SetLocale("en")
	t.Cleanup(func() { a.close(); close(b.stop) })
	return h
}

func (h *viewHarness) click(t *testing.T, label string) {
	t.Helper()
	if err := h.tt.Click(label); err != nil {
		t.Fatalf("click %q: %v; texts %q", label, err, h.tt.Texts())
	}
}

func (h *viewHarness) request(t *testing.T, method string) *readRequest {
	t.Helper()
	for i, request := range h.pending {
		if request.method == method {
			h.pending = append(h.pending[:i], h.pending[i+1:]...)
			return request
		}
	}
	for {
		request := waitEvent(t, h.b.requests)
		if request.method == method {
			return request
		}
		h.pending = append(h.pending, request)
	}
}

func (h *viewHarness) apply(t *testing.T) {
	t.Helper()
	waitEvent(t, h.updates)()
	h.tt.Frame()
}

func (h *viewHarness) answer(t *testing.T, method string, value any, err error) *readRequest {
	t.Helper()
	r := h.request(t, method)
	r.reply <- readReply{value, err}
	h.apply(t)
	return r
}

func (h *viewHarness) open(t *testing.T, files []backend.File) {
	t.Helper()
	h.a.ready = true
	h.tt.Frame()
	h.click(t, locales.In("en").OpenRepository())
	h.tt.Type("C:/repos/example")
	h.click(t, "Open")
	r := h.answer(t, "open", backend.Repository{ID: 1, Path: "C:/repos/example", Name: "example"}, nil)
	if r.path != "C:/repos/example" {
		t.Fatalf("open path %q", r.path)
	}
	branch := "main"
	h.answer(t, "status", backend.Status{Branch: &branch, Files: files}, nil)
}

func patch(name, before, after string) backend.Diff {
	text := fmt.Sprintf("diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -7,2 +7,2 @@\n context\n-%s\n+%s\n", name, name, name, name, before, after)
	return backend.Diff{Kind: "text", Text: &text}
}

func TestNativeOpenStatusAndPatch(t *testing.T) {
	h := newViewHarness(t)
	h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}})
	r := h.answer(t, "diff", patch("first.txt", "before", "after"), nil)
	if r.id != 1 || r.path != "first.txt" || r.commit != "" {
		t.Fatalf("working-tree diff request: %+v", r)
	}
	if !h.tt.HasText("example") || !h.tt.HasText("File changes") || !h.tt.HasText("Changed files") || h.tt.HasText("Current changes") || h.tt.HasText("1 changed file") || h.tt.HasText("@@ -7,2 +7,2 @@") {
		t.Fatalf("overview did not keep the page hierarchy compact: %q", h.tt.Texts())
	}
	h.click(t, "Branch details")
	if !h.tt.HasText("Branch: main") {
		t.Fatal("real branch details are not reachable")
	}
	h.tt.Key(0, ui.KeyEscape)
	h.click(t, "Review first.txt")
	if h.a.preview == nil || len(h.a.preview.patches) != 1 {
		t.Fatal("native patch viewer not prepared")
	}
	diff := h.a.preview.patches[0].state
	if added, deleted := diff.Stats(); added != 1 || deleted != 1 || diff.Editable {
		t.Fatalf("native read-only diff: added=%d deleted=%d editable=%v", added, deleted, diff.Editable)
	}
	hunk := h.a.preview.patches[0].file.Hunks[0]
	if hunk.OldStart != 7 || hunk.NewStart != 7 || !h.tt.HasText("Changed section") || diff.Layout != editor.DiffUnified || diff.Old.String() != "context\nbefore" || diff.New.String() != "context\nafter" {
		t.Fatalf("review context or original file line numbers were lost: %q / %+v", h.tt.Texts(), hunk)
	}
}

func TestOldFileReplyCannotReplaceSelection(t *testing.T) {
	h := newViewHarness(t)
	h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}, {Path: "second.txt", Status: "Modified"}})
	old := h.request(t, "diff")
	h.click(t, "Review second.txt")
	current := h.request(t, "diff")
	if current.path != "second.txt" {
		t.Fatalf("selected path: %q", current.path)
	}
	current.reply <- readReply{value: patch("second.txt", "old second", "current second")}
	h.apply(t)
	old.reply <- readReply{value: patch("first.txt", "old first", "stale first")}
	h.apply(t)
	if h.a.changeIndex != 1 || h.a.preview.patches[0].state.New.String() != "context\ncurrent second" {
		t.Fatalf("stale diff replaced selection: index=%d preview=%+v", h.a.changeIndex, h.a.preview)
	}
}

func TestOldRepositoryReplyCannotReplaceSelection(t *testing.T) {
	h := newViewHarness(t)
	h.a.ready = true
	h.tt.Frame()
	h.click(t, "Open project…")
	h.tt.Type("C:/repos/old")
	h.click(t, "Open")
	old := h.request(t, "open")
	h.click(t, "Project folder")
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("C:/repos/current")
	h.click(t, "Open")
	current := h.request(t, "open")
	current.reply <- readReply{value: backend.Repository{ID: 2, Path: current.path, Name: "current"}}
	h.apply(t)
	old.reply <- readReply{value: backend.Repository{ID: 1, Path: old.path, Name: "stale"}}
	h.apply(t)
	if h.a.repo == nil || h.a.repo.ID != 2 || h.tt.HasText("stale") {
		t.Fatalf("stale open replaced repository: %+v", h.a.repo)
	}
	h.answer(t, "status", backend.Status{Files: []backend.File{}}, nil)
}

func TestHistorySelectionReadsChangesetAndCommitDiff(t *testing.T) {
	h := newViewHarness(t)
	h.open(t, []backend.File{})
	h.click(t, "History")
	commits := []backend.Commit{
		{SHA: "aaaaaaa111", ShortSHA: "aaaaaaa", Summary: "First commit", AuthorName: "Author", Date: "2026-01-01T00:00:00Z", Tags: []string{}},
		{SHA: "bbbbbbb222", ShortSHA: "bbbbbbb", Summary: "Second commit", AuthorName: "Author", Date: "2026-01-02T00:00:00Z", Tags: []string{"v1"}},
	}
	r := h.answer(t, "history", commits, nil)
	if r.skip != 0 || r.limit != historyPage {
		t.Fatalf("history pagination: %+v", r)
	}
	old := h.request(t, "changeset")
	h.click(t, "Second commit")
	current := h.request(t, "changeset")
	current.reply <- readReply{value: backend.Changeset{Files: []backend.File{{Path: "new.txt", Status: "Renamed", OldPath: "old.txt"}}, LinesAdded: 1, LinesDeleted: 1}}
	h.apply(t)
	old.reply <- readReply{value: backend.Changeset{Files: []backend.File{{Path: "stale.txt", Status: "Added"}}}}
	h.apply(t)
	diff := h.answer(t, "diff", patch("new.txt", "before", "after"), nil)
	if diff.commit != "bbbbbbb222" || current.commit != "bbbbbbb222" || diff.path != "new.txt" {
		t.Fatalf("commit selection requests: %+v %+v", current, diff)
	}
	if h.tt.HasText("@@ -7,2 +7,2 @@") {
		t.Fatal("history file overview exposed a permanent diff pane")
	}
	h.click(t, "Review new.txt")
	if h.a.commitIndex != 1 || h.a.changeset.Files[0].Path != "new.txt" || h.tt.HasText("stale.txt") || !h.tt.HasText("Previous path: old.txt") {
		t.Fatalf("history selection: %q", h.tt.Texts())
	}
}

func TestNativeErrorsDoNotInventEmptySuccess(t *testing.T) {
	h := newViewHarness(t)
	h.a.serviceErr = errors.New("service.js not found")
	h.tt.Frame()
	if !h.tt.HasText("Repository service unavailable: service.js not found") || h.tt.HasText("Working tree is clean.") || h.tt.HasText("No commits in this repository.") {
		t.Fatalf("missing-service state: %q", h.tt.Texts())
	}
	h.a.serviceErr, h.a.ready = nil, true
	h.tt.Frame()
	h.click(t, "Open project…")
	h.tt.Type("C:/repos/example")
	h.click(t, "Open")
	h.answer(t, "open", backend.Repository{ID: 1, Path: "C:/repos/example", Name: "example"}, nil)
	h.answer(t, "status", backend.Status{}, errors.New("Git status failed"))
	if !h.tt.HasText("Could not read changes: Git status failed") || h.tt.HasText("Working tree is clean.") {
		t.Fatalf("failed status became clean tree: %q", h.tt.Texts())
	}
}

func TestNativeLanguageAndFollowSystemActions(t *testing.T) {
	h := newViewHarness(t)
	var requested []string
	override := "zh-Hans"
	h.a.localeOverride = func() string { return override }
	h.a.setLocale = func(tag string) {
		requested = append(requested, tag)
		override = tag
	}
	h.tt.SetLocale("zh-Hans")
	if !h.tt.HasText("打开项目…") || !h.tt.HasText("只读项目浏览") || h.tt.HasText("Open project…") || h.tt.HasText("Read-only project browser") {
		t.Fatalf("mixed-language view: %q", h.tt.Texts())
	}
	h.click(t, "设置")
	h.click(t, "简体中文")
	h.click(t, "English")
	if len(requested) != 1 || requested[0] != "en" {
		t.Fatalf("language action: %q", requested)
	}
	h.tt.SetLocale("en")
	if h.tt.HasText("Follow system") {
		t.Fatal("follow-system is still a separate control outside the language menu")
	}
	h.click(t, "English")
	h.click(t, "Follow system")
	if len(requested) != 2 || requested[1] != "" {
		t.Fatalf("system language action: %q", requested)
	}
	h.tt.SetLocale("zh-Hans")
	control, ok := h.tt.AccessByRole(ui.RolePopUpButton, "界面语言")
	if !ok || control.Value != "跟随系统" {
		t.Fatalf("system choice replaced by its resolved language: %+v / %v", control, ok)
	}
	h.tt.SetLocale("en")
	h.click(t, "Close")
	h.tt.SetLocale("zh-Hant")
	if !h.tt.HasText("開啟專案…") || h.tt.HasText("Open project…") {
		t.Fatalf("traditional Chinese view: %q", h.tt.Texts())
	}
}

func TestMinimumWindowAndVirtualFileList(t *testing.T) {
	h := newViewHarness(t)
	files := make([]backend.File, 10000)
	for i := range files {
		files[i] = backend.File{Path: fmt.Sprintf("file-%05d.txt", i), Status: "Modified"}
	}
	h.a.ready, h.a.commitCapable = true, true
	h.a.repo = &backend.Repository{ID: 1, Name: "A long project name must not push real actions outside the window"}
	h.a.status = &backend.Status{Files: files}
	h.tt.SetTitleBar(ui.TitleBar{Height: 48, Right: 138})
	h.tt.SetSize(900, 620)
	row, found := h.tt.Find("Review file-00000.txt")
	if !found || row.X < projectRailWidth || row.X+row.W > 900 || row.H != 48 || h.tt.HasText("Collapse diff") {
		t.Fatalf("minimum-window central overview row: %+v / %v", row, found)
	}
	first, last := h.a.changesList.list().Visible()
	if first != 0 || last < first || last > 100 || h.tt.HasText("file-09999.txt") {
		t.Fatalf("list did not virtualize: visible %d..%d, text count %d", first, last, len(h.tt.Texts()))
	}
	for _, label := range []string{"Open project…", "Refresh", "Settings", "Version summary", "Save version"} {
		r, found := h.tt.Find(label)
		if !found || r.X < 0 || r.X+r.W > 900 || r.Y+r.H > 620 {
			t.Fatalf("control %q outside minimum window: %+v / %v", label, r, found)
		}
	}
	settings, _ := h.tt.Find("Settings")
	if settings.X >= projectRailWidth || settings.Y < 550 {
		t.Fatalf("settings did not stay at the rail's bottom: %+v", settings)
	}
}

func TestPreviewKindsUseProductionNativeView(t *testing.T) {
	for _, test := range []struct {
		kind    string
		details string
		message string
	}{
		{"binary", "", "Binary file — no text preview."},
		{"unrenderable", "", "This file cannot be previewed."},
		{"large-text", "", "This text diff is too large for the service to return."},
		{"submodule", `{"oldSha":"abc","newSha":"def"}`, "Submodule changes"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			h := newViewHarness(t)
			p, err := preparePreview(backend.Diff{Kind: test.kind, Submodule: []byte(test.details)}, "file")
			if err != nil {
				t.Fatal(err)
			}
			h.a.repo, h.a.ready, h.a.preview = &backend.Repository{ID: 1, Name: "example"}, true, p
			h.a.reviewingFile = true
			h.tt.Frame()
			if !h.tt.HasText(test.message) {
				t.Fatalf("%s preview: %q", test.kind, h.tt.Texts())
			}
		})
	}
}

func TestPatchParsingRejectsInvalidAndPreservesAllParts(t *testing.T) {
	invalid := "not a patch"
	if _, err := preparePreview(backend.Diff{Kind: "text", Text: &invalid}, "file.txt"); err == nil {
		t.Fatal("invalid patch was silently accepted")
	}
	first, second := patch("a.txt", "a", "b"), patch("a.txt", "b", "c")
	text := *first.Text + *second.Text
	p, err := preparePreview(backend.Diff{Kind: "text", Text: &text}, "a.txt")
	if err != nil || len(p.patches) != 2 {
		t.Fatalf("patch parts: %+v, %v", p, err)
	}
	h := newViewHarness(t)
	h.a.repo, h.a.ready, h.a.preview = &backend.Repository{ID: 1, Name: "example"}, true, p
	h.a.reviewingFile = true
	h.tt.SetSize(900, 620)
	if p.patches[0].state.Layout != editor.DiffUnified || p.patches[1].state.Layout != editor.DiffUnified || !p.patches[0].state.FitContent {
		t.Fatal("multiple patches did not use native responsive viewers")
	}
	if !strings.Contains(p.patches[1].state.New.String(), "c") {
		t.Fatal("second patch was hidden")
	}
}

func TestCatalogsHaveOneLanguagePerView(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hans", "zh-Hant"} {
		tx := locales.In(tag)
		h := newViewHarness(t)
		h.tt.SetLocale(tag)
		if !h.tt.HasText(tx.OpenRepository()) || !h.tt.HasText(tx.Settings()) || !h.tt.HasText(tx.ReadOnly()) {
			t.Fatalf("catalog %s: %q", tag, h.tt.Texts())
		}
	}
}

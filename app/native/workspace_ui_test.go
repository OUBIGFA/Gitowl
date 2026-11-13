package main

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func visibleControl(t *testing.T, tt *ui.Tester, label string, width, height int) ui.Rect {
	t.Helper()
	r, found := tt.Find(label)
	if !found || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+1 || r.Y+r.H > float32(height)+1 {
		t.Fatalf("%q outside %d×%d: %+v / %v; texts %q", label, width, height, r, found, tt.Texts())
	}
	return r
}

func TestComposerSecondaryTextFollowsSystemTextScale(t *testing.T) {
	h := newViewHarness(t)
	enableCommitCapability(t, h)
	h.a.repo = &backend.Repository{ID: 1, Name: "example"}
	h.a.status = &backend.Status{Files: []backend.File{{Path: "first.txt", Status: "Modified"}}}
	h.tt.SetSize(1440, 900)
	label := locales.In("en").SaveShortcut()
	h.tt.Frame()
	normal, ok := h.tt.AccessByRole(ui.RoleStatus, label)
	if !ok {
		t.Fatalf("composer status missing: %q", h.tt.Texts())
	}
	h.tt.SetPreferences(ui.Preferences{TextScale: 1.5})
	h.tt.Frame()
	large, ok := h.tt.AccessByRole(ui.RoleStatus, label)
	if !ok || large.Bounds.H < normal.Bounds.H*1.4 {
		t.Fatalf("secondary text ignored system scaling: normal=%+v large=%+v", normal.Bounds, large.Bounds)
	}
	visibleControl(t, h.tt, locales.In("en").SaveVersion(), 1440, 900)
}

func typeDraft(t *testing.T, h *viewHarness, tx locales.Messages, summary, description string) {
	t.Helper()
	h.click(t, tx.CommitSummary())
	h.tt.Type(summary)
	if description != "" {
		if !h.a.descriptionOpen {
			h.click(t, tx.CommitDescription())
			h.tt.Advance(250 * time.Millisecond)
		}
		h.click(t, tx.VersionDescription())
		h.tt.Type(description)
	}
}

// The same production view is exercised at every size, in one language at a
// time. Controlled reads test navigation; only the real integration can confirm
// a successful commit.
func TestWorkspaceOverviewReviewBackAcrossSizesAndLanguages(t *testing.T) {
	for _, size := range []struct{ width, height int }{{900, 620}, {1180, 760}, {1920, 1080}} {
		for _, tag := range []string{"en", "zh-Hans", "zh-Hant"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, tag), func(t *testing.T) {
				h := newViewHarness(t)
				enableCommitCapability(t, h)
				h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}, {Path: "second.txt", Status: "Untracked"}})
				h.answer(t, "diff", patch("first.txt", "before", "after"), nil)
				h.tt.SetSize(size.width, size.height)
				h.tt.SetTitleBar(ui.TitleBar{Height: 48, Right: 138})
				h.tt.SetLocale(tag)
				tx := locales.In(tag)
				for _, label := range []string{tx.OpenRepository(), tx.Changes(), tx.History(), tx.Settings(), tx.SelectedOfTotal(0, 2), tx.IncludeAll(), tx.CommitSummary(), tx.SaveVersion()} {
					visibleControl(t, h.tt, label, size.width, size.height)
				}
				for _, other := range []string{"en", "zh-Hans", "zh-Hant"} {
					if h.tt.HasText(locales.In(other).CurrentChanges()) {
						t.Fatal("the changes page repeats its tab as a large heading")
					}
					if other != tag && h.tt.HasText(locales.In(other).IncludeAll()) {
						t.Fatalf("mixed system languages: %q", h.tt.Texts())
					}
				}
				rail := visibleControl(t, h.tt, tx.Settings(), size.width, size.height)
				if rail.X+rail.W > projectRailWidth || rail.Y < float32(size.height-100) {
					t.Fatalf("settings left the bottom project rail: %+v", rail)
				}
				row := visibleControl(t, h.tt, tx.OpenFile("first.txt"), size.width, size.height)
				checkbox := visibleControl(t, h.tt, tx.IncludeFile("first.txt"), size.width, size.height)
				if row.X < projectRailWidth || row.H != 48 || checkbox.X+checkbox.W > row.X {
					t.Fatalf("include and review are not separate central actions: row=%+v checkbox=%+v", row, checkbox)
				}
				h.click(t, tx.IncludeFile("first.txt"))
				if h.a.reviewingFile {
					t.Fatal("including a file opened its review")
				}
				summary, description := "Review the itinerary", "Keep the unselected changes\nfor another version"
				if tag != "en" {
					summary, description = "调整行程安排", "保留未选中的改动\n下次再保存"
				}
				typeDraft(t, h, tx, summary, description)
				before := visibleControl(t, h.tt, tx.SaveVersion(), size.width, size.height)
				// The outlined composer adds its own 12px inset and 1px border
				// inside the 16px canvas gutter. Keep Save in the bottom 32px.
				if before.H != 40 || before.X < projectRailWidth || before.Y+before.H < float32(size.height-32) {
					t.Fatalf("primary action is not naturally visible at the canvas bottom: %+v", before)
				}
				h.click(t, tx.OpenFile("first.txt"))
				if !h.a.reviewingFile || h.tt.HasText(tx.OpenFile("second.txt")) || h.tt.HasText(tx.IncludeAll()) {
					t.Fatalf("review did not replace the overview: %q", h.tt.Texts())
				}
				diff := visibleControl(t, h.tt, tx.DiffTitle(), size.width, size.height)
				padding := float32(32)
				if size.width < 1100 {
					padding = 24
				}
				canvasWidth := min(1440, float32(size.width)-projectRailWidth) - 2*padding
				if diff.X < projectRailWidth || diff.W < canvasWidth-1 {
					t.Fatalf("focused native diff is not canvas-wide: %+v, canvas %v", diff, canvasWidth)
				}
				layout := editor.DiffSplit
				if canvasWidth < 760 {
					layout = editor.DiffUnified
				}
				if h.a.preview.patches[0].state.Layout != layout {
					t.Fatal("diff measured the whole window instead of its canvas")
				}
				if got := visibleControl(t, h.tt, tx.SaveVersion(), size.width, size.height); got != before {
					t.Fatalf("primary action moved on entering review: before %+v after %+v", before, got)
				}
				h.click(t, tx.BackToOverview())
				if h.a.reviewingFile || h.a.commitSummary != summary || h.a.commitDescription != description || !h.a.included["first.txt"] || h.a.included["second.txt"] || !h.a.descriptionOpen {
					t.Fatalf("review/back lost project draft or selection: %+v", h.a.included)
				}
				if got := visibleControl(t, h.tt, tx.SaveVersion(), size.width, size.height); got != before {
					t.Fatalf("primary action moved on returning: before %+v after %+v", before, got)
				}
				if !h.a.canSaveVersion() || !h.tt.HasText(tx.OpenFile("second.txt")) {
					t.Fatal("the returned overview is not ready to continue the real save workflow")
				}
			})
		}
	}
}

func openHarnessProject(t *testing.T, h *viewHarness, path, name string, id int, files []backend.File) {
	t.Helper()
	tx := locales.In("en")
	h.click(t, tx.OpenRepository())
	h.click(t, tx.RepositoryPath())
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type(path)
	h.click(t, tx.Open())
	h.answer(t, "open", backend.Repository{ID: id, Name: name, Path: path}, nil)
	h.answer(t, "status", backend.Status{Files: files}, nil)
	if len(files) > 0 {
		h.answer(t, "diff", patch(files[0].Path, "before", "after"), nil)
	}
}

func TestWorkspaceDraftsAndSelectionsBelongToEachProject(t *testing.T) {
	h := newViewHarness(t)
	enableCommitCapability(t, h)
	files := []backend.File{{Path: "first.txt", Status: "Modified"}, {Path: "second.txt", Status: "Modified"}}
	h.open(t, files)
	h.answer(t, "diff", patch("first.txt", "before A", "after A"), nil)
	tx := locales.In("en")
	h.click(t, tx.IncludeAll())
	typeDraft(t, h, tx, "Project A draft", "Only for project A")
	openHarnessProject(t, h, "C:/repos/second", "second", 2, files)
	if h.a.commitSummary != "" || h.a.commitDescription != "" || !h.a.descriptionOpen || len(h.a.included) != 0 {
		t.Fatal("a new project inherited another project's draft or selected same-name files")
	}
	// The native text-input identity also changes with the project: Undo in
	// a new project must not import the previous project's summary.
	h.click(t, tx.CommitSummary())
	h.tt.Key(ui.Cmd, ui.KeyZ)
	if h.a.commitSummary != "" {
		t.Fatal("native Undo crossed the project boundary")
	}
	h.click(t, tx.IncludeFile("second.txt"))
	typeDraft(t, h, tx, "Project B draft", "Only for project B")
	openHarnessProject(t, h, "C:/repos/example", "example", 3, files[:1])
	if h.a.commitSummary != "Project A draft" || h.a.commitDescription != "Only for project A" || !h.a.descriptionOpen || !reflect.DeepEqual(h.a.includedPaths(), []string{"first.txt"}) {
		t.Fatalf("project A draft was not restored/pruned against real status: %q %q %v", h.a.commitSummary, h.a.commitDescription, h.a.included)
	}
	openHarnessProject(t, h, "C:/repos/second", "second", 4, files)
	if h.a.commitSummary != "Project B draft" || h.a.commitDescription != "Only for project B" || !reflect.DeepEqual(h.a.includedPaths(), []string{"second.txt"}) {
		t.Fatal("project B draft or selection was overwritten while revisiting project A")
	}
}

func TestFailedOrCanceledProjectOpenPreservesActiveWorkspace(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%v", canceled), func(t *testing.T) {
			h := newViewHarness(t)
			enableCommitCapability(t, h)
			h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}})
			h.answer(t, "diff", patch("first.txt", "before", "after"), nil)
			tx := locales.In("en")
			h.click(t, tx.IncludeFile("first.txt"))
			typeDraft(t, h, tx, "Keep this project", "Keep this description")
			h.click(t, tx.OpenFile("first.txt"))
			original, status, preview := h.a.repo, h.a.status, h.a.preview
			h.click(t, tx.OpenRepository())
			h.click(t, tx.RepositoryPath())
			h.tt.Key(ui.Cmd, ui.KeyA)
			h.tt.Type("C:/repos/not-a-project")
			h.click(t, tx.Open())
			request := h.request(t, "open")
			if h.a.repo != original || h.a.status != status || h.a.repoContext.Err() != nil {
				t.Fatal("starting an open discarded or canceled the active project")
			}
			if canceled {
				h.click(t, tx.Cancel())
				request.reply <- readReply{value: backend.Repository{ID: 2, Name: "canceled", Path: request.path}}
			} else {
				request.reply <- readReply{err: errors.New("not a Git repository")}
			}
			h.apply(t)
			if !canceled {
				if h.a.openErr == nil {
					t.Fatal("failed open invented success")
				}
				h.click(t, tx.Cancel())
			}
			if h.a.repo != original || h.a.status != status || h.a.preview != preview || h.a.repoContext.Err() != nil || !h.a.reviewingFile || h.a.commitSummary != "Keep this project" || h.a.commitDescription != "Keep this description" || !h.a.included["first.txt"] {
				t.Fatal("failed/canceled open lost the active project, review, draft or selection")
			}
			visibleControl(t, h.tt, tx.SaveVersion(), 1180, 760)
		})
	}
}

func TestLongHookDiagnosticsAreExpandableCopyableAndBounded(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hans"} {
		t.Run(tag, func(t *testing.T) {
			h := newViewHarness(t)
			enableCommitCapability(t, h)
			h.a.repo = &backend.Repository{ID: 1, Name: "example", Path: "C:/repos/example"}
			h.a.status = &backend.Status{Files: []backend.File{{Path: "first.txt", Status: "Modified"}}}
			h.a.included["first.txt"], h.a.commitSummary = true, "Keep my draft"
			message := "pre-commit hook rejected changes\n" + strings.Repeat("Detailed hook output with recovery instructions\n", 1000)
			h.a.saveErr = errors.New(message)
			h.tt.SetSize(900, 620)
			h.tt.SetLocale(tag)
			tx := locales.In(tag)
			if !h.tt.HasText(tx.SaveNotConfirmed()) || !h.tt.HasText(tx.NoAutomaticRetry()) || h.tt.HasText(tx.FullErrorDetails()) {
				t.Fatal("error is not a short summary with explicit, initially collapsed details")
			}
			visibleControl(t, h.tt, tx.SaveVersion(), 900, 620)
			h.click(t, tx.ErrorDetails())
			h.tt.Advance(250 * time.Millisecond)
			details := visibleControl(t, h.tt, tx.FullErrorDetails(), 900, 620)
			if details.H != 72 {
				t.Fatalf("hook output is not bounded: %+v", details)
			}
			visibleControl(t, h.tt, tx.SaveVersion(), 900, 620)
			h.click(t, tx.FullErrorDetails())
			h.tt.Key(ui.Cmd, ui.KeyA)
			h.tt.Key(ui.Cmd, ui.KeyC)
			if h.tt.Clipboard() != message {
				t.Fatal("the complete hook diagnostic cannot be selected/copied")
			}
			if h.a.commitSummary != "Keep my draft" || !h.a.included["first.txt"] || h.a.savingVersion {
				t.Fatal("showing/copying an error changed the draft or retried the write")
			}
			select {
			case request := <-h.b.requests:
				t.Fatalf("error inspection sent %s", request.method)
			default:
			}
		})
	}
}

func TestHistoryPaginationUsesTheIndependentCanvas(t *testing.T) {
	h := newViewHarness(t)
	h.open(t, nil)
	h.click(t, "History")
	commits := make([]backend.Commit, historyPage)
	for i := range commits {
		commits[i] = backend.Commit{SHA: fmt.Sprintf("sha-%03d", i), ShortSHA: fmt.Sprintf("%03d", i), Summary: fmt.Sprintf("Version %d", i), AuthorName: "Author"}
	}
	h.answer(t, "history", commits, nil)
	h.answer(t, "changeset", backend.Changeset{Files: []backend.File{}}, nil)
	list := visibleControl(t, h.tt, "Saved versions", 1180, 760)
	if list.X < projectRailWidth || list.W < 800 || h.tt.HasText("Commit files") || h.tt.HasText("Version summary") {
		t.Fatalf("history is not an independent central page: %+v", list)
	}
	h.click(t, "Load older commits")
	request := h.request(t, "history")
	if request.skip != historyPage || request.limit != historyPage {
		t.Fatalf("pagination request: %+v", request)
	}
	request.reply <- readReply{value: []backend.Commit{commits[len(commits)-1], {SHA: "sha-100", Summary: "Older version"}}}
	h.apply(t)
	if len(h.a.commits) != historyPage+1 || h.a.historyMore || h.tt.HasText("Load older commits") {
		t.Fatal("pagination lost deduplication or its real end state")
	}
}

func contrastRatio(a, b ui.Color) float64 {
	luminance := func(c ui.Color) float64 {
		channel := func(v uint8) float64 {
			f := float64(v) / 255
			if f <= 0.04045 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
	}
	light, dark := luminance(a), luminance(b)
	return (max(light, dark) + 0.05) / (min(light, dark) + 0.05)
}

func TestWorkspaceTypeAndRenderedSelectionContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, tag := range []string{"en", "zh-Hans"} {
			t.Run(fmt.Sprintf("dark=%v/%s", dark, tag), func(t *testing.T) {
				a := newApp(nil, nil)
				defer a.close()
				a.ready, a.commitCapable = true, true
				a.repo = &backend.Repository{ID: 1, Name: "example"}
				a.status = &backend.Status{Files: []backend.File{{Path: "notes.md", Status: "Modified"}}}
				a.included["notes.md"] = true
				var theme ui.Theme
				tt := ui.NewTester(func(c *ui.Context) { a.view(c); theme = *c.Theme() }, 1180, 760)
				tt.SetLocale(tag)
				tt.SetDark(dark)
				tx := locales.In(tag)
				if theme.FontSize != 15 {
					t.Fatalf("body text shrank below the agreed 15 DIP: %v", theme.FontSize)
				}
				if runtime.GOOS == "windows" {
					font := "Segoe UI, Microsoft YaHei UI"
					if tag == "zh-Hans" {
						font = "Microsoft YaHei UI"
					}
					if theme.Font != font {
						t.Fatalf("language %s used font %q, want %q", tag, theme.Font, font)
					}
				}
				row := visibleControl(t, tt, tx.OpenFile("notes.md"), 1180, 760)
				pixel := tt.Image().RGBAAt(int(row.X+row.W/2), int(row.Y+4))
				selected := ui.RGB(pixel.R, pixel.G, pixel.B)
				for _, pair := range []struct {
					name       string
					foreground ui.Color
					background ui.Color
				}{{"selected body", theme.Text, selected}, {"selected secondary", theme.TextMuted, selected}, {"input placeholder", theme.TextMuted, theme.Surface}, {"primary", theme.AccentText, theme.Accent}} {
					if ratio := contrastRatio(pair.foreground, pair.background); ratio < 4.5 {
						t.Errorf("%s contrast %.2f:1, requires 4.5:1", pair.name, ratio)
					}
				}
				tt.SetPreferences(ui.Preferences{HighContrast: true, TextScale: 1.25})
				if theme.FontSize != 18.75 || theme.Focus.A != 255 {
					t.Fatal("workspace overrode native text scaling or high-contrast focus")
				}
			})
		}
	}
}

package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestComposerShortcutSubmitsExactlyOnceFromEitherInput(t *testing.T) {
	for _, description := range []bool{false, true} {
		name := "summary"
		if description {
			name = "description"
		}
		t.Run(name, func(t *testing.T) {
			h := newViewHarness(t)
			enableCommitCapability(t, h)
			h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}, {Path: "other.txt", Status: "Modified"}})
			h.answer(t, "diff", patch("first.txt", "before", "after"), nil)
			tx := locales.In("en")
			h.click(t, tx.IncludeFile("first.txt"))
			body := ""
			if description {
				body = "Keep this explanation"
			}
			typeDraft(t, h, tx, "Save with the keyboard", body)
			if !h.tt.HasText(tx.SaveShortcut()) || h.tt.HasText(tx.LocalVersionsOnly()) {
				t.Fatal("composer did not show its real shortcut, or retained the development limitation footer")
			}
			h.tt.Key(ui.Ctrl, ui.KeyEnter)
			request := h.request(t, "commit")
			if request.summary != "Save with the keyboard" || request.description != body || !reflect.DeepEqual(request.paths, []string{"first.txt"}) {
				t.Fatalf("shortcut changed the draft or selected whole files: %+v", request)
			}
			h.tt.Key(ui.Ctrl, ui.KeyEnter)
			h.tt.Key(ui.Ctrl, ui.KeyEnter)
			h.click(t, tx.SaveVersion())
			select {
			case duplicate := <-h.b.requests:
				t.Fatalf("an in-flight shortcut submitted a second %s", duplicate.method)
			default:
			}
			request.reply <- readReply{err: errors.New("hook rejected the version")}
			h.apply(t)
			if h.a.savingVersion || h.a.commitSummary != request.summary || h.a.commitDescription != body || !h.a.included["first.txt"] || h.a.saveErr == nil {
				t.Fatal("a rejected shortcut save lost its draft or failed to release the operating guard")
			}
		})
	}
}

func TestComposerShortcutRespectsStatusGuardsAndFocusScope(t *testing.T) {
	h := newViewHarness(t)
	enableCommitCapability(t, h)
	h.open(t, []backend.File{{Path: "first.txt", Status: "Modified"}})
	h.answer(t, "diff", patch("first.txt", "before", "after"), nil)
	tx := locales.In("en")
	h.click(t, tx.IncludeFile("first.txt"))
	typeDraft(t, h, tx, "Keep this draft", "")
	operation := "Rebase"
	for _, state := range []struct {
		conflict bool
		op       *string
		loading  bool
		err      error
	}{{conflict: true}, {op: &operation}, {loading: true}, {err: errors.New("status is unconfirmed")}} {
		h.a.status.Conflicted, h.a.status.Operation, h.a.loadingStatus, h.a.statusErr = state.conflict, state.op, state.loading, state.err
		h.tt.Frame()
		h.click(t, tx.CommitSummary())
		h.tt.Key(ui.Ctrl, ui.KeyEnter)
		if h.a.savingVersion {
			t.Fatalf("shortcut bypassed an operating/status guard: %+v", state)
		}
	}
	h.a.status.Conflicted, h.a.status.Operation, h.a.loadingStatus, h.a.statusErr = false, nil, false, nil
	h.tt.Frame()
	h.click(t, tx.Settings())
	h.tt.Key(ui.Ctrl, ui.KeyEnter)
	if h.a.savingVersion {
		t.Fatal("a shortcut in Settings submitted the canvas draft behind its native modal")
	}
	select {
	case request := <-h.b.requests:
		t.Fatalf("a guarded or out-of-composer key submitted %s", request.method)
	default:
	}
}

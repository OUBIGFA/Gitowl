package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

const flowMermaidMD = "```mermaid\nflowchart LR\n    A[Begin] --> B[End]\n```"
const seqMermaidMD = "```mermaid\nsequenceDiagram\n    Alice->>Bob: Ping\n    Bob-->>Alice: Pong\n```"

func TestMermaidReadingAndEditing(t *testing.T) {
	e, tt := newTester("Before\n\n" + flowMermaidMD + "\n\nAfter")
	r := e.rows[1]
	if !isMermaidBlock(r.leaf.Block) || r.mermaid == nil || r.mermaidErr != "" {
		t.Fatalf("mermaid block did not lay out: row=%+v", r)
	}
	if !r.mermaidHidden || r.mermaidPanel != 0 {
		t.Fatalf("expected collapsed mermaid diagram at rest, hidden=%v panel=%v", r.mermaidHidden, r.mermaidPanel)
	}

	bounds, _ := tt.Find("文档")
	tt.ClickAt(bounds.X+r.rect.X+r.rect.W/2, bounds.Y+r.rect.Y+r.mermaidTop+12)
	tt.Frame()

	r = e.rows[1]
	if r.mermaidHidden || r.mermaidPanel == 0 || r.y <= r.rect.Y+r.mermaidTop {
		t.Fatalf("click did not expand mermaid preview + source well: hidden=%v panel=%v r.y=%v", r.mermaidHidden, r.mermaidPanel, r.y)
	}

	e.SetSelection(r.leaf.End, r.leaf.End)
	tt.Frame()
	tt.Type("\n    B --> C[Done]")
	if !strings.Contains(e.mermaids[r.leaf.Block.ID].src, "C[Done]") {
		t.Fatalf("live preview did not re-parse updated source: %q", e.mermaids[r.leaf.Block.ID].src)
	}

	// Moving caret outside collapses the source well back to centered vector diagram.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	if !e.rows[1].mermaidHidden || e.rows[1].mermaidPanel != 0 {
		t.Fatalf("expected mermaid to collapse after leaving block")
	}
}

func TestMermaidTabAndDoubleEnterExit(t *testing.T) {
	e, tt := newTester("Intro\n\n" + seqMermaidMD)
	focus(e, tt)
	r := e.rows[1]
	e.SetSelection(r.leaf.Start, r.leaf.Start)
	tt.Frame()
	if e.mermaidEditing != r.leaf.Block.ID {
		t.Fatalf("expected mermaidEditing=%q, got %q", r.leaf.Block.ID, e.mermaidEditing)
	}

	// Tab exits to a new paragraph below and collapses the sequence diagram.
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if e.mermaidEditing != "" || !e.rows[1].mermaidHidden {
		t.Fatalf("Tab did not exit & collapse mermaid block: editing=%q hidden=%v", e.mermaidEditing, e.rows[1].mermaidHidden)
	}
	if len(e.Doc.Blocks) != 3 || e.Doc.Blocks[2].Kind != "paragraph" {
		t.Fatalf("Tab did not create/focus paragraph below: blocks=%+v", e.Doc.Blocks)
	}

	// Re-enter at end and exit via double Enter (Enter to trailing blank line, Enter again).
	e.SetSelection(e.rows[1].leaf.End, e.rows[1].leaf.End)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if e.mermaidEditing != "" || !e.rows[1].mermaidHidden {
		t.Fatalf("double Enter did not exit & collapse mermaid block: editing=%q hidden=%v", e.mermaidEditing, e.rows[1].mermaidHidden)
	}
}

func TestMermaidErrorStaysVisibleAndInsertHelpers(t *testing.T) {
	badMD := "```mermaid\nunknownDiagram\nfoo -> bar\n```"
	e, _ := newTester(badMD)
	r := e.rows[0]
	if r.mermaidErr == "" || r.mermaidHidden {
		t.Fatalf("malformed mermaid should remain visible with error: err=%q hidden=%v", r.mermaidErr, r.mermaidHidden)
	}

	e2, tt2 := newTester("")
	focus(e2, tt2)
	e2.InsertMermaidFlowchart()
	tt2.Frame()
	if !isMermaidBlock(e2.Doc.Blocks[0]) || e2.Doc.Blocks[0].Kind != "code" {
		t.Fatalf("InsertMermaidFlowchart did not convert empty block: %+v", e2.Doc.Blocks[0])
	}
	if !e2.rows[0].mermaidHidden && e2.rows[0].mermaid == nil {
		t.Fatalf("inserted flowchart did not parse: err=%q", e2.rows[0].mermaidErr)
	}

	e3, tt3 := newTester("First")
	focus(e3, tt3)
	e3.SetSelection(e3.Len(), e3.Len())
	e3.InsertMermaidSequence()
	tt3.Frame()
	if len(e3.Doc.Blocks) != 2 || !isMermaidBlock(e3.Doc.Blocks[1]) || e3.mermaidEditing != e3.Doc.Blocks[1].ID {
		t.Fatalf("InsertMermaidSequence did not insert and focus block: blocks=%+v editing=%q", e3.Doc.Blocks, e3.mermaidEditing)
	}
	for _, it := range turnItems(document.NewBlock("paragraph", "hi")) {
		if it.kind == "mermaid-flow" || it.kind == "mermaid-seq" {
			t.Fatalf("mermaid insert item %q leaked into turnItems", it.kind)
		}
	}
}

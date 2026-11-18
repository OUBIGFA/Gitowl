package editor

import (
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// SetReadOnly locks this editor instance, including commands and input methods.
// It cancels provisional composition and leaves copy, find and navigation alive.
func (e *Editor) SetReadOnly(on bool) {
	if on {
		e.cancelComposition()
	}
	e.ReadOnly = on
	e.ready = false
	e.slashOpen, e.linkOpen, e.menuOpen, e.colorOpen, e.langOpen = false, false, false, false, false
	e.cellFillOpen, e.annotatePanel = false, false
	e.find.replace = false
	e.pairs = nil
	e.dragID = ""
	e.dragging = false
	e.resizing = nil
	e.imageResize = imageResize{}
	e.imageCaptionID = ""
	e.closeHeader()
}

func (e *Editor) InsertInlineMath() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
	e.Engine.Edit(func() { e.Engine.Replace(z, z, "$"); e.Engine.Replace(a, a, "$"); e.SetSelection(a+1, z+1) })
	e.sync()
	e.pairs = append(e.pairs, autoPair{a, z + 1, "$", "$"})
	e.pairRevision = e.Revision()
	e.RequestFocus, e.reveal = true, true
}

// InsertFootnote retains the new reference as the definition's return target.
func (e *Editor) InsertFootnote() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	at := min(e.Anchor, e.Caret)
	leaf := e.LeafAt(at)
	e.noteOriginID, e.noteOriginOffset = leaf.Block.ID, at-leaf.Start
	e.noteReturn, e.noteHasReturn = at, true
	e.Engine.InsertFootnote()
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// readOnlyHold and readOnlyFade are how long the read-only notice stays,
// then fades, after typing reached a locked page.
const (
	readOnlyHold = 1400 * time.Millisecond
	readOnlyFade = 300 * time.Millisecond
)

// paintReadOnlyNotice says, at the top of the visible page, why typing did
// nothing: the page is locked, and where it unlocks.
func (e *Editor) paintReadOnlyNotice(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	if e.refused {
		e.refused = false
		if e.ReadOnly {
			e.refusedAt = p.Now()
		}
	}
	if e.refusedAt.IsZero() || !e.ReadOnly {
		e.refusedAt = time.Time{}
		return
	}
	since := p.Now().Sub(e.refusedAt)
	if since >= readOnlyHold+readOnlyFade {
		e.refusedAt = time.Time{}
		return
	}
	k := float32(1)
	if since > readOnlyHold {
		k = 1 - float32(since-readOnlyHold)/float32(readOnlyFade)
	}
	span := ui.Span{Text: i18n.T("当前为只读 · 点击右上角的锁恢复编辑"), Size: 13, Color: e.page.OnScrim.Alpha(k)}
	w, h := p.MeasureText(0, span)
	r := ui.Rect{X: bounds.X + (bounds.W-w)/2 - 14, Y: bounds.Y + e.Scroll.Y + 14, W: w + 28, H: h + 14}
	p.Fill(r, e.page.Scrim.Alpha(float32(e.page.Scrim.A)/255*k*1.6), r.H/2)
	p.RichText(r.X+14, r.Y+7, 0, span)
	p.After(0)
}

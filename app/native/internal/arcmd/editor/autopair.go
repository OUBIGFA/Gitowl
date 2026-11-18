package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

type autoPair struct {
	start, end  int
	open, close string
}

var bracketPairs = map[string][2]string{
	"(":  {"(", ")"},
	"[":  {"[", "]"},
	"{":  {"{", "}"},
	"\"": {"“", "”"},
	"'":  {"‘", "’"},
	"$":  {"$", "$"},
	"「":  {"「", "」"},
	"『":  {"『", "』"},
	"（":  {"（", "）"},
	"【":  {"【", "】"},
	"《":  {"《", "》"},
	"“":  {"“", "”"},
	"‘":  {"‘", "’"},
}

func pairCloserMatches(close, typed string) bool {
	if close == typed {
		return true
	}
	return (close == "”" && typed == "\"") || (close == "’" && typed == "'")
}

func (e *Editor) inInlineMathAt(pos int) bool {
	if e.inlineMath.blockID == "" {
		return false
	}
	l := e.LeafAt(pos)
	if l.Block.ID != e.inlineMath.blockID {
		return false
	}
	off := pos - l.Start
	return off > e.inlineMath.start && off < e.inlineMath.end
}

func (e *Editor) upgradeEmptyDollarPair(l document.Leaf, pos int) bool {
	if l.Block.Kind != "paragraph" || l.Parent == nil || pos != l.End-1 {
		return false
	}
	raw := l.Block.Text()
	if !strings.HasSuffix(raw, "$$") || len(raw) > 5 || strings.TrimLeft(raw[:len(raw)-2], " ") != "" {
		return false
	}
	e.promoteParagraphToMath(l, "", raw)
	return true
}

func (e *Editor) pairInput(r *ui.TextInputRange, text string) bool {
	if e.noAutoPair || e.ReadOnly {
		e.pairs = nil
		return false
	}
	if e.pairRevision != e.Revision() {
		e.pairs = nil
	}
	target := e.Selection().Range
	if e.mark != nil {
		target = *e.mark
	}
	if r != nil {
		target = e.validRange(*r)
	}
	l := e.LeafAt(target.Start)
	if literalBlock(l.Block.Kind) || e.CurrentStyle().Code {
		e.pairs = nil
		return false
	}
	inMath := e.inInlineMathAt(target.Start)
	// An IME owns its preedit range. Pair only the final committed character,
	// never the provisional candidate or an arbitrary replacement range.
	if e.mark == nil && r == nil && target.Start == target.End {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			p := e.pairs[i]
			clLen := document.Len(p.close)
			if p.end == target.Start && pairCloserMatches(p.close, text) && e.slice(p.end, p.end+clLen) == p.close {
				if text == "$" && e.upgradeEmptyDollarPair(l, target.Start) {
					return true
				}
				e.SetSelection(p.end+clLen, p.end+clLen)
				e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
				e.smartBack = smartRollback{}
				if text == "$" {
					e.tryCompileDisplayMathParagraph()
				}
				return true
			}
		}
		if text == "$" && inMath && l.Start+e.inlineMath.end-1 == target.Start {
			if e.upgradeEmptyDollarPair(l, target.Start) {
				return true
			}
			e.SetSelection(target.Start+1, target.Start+1)
			e.smartBack = smartRollback{}
			e.tryCompileDisplayMathParagraph()
			return true
		}
	}
	pair, ok := bracketPairs[text]
	if utf8.RuneCountInString(text) != 1 {
		ok = false
	}
	if ok && inMath && (text == "\"" || text == "'" || text == "$") {
		ok = false
	}
	if !ok {
		if e.trySmartInput(r, text) {
			return true
		}
		e.smartBack = smartRollback{}
		if text == " " && r == nil && e.mark == nil && e.mathFence.dismissed && e.mathFence.rev == e.Revision() && l.Block.ID == e.mathFence.blockID {
			e.mathFence = mathFenceRollback{}
			e.shiftPairs(target, document.Len(text))
			e.Engine.ReplaceLiteral(target.Start, target.End, text)
			e.sync()
			e.pairRevision = e.Revision()
			return true
		}
		e.shiftPairs(target, document.Len(text))
		e.Buffer.ReplaceText(r, text)
		e.pairRevision = e.Revision()
		return true
	}
	open, close := pair[0], pair[1]
	if target.Start > l.Start {
		before := e.slice(l.Start, target.Start)
		prev, _ := utf8.DecodeLastRuneInString(before)
		if text == "$" && prev == '$' && e.mark == nil && r == nil && target.Start == target.End && target.Start == l.End && l.Block.Kind == "paragraph" && l.Parent != nil {
			e.smartBack = smartRollback{}
			e.Buffer.ReplaceText(r, text)
			e.pairRevision = e.Revision()
			e.tryCompileDisplayMathParagraph()
			return true
		}
		if prev == '\\' || (text == "'" || text == "\"") && (unicode.IsLetter(prev) || unicode.IsDigit(prev)) || text == "$" && prev == '$' || strings.Contains(before, "://") {
			e.pairs = nil
			return false
		}
	}
	// Quotes within a word are literal; selected words are still wrapped.
	if target.Start == target.End && target.End < l.End {
		next, _ := utf8.DecodeRuneInString(e.slice(target.End, l.End))
		if (text == "'" || text == "\"") && (unicode.IsLetter(next) || unicode.IsDigit(next)) {
			return false
		}
		if text == "$" && (unicode.IsDigit(next) || next == '$') {
			return false
		}
	}
	a, z := target.Start, target.End
	nOpen, nClose := document.Len(open), document.Len(close)
	selected := e.slice(a, z)
	if e.mark != nil {
		selected = ""
	}
	e.smartBack = smartRollback{}
	e.shiftPairs(target, nOpen+nClose+document.Len(selected))
	if selected != "" && e.mark == nil {
		// Insert at the ends so each selected run retains its own marks.
		e.Engine.Edit(func() {
			e.Engine.Replace(z, z, close)
			e.Engine.Replace(a, a, open)
			e.SetSelection(a+nOpen, z+nOpen)
		})
		e.sync()
	} else {
		e.Buffer.ReplaceText(r, open+close)
		e.SetSelection(a+nOpen, a+nOpen)
	}
	end := a + nOpen + document.Len(selected)
	e.pairs = append(e.pairs, autoPair{a, end, open, close})
	e.pairRevision = e.Revision()
	return true
}

func (e *Editor) shiftPairs(r ui.TextInputRange, inserted int) {
	delta := inserted - (r.End - r.Start)
	kept := e.pairs[:0]
	for _, p := range e.pairs {
		if r.Start <= p.start && r.End > p.start || r.Start <= p.end && r.End > p.end {
			continue
		}
		if p.start >= r.End {
			p.start += delta
		}
		if p.end >= r.End {
			p.end += delta
		}
		kept = append(kept, p)
	}
	e.pairs = kept
}

func (e *Editor) deletePair() bool {
	if e.mark != nil || e.Anchor != e.Caret || e.pairRevision != e.Revision() {
		e.pairs = nil
		return false
	}
	for i, p := range e.pairs {
		nOpen, nClose := document.Len(p.open), document.Len(p.close)
		if e.Caret == p.end && p.end == p.start+nOpen && e.slice(p.start, p.end+nClose) == p.open+p.close {
			r := ui.TextInputRange{Start: p.start, End: p.end + nClose}
			e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
			e.shiftPairs(r, 0)
			e.Buffer.ReplaceText(&r, "")
			e.pairRevision = e.Revision()
			return true
		}
	}
	return false
}

// activePair returns the innermost tracked autoPair enclosing the caret.
func (e *Editor) activePair() (autoPair, int, bool) {
	if e.mark != nil || e.Anchor != e.Caret || e.pairRevision != e.Revision() || len(e.pairs) == 0 {
		return autoPair{}, -1, false
	}
	for i := len(e.pairs) - 1; i >= 0; i-- {
		p := e.pairs[i]
		nOpen, nClose := document.Len(p.open), document.Len(p.close)
		if e.Caret < p.start+nOpen || e.Caret > p.end {
			continue
		}
		if p.start < 0 || p.end+nClose > e.Len() {
			continue
		}
		if e.slice(p.start, p.start+nOpen) != p.open || e.slice(p.end, p.end+nClose) != p.close {
			continue
		}
		return p, i, true
	}
	return autoPair{}, -1, false
}

// tabOutPair jumps the caret past the closing delimiter of an active pair.
func (e *Editor) tabOutPair() bool {
	p, idx, ok := e.activePair()
	if !ok {
		return false
	}
	after := p.end + document.Len(p.close)
	e.pairs = append(e.pairs[:idx], e.pairs[idx+1:]...)
	e.SetSelection(after, after)
	e.smartBack = smartRollback{}
	e.reveal = true
	return true
}

func (e *Editor) paintActivePair(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	if !e.focused || e.ReadOnly || e.printing {
		return
	}
	ap, _, ok := e.activePair()
	if !ok {
		return
	}
	if ap.open == "$" && e.inlineMath.blockID != "" {
		return
	}
	nClose := document.Len(ap.close)
	idx := e.rowIndexAt(ap.start)
	if idx < 0 {
		return
	}
	r := *e.ensureRow(idx)
	if r.layout == nil || ap.start < r.leaf.Start || ap.end+nClose > r.leaf.End {
		return
	}
	rects := r.layout.SelectionRects(ui.TextInputRange{
		Start: ap.start - r.leaf.Start,
		End:   ap.end + nClose - r.leaf.Start,
	})
	if len(rects) == 0 {
		return
	}
	dx, dy := e.visualOffset(r)
	ox, oy := bounds.X+r.x+dx, bounds.Y+r.y+dy
	for _, sr := range rects {
		if sr.W <= 0 || sr.H <= 0 {
			continue
		}
		box := ui.Rect{X: ox + sr.X - 1.5, Y: oy + sr.Y + 1, W: sr.W + 3, H: max(16, sr.H-2)}
		p.Fill(box, t.Accent.Alpha(0.055), 5)
		p.Stroke(box, t.Accent.Alpha(0.28), 5, 1)
	}
	last := rects[len(rects)-1]
	lastBox := ui.Rect{X: ox + last.X - 1.5, Y: oy + last.Y + 1, W: last.W + 3, H: max(16, last.H-2)}
	e.paintTabOutBadge(p, lastBox, t)
}

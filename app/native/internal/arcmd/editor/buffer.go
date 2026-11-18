package editor

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

type leafComposition struct {
	state        document.LeafState
	committedDoc *document.Document
	committedRev uint64
}

type Buffer struct {
	*document.Engine
	text        string
	textDoc     *document.Document
	textVersion uint64
	textLength  int
	textKnown   bool
	mark        *ui.TextInputRange
	composition *document.Snapshot
	leafComp    *leafComposition
	// refused is set when typing reached a read-only document, so the page
	// can say why nothing happened.
	refused bool
}

func newBuffer(doc *document.Document) *Buffer {
	b := &Buffer{Engine: document.NewEngine(doc)}
	b.sync()
	return b
}

func newOwnedBuffer(doc *document.Document) *Buffer {
	b := &Buffer{Engine: document.NewEngineOwned(doc)}
	b.sync()
	return b
}
func (b *Buffer) sync() {
	if b.textDoc == b.Doc && b.textVersion == b.Revision() {
		return
	}
	b.textLength = b.Engine.TextLength()
	b.text, b.textKnown = "", false
	b.textDoc, b.textVersion = b.Doc, b.Revision()
}
func (b *Buffer) Composing() bool {
	return b.composition != nil || b.leafComp != nil
}

func (b *Buffer) Text() string {
	if b.composition != nil || b.leafComp != nil {
		return b.Committed().PlainText()
	}
	return b.displayText()
}

// displayText materializes only when a consumer needs the complete value.
// Native input ranges and navigation read the engine's leaves directly.
func (b *Buffer) displayText() string {
	b.sync()
	if !b.textKnown {
		b.text = b.Engine.TextRange(0, b.textLength)
		b.textKnown = true
	}
	return b.text
}
func (b *Buffer) Committed() *document.Document {
	if b.composition != nil {
		return b.composition.Doc
	}
	if b.leafComp != nil {
		rev := b.Revision()
		if b.leafComp.committedDoc == nil || b.leafComp.committedRev != rev {
			clone := b.Doc.Clone()
			replaceBlockRunsByID(clone.Blocks, b.leafComp.state.BlockID, b.leafComp.state.Runs)
			b.leafComp.committedDoc = clone
			b.leafComp.committedRev = rev
		}
		return b.leafComp.committedDoc
	}
	return b.Doc
}

func replaceBlockRunsByID(blocks []*document.Block, id string, runs []document.Run) bool {
	for _, blk := range blocks {
		if blk.ID == id {
			blk.Runs = slices.Clone(runs)
			return true
		}
		if len(blk.Children) > 0 && replaceBlockRunsByID(blk.Children, id, runs) {
			return true
		}
	}
	return false
}

func (b *Buffer) Len() int     { b.sync(); return b.textLength }
func (b *Buffer) ByteLen() int { b.sync(); return b.Engine.TextByteLength() }
func (b *Buffer) Selection() ui.TextInputSelection {
	return ui.TextInputSelection{Range: ui.TextInputRange{Start: min(b.Anchor, b.Caret), End: max(b.Anchor, b.Caret)}, Reversed: b.Caret < b.Anchor}
}
func (b *Buffer) MarkedRange() (ui.TextInputRange, bool) {
	if b.mark != nil {
		return *b.mark, true
	}
	return ui.TextInputRange{}, false
}
func (b *Buffer) TextForRange(r ui.TextInputRange) (string, ui.TextInputRange) {
	b.sync()
	r = b.validRange(r)
	return b.slice(r.Start, r.End), r
}

// Position conversion reads only the containing leaf. The engine updates both
// offset indexes on text edits and rebuilds them after structural changes/IME.
func (b *Buffer) byteOffset(at int) int {
	b.sync()
	l := b.Engine.LeafAt(at)
	return l.ByteStart + ui.UTF16ByteOffset(l.Block.Text(), at-l.Start)
}
func (b *Buffer) textOffset(at int) int {
	b.sync()
	l := b.Engine.LeafAtByte(at)
	return l.Start + ui.UTF16Len(l.Block.Text()[:max(0, min(at-l.ByteStart, l.ByteEnd-l.ByteStart))])
}
func (b *Buffer) clamp(at int) int {
	b.sync()
	l := b.Engine.LeafAt(at)
	text := l.Block.Text()
	return l.Start + ui.UTF16Len(text[:ui.UTF16ByteOffset(text, at-l.Start)])
}
func (b *Buffer) validRange(r ui.TextInputRange) ui.TextInputRange {
	return ui.TextInputRange{Start: b.clamp(min(r.Start, r.End)), End: b.clamp(max(r.Start, r.End))}
}
func (b *Buffer) slice(a, z int) string {
	b.sync()
	return b.Engine.TextRange(a, z)
}
func (b *Buffer) ReplaceText(r *ui.TextInputRange, text string) {
	if b.ReadOnly {
		b.refused = true
		return
	}
	b.sync()
	target := b.Selection().Range
	if b.mark != nil {
		target = *b.mark
	}
	if r != nil {
		target = b.validRange(*r)
	}
	switch {
	case b.leafComp != nil:
		ls := b.leafComp.state
		b.leafComp, b.mark = nil, nil
		if !b.Engine.CommitLeaf(ls, target.Start, target.End, text) {
			preview := b.Engine.Capture()
			b.Engine.RestoreLeaf(ls)
			b.Engine.Edit(func() {
				b.Engine.Restore(preview)
				b.Engine.Replace(target.Start, target.End, text)
			})
		}
	case b.composition != nil:
		preview := b.Engine.Capture()
		b.Engine.Restore(*b.composition)
		b.composition, b.mark = nil, nil
		b.Engine.Edit(func() {
			b.Engine.Restore(preview)
			b.Engine.Replace(target.Start, target.End, text)
		})
	default:
		b.Engine.Replace(target.Start, target.End, text)
	}
	b.sync()
}
func (b *Buffer) SetMarkedText(r *ui.TextInputRange, text string, selected ui.TextInputRange) {
	if b.ReadOnly {
		b.refused = text != ""
		return
	}
	b.sync()
	target := b.Selection().Range
	if b.mark != nil {
		target = *b.mark
	}
	if r != nil {
		target = b.validRange(*r)
	}
	handledInLeaf := false
	if b.composition == nil {
		if b.leafComp == nil {
			if state, ok := b.Engine.CaptureLeaf(target.Start, target.End); ok && b.Engine.PreviewLeafReplace(target.Start, target.End, text) {
				b.leafComp = &leafComposition{state: state}
				handledInLeaf = true
			}
		} else {
			b.leafComp.committedDoc = nil
			if b.Engine.PreviewLeafReplace(target.Start, target.End, text) {
				handledInLeaf = true
			} else {
				preview := b.Engine.Capture()
				ls := b.leafComp.state
				b.leafComp = nil
				b.Engine.RestoreLeaf(ls)
				before := b.Engine.Capture()
				b.composition = &before
				b.Engine.Restore(preview)
			}
		}
		if !handledInLeaf && b.composition == nil {
			before := b.Engine.Capture()
			b.composition = &before
		}
	}
	if !handledInLeaf {
		b.Engine.PreviewReplace(target.Start, target.End, text)
	}
	marked := ui.TextInputRange{Start: target.Start, End: target.Start + ui.UTF16Len(text)}
	b.mark = &marked
	b.Engine.SetSelection(marked.Start+max(0, min(selected.Start, ui.UTF16Len(text))), marked.Start+max(0, min(selected.End, ui.UTF16Len(text))))
	b.sync()
}
func (b *Buffer) UnmarkText() {
	if b.mark != nil {
		text := b.slice(b.mark.Start, b.mark.End)
		b.ReplaceText(nil, text)
	}
}
func (b *Buffer) cancelComposition() {
	if b.leafComp != nil {
		b.Engine.RestoreLeaf(b.leafComp.state)
		b.leafComp = nil
	} else if b.composition != nil {
		b.Engine.Restore(*b.composition)
	}
	b.composition, b.mark = nil, nil
	b.sync()
}
func (b *Buffer) Undo() {
	if b.Composing() {
		b.cancelComposition()
		return
	}
	b.Engine.Undo()
	b.sync()
}
func (b *Buffer) Redo() { b.UnmarkText(); b.Engine.Redo(); b.sync() }
func (b *Buffer) Wrap(marker string) {
	b.UnmarkText()
	marks := map[string]string{"**": "bold", "*": "italic", "~~": "strike", "`": "code", "_": "underline"}
	b.Engine.Toggle(marks[marker])
	b.sync()
}
func (b *Buffer) Prefix(prefix string) {
	b.UnmarkText()
	kind, level := "paragraph", 0
	switch prefix {
	case "- ":
		kind = "bullet"
	case "1. ":
		kind = "numbered"
	case "- [ ] ":
		kind = "task"
	case "> ":
		kind = "quote"
	default:
		if strings.HasPrefix(prefix, "#") {
			kind, level = "heading", len(strings.TrimSpace(prefix))
		}
	}
	b.Engine.SetKind(kind, level)
	b.sync()
}
func (b *Buffer) newline() { b.UnmarkText(); b.Engine.Enter(false); b.sync() }

func (b *Buffer) lineRange(at int) (int, int) {
	l := b.Engine.LeafAt(at)
	text := l.Block.Text()
	i := ui.UTF16ByteOffset(text, at-l.Start)
	a := strings.LastIndexByte(text[:i], '\n') + 1
	z := len(text)
	if n := strings.IndexByte(text[i:], '\n'); n >= 0 {
		z = i + n
	}
	if z > a && text[z-1] == '\r' {
		z--
	}
	return l.Start + ui.UTF16Len(text[:a]), l.Start + ui.UTF16Len(text[:z])
}

func (b *Buffer) boundary(at, direction int) int {
	a, z := b.lineRange(at)
	if direction < 0 && at <= a {
		if a >= 2 && b.slice(a-2, a) == "\r\n" {
			return a - 2
		}
		return max(0, a-1)
	}
	if direction > 0 && at >= z {
		if z+2 <= b.Len() && b.slice(z, z+2) == "\r\n" {
			return z + 2
		}
		return min(b.Len(), z+1)
	}
	line := ui.ShapeText(b.slice(a, z), ui.Font{Size: 16}, 0)
	if direction < 0 {
		return a + line.PreviousBoundary(at-a)
	}
	return a + line.NextBoundary(at-a)
}

func (b *Buffer) wordBoundary(at, direction int) int {
	at = b.clamp(at)
	spaces := direction < 0
	for {
		l := b.Engine.LeafAt(at)
		text := l.Block.Text()
		i := ui.UTF16ByteOffset(text, at-l.Start)
		if direction < 0 {
			for i > 0 {
				r, n := utf8.DecodeLastRuneInString(text[:i])
				if unicode.IsSpace(r) {
					if !spaces {
						return at
					}
				} else {
					spaces = false
				}
				i -= n
				at -= runeUnits(r)
			}
			if at == 0 || !spaces {
				return at
			}
			at-- // the separator belongs to the preceding leaf
		} else {
			for i < len(text) {
				r, n := utf8.DecodeRuneInString(text[i:])
				if unicode.IsSpace(r) {
					spaces = true
				} else if spaces {
					return at
				}
				i += n
				at += runeUnits(r)
			}
			if at == b.Len() {
				return at
			}
			spaces = true
			at++
		}
	}
}
func runeUnits(r rune) int {
	if r > 0xffff {
		return 2
	}
	return 1
}

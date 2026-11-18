package document

import (
	"slices"
	"strings"
)

type Snapshot struct {
	Doc           *Document
	Anchor, Caret int
	Typing        Style
	Explicit      bool
}

// Engine applies structural changes atomically and owns one history for every
// paragraph, table cell and column. Snapshots never alias mutable documents.
type Engine struct {
	ReadOnly             bool // Session reading lock; does not change the stored document.
	depth                int
	Doc                  *Document
	Anchor, Caret        int
	Typing               Style
	Explicit             bool
	version, revision    uint64
	undo, redo           []historyEntry
	indexDoc             *Document
	indexRevision        uint64
	leaves               []Leaf
	leafIDs              map[string]int
	unitIndex, byteIndex offsetIndex
	hiddenDoc            *Document
	hiddenRevision       uint64
	hidden               map[string]string
	textChange           TextChange
	OnBeforeChange       func()
}

func (e *Engine) notifyBeforeChange() {
	if fn := e.OnBeforeChange; fn != nil {
		e.OnBeforeChange = nil
		fn()
	}
}

func NewEngine(doc *Document) *Engine { return &Engine{Doc: doc.Clone()} }

// NewEngineOwned takes exclusive ownership of doc without cloning it. Callers
// must not alias or mutate doc outside the returned Engine.
func NewEngineOwned(doc *Document) *Engine { return &Engine{Doc: doc} }
func (e *Engine) Version() uint64          { return e.version }
func (e *Engine) Revision() uint64         { return e.revision }
func (e *Engine) CanUndo() bool            { return len(e.undo) > 0 }
func (e *Engine) CanRedo() bool            { return len(e.redo) > 0 }
func (e *Engine) Positions() (int, int)    { return e.Anchor, e.Caret }
func (e *Engine) Capture() Snapshot {
	return Snapshot{e.Doc.Clone(), e.Anchor, e.Caret, e.Typing, e.Explicit}
}

func (e *Engine) Restore(s Snapshot) {
	e.notifyBeforeChange()
	e.Doc, e.Anchor, e.Caret = s.Doc.Clone(), s.Anchor, s.Caret
	e.Typing, e.Explicit = s.Typing, s.Explicit
	e.revision++
}

func (e *Engine) SetSelection(a, z int) {
	clamp := func(at int) int {
		leaf := e.LeafAt(max(0, at))
		text := leaf.Block.Text()
		return leaf.Start + Len(text[:ByteOffset(text, max(0, at-leaf.Start))])
	}
	e.Anchor, e.Caret = clamp(a), clamp(z)
	e.Explicit = false
}

func (e *Engine) Edit(change func()) {
	if e.ReadOnly {
		return
	}
	if e.depth > 0 {
		change()
		return
	}
	e.depth++
	defer func() { e.depth--; e.indexDoc = nil }()
	e.notifyBeforeChange()
	before := e.Capture()
	change()
	dropStrayColors(e.Doc.Blocks)
	if SameDocument(before.Doc, e.Doc) {
		return
	}
	e.recordUndo(historyEntry{state: before})
	e.version++
	e.revision++
}

// dropStrayColors clears the color of blocks no longer callouts, as a
// callout turned into another kind, so every edit leaves a valid document.
func dropStrayColors(blocks []*Block) {
	for _, b := range blocks {
		if b.Kind != "image" {
			b.ImageWidth = 0
		}
		if b.Kind != "footnote" {
			b.NoteLabel = ""
		}
		if b.Kind != "callout" {
			b.Color, b.Emoji = "", ""
		}
		if b.Kind != "heading" && !isListKind(b.Kind) && b.Kind != "toggle" {
			b.Folded = false
		}
		if b.Kind == "table" {
			normalizeSpans(b)
		}
		dropStrayColors(b.Children)
	}
}

// SetColor colors the callout id, or the callout holding the caret when id
// is empty; it does nothing to other blocks or with an unknown color.
func (e *Engine) SetColor(id, color string) {
	if !validCalloutColor(color) {
		return
	}
	if id == "" {
		id = e.LeafAt(e.Caret).Block.ID
	}
	e.Edit(func() {
		if parent, index := e.find(id); parent != nil && (*parent)[index].Kind == "callout" {
			(*parent)[index].Color = color
		}
	})
}

// SetEmoji gives callout id an emoji icon; "" goes back to its color's.
func (e *Engine) SetEmoji(id, emoji string) {
	e.Edit(func() {
		if parent, index := e.find(id); parent != nil && (*parent)[index].Kind == "callout" && len(emoji) <= 32 {
			(*parent)[index].Emoji = emoji
		}
	})
}

// SetLanguage sets the fence language of code block id, as one undo step.
func (e *Engine) SetLanguage(id, language string) {
	e.Edit(func() {
		if parent, index := e.find(id); parent != nil && (*parent)[index].Kind == "code" {
			(*parent)[index].Language = language
		}
	})
}

func (e *Engine) Undo() {
	if e.ReadOnly {
		return
	}
	if !e.CanUndo() {
		return
	}
	entry := e.undo[len(e.undo)-1]
	e.redo = append(e.redo, e.captureHistory(entry.blockID))
	e.restoreHistory(entry)
	e.undo = e.undo[:len(e.undo)-1]
	e.version++
}

func (e *Engine) Redo() {
	if e.ReadOnly {
		return
	}
	if !e.CanRedo() {
		return
	}
	entry := e.redo[len(e.redo)-1]
	e.undo = append(e.undo, e.captureHistory(entry.blockID))
	e.restoreHistory(entry)
	e.redo = e.redo[:len(e.redo)-1]
	e.version++
}

func (e *Engine) CurrentStyle() Style {
	if e.Explicit {
		return e.Typing
	}
	leaf := e.LeafAt(min(e.Anchor, e.Caret))
	position := leaf.Start
	for _, run := range leaf.Block.Runs {
		position += Len(run.Text)
		if position > min(e.Anchor, e.Caret) || e.Anchor == e.Caret && position == e.Caret {
			return run.Style
		}
	}
	return Style{}
}

func (e *Engine) Replace(a, z int, text string) {
	if e.ReadOnly {
		return
	}
	if e.replaceText(a, z, text) {
		return
	}
	e.Edit(func() { e.replace(a, z, text); e.autoformat(text) })
}

// ReplaceLiteral replaces [a, z) with text without running Markdown prefix
// autoformatting, preserving literal prefixes after an explicit rollback.
func (e *Engine) ReplaceLiteral(a, z int, text string) {
	if e.ReadOnly {
		return
	}
	if e.replaceTextLiteral(a, z, text) {
		return
	}
	e.Edit(func() { e.replace(a, z, text) })
}

// PreviewReplace updates the display document during IME composition. The
// input adapter retains the committed snapshot and creates history on commit.
func (e *Engine) PreviewReplace(a, z int, text string) {
	if e.ReadOnly {
		return
	}
	if e.replaceTextMode(a, z, text, false) {
		return
	}
	e.notifyBeforeChange()
	e.replace(a, z, text)
	e.revision++
}

func (e *Engine) currentStyleIn(leaf Leaf, a, z int) Style {
	if e.Explicit {
		return e.Typing
	}
	at := min(a, z)
	position := leaf.Start
	for _, run := range leaf.Block.Runs {
		position += Len(run.Text)
		if position > at || a == z && position == z {
			return run.Style
		}
	}
	return Style{}
}

func (e *Engine) replace(a, z int, text string) {
	a, z = max(0, min(a, z)), max(0, max(a, z))
	e.rebuildIndex()
	first := e.leafAtIndex(e.unitIndex.containing(a))
	last := e.leafAtIndex(e.unitIndex.containing(z))
	left, right := first.Block.Text(), last.Block.Text()
	a = first.Start + Len(left[:ByteOffset(left, a-first.Start)])
	z = last.Start + Len(right[:ByteOffset(right, z-last.Start)])
	style := e.currentStyleIn(first, a, z)
	style.NoMath = false
	normText := strings.ReplaceAll(text, "\r\n", "\n")
	before := SliceRuns(first.Block.Runs, 0, a-first.Start)
	after := SliceRuns(last.Block.Runs, z-last.Start, last.End-last.Start)
	runs := Compact(append(append(before, Run{Text: normText, Style: style}), after...))
	if first.Parent != last.Parent || first.Block != last.Block && first.Block.Kind == "cell" {
		if a == 0 && z == max(0, e.unitIndex.prefix(len(e.leaves))-1) {
			e.Doc.Blocks = []*Block{NewBlock("paragraph", "")}
			first = e.Doc.Leaves()[0]
			first.Block.Runs = runs
		} else {
			e.replaceAcross(a, z, normText, style)
			return
		}
	} else {
		if first.Block != last.Block && a == first.Start && (isRule(first.Block) || isMedia(first.Block)) {
			// A rule or attachment selected whole goes with the selection
			// rather than taking the text after it.
			first.Block.Kind, first.Block.URL = "paragraph", ""
		}
		first.Block.Runs = runs
		*first.Parent = append((*first.Parent)[:first.Index+1], (*first.Parent)[last.Index+1:]...)
	}
	// Code, preserved source and cells retain internal newlines. Paragraph
	// separators split blocks and carry the marks on each side of the caret.
	if first.Block.Kind != "code" && first.Block.Kind != "cell" && first.Block.Kind != "raw" && first.Block.Kind != "math" {
		split := splitRunsByNewline(runs)
		if len(split) > 1 {
			blocks := make([]*Block, 0, len(split))
			for i, lineRuns := range split {
				block := first.Block.Clone()
				if i > 0 {
					block.ID = NewID()
					block.Checked = false
					if block.Kind != "bullet" && block.Kind != "numbered" && block.Kind != "task" && block.Kind != "quote" {
						block.Kind, block.Level = "paragraph", 0
					}
				}
				block.Runs = lineRuns
				blocks = append(blocks, block)
			}
			parent, index := e.find(first.Block.ID)
			if parent != nil {
				*parent = append(append(append([]*Block(nil), (*parent)[:index]...), blocks...), (*parent)[index+1:]...)
			}
		}
	}
	caret := a + Len(normText)
	e.Anchor, e.Caret = caret, caret
	e.Typing, e.Explicit = style, true
}

// splitRunsByNewline splits a compacted run slice on '\n' in a single O(N) pass,
// matching repeated SliceRuns over newline-delimited ranges.
func splitRunsByNewline(runs []Run) [][]Run {
	var out [][]Run
	var cur []Run
	for _, run := range runs {
		rest := run.Text
		for {
			idx := strings.IndexByte(rest, '\n')
			if idx < 0 {
				if rest != "" {
					cur = append(cur, Run{Text: rest, Style: run.Style})
				}
				break
			}
			if idx > 0 {
				cur = append(cur, Run{Text: rest[:idx], Style: run.Style})
			}
			out = append(out, Compact(cur))
			cur = nil
			rest = rest[idx+1:]
		}
	}
	out = append(out, Compact(cur))
	return out
}

func isOrderedListPrefix(s string) bool {
	if len(s) < 3 || len(s) > 5 || !strings.HasSuffix(s, ". ") {
		return false
	}
	for i := 0; i < len(s)-2; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (e *Engine) autoformat(inserted string) {
	if inserted != " " || e.Anchor != e.Caret {
		return
	}
	leaf := e.LeafAt(e.Caret)
	b := leaf.Block
	if b.Kind != "paragraph" && b.Kind != "bullet" {
		return
	}
	prefixUnits := e.Caret - leaf.Start
	if prefixUnits <= 0 || prefixUnits > 10 {
		return
	}
	for _, r := range SliceRuns(b.Runs, 0, prefixUnits) {
		if r.Style.Code || r.Style.Link != "" {
			return
		}
	}
	full := b.Text()
	rawPrefix := full[:ByteOffset(full, prefixUnits)]
	lead := len(rawPrefix) - len(strings.TrimLeft(rawPrefix, " "))
	if lead > 3 {
		return
	}
	prefix := rawPrefix[lead:]
	kind, level, checked := "", 0, false
	if b.Kind == "bullet" {
		switch prefix {
		case "[] ", "[ ] ":
			kind = "task"
		case "[x] ", "[X] ":
			kind, checked = "task", true
		}
	} else {
		switch prefix {
		case "# ", "## ", "### ", "#### ", "##### ", "###### ":
			kind, level = "heading", len(prefix)-1
		case "- ", "* ", "+ ":
			kind = "bullet"
		case "> ":
			kind = "quote"
		case "[] ", "[ ] ":
			kind = "task"
		case "[x] ", "[X] ":
			kind, checked = "task", true
		case "``` ":
			kind = "code"
		case "$$ ":
			kind = "math"
		default:
			if isOrderedListPrefix(prefix) {
				kind = "numbered"
			}
		}
	}
	if kind == "" {
		return
	}
	rest := SliceRuns(b.Runs, prefixUnits, leaf.End-leaf.Start)
	if kind == "code" || kind == "math" {
		var plain strings.Builder
		for _, r := range rest {
			plain.WriteString(r.Text)
		}
		if s := plain.String(); s != "" {
			rest = []Run{{Text: s}}
		} else {
			rest = nil
		}
	} else {
		rest = Compact(rest)
		if len(rest) == 0 {
			rest = nil
		}
	}
	b.Kind, b.Level, b.Checked, b.Runs = kind, level, checked, rest
	e.Anchor, e.Caret = leaf.Start, leaf.Start
}

func (e *Engine) Enter(soft bool) {
	if soft {
		e.Replace(min(e.Anchor, e.Caret), max(e.Anchor, e.Caret), "\u2028")
		return
	}
	leaf := e.LeafAt(e.Caret)
	if e.enterMath(leaf) || e.enterCodeFence(leaf) || e.enterMermaidExit(leaf) || e.enterPipeTable(leaf) {
		return
	}
	if leaf.Block.Kind == "image" && leaf.Parent != nil {
		e.Edit(func() {
			next := NewBlock("paragraph", "")
			next.Indent = leaf.Block.Indent
			*leaf.Parent = append((*leaf.Parent)[:leaf.Index+1], append([]*Block{next}, (*leaf.Parent)[leaf.Index+1:]...)...)
			e.FocusBlock(next.ID)
		})
		return
	}
	if !soft && e.Anchor == e.Caret && leaf.Block.Text() == "" && (leaf.Block.Kind == "bullet" || leaf.Block.Kind == "numbered" || leaf.Block.Kind == "task" || leaf.Block.Kind == "quote" || leaf.Block.Kind == "toggle") {
		e.SetKind("paragraph", 0)
		return
	}
	if b := leaf.Block; b.Kind == "toggle" && e.Anchor == e.Caret && e.Caret == leaf.End && leaf.Owner == b.ID {
		e.enterToggle(b)
		return
	}
	e.Replace(min(e.Anchor, e.Caret), max(e.Anchor, e.Caret), "\n")
}

// OpenToggle gives the toggle id an empty line of content, indented under
// it, unless it has content already, keeping the caret in its title.
func (e *Engine) OpenToggle(id string) {
	e.Edit(func() {
		parent, index := e.find(id)
		if parent == nil || (*parent)[index].Kind != "toggle" {
			return
		}
		b := (*parent)[index]
		b.Folded = false
		if index+1 < len(*parent) && (*parent)[index+1].Indent > b.Indent && (*parent)[index+1].Kind != "heading" {
			return
		}
		inner := NewBlock("paragraph", "")
		inner.Indent = min(12, b.Indent+1)
		*parent = append(append(append([]*Block(nil), (*parent)[:index+1]...), inner), (*parent)[index+1:]...)
	})
}

// enterToggle starts a line after the end of toggle b, as Notion's: inside
// it, indented, when it is open; after what it hides when it is folded.
func (e *Engine) enterToggle(b *Block) {
	e.Edit(func() {
		parent, index := e.find(b.ID)
		if parent == nil {
			return
		}
		next := NewBlock("paragraph", "")
		next.Indent = min(12, b.Indent+1)
		at := index + 1
		if b.Folded {
			next.Indent = b.Indent
			at += foldReach(*parent, index)
		}
		*parent = append(append(append([]*Block(nil), (*parent)[:at]...), next), (*parent)[at:]...)
		e.FocusBlock(next.ID)
	})
}

// Fragment captures selected structure for ArcMD's clipboard representation.
func (e *Engine) Fragment() *Document {
	a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
	if a == 0 && z == Len(e.Doc.PlainText()) {
		return e.Doc.Clone()
	}
	out := New()
	out.Blocks = nil
	leaves := e.Doc.Leaves()
	whole := map[string]*Block{}
	for _, block := range e.Doc.Blocks {
		if len(block.Children) == 0 {
			continue
		}
		start, end := -1, -1
		for _, leaf := range leaves {
			if leaf.Owner == block.ID {
				if start < 0 {
					start = leaf.Start
				}
				end = leaf.End
			}
		}
		if start >= a && end <= z {
			whole[block.ID] = block
		}
	}
	copied := map[string]bool{}
	for _, leaf := range leaves {
		if leaf.End <= a || leaf.Start >= z {
			continue
		}
		if block := whole[leaf.Owner]; block != nil {
			if !copied[leaf.Owner] {
				out.Blocks = append(out.Blocks, block.Clone())
				copied[leaf.Owner] = true
			}
			continue
		}
		b := leaf.Block.Clone()
		b.Runs = SliceRuns(b.Runs, max(0, a-leaf.Start), min(z, leaf.End)-leaf.Start)
		if b.Kind == "cell" || a > leaf.Start || z < leaf.End {
			b.Kind = "paragraph"
			b.Level, b.NoteLabel, b.ImageWidth, b.URL = 0, "", 0, ""
			b.Background, b.ColSpan, b.RowSpan = "", 0, 0
		}
		out.Blocks = append(out.Blocks, b)
	}
	dropStrayColors(out.Blocks)
	if len(out.Blocks) == 0 {
		out.Blocks = []*Block{NewBlock("paragraph", "")}
	}
	return out
}

// Paste inserts a rich fragment in a single transaction, generating fresh IDs.
func (e *Engine) Paste(fragment *Document) {
	e.Edit(func() {
		fragment = e.pastedNotes(fragment)
		a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
		leaf := e.LeafAt(a)
		// A formula remains a block even when it is the only copied item.
		if len(fragment.Blocks) == 1 && len(fragment.Blocks[0].Children) == 0 && (fragment.Blocks[0].Kind == "paragraph" || isVerbatim(leaf.Block) || leaf.Block.Kind == "cell") {
			insert := fragment.Blocks[0]
			e.replace(a, z, insert.Text())
			for _, target := range e.Doc.Leaves() {
				start, end := max(a, target.Start), min(a+Len(insert.Text()), target.End)
				if end <= start {
					continue
				}
				runs := target.Block.Runs
				middle := SliceRuns(insert.Runs, start-a, end-a)
				target.Block.Runs = Compact(append(append(SliceRuns(runs, 0, start-target.Start), middle...), SliceRuns(runs, end-target.Start, target.End-target.Start)...))
			}
			return
		}
		if leaf.Block.Kind == "cell" {
			e.replace(a, z, fragment.PlainText())
			return
		}
		e.replace(a, z, "")
		leaf = e.LeafAt(e.Caret)
		prefix := SliceRuns(leaf.Block.Runs, 0, e.Caret-leaf.Start)
		suffix := SliceRuns(leaf.Block.Runs, e.Caret-leaf.Start, leaf.End-leaf.Start)
		var blocks []*Block
		if len(prefix) > 0 {
			b := leaf.Block.Clone()
			b.Runs = prefix
			blocks = append(blocks, b)
		}
		var fresh func(*Block)
		fresh = func(b *Block) {
			b.ID = NewID()
			for _, child := range b.Children {
				fresh(child)
			}
		}
		for _, block := range fragment.Blocks {
			b := block.Clone()
			fresh(b)
			blocks = append(blocks, b)
		}
		// Keep the last editable leaf before adding the destination's suffix.
		// Containers can end in nested leaves or merged-over table cells.
		leaves := (&Document{Blocks: blocks}).Leaves()
		last := leaves[len(leaves)-1].Block.ID
		if len(suffix) > 0 {
			b := NewBlock("paragraph", "")
			b.Runs = suffix
			blocks = append(blocks, b)
		}
		parent, index := e.find(leaf.Block.ID)
		*parent = append(append(append([]*Block(nil), (*parent)[:index]...), blocks...), (*parent)[index+1:]...)
		for _, target := range e.Doc.Leaves() {
			if target.Block.ID == last {
				e.SetSelection(target.End, target.End)
				break
			}
		}
	})
}

func (e *Engine) SetKind(kind string, level int) {
	e.Edit(func() {
		a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
		for _, leaf := range e.Doc.Leaves() {
			if leaf.End < a || leaf.Start > z || leaf.Block.Kind == "cell" {
				continue
			}
			leaf.Block.Kind, leaf.Block.Level = kind, level
			leaf.Block.URL = ""
		}
	})
}

func (e *Engine) Toggle(mark string) {
	if e.ReadOnly {
		return
	}
	style := e.CurrentStyle()
	get := func(s *Style) *bool {
		switch mark {
		case "bold":
			return &s.Bold
		case "italic":
			return &s.Italic
		case "underline":
			return &s.Underline
		case "strike":
			return &s.Strike
		default:
			return &s.Code
		}
	}
	on := !*get(&style)
	if e.Anchor == e.Caret {
		*get(&style) = on
		e.Typing, e.Explicit = style, true
		return
	}
	e.applyFormat(func(s *Style) { *get(s) = on })
}

// SetTextColor colors the selection's text, or what is typed next at a
// collapsed caret: color and highlight replace the current ones unless
// nil; "" clears one.
func (e *Engine) SetTextColor(color, highlight *string) {
	if e.ReadOnly {
		return
	}
	change := func(s *Style) {
		if color != nil {
			s.Color = *color
		}
		if highlight != nil {
			s.Highlight = *highlight
		}
	}
	if e.Anchor == e.Caret {
		style := e.CurrentStyle()
		change(&style)
		e.Typing, e.Explicit = style, true
		return
	}
	e.applyFormat(change)
}

// SetAnnotation draws a hand-drawn mark on the selection, or on what is
// typed next at a collapsed caret: variant is one of Annotations, or ""
// to take the mark away; color one of TextColors, or "" for the variant's
// own.
func (e *Engine) SetAnnotation(variant, color string) {
	if e.ReadOnly {
		return
	}
	if variant == "" {
		color = ""
	}
	change := func(s *Style) { s.Annotation, s.AnnotationColor = variant, color }
	if e.Anchor == e.Caret {
		style := e.CurrentStyle()
		change(&style)
		e.Typing, e.Explicit = style, true
		return
	}
	e.applyFormat(change)
}

func (e *Engine) SetLink(url string) {
	e.applyFormat(func(s *Style) { s.Link = url })
}

func (e *Engine) applyFormat(change func(*Style)) {
	if e.ReadOnly {
		return
	}
	if e.depth == 0 {
		a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
		first := e.LeafAt(a)
		if z <= first.End && a >= first.Start {
			if z > a {
				runs := first.Block.Runs
				localA, localZ := a-first.Start, z-first.Start
				middle := SliceRuns(runs, localA, localZ)
				for i := range middle {
					change(&middle[i].Style)
				}
				next := Compact(append(append(SliceRuns(runs, 0, localA), middle...), SliceRuns(runs, localZ, first.End-first.Start)...))
				if !slices.Equal(runs, next) {
					e.notifyBeforeChange()
					i := e.leafIDs[first.Block.ID]
					entry := historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, first.Block.ID, slices.Clone(runs)}
					first.Block.Runs = next
					e.recordUndo(entry)
					e.version++
					txt := first.Block.Text()
					e.publishTextChange(i, txt, txt)
				}
			}
			return
		}
	}
	e.Edit(func() { e.format(change) })
}

func (e *Engine) format(change func(*Style)) {
	a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
	for _, leaf := range e.Doc.Leaves() {
		start, end := max(a, leaf.Start)-leaf.Start, min(z, leaf.End)-leaf.Start
		if end <= start {
			continue
		}
		runs := leaf.Block.Runs
		middle := SliceRuns(runs, start, end)
		for i := range middle {
			change(&middle[i].Style)
		}
		leaf.Block.Runs = Compact(append(append(SliceRuns(runs, 0, start), middle...), SliceRuns(runs, end, leaf.End-leaf.Start)...))
	}
}

func (e *Engine) find(id string) (*[]*Block, int) {
	var walk func(*[]*Block) (*[]*Block, int)
	walk = func(blocks *[]*Block) (*[]*Block, int) {
		for i, b := range *blocks {
			if b.ID == id {
				return blocks, i
			}
			if p, j := walk(&b.Children); p != nil {
				return p, j
			}
		}
		return nil, 0
	}
	return walk(&e.Doc.Blocks)
}

func (e *Engine) FocusBlock(id string) {
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == id || leaf.Owner == id {
			e.SetSelection(leaf.Start, leaf.Start)
			return
		}
	}
}

func (e *Engine) Insert(block *Block) {
	e.Edit(func() {
		leaf := e.LeafAt(e.Caret)
		parent, index := e.find(leaf.Owner)
		if leaf.Block.Kind == "paragraph" && leaf.Block.Text() == "" && leaf.Block.ID == leaf.Owner {
			(*parent)[index] = block
		} else {
			index++
			*parent = append(append(append([]*Block(nil), (*parent)[:index]...), block), (*parent)[index:]...)
		}
		e.FocusBlock(block.ID)
	})
}

// InsertAt puts blocks among the top-level blocks before the one at index
// (clamped; len for the end), as one step of the history, and puts the
// caret on the last. An empty document's only paragraph gives way to
// them.
func (e *Engine) InsertAt(index int, blocks ...*Block) {
	if len(blocks) == 0 {
		return
	}
	e.Edit(func() {
		top := &e.Doc.Blocks
		if len(*top) == 1 && (*top)[0].Kind == "paragraph" && (*top)[0].Text() == "" && len((*top)[0].Children) == 0 {
			*top = nil
		}
		index = max(0, min(index, len(*top)))
		*top = append(append(append([]*Block(nil), (*top)[:index]...), blocks...), (*top)[index:]...)
		e.FocusBlock(blocks[len(blocks)-1].ID)
	})
}

func (e *Engine) DeleteBlock(id string) {
	e.Edit(func() {
		parent, index := e.find(id)
		if parent == nil || parent != &e.Doc.Blocks {
			return
		}
		*parent = append((*parent)[:index], (*parent)[index+1:]...)
		if len(*parent) == 0 {
			*parent = []*Block{NewBlock("paragraph", "")}
		}
		e.FocusBlock((*parent)[min(index, len(*parent)-1)].ID)
	})
}

func (e *Engine) MoveBlock(id string, to int) {
	e.Edit(func() {
		parent, index := e.find(id)
		if parent != &e.Doc.Blocks {
			return
		}
		to = max(0, min(to, len(*parent)-1))
		b := (*parent)[index]
		*parent = append((*parent)[:index], (*parent)[index+1:]...)
		*parent = append(append(append([]*Block(nil), (*parent)[:to]...), b), (*parent)[to:]...)
		e.FocusBlock(id)
	})
}

func (e *Engine) Duplicate(id string) {
	parent, index := e.find(id)
	if parent == nil {
		return
	}
	block := (*parent)[index].Clone()
	var fresh func(*Block)
	fresh = func(b *Block) {
		b.ID = NewID()
		for _, child := range b.Children {
			fresh(child)
		}
	}
	fresh(block)
	e.FocusBlock(id)
	e.Insert(block)
}

func (e *Engine) ToggleTask(id string) {
	e.Edit(func() {
		if parent, index := e.find(id); parent != nil {
			b := (*parent)[index]
			b.Checked = !b.Checked
		}
	})
}

func (e *Engine) Align(align string) {
	e.Edit(func() { e.LeafAt(e.Caret).Block.Align = align })
}

func (e *Engine) Indent(delta int) {
	e.Edit(func() { b := e.LeafAt(e.Caret).Block; b.Indent = max(0, min(12, b.Indent+delta)) })
}

func (e *Engine) ResizeTable(rows bool, delta int) {
	e.Edit(func() {
		leaf := e.LeafAt(e.Caret)
		parent, index := e.find(leaf.Owner)
		if parent == nil {
			return
		}
		table := (*parent)[index]
		if table.Kind != "table" {
			return
		}
		if rows {
			if delta > 0 {
				table.Children = append(table.Children, Table(1, len(table.Children[0].Children)).Children[0])
			} else if len(table.Children) > 1 {
				table.Children = table.Children[:len(table.Children)-1]
			}
		} else {
			for _, row := range table.Children {
				if delta > 0 {
					row.Children = append(row.Children, NewBlock("cell", ""))
				} else if len(row.Children) > 1 {
					row.Children = row.Children[:len(row.Children)-1]
				}
			}
			table.Widths = nil
		}
		e.FocusBlock(table.ID)
	})
}

// TableInsert adds an empty row (rows) or column to table id before line
// at, which may be the count to append; the caret moves into the new line.
func (e *Engine) TableInsert(id string, rows bool, at int) {
	e.Edit(func() {
		parent, index := e.find(id)
		if parent == nil || (*parent)[index].Kind != "table" {
			return
		}
		table := (*parent)[index]
		focus := ""
		shiftSpans(table, rows, at, 1)
		if rows {
			at = max(0, min(at, len(table.Children)))
			line := Table(1, len(table.Children[0].Children)).Children[0]
			table.Children = append(table.Children[:at:at], append([]*Block{line}, table.Children[at:]...)...)
			focus = line.Children[0].ID
		} else {
			at = max(0, min(at, len(table.Children[0].Children)))
			for i, row := range table.Children {
				cell := NewBlock("cell", "")
				row.Children = append(row.Children[:at:at], append([]*Block{cell}, row.Children[at:]...)...)
				if i == 0 {
					focus = cell.ID
				}
			}
			if len(table.Widths) > 0 {
				// The new column takes an average share.
				sum := float32(0)
				for _, w := range table.Widths {
					sum += w
				}
				avg := sum / float32(len(table.Widths))
				table.Widths = append(table.Widths[:at:at], append([]float32{avg}, table.Widths[at:]...)...)
			}
		}
		e.FocusBlock(focus)
	})
}

// TableDelete removes row (rows) or column at from table id; removing its
// last row or column removes the table.
func (e *Engine) TableDelete(id string, rows bool, at int) {
	e.Edit(func() {
		parent, index := e.find(id)
		if parent == nil || (*parent)[index].Kind != "table" {
			return
		}
		table := (*parent)[index]
		count := len(table.Children)
		if !rows {
			count = len(table.Children[0].Children)
		}
		if at < 0 || at >= count {
			return
		}
		if count == 1 {
			e.removeTop(index, true)
			return
		}
		shiftSpans(table, rows, at, -1)
		if rows {
			table.Children = append(table.Children[:at:at], table.Children[at+1:]...)
		} else {
			for _, row := range table.Children {
				row.Children = append(row.Children[:at:at], row.Children[at+1:]...)
			}
			if len(table.Widths) > at {
				table.Widths = append(table.Widths[:at:at], table.Widths[at+1:]...)
			}
		}
		e.FocusBlock(table.ID)
	})
}

func (e *Engine) SetWidths(id string, widths []float32) {
	e.Edit(func() {
		if parent, index := e.find(id); parent != nil {
			(*parent)[index].Widths = append([]float32(nil), widths...)
		}
	})
}

package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

// smartRollback remembers the most recent automatic typography or inline
// Markdown compilation so an immediate Backspace restores the literal text.
type smartRollback struct {
	rev          uint64
	blockID      string
	start        int
	end          int
	caret        int
	restoreCaret int
	literal      string
	clearMark    string
}

// mathFenceRollback remembers an automatic promotion from a paragraph to a
// display math block so an immediate Backspace or Escape restores the literal
// "$$" or "$$ ... $$" source.
type mathFenceRollback struct {
	rev       uint64
	blockID   string
	literal   string
	dismissed bool
}

// trySmartInput applies smart typography (-- → —, --- → divider, ... → …) and
// live inline Markdown compilation (**bold**, *italic*, ~~strike~~, ~strike~,
// `code`) when typing single characters at a collapsed caret in prose.
func (e *Editor) trySmartInput(r *ui.TextInputRange, text string) bool {
	if r != nil || e.mark != nil || e.ReadOnly || e.noAutoPair {
		return false
	}
	if e.Anchor != e.Caret || utf8.RuneCountInString(text) != 1 {
		return false
	}
	l := e.LeafAt(e.Caret)
	if literalBlock(l.Block.Kind) || e.CurrentStyle().Code || e.inlineMath.blockID != "" {
		return false
	}
	raw := l.Block.Text()
	local := e.Caret - l.Start
	bytePos := document.ByteOffset(raw, local)
	lineStartByte := strings.LastIndexByte(raw[:bytePos], '\n') + 1
	lineBefore := raw[lineStartByte:bytePos]

	// Skip em-dash conversion inside pipe-table separator lines, and skip all
	// smart conversions inside URLs.
	trimmedLine := strings.TrimLeft(lineBefore, " \t")
	if text == "-" && strings.HasPrefix(trimmedLine, "|") {
		return false
	}
	lastToken := lineBefore
	if sp := strings.LastIndexAny(lineBefore, " \t([“‘\"'"); sp >= 0 {
		lastToken = lineBefore[sp+1:]
	}
	if strings.Contains(lastToken, "://") {
		return false
	}

	switch text {
	case "-":
		return e.trySmartDash(l, lineBefore)
	case ".":
		return e.trySmartEllipsis(l, lineBefore)
	case "*", "~", "`":
		return e.tryLiveInlineMarkdown(l, raw, lineStartByte, bytePos, text)
	}
	return false
}

func (e *Editor) trySmartDash(l document.Leaf, lineBefore string) bool {
	// Third hyphen right after an em-dash on an otherwise empty paragraph
	// compiles "---" into a horizontal divider rule.
	if l.Block.Kind == "paragraph" && l.Block.ID == l.Owner && l.Block.Text() == "—" && e.Caret == l.End {
		e.smartBack = smartRollback{}
		e.Engine.Edit(func() {
			parent, index := l.Parent, l.Index
			if parent == nil {
				return
			}
			rule := document.NewBlock("rule", "")
			next := document.NewBlock("paragraph", "")
			*parent = append(append(append([]*document.Block(nil), (*parent)[:index]...), rule, next), (*parent)[index+1:]...)
			e.Engine.FocusBlock(next.ID)
		})
		e.sync()
		e.pairs = nil
		return true
	}
	// "--" → "—" when preceded by a single hyphen (not "---" or "<!-").
	if !strings.HasSuffix(lineBefore, "-") || strings.HasSuffix(lineBefore, "--") || strings.HasSuffix(lineBefore, "<!-") {
		return false
	}
	if len(lineBefore) >= 2 && lineBefore[len(lineBefore)-2] == '\\' {
		return false
	}
	rep := ui.TextInputRange{Start: e.Caret - 1, End: e.Caret}
	e.shiftPairs(rep, document.Len("—"))
	e.Buffer.ReplaceText(&rep, "—")
	e.pairRevision = e.Revision()
	e.smartBack = smartRollback{
		rev:          e.Revision(),
		blockID:      l.Block.ID,
		start:        rep.Start,
		end:          rep.Start + document.Len("—"),
		caret:        rep.Start + document.Len("—"),
		restoreCaret: rep.Start + document.Len("--"),
		literal:      "--",
	}
	return true
}

func (e *Editor) trySmartEllipsis(l document.Leaf, lineBefore string) bool {
	if !strings.HasSuffix(lineBefore, "..") || strings.HasSuffix(lineBefore, "...") {
		return false
	}
	if len(lineBefore) >= 3 && lineBefore[len(lineBefore)-3] == '\\' {
		return false
	}
	rep := ui.TextInputRange{Start: e.Caret - 2, End: e.Caret}
	e.shiftPairs(rep, document.Len("…"))
	e.Buffer.ReplaceText(&rep, "…")
	e.pairRevision = e.Revision()
	e.smartBack = smartRollback{
		rev:          e.Revision(),
		blockID:      l.Block.ID,
		start:        rep.Start,
		end:          rep.Start + document.Len("…"),
		caret:        rep.Start + document.Len("…"),
		restoreCaret: rep.Start + document.Len("..."),
		literal:      "...",
	}
	return true
}

func (e *Editor) tryLiveInlineMarkdown(l document.Leaf, raw string, lineStartByte, caretByte int, ch string) bool {
	if idx := strings.LastIndex(raw[:caretByte], "\u2028"); idx >= 0 && idx+len("\u2028") > lineStartByte {
		lineStartByte = idx + len("\u2028")
	}
	lineEndByte := len(raw)
	if idx := strings.IndexByte(raw[caretByte:], '\n'); idx >= 0 {
		lineEndByte = caretByte + idx
	}
	if idx := strings.Index(raw[caretByte:lineEndByte], "\u2028"); idx >= 0 {
		lineEndByte = caretByte + idx
	}
	line := raw[lineStartByte:caretByte]
	if strings.HasSuffix(line, `\`) {
		return false
	}
	var delim, mark string
	openIdx := -1

	switch ch {
	case "`":
		// Match single `code` (not ``` fences).
		if strings.HasSuffix(line, "`") {
			return false
		}
		openIdx = findInlineOpener(line, "`")
		if openIdx >= 0 {
			inner := line[openIdx+1:]
			if inner == "" || strings.Contains(inner, "`") {
				openIdx = -1
			} else {
				delim, mark = "`", "code"
			}
		}

	case "*":
		switch {
		case strings.HasSuffix(line, "**") && !strings.HasSuffix(line, "***"):
			prefix := line[:len(line)-2]
			openIdx = findInlineOpener(prefix, "***")
			if openIdx >= 0 {
				delim, mark = "***", "bold+italic"
			}
		case strings.HasSuffix(line, "*") && !strings.HasSuffix(line, "**"):
			prefix := line[:len(line)-1]
			openIdx = findInlineOpener(prefix, "**")
			if openIdx >= 0 {
				delim, mark = "**", "bold"
			}
		default:
			openIdx = findInlineOpener(line, "*")
			if openIdx >= 0 {
				delim, mark = "*", "italic"
			}
		}

	case "~":
		if strings.HasSuffix(line, "~") && !strings.HasSuffix(line, "~~") {
			prefix := line[:len(line)-1]
			openIdx = findInlineOpener(prefix, "~~")
			if openIdx >= 0 {
				delim, mark = "~~", "strike"
			}
		} else if !strings.HasSuffix(line, "~") {
			openIdx = findInlineOpener(line, "~")
			if openIdx >= 0 {
				delim, mark = "~", "strike"
			}
		}
	}

	forward := false
	var openStartByte, innerStartByte, innerEndByte, closeEndByte int
	if openIdx >= 0 && delim != "" {
		closeAlready := len(delim) - 1
		openStartByte = lineStartByte + openIdx
		innerStartByte = openStartByte + len(delim)
		innerEndByte = caretByte - closeAlready
		closeEndByte = caretByte
	} else {
		delim, mark = "", ""
		after := raw[caretByte:lineEndByte]
		var closeOff int
		switch ch {
		case "`":
			if !strings.HasSuffix(line, "`") && !strings.HasPrefix(after, "`") {
				if c := findInlineCloser(after, "`"); c > 0 && !strings.Contains(after[:c], "`") {
					delim, mark, openStartByte, closeOff, forward = "`", "code", caretByte, c, true
				}
			}
		case "*":
			switch {
			case strings.HasSuffix(line, "**") && !strings.HasSuffix(line, "***"):
				delim, mark, openStartByte = "***", "bold+italic", caretByte-2
			case strings.HasSuffix(line, "*") && !strings.HasSuffix(line, "**"):
				delim, mark, openStartByte = "**", "bold", caretByte-1
			case !strings.HasSuffix(line, "*"):
				delim, mark, openStartByte = "*", "italic", caretByte
			}
			if delim != "" && !strings.HasPrefix(after, "*") && !(openStartByte > lineStartByte && (raw[openStartByte-1] == '*' || raw[openStartByte-1] == '\\')) {
				if c := findInlineCloser(after, delim); c > 0 {
					closeOff, forward = c, true
				}
			}
		case "~":
			switch {
			case strings.HasSuffix(line, "~") && !strings.HasSuffix(line, "~~"):
				delim, mark, openStartByte = "~~", "strike", caretByte-1
			case !strings.HasSuffix(line, "~"):
				delim, mark, openStartByte = "~", "strike", caretByte
			}
			if delim != "" && !strings.HasPrefix(after, "~") && !(openStartByte > lineStartByte && (raw[openStartByte-1] == '~' || raw[openStartByte-1] == '\\')) {
				if c := findInlineCloser(after, delim); c > 0 {
					closeOff, forward = c, true
				}
			}
		}
		if !forward || delim == "" {
			return false
		}
		innerStartByte = caretByte
		innerEndByte = caretByte + closeOff
		closeEndByte = innerEndByte + len(delim)
	}

	if innerEndByte <= innerStartByte {
		return false
	}
	inner := raw[innerStartByte:innerEndByte]
	if mark != "code" {
		first, _ := utf8.DecodeRuneInString(inner)
		last, _ := utf8.DecodeLastRuneInString(inner)
		if unicode.IsSpace(first) || unicode.IsSpace(last) {
			return false
		}
	}

	blockOpenU16 := document.Len(raw[:openStartByte])
	blockInnerStartU16 := document.Len(raw[:innerStartByte])
	blockInnerEndU16 := document.Len(raw[:innerEndByte])
	blockCloseEndU16 := document.Len(raw[:closeEndByte])

	// Ensure no part of the delimiter or content sits inside an existing code or math span.
	for _, run := range document.SliceRuns(l.Block.Runs, blockOpenU16, blockCloseEndU16) {
		if run.Style.Code || run.Style.Link != "" {
			return false
		}
	}
	for _, tok := range inlineTokens(raw) {
		ta, tz := document.Len(raw[:tok.start]), document.Len(raw[:tok.end])
		if blockOpenU16 < tz && blockCloseEndU16 > ta {
			return false
		}
	}

	docStart := l.Start + blockOpenU16
	docCloseEnd := l.Start + blockCloseEndU16
	innerLen := blockInnerEndU16 - blockInnerStartU16
	literal := delim + inner + delim

	newCaret := docStart + innerLen
	restoreCaret := docStart + document.Len(literal)
	if forward {
		newCaret = docStart
		restoreCaret = docStart + document.Len(delim)
	}

	baseTyping := e.CurrentStyle()
	e.shiftPairs(ui.TextInputRange{Start: docStart, End: docCloseEnd}, innerLen)
	totalU16 := l.End - l.Start
	runs := l.Block.Runs
	prefix := document.SliceRuns(runs, 0, blockOpenU16)
	middle := document.SliceRuns(runs, blockInnerStartU16, blockInnerEndU16)
	suffix := document.SliceRuns(runs, blockCloseEndU16, totalU16)
	for i := range middle {
		switch mark {
		case "bold":
			middle[i].Style.Bold = true
		case "italic":
			middle[i].Style.Italic = true
		case "bold+italic":
			middle[i].Style.Bold = true
			middle[i].Style.Italic = true
		case "strike":
			middle[i].Style.Strike = true
		case "code":
			middle[i].Style.Code = true
		}
	}
	nextStyle := baseTyping
	switch mark {
	case "bold":
		nextStyle.Bold = false
	case "italic":
		nextStyle.Italic = false
	case "bold+italic":
		nextStyle.Bold, nextStyle.Italic = false, false
	case "strike":
		nextStyle.Strike = false
	case "code":
		nextStyle.Code = false
	}
	nextRuns := append(append(prefix, middle...), suffix...)
	e.Engine.ReplaceBlockRuns(l.Block.ID, nextRuns, newCaret, newCaret, nextStyle, true)
	e.sync()
	e.pairRevision = e.Revision()
	e.smartBack = smartRollback{
		rev:          e.Revision(),
		blockID:      l.Block.ID,
		start:        docStart,
		end:          docStart + innerLen,
		caret:        newCaret,
		restoreCaret: restoreCaret,
		literal:      literal,
		clearMark:    mark,
	}
	return true
}

// findInlineCloser locates the leftmost exact delimiter run of delim in s
// that is neither escaped nor part of a longer run of the same character.
func findInlineCloser(s, delim string) int {
	ch := delim[0]
	n := len(delim)
	for i := 1; i+n <= len(s); i++ {
		if s[i:i+n] != delim {
			continue
		}
		if s[i-1] == ch || s[i-1] == '\\' {
			continue
		}
		if i+n < len(s) && s[i+n] == ch {
			continue
		}
		if ch != '`' {
			prev, _ := utf8.DecodeLastRuneInString(s[:i])
			if unicode.IsSpace(prev) {
				continue
			}
		}
		return i
	}
	return -1
}

// findInlineOpener locates the rightmost exact delimiter run of delim in s
// that is neither escaped nor part of a longer run of the same character.
func findInlineOpener(s, delim string) int {
	ch := delim[0]
	n := len(delim)
	for i := len(s) - n; i >= 0; i-- {
		if s[i:i+n] != delim {
			continue
		}
		if i > 0 && s[i-1] == ch {
			continue
		}
		if i+n < len(s) && s[i+n] == ch {
			continue
		}
		if i > 0 && s[i-1] == '\\' {
			continue
		}
		if ch != '`' && i+n < len(s) {
			next, _ := utf8.DecodeRuneInString(s[i+n:])
			if unicode.IsSpace(next) {
				continue
			}
		}
		return i
	}
	return -1
}

func (e *Editor) promoteParagraphToMath(l document.Leaf, formula, literal string) {
	e.pairs = nil
	e.smartBack = smartRollback{}
	e.Engine.Edit(func() {
		l.Block.Kind = "math"
		if formula == "" {
			l.Block.Runs = nil
		} else {
			l.Block.Runs = []document.Run{{Text: formula}}
		}
		caret := l.Start + document.Len(formula)
		e.Engine.Anchor, e.Engine.Caret = caret, caret
		e.Engine.Typing, e.Engine.Explicit = document.Style{}, false
	})
	e.sync()
	e.pairRevision = e.Revision()
	e.mathFence = mathFenceRollback{
		rev:     e.Revision(),
		blockID: l.Block.ID,
		literal: literal,
	}
}

func (e *Editor) tryCompileDisplayMathParagraph() bool {
	if e.Anchor != e.Caret {
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.Kind != "paragraph" || l.Parent == nil || e.Caret != l.End {
		return false
	}
	raw := l.Block.Text()
	if !strings.HasSuffix(raw, "$$") {
		return false
	}
	formula, ok := document.SingleLineDisplayMath(l.Block)
	if !ok {
		return false
	}
	e.promoteParagraphToMath(l, formula, raw)
	return true
}

func (e *Editor) rollbackMathFence() bool {
	mf := e.mathFence
	if mf.dismissed || mf.rev == 0 || mf.rev != e.Revision() || e.Anchor != e.Caret {
		e.mathFence = mathFenceRollback{}
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.ID != mf.blockID || l.Block.Kind != "math" || e.Caret != l.End {
		e.mathFence = mathFenceRollback{}
		return false
	}
	e.mathFence = mathFenceRollback{}
	e.pairs = nil
	e.Engine.Edit(func() {
		l.Block.Kind = "paragraph"
		l.Block.Runs = []document.Run{{Text: mf.literal}}
		pos := l.Start + document.Len(mf.literal)
		e.Engine.Anchor, e.Engine.Caret = pos, pos
		e.Engine.Typing, e.Engine.Explicit = document.Style{}, true
	})
	e.sync()
	e.pairRevision = e.Revision()
	e.mathFence = mathFenceRollback{
		rev:       e.Revision(),
		blockID:   l.Block.ID,
		dismissed: true,
	}
	return true
}

func (e *Editor) consumeMathFenceEnter() bool {
	mf := e.mathFence
	e.mathFence = mathFenceRollback{}
	if mf.dismissed || mf.rev == 0 || mf.rev != e.Revision() || e.Anchor != e.Caret {
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.ID != mf.blockID || l.Block.Kind != "math" {
		return false
	}
	if l.Block.Text() == "" && e.Caret == l.Start {
		return true
	}
	if l.Block.Text() != "" && e.Caret == l.End {
		return e.exitDisplayMath()
	}
	return false
}

func (e *Editor) rollbackSmartInput() bool {
	if e.rollbackMathFence() {
		return true
	}
	sb := e.smartBack
	if sb.rev == 0 || sb.rev != e.Revision() || e.Anchor != e.Caret || e.Caret != sb.caret {
		e.smartBack = smartRollback{}
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.ID != sb.blockID || sb.start < l.Start || sb.end > l.End {
		e.smartBack = smartRollback{}
		return false
	}
	e.smartBack = smartRollback{}
	rep := ui.TextInputRange{Start: sb.start, End: sb.end}
	e.shiftPairs(rep, document.Len(sb.literal))
	if sb.clearMark != "" {
		runs := l.Block.Runs
		a, z := sb.start-l.Start, sb.end-l.Start
		total := l.End - l.Start
		plain := document.Style{}
		nextRuns := append(append(document.SliceRuns(runs, 0, a), document.Run{Text: sb.literal, Style: plain}), document.SliceRuns(runs, z, total)...)
		pos := sb.restoreCaret
		e.Engine.ReplaceBlockRuns(l.Block.ID, nextRuns, pos, pos, plain, true)
		e.sync()
	} else {
		e.Buffer.ReplaceText(&rep, sb.literal)
		e.SetSelection(sb.restoreCaret, sb.restoreCaret)
	}
	e.pairRevision = e.Revision()
	return true
}

// paintTabOutBadge draws a compact "⇥" hint badge anchored to the top-right
// corner of an active pair capsule or inline math frame.
func (e *Editor) paintTabOutBadge(p *ui.Painter, frame ui.Rect, t *ui.Theme) {
	if e.printing || e.ReadOnly {
		return
	}
	span := ui.Span{Text: "⇥", Font: "system-ui", Size: 9.5, Weight: 600, Color: t.Accent}
	tw, th := p.MeasureText(0, span)
	bw, bh := max(15, tw+7), max(13, th+2)
	pill := ui.Rect{
		X: frame.X + frame.W - bw*0.45,
		Y: frame.Y - bh*0.58,
		W: bw,
		H: bh,
	}
	p.Fill(pill, e.page.Popover, bh/2)
	p.Stroke(pill, t.Accent.Alpha(0.4), bh/2, 1)
	p.RichText(pill.X+(pill.W-tw)/2, pill.Y+(pill.H-th)/2, 0, span)
}

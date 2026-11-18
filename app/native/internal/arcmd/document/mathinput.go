package document

import "strings"

// SingleLineDisplayMath extracts the formula from a single-line paragraph
// fenced by $$ ... $$ on both sides (with at most 3 CommonMark leading spaces).
func SingleLineDisplayMath(b *Block) (string, bool) {
	if b == nil || b.Kind != "paragraph" {
		return "", false
	}
	for _, r := range b.Runs {
		if r.Style.Code || r.Style.Link != "" || r.Style.NoMath {
			return "", false
		}
	}
	raw := b.Text()
	if strings.ContainsAny(raw, "\r\n\u2028") {
		return "", false
	}
	lead := len(raw) - len(strings.TrimLeft(raw, " "))
	if lead > 3 || (lead < len(raw) && raw[lead] == '\t') {
		return "", false
	}
	trimmed := strings.Trim(raw[lead:], " \t")
	if len(trimmed) <= 4 || !strings.HasPrefix(trimmed, "$$") || !strings.HasSuffix(trimmed, "$$") {
		return "", false
	}
	body := trimmed[2 : len(trimmed)-2]
	if strings.Contains(body, "$$") || strings.HasSuffix(body, `\`) {
		return "", false
	}
	formula := strings.TrimSpace(body)
	if formula == "" {
		return "", false
	}
	return formula, true
}

// enterMath recognizes display fences and trailing double-Enter exits without
// reparsing the document or changing ordinary Enter inside multiline TeX.
func (e *Engine) enterMath(leaf Leaf) bool {
	if e.Anchor != e.Caret || e.Caret != leaf.End || e.CurrentStyle().Code {
		return false
	}
	b := leaf.Block
	source := b.Text()
	if b.Kind == "paragraph" {
		lead := len(source) - len(strings.TrimLeft(source, " "))
		if lead <= 3 && strings.TrimRight(source[lead:], " \t") == "$$" {
			e.Edit(func() {
				b.Kind, b.Runs = "math", nil
				e.Anchor, e.Caret = leaf.Start, leaf.Start
				e.Typing, e.Explicit = Style{}, false
			})
			return true
		}
		if leaf.Parent != nil {
			if formula, ok := SingleLineDisplayMath(b); ok {
				e.Edit(func() {
					b.Kind = "math"
					b.Runs = []Run{{Text: formula}}
					next := NewBlock("paragraph", "")
					next.Indent = b.Indent
					parent, index := leaf.Parent, leaf.Index+1
					*parent = append(append(append([]*Block(nil), (*parent)[:index]...), next), (*parent)[index:]...)
					e.FocusBlock(next.ID)
				})
				return true
			}
		}
		return false
	}
	fenceExit := strings.HasSuffix(source, "$$") && !strings.HasSuffix(source, `\$$`)
	doubleEnterExit := strings.HasSuffix(source, "\n")
	if b.Kind != "math" || !fenceExit && !doubleEnterExit {
		return false
	}
	e.Edit(func() {
		body := source
		switch {
		case strings.HasSuffix(source, "\n$$") || source == "$$":
			body = strings.TrimSuffix(strings.TrimSuffix(source, "$$"), "\n")
		case fenceExit:
			body = strings.TrimRight(strings.TrimSuffix(source, "$$"), " \t")
		default:
			body = strings.TrimSuffix(source, "\n")
		}
		b.Runs = SliceRuns(b.Runs, 0, Len(body))
		next := NewBlock("paragraph", "")
		parent, index := leaf.Parent, leaf.Index+1
		*parent = append(append(append([]*Block(nil), (*parent)[:index]...), next), (*parent)[index:]...)
		e.FocusBlock(next.ID)
	})
	return true
}

// enterPipeTable compiles a single-line pipe header row such as
// "| Port | Low | High" into a two-row table when Enter is pressed at its end.
func (e *Engine) enterPipeTable(leaf Leaf) bool {
	if e.Anchor != e.Caret || e.Caret != leaf.End || e.CurrentStyle().Code {
		return false
	}
	b := leaf.Block
	if b.Kind != "paragraph" || leaf.Parent == nil {
		return false
	}
	raw := b.Text()
	if strings.ContainsAny(raw, "\r\n\u2028") {
		return false
	}
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	spans := splitPipeCellRanges(raw)
	if len(spans) < 2 {
		return false
	}
	allRule := true
	for _, sp := range spans {
		cellText := raw[ByteOffset(raw, sp[0]):ByteOffset(raw, sp[1])]
		if !isPipeRuleCell(cellText) {
			allRule = false
			break
		}
	}
	e.Edit(func() {
		table := Table(2, len(spans))
		if !allRule {
			for i, sp := range spans {
				runs := SliceRuns(b.Runs, sp[0], sp[1])
				for j := range runs {
					runs[j].Text = strings.ReplaceAll(runs[j].Text, `\|`, "|")
				}
				table.Children[0].Children[i].Runs = Compact(runs)
			}
		}
		parent, index := leaf.Parent, leaf.Index
		(*parent)[index] = table
		firstBody := table.Children[1].Children[0].ID
		e.FocusBlock(firstBody)
		e.Typing, e.Explicit = Style{}, false
	})
	return true
}

// splitPipeCellRanges returns trimmed UTF-16 [start, end) ranges for each cell
// in a pipe row beginning with "|". A trailing "|" is optional when at least
// two pipe delimiters are present.
func splitPipeCellRanges(raw string) [][2]int {
	lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
	if lead >= len(raw) || raw[lead] != '|' {
		return nil
	}
	var pipes []int
	for i := lead; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
			continue
		}
		if raw[i] == '|' {
			pipes = append(pipes, i)
		}
	}
	if len(pipes) < 2 {
		return nil
	}
	end := len(raw)
	tail := strings.TrimRight(raw, " \t")
	hasTrailingPipe := pipes[len(pipes)-1] == len(tail)-1
	if !hasTrailingPipe && len(pipes) < 2 {
		return nil
	}
	var out [][2]int
	count := len(pipes)
	if hasTrailingPipe {
		count--
	}
	for k := 0; k < count; k++ {
		segStart := pipes[k] + 1
		segEnd := end
		if k+1 < len(pipes) {
			segEnd = pipes[k+1]
		}
		seg := raw[segStart:segEnd]
		ltrim := len(seg) - len(strings.TrimLeft(seg, " \t"))
		rtrim := len(strings.TrimRight(seg, " \t"))
		aBytes := segStart + ltrim
		zBytes := segStart + max(ltrim, rtrim)
		out = append(out, [2]int{Len(raw[:aBytes]), Len(raw[:zBytes])})
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func isPipeRuleCell(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ":")
	s = strings.TrimSuffix(s, ":")
	if len(s) < 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			return false
		}
	}
	return true
}

// enterCodeFence converts a paragraph holding a fenced code header such as
// "```go" or "```mermaid" into a code block when Enter is pressed at its end.
func (e *Engine) enterCodeFence(leaf Leaf) bool {
	if e.Anchor != e.Caret || e.Caret != leaf.End || e.CurrentStyle().Code {
		return false
	}
	b := leaf.Block
	if b.Kind != "paragraph" {
		return false
	}
	source := b.Text()
	if strings.ContainsAny(source, "\r\n\u2028") {
		return false
	}
	lead := len(source) - len(strings.TrimLeft(source, " "))
	if lead > 3 || (lead < len(source) && source[lead] == '\t') {
		return false
	}
	rest := source[lead:]
	if !strings.HasPrefix(rest, "```") {
		return false
	}
	lang := strings.TrimSpace(rest[3:])
	for i := 0; i < len(lang); i++ {
		c := lang[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '+' || c == '-'
		if !ok {
			return false
		}
	}
	e.Edit(func() {
		b.Kind = "code"
		b.Language = lang
		b.Runs = nil
		e.Anchor, e.Caret = leaf.Start, leaf.Start
		e.Typing, e.Explicit = Style{}, false
	})
	return true
}

// enterMermaidExit exits a Mermaid block when Enter is pressed on a trailing
// empty line (double-Enter), matching display math behavior.
func (e *Engine) enterMermaidExit(leaf Leaf) bool {
	if e.Anchor != e.Caret || e.Caret != leaf.End || e.CurrentStyle().Code {
		return false
	}
	b := leaf.Block
	if b.Kind != "code" || !strings.EqualFold(strings.TrimSpace(b.Language), "mermaid") {
		return false
	}
	source := b.Text()
	if !strings.HasSuffix(source, "\n") || leaf.Parent == nil {
		return false
	}
	e.Edit(func() {
		body := strings.TrimSuffix(source, "\n")
		b.Runs = SliceRuns(b.Runs, 0, Len(body))
		next := NewBlock("paragraph", "")
		next.Indent = b.Indent
		parent, index := leaf.Parent, leaf.Index+1
		*parent = append(append(append([]*Block(nil), (*parent)[:index]...), next), (*parent)[index:]...)
		e.FocusBlock(next.ID)
	})
	return true
}

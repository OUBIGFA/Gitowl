package editor

import (
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
)

// spaced puts a space before text typed at a collapsed caret where it
// meets the text before across Chinese and Latin: a letter or digit after
// a Han character, or a Han character after a letter or digit, as the
// common style for mixed Chinese and English. Code, formulas and source
// keep what is typed.
func (e *Editor) spaced(text string, at int) string {
	if text == "" {
		return text
	}
	leaf := e.Engine.LeafAt(at)
	if k := leaf.Block.Kind; k == "code" || k == "raw" || k == "math" || e.Engine.CurrentStyle().Code {
		return text
	}
	block := leaf.Block.Text()
	before := block[:document.ByteOffset(block, at-leaf.Start)]
	if before == "" {
		return text
	}
	prev, _ := utf8.DecodeLastRuneInString(before)
	next, _ := utf8.DecodeRuneInString(text)
	if han(prev) && latin(next) || latin(prev) && han(next) {
		return " " + text
	}
	return text
}

func han(r rune) bool { return unicode.Is(unicode.Han, r) }

// latin is an ASCII letter or digit: other scripts and punctuation take no
// space.
func latin(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

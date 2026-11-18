package editor

import (
	"reflect"

	"github.com/egoist/mygo/ui"
)

const (
	// Keep full accessibility text for ordinary documents, and bound huge
	// documents to a quantized window around the selection so frame-end AX
	// serialization never flattens or copies megabytes on every keystroke.
	accessWindowThreshold = 16_384
	accessWindowHalf      = 1024
	accessWindowStep      = 512
)

// MyGo 0.7.4 exposes a lazy Access callback but keeps its node type internal.
// Infer that callback's type and set only its exported Value field. This keeps
// screen readers' full text current without building it on every visual frame;
// no unsafe access, private fields or changes to MyGo's module are involved.
func accessTextValue[T any](access func(func(T)) ui.Element, text func() string) {
	access(func(node T) { reflect.ValueOf(node).Elem().FieldByName("Value").SetString(text()) })
}

func (e *Editor) accessText() string {
	n := e.Len()
	if n <= accessWindowThreshold {
		return e.displayText()
	}
	sel := e.Selection().Range
	lo := max(0, (sel.Start-accessWindowHalf)/accessWindowStep*accessWindowStep)
	hi := min(n, ((max(sel.End, sel.Start)+accessWindowHalf+accessWindowStep-1)/accessWindowStep)*accessWindowStep)
	if hi-lo > 4*accessWindowHalf {
		hi = min(n, lo+4*accessWindowHalf)
	}
	rev := e.Revision()
	if e.accessDoc == e.Doc && e.accessRev == rev && e.accessStart == lo && e.accessEnd == hi {
		return e.accessVal
	}
	e.accessVal = e.Engine.TextRange(lo, hi)
	e.accessDoc, e.accessRev, e.accessStart, e.accessEnd = e.Doc, rev, lo, hi
	return e.accessVal
}

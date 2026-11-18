package document

import "strings"

// offsetIndex is a Fenwick tree of leaf lengths, each including its following
// newline. Point edits and prefix lookups are logarithmic; structural edits
// rebuild in linear time. The final synthetic newline is excluded from totals.
type offsetIndex []int

func (x offsetIndex) add(i, delta int) {
	for i++; i < len(x); i += i & -i {
		x[i] += delta
	}
}
func (x offsetIndex) prefix(n int) int {
	sum := 0
	for ; n > 0; n -= n & -n {
		sum += x[n]
	}
	return sum
}
func (x offsetIndex) containing(at int) int {
	i, sum, bit := 0, 0, 1
	for bit < len(x) {
		bit <<= 1
	}
	for bit >>= 1; bit > 0; bit >>= 1 {
		if next := i + bit; next < len(x) && sum+x[next] <= at {
			i, sum = next, sum+x[next]
		}
	}
	return min(i, len(x)-2)
}
func (e *Engine) rebuildIndex() {
	e.leaves = e.Doc.Leaves()
	e.leafIDs = make(map[string]int, len(e.leaves))
	e.unitIndex, e.byteIndex = make(offsetIndex, len(e.leaves)+1), make(offsetIndex, len(e.leaves)+1)
	for i, l := range e.leaves {
		e.leafIDs[l.Block.ID] = i
		e.unitIndex[i+1] += l.End - l.Start + 1
		e.byteIndex[i+1] += l.ByteEnd - l.ByteStart + 1
		if parent := i + 1 + ((i + 1) & -(i + 1)); parent < len(e.unitIndex) {
			e.unitIndex[parent] += e.unitIndex[i+1]
			e.byteIndex[parent] += e.byteIndex[i+1]
		}
	}
	e.indexDoc, e.indexRevision = e.Doc, e.revision
}

func (e *Engine) ensureIndex() {
	// Transactions can query intermediate mutations before publishing a revision.
	if e.depth == 0 && e.indexDoc == e.Doc && e.indexRevision == e.revision {
		return
	}
	e.rebuildIndex()
}
func (e *Engine) leafAtIndex(i int) Leaf {
	l := e.leaves[i]
	start, byteStart := e.unitIndex.prefix(i), e.byteIndex.prefix(i)
	l.Start, l.End = start, start+l.End-l.Start
	l.ByteStart, l.ByteEnd = byteStart, byteStart+l.ByteEnd-l.ByteStart
	return l
}

func (e *Engine) LeafAt(at int) Leaf {
	e.ensureIndex()
	return e.leafAtIndex(e.unitIndex.containing(max(0, at)))
}

// LeafAtByte finds the leaf containing a byte offset in PlainText. Separator
// offsets select the preceding leaf; offsets past the end select the last.
func (e *Engine) LeafAtByte(at int) Leaf {
	e.ensureIndex()
	return e.leafAtIndex(e.byteIndex.containing(max(0, at)))
}

// LeafByID resolves a stable leaf identity without walking unrelated blocks.
func (e *Engine) LeafByID(id string) (Leaf, bool) {
	e.ensureIndex()
	i, ok := e.leafIDs[id]
	if !ok {
		return Leaf{}, false
	}
	return e.leafAtIndex(i), true
}

// LeafCount returns the number of leaf blocks in the indexed document.
func (e *Engine) LeafCount() int {
	e.ensureIndex()
	return len(e.leaves)
}

// LeafAtIndex returns the leaf at zero-based index i with current offsets.
func (e *Engine) LeafAtIndex(i int) Leaf {
	e.ensureIndex()
	return e.leafAtIndex(i)
}

// HasLeaf reports whether id currently exists in the indexed leaf set.
func (e *Engine) HasLeaf(id string) bool {
	e.ensureIndex()
	_, ok := e.leafIDs[id]
	return ok
}

// InvalidateIndex marks the cached leaf index stale so the next query rebuilds
// it, including after callers mutate Doc directly outside Engine methods.
func (e *Engine) InvalidateIndex() {
	e.indexDoc = nil
}

// TextLength is the flattened document's UTF-16 length, including separators.
func (e *Engine) TextLength() int {
	e.ensureIndex()
	return max(0, e.unitIndex.prefix(len(e.leaves))-1)
}

// TextByteLength is the flattened document's UTF-8 byte length, including separators.
func (e *Engine) TextByteLength() int {
	e.ensureIndex()
	return max(0, e.byteIndex.prefix(len(e.leaves))-1)
}

// TextRange reads only the intersected leaves. Native offsets inside surrogate
// pairs round down, matching ByteOffset and the system text input protocol.
func (e *Engine) TextRange(a, z int) string {
	a, z = max(0, min(a, z)), max(0, max(a, z))
	e.ensureIndex()
	i, j := e.unitIndex.containing(max(0, a)), e.unitIndex.containing(max(0, z))
	first, last := e.leafAtIndex(i), e.leafAtIndex(j)
	left, right := first.Block.Text(), last.Block.Text()
	lo, hi := ByteOffset(left, max(0, a-first.Start)), ByteOffset(right, max(0, z-last.Start))
	if i == j {
		return left[lo:max(lo, hi)]
	}
	var out strings.Builder
	out.Grow(last.ByteStart + hi - first.ByteStart - lo)
	out.WriteString(left[lo:])
	for k := i + 1; k < j; k++ {
		out.WriteByte('\n')
		out.WriteString(e.leaves[k].Block.Text())
	}
	out.WriteByte('\n')
	out.WriteString(right[:hi])
	return out.String()
}

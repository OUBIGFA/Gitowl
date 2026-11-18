package editor

import "github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"

// Width changes do not change folded structure. Reuse folded and foldable
// lookups while the model is unchanged, relying on Engine's leaf index rather
// than duplicating every leaf struct in another map.
type layoutInputs struct {
	doc      *document.Document
	revision uint64
	hidden   map[string]string
	foldable map[string]bool
}

func (in *layoutInputs) refresh(doc *document.Document, revision uint64, ready bool) {
	if ready && in.doc == doc && in.revision == revision {
		return
	}
	in.doc, in.revision = doc, revision
	in.hidden, in.foldable = doc.HiddenBlocks(), doc.FoldableBlocks()
}

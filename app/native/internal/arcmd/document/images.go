package document

import "slices"

// ReplaceImage changes one picture without losing its description or ID.
// expected prevents a delayed file picker from overwriting a newer choice.
func (e *Engine) ReplaceImage(id, expected, next string) bool {
	if e.ReadOnly {
		return false
	}
	parent, index := e.find(id)
	if parent == nil || (*parent)[index].Kind != "image" || (*parent)[index].URL != expected || next == "" {
		return false
	}
	e.Edit(func() { (*parent)[index].URL = next })
	return true
}

// MapImages changes image and cover URLs without touching text or selection.
func (d *Document) MapImages(change func(string) string) {
	if d.Cover != "" {
		d.Cover = change(d.Cover)
	}
	var walk func([]*Block)
	walk = func(blocks []*Block) {
		for _, b := range blocks {
			if b.Kind == "image" {
				b.URL = change(b.URL)
			}
			walk(b.Children)
		}
	}
	walk(d.Blocks)
}

// ImageURLs includes images reachable through undo and redo, so moving a
// document does not strand an attachment that an earlier edit can restore.
func (e *Engine) ImageURLs() []string {
	var urls []string
	collect := func(d *Document) {
		if d == nil {
			return
		}
		d.MapImages(func(url string) string {
			if url != "" && !slices.Contains(urls, url) {
				urls = append(urls, url)
			}
			return url
		})
	}
	collect(e.Doc)
	for _, h := range e.undo {
		collect(h.state.Doc)
	}
	for _, h := range e.redo {
		collect(h.state.Doc)
	}
	return urls
}

// RelocateImages rebases all retained states as a file-system operation, not
// an editing step. Undo must never restore URLs relative to the old folder.
func (e *Engine) RelocateImages(urls map[string]string) {
	change := func(url string) string {
		if to, ok := urls[url]; ok {
			return to
		}
		return url
	}
	e.notifyBeforeChange()
	e.Doc.MapImages(change)
	for _, entries := range [][]historyEntry{e.undo, e.redo} {
		for _, h := range entries {
			if h.state.Doc != nil {
				h.state.Doc.MapImages(change)
			}
		}
	}
	e.revision++
}

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/wiki"
	"github.com/egoist/mygo/ui"
)

type notebookSearch struct {
	mode, query, root string
	results           []string
	err               error
	busy              bool
	generation        int
	cancel            context.CancelFunc
	list              nativeListState
}

func (a *app) startNotebookSearch(mode, query string) {
	s := &a.notebook.search
	s.mode = mode
	s.query = query
	a.notebook.searchOpen = true
	a.runNotebookSearch()
}
func (a *app) runNotebookSearch() {
	s := &a.notebook.search
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	s.cancel = cancel
	s.generation++
	gen := s.generation
	s.root = a.repo.Path
	s.results = nil
	s.err = nil
	s.busy = true
	root, mode, query := s.root, s.mode, s.query
	go func() {
		hits, err := searchNotebook(ctx, root, mode, query)
		if ctx.Err() != nil {
			return
		}
		a.update(func() {
			if !a.closed && s.generation == gen {
				s.results = hits
				s.err = err
				s.busy = false
			}
		})
	}()
}
func searchNotebook(ctx context.Context, root, mode, query string) ([]string, error) {
	var hits []string
	tree, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	query = strings.TrimSpace(query)
	if query == "" && (mode == "text" || mode == "backlinks") {
		return nil, nil
	}
	err = fs.WalkDir(tree.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), ".git") {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.EqualFold(d.Name(), ".git") {
			return nil
		}
		lower := strings.ToLower(name)
		var match bool
		switch mode {
		case "files":
			match = strings.Contains(lower, strings.ToLower(query))
		case "wiki":
			match = wiki.Same(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), query) || wiki.Same(strings.TrimSuffix(name, filepath.Ext(name)), query)
		default:
			if mode == "backlinks" && !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
				return nil
			}
			data, err := tree.ReadFile(filepath.FromSlash(name))
			if err != nil {
				return err
			}
			text, _, err := decodeNotebookText(data)
			if err != nil {
				return nil
			}
			if mode == "backlinks" {
				match = wiki.LinksTo(text, query)
			} else {
				match = strings.Contains(strings.ToLower(text), strings.ToLower(query))
			}
		}
		if match {
			hits = append(hits, filepath.FromSlash(name))
			if len(hits) == 100 {
				return fs.SkipAll
			}
		}
		return nil
	})
	return hits, err
}
func (a *app) notebookSearchDialog(c *ui.Context) {
	n := &a.notebook
	s := &n.search
	if s.root != a.repo.Path {
		if s.cancel != nil {
			s.cancel()
		}
		n.searchOpen = false
		return
	}
	ui.Modal(c.Key("notebook-search"), &n.searchOpen, func() {
		ui.Row(c).Gap(8).Children(func() {
			for _, mode := range []struct{ id, key string }{{"files", "Quick"}, {"text", "Search"}, {"backlinks", "Backlinks"}} {
				if mode.id == "backlinks" && a.notes().current == nil {
					continue
				}
				quietButton(c.Key(mode.id), bookLabel(c, mode.key), "").OnClick(func() {
					q := s.query
					if mode.id == "backlinks" {
						name := filepath.Base(a.notes().current.name)
						q = strings.TrimSuffix(name, filepath.Ext(name))
					}
					a.startNotebookSearch(mode.id, q)
				})
			}
		})
		ui.SearchField(c.Key("note-query"), &s.query).FillWidth().Label(bookLabel(c, "Search")).Placeholder(bookLabel(c, "Search")).OnChange(a.runNotebookSearch)
		if s.err != nil && !errors.Is(s.err, context.Canceled) {
			showError(c, s.err.Error())
		}
		if s.busy {
			ui.Text(c, bookLabel(c, "Searching")).Role(ui.RoleStatus)
		} else if len(s.results) == 0 {
			ui.Text(c, bookLabel(c, "NoResults")).Role(ui.RoleStatus)
		}
		ui.List(c.Key("note-search-results"), s.list.list(), len(s.results), func(i int) {
			name := s.results[i]
			quietButton(c.Key(name), filepath.ToSlash(name), fileIcon(name, false)).FillWidth().Height(32).OnClick(func() { n.searchOpen = false; a.openNote(name) })
		}).FillWidth().Height(260)
		quietButton(c.Key("close-search"), bookLabel(c, "Close"), "").OnClick(func() { n.searchOpen = false })
	}).Width(500)
	if !n.searchOpen && s.cancel != nil {
		s.cancel()
	}
}

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/wiki"
	"github.com/egoist/mygo/ui"
)

type notebookSearch struct {
	mode, query, root  string
	results            []string
	err                error
	busy               bool
	generation, active int
	cancel             context.CancelFunc
	list               nativeListState
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
	s.active = 0
	s.list = nativeListState{}
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
		modes := []string{"files", "text"}
		labels := []string{bookLabel(c, "Quick"), bookLabel(c, "SearchText")}
		if a.notes().current != nil {
			modes = append(modes, "backlinks")
			labels = append(labels, bookLabel(c, "Backlinks"))
		}
		if s.mode == "wiki" {
			modes = append(modes, "wiki")
			labels = append(labels, bookLabel(c, "LinkTarget"))
		}
		mode := max(0, slices.Index(modes, s.mode))
		ui.Segmented(c.Key("search-mode"), &mode, labels...).AlignSelf(ui.Start).Label(bookLabel(c, "Search")).OnChange(func() {
			q := s.query
			if modes[mode] == "backlinks" {
				name := filepath.Base(a.notes().current.name)
				q = strings.TrimSuffix(name, filepath.Ext(name))
			}
			a.startNotebookSearch(modes[mode], q)
		})
		hint := "FileName"
		switch s.mode {
		case "text":
			hint = "SearchText"
		case "backlinks", "wiki":
			hint = "LinkTarget"
		}
		query := ui.SearchField(c.Key("note-query"), &s.query).FillWidth().AutoFocus().Label(bookLabel(c, hint)).Placeholder(bookLabel(c, hint)).OnChange(a.runNotebookSearch)
		results := s.results
		openResult := func(name string) {
			n.searchOpen = false
			a.openNote(name)
		}
		if len(results) != 0 {
			// Keep typing in the field while arrows move through matching files.
			move := 0
			if query.Shortcut(0, ui.KeyDown) {
				move = 1
			}
			if query.Shortcut(0, ui.KeyUp) {
				move = -1
			}
			if move != 0 {
				s.active = min(len(results)-1, max(0, s.active+move))
				s.list.list().ScrollIntoView(s.active)
			}
			query.OnSubmit(func() { openResult(results[s.active]) })
		}
		ui.Column(c.Key("result-area")).FillWidth().Height(260).Children(func() {
			switch {
			case s.err != nil && !errors.Is(s.err, context.Canceled):
				showError(c, s.err.Error())
			case s.busy:
				ui.Text(c, bookLabel(c, "Searching")).Role(ui.RoleStatus).TextColor(c.Theme().TextMuted)
			case len(results) == 0:
				msg := "NoResults"
				if s.mode == "text" && strings.TrimSpace(s.query) == "" {
					msg = "EnterText"
				}
				ui.Text(c, bookLabel(c, msg)).Role(ui.RoleStatus).TextColor(c.Theme().TextMuted)
			default:
				ui.List(c.Key("note-search-results"), s.list.list(), len(results), func(i int) {
					name := results[i]
					path := filepath.ToSlash(name)
					row := notebookPathButton(c.Key(name), path, fileIcon(name, false), path).FillWidth().OnClick(func() { openResult(name) })
					if i == s.active {
						row.Background(c.Theme().SurfacePressed)
					}
				}).Fill()
			}
		})
		ui.Button(c.Key("close-search"), bookLabel(c, "Close")).AlignSelf(ui.End).OnClick(func() { n.searchOpen = false })
	}).Width(500)
	if !n.searchOpen && s.cancel != nil {
		s.cancel()
	}
}

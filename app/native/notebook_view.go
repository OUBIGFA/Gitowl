package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	rich "github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/editor"
	arclang "github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type notebookNode struct {
	name   string
	depth  int
	folder bool
}
type notebookProject struct {
	sortByTime, descending bool
	docs                   []*notebookDocument
	current                *notebookDocument
	expanded               map[string]bool
	rows                   []notebookNode
	list                   nativeListState
}
type notebookWorkspace struct {
	projects                                                    map[string]*notebookProject
	projectPicker, searchOpen, outline, quitPrompt, discardQuit bool
	closing                                                     *notebookDocument
	err                                                         error
	width                                                       float32
	search                                                      notebookSearch
}

func (a *app) notes() *notebookProject {
	root := a.repo.Path
	if a.notebook.projects == nil {
		a.notebook.projects = map[string]*notebookProject{}
	}
	p := a.notebook.projects[root]
	if p == nil {
		p = &notebookProject{expanded: map[string]bool{".": true}}
		a.notebook.projects[root] = p
		a.notebook.err = p.load(root)
	}
	return p
}
func (p *notebookProject) load(root string) error {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	var rows []notebookNode
	var visit func(string, int) error
	visit = func(parent string, depth int) error {
		f, err := tree.Open(parent)
		if err != nil {
			return err
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return err
		}
		// Sort siblings before descending so folders always retain their children.
		type entry struct {
			name, folded string
			folder       bool
			modified     time.Time
		}
		children := make([]entry, 0, len(entries))
		for _, item := range entries {
			if strings.EqualFold(item.Name(), ".git") || item.Type()&os.ModeSymlink != 0 {
				continue
			}
			e := entry{name: item.Name(), folded: strings.ToLower(item.Name()), folder: item.IsDir()}
			if p.sortByTime {
				info, err := item.Info()
				if err != nil {
					return err
				}
				e.modified = info.ModTime()
			}
			children = append(children, e)
		}
		slices.SortFunc(children, func(a, b entry) int {
			if a.folder != b.folder {
				if a.folder {
					return -1
				}
				return 1
			}
			if p.descending {
				a, b = b, a
			}
			if order := a.modified.Compare(b.modified); order != 0 {
				return order
			}
			if order := strings.Compare(a.folded, b.folded); order != 0 {
				return order
			}
			return strings.Compare(a.name, b.name)
		})
		for _, e := range children {
			name := filepath.Join(parent, e.name)
			rows = append(rows, notebookNode{name, depth, e.folder})
			if e.folder && p.expanded[name] {
				if err := visit(name, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(".", 0); err != nil {
		return err
	}
	p.rows = rows
	return nil
}

// A failed directory read keeps the previously displayed order and controls.
func (p *notebookProject) changeOrder(root string, byTime, descending bool) error {
	oldTime, oldDescending := p.sortByTime, p.descending
	p.sortByTime, p.descending = byTime, descending
	if err := p.load(root); err != nil {
		p.sortByTime, p.descending = oldTime, oldDescending
		return err
	}
	return nil
}

func (a *app) openNote(name string) {
	p := a.notes()
	for _, d := range p.docs {
		if d.name == name {
			p.current = d
			a.backFromReview()
			a.notebook.err = nil
			return
		}
	}
	d, err := openNotebookDocument(a.repo.Path, name)
	a.notebook.err = err
	if err == nil {
		p.docs = append(p.docs, d)
		p.current = d
		a.backFromReview()
	}
}
func (p *notebookProject) close(d *notebookDocument) {
	if i := slices.Index(p.docs, d); i >= 0 {
		if d.cancelImages != nil {
			d.cancelImages()
		}
		p.docs = slices.Delete(p.docs, i, i+1)
		if p.current == d {
			p.current = nil
			if len(p.docs) > 0 {
				p.current = p.docs[min(i, len(p.docs)-1)]
			}
		}
	}
}
func (a *app) saveNote(d *notebookDocument) bool {
	if d == nil {
		return false
	}
	a.notebook.err = d.save()
	if a.notebook.err != nil {
		return false
	}
	if ref, ok := a.coreReference(); ok && ref.Path == d.root {
		core, ctx := a.core, a.ctx
		a.runCoreTask(func() (backend.KernelResult, error) { return core.RefreshChanges(ctx, ref) }, func(_ backend.KernelResult, err error) {
			if err != nil {
				a.notebook.err = err
			}
		})
	}
	if project := a.notebook.projects[d.root]; project != nil && project.sortByTime {
		a.notebook.err = project.load(d.root)
	}
	return true
}
func bookLabel(c *ui.Context, key string) string {
	return locales.Catalog.Format(c.Locale(), "editor"+key, nil)
}

func (a *app) notebookRail(c *ui.Context, tx locales.Messages, colors workspaceColors) {
	p := a.notes()
	n := &a.notebook
	picker := notebookPathButton(c.Key("project-picker"), a.repo.Name, "chevron-down", a.repo.Path).FillWidth().Height(36).OnClick(func() { n.projectPicker = !n.projectPicker })
	if ref, ok := a.coreReference(); ok {
		picker.PopoverMenu(func(m *ui.Menu) { a.renderRepositoryMenu(m, c.Locale(), ref) })
	}
	if a.coreFocusTarget == "focus-projects" {
		n.projectPicker = true
	}
	if n.projectPicker {
		a.coreProjectList(c, tx, colors)
		return
	}
	ui.Row(c).FillWidth().MinHeight(36).Padding(8, 0, 2, 8).Margin(4, 0, 0, 0).AlignItems(ui.Center).Gap(4).
		BorderWidth(1, 0, 0, 0).BorderColor(colors.hairline).Children(func() {
		ui.Text(c, bookLabel(c, "Files")).Grow(1).FontSize(c.Theme().Rem(13.0 / 15)).FontWeight(500).TextColor(c.Theme().TextMuted)
		notebookIcon(c, "find-files", bookLabel(c, "Search"), "search", func() { a.startNotebookSearch("files", "") })
		notebookIcon(c, "reload-tree", tx.Refresh(), "refresh-cw", func() { n.err = p.load(a.repo.Path) })
	})
	ui.Row(c.Key("tree-sort")).FillWidth().Height(34).Margin(0, 0, 4, 0).AlignItems(ui.Center).Gap(6).Children(func() {
		method := 0
		if p.sortByTime {
			method = 1
		}
		ui.Segmented(c.Key("method"), &method, bookLabel(c, "SortName"), bookLabel(c, "SortTime")).Label(bookLabel(c, "Sort")).OnChange(func() {
			n.err = p.changeOrder(a.repo.Path, method == 1, method == 1)
		})
		ui.Spacer(c)
		label, icon := "Ascending", "arrow-up"
		if p.descending {
			label, icon = "Descending", "arrow-down"
		}
		notebookIcon(c, "reverse", bookLabel(c, label), icon, func() {
			n.err = p.changeOrder(a.repo.Path, p.sortByTime, !p.descending)
		})
	})
	status := map[string]string{}
	if a.status != nil {
		for _, f := range a.status.Files {
			status[filepath.Clean(f.Path)] = f.Status
		}
	}
	rows := p.rows
	p.list.list().KeepOffset = true
	p.list.list().Key = func(i int) any { return rows[i].name }
	ui.List(c.Key("notebook-tree"), p.list.list(), len(p.rows), func(i int) {
		row := p.rows[i]
		label := filepath.Base(row.name)
		icon := fileIcon(label, false)
		if row.folder {
			icon = "chevron-right"
			if p.expanded[row.name] {
				icon = "chevron-down"
			}
		}
		el := ui.ButtonBase(c.Key(row.name)).Justify(ui.Start).AlignItems(ui.Center).Height(32).FillWidth().Padding(0, 8, 0, float32(8+min(7, row.depth)*14)).Gap(6).Label(label).Tooltip(row.name)
		quietSurface(c, el)
		if p.current != nil && p.current.name == row.name {
			el.Background(colors.selected)
		}
		if row.folder {
			el.Expanded(p.expanded[row.name])
		}
		el.Children(func() {
			ui.Icon(c, icons[icon]).FontSize(15).Shrink(0)
			ui.Text(c, label).SingleLine().TruncateMiddle().Grow(1).MinWidth(0)
			if kind := status[row.name]; kind != "" {
				ui.Text(c, fileStatus(tx, kind)).SingleLine().Shrink(0).TextColor(fileStatusColor(c.Theme(), kind)).FontSize(c.Theme().Rem(12.0 / 15))
			}
		}).OnClick(func() {
			if row.folder {
				p.expanded[row.name] = !p.expanded[row.name]
				n.err = p.load(a.repo.Path)
			} else {
				a.openNote(row.name)
			}
		})
	}).Grow(1).MinHeight(0).FillWidth().Label(bookLabel(c, "Files"))
}

// Paths can be wider than their container. Keep both ends readable and
// expose the complete path on hover and to assistive technology.
func notebookPathButton(c *ui.Context, text, icon, fullPath string) ui.Element {
	b := ui.ButtonBase(c).Height(32).Padding(0, 8).Gap(8).Justify(ui.Start).AlignItems(ui.Center).Clip().Label(text).Tooltip(fullPath)
	return quietSurface(c, b).Children(func() {
		ui.Icon(c, icons[icon]).FontSize(16).Shrink(0)
		ui.Text(c, text).MinWidth(0).Shrink(1).SingleLine().TruncateMiddle()
	})
}

func notebookIcon(c *ui.Context, id, label, icon string, action func()) {
	b := ui.ButtonBase(c.Key(id)).Size(32, 32).Padding(8).Center().Label(label).Tooltip(label)
	quietSurface(c, b).Children(func() { ui.Icon(c, icons[icon]).FontSize(16) }).OnClick(action)
}
func (a *app) notebookView(c *ui.Context, tx locales.Messages, colors workspaceColors, width float32) {
	n := &a.notebook
	p := a.notes()
	if n.width == 0 {
		n.width = width * .54
	}
	minimumPane := min(float32(280), width*.41)
	if a.tab == changesTab {
		// Reserve enough room for the native Save split button and one full
		// change row when a larger window is reduced to its supported minimum.
		minimumPane = min(float32(340), width*.55)
	}
	n.width = max(width*.38, min(n.width, width-minimumPane))
	ui.Split(c.Key("notebook-split"), &n.width, func() {
		ui.Column(c.Key("note-pane")).Fill().MinWidth(0).Padding(0, 12, 0, 0).Children(func() {
			ui.Row(c.Key("note-tabs-bar")).Height(40).FillWidth().Padding(0, 0, 8, 0).Margin(0, 0, 10, 0).
				AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors.hairline).Children(func() {
				ui.ScrollHorizontal(c.Key("note-tabs")).Height(32).Grow(1).MinWidth(0).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
						for _, d := range p.docs {
							title := filepath.Base(d.name)
							if d.dirty() {
								title += " *"
							}
							tab := notebookPathButton(c.Key(d.name), title, fileIcon(d.name, false), d.name).MaxWidth(208).
								Role(ui.RoleTab).AccessSelected(p.current == d && !a.reviewingFile)
							if p.current == d && !a.reviewingFile {
								tab.Background(colors.selected).FontWeight(500)
							}
							tab.OnClick(func() { p.current = d; a.backFromReview() })
						}
						if !a.reviewingFile {
							a.workspaceDiffToggle(c, tx)
						}
						if a.reviewingFile {
							reviewTab := quietButton(c.Key("review-tab"), tx.Changes(), "git-branch").Height(32).MinHeight(32).Padding(6, 12).OnClick(a.backFromReview)
							reviewTab.Background(colors.selected).FontWeight(500)
						}
					})
				})
			})
			if n.err != nil {
				msg := n.err.Error()
				for _, e := range []struct {
					value error
					key   string
				}{{errNotebookPath, "Path"}, {errNotebookBinary, "Binary"}, {errNotebookConflict, "Conflict"}, {errNotebookTooLarge, "TooLarge"}, {errNotebookLink, "LinkError"}} {
					if errors.Is(n.err, e.value) {
						msg = bookLabel(c, e.key)
						break
					}
				}
				showError(c, msg)
			}
			d := p.current
			if a.reviewingFile {
				theme := *c.Theme()
				style := theme.ScrollbarStyle.Resolve(&theme)
				theme.ScrollbarStyle.TrackWidth = max(style.TrackWidth, style.ThumbWidth+12)
				ui.Themed(c.Key("notebook-diff"), &theme).Grow(1).MinHeight(0).FillWidth().Children(func() { a.diffPane(c, tx, n.width-12) })
				return
			}
			if d == nil {
				ui.Spacer(c)
				ui.Text(c, bookLabel(c, "Choose")).TextColor(c.Theme().TextMuted).AlignSelf(ui.Center)
				ui.Spacer(c)
				return
			}
			ui.Row(c.Key("note-tools")).Height(36).FillWidth().Padding(0, 2).Margin(0, 0, 8, 0).AlignItems(ui.Center).Gap(4).Children(func() {
				ui.Text(c, filepath.ToSlash(d.name)).FontSize(c.Theme().Rem(13.0 / 15)).SingleLine().TruncateMiddle().Grow(1).MinWidth(0).Tooltip(d.name).TextColor(c.Theme().TextMuted)
				if d.rich != nil && d.rich.OutlineAvailable() {
					toggle := quietButton(c.Key("note-outline"), bookLabel(c, "Outline"), "").Height(30).MinHeight(30).Padding(4, 10).
						Expanded(d.rich.OutlineOpen()).OnClick(d.rich.ToggleOutline)
					if d.rich.OutlineOpen() {
						toggle.Background(colors.selected)
					}
				}
				if d.markdown {
					key := "Preview"
					if d.rich != nil {
						key = "Code"
					}
					quietButton(c.Key("note-mode"), bookLabel(c, key), "").Height(30).MinHeight(30).Padding(4, 10).OnClick(d.switchMode)
				}
				quietButton(c.Key("save-note"), bookLabel(c, "Save"), "").Height(30).MinHeight(30).Padding(4, 10).Disabled(!d.dirty()).OnClick(func() { a.saveNote(d) })
				notebookIcon(c, "close-note", bookLabel(c, "Close"), "x", func() {
					if d.dirty() {
						n.closing = d
					} else {
						p.close(d)
					}
				})
			})
			if d.rich == nil {
				editor.View(c.Key(d.name), &d.code).Grow(1).FillWidth().MinHeight(0)
				return
			}
			arclang.SetLanguage(c.Locale())
			rich.View(c.Key(d.name), d.rich, rich.Options{
				Width: max(90, n.width-12), Theme: c.Theme(), FontSize: 16, LineHeight: 1.55,
				LoadImage: func(src string) rich.ImageResource { return a.notebookImage(d, src, false) },
				RetryImage: func(src string) {
					delete(d.webImages, src)
					a.notebookImage(d, src, true)
					d.rich.InvalidateImage(src)
				},
				Outline: n.outline, OutlineInToolbar: true, SetOutline: func(open bool) { n.outline = open },
				OpenWiki: func(name string) { a.startNotebookSearch("wiki", name) },
				OpenLink: func(url string) { a.openNotebookLink(d, url) },
			}).Grow(1).FillWidth().MinHeight(0)
		})
	}, func() {
		ui.Column(c.Key("review-pane")).Fill().MinWidth(0).Padding(0, 0, 0, 12).Children(func() {
			ui.Row(c.Key("review-tabs")).FillWidth().Height(40).Padding(0, 0, 8, 0).Margin(0, 0, 12, 0).
				AlignItems(ui.Center).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(colors.hairline).Children(func() { a.reviewTabs(c, tx, colors) })
			if a.workspaceStashView(c) {
				return
			}
			if a.tab == historyTab {
				a.historyPage(c, tx)
			} else {
				a.changesOverview(c, tx, colors)
				if a.commitCapable {
					a.commitForm(c, tx)
				}
			}
		})
	}).Fill().MinHeight(0)
	if c.Shortcut(ui.Cmd, ui.KeyS) && p.current != nil {
		a.saveNote(p.current)
	}
	if c.Shortcut(ui.Cmd|ui.Alt, ui.KeyO) {
		a.startNotebookSearch("files", "")
	}
	if c.Shortcut(ui.Cmd|ui.Alt, ui.KeyF) {
		a.startNotebookSearch("text", "")
	}
}
func (a *app) notebookDialogs(c *ui.Context) {
	n := &a.notebook
	if n.searchOpen && a.repo != nil {
		a.notebookSearchDialog(c)
	}
	if n.closing == nil && !n.quitPrompt {
		return
	}
	open := true
	ui.Modal(c.Key("unsaved-note"), &open, func() {
		ui.Text(c, bookLabel(c, "Unsaved")).FontWeight(500)
		if n.closing != nil {
			ui.Text(c, n.closing.name).SingleLine()
		}
		ui.Row(c).Gap(8).Children(func() {
			quietButton(c, bookLabel(c, "Cancel"), "").OnClick(func() { open = false })
			quietButton(c, bookLabel(c, "Discard"), "").OnClick(func() {
				if n.quitPrompt {
					n.discardQuit = true
					open = false
					if a.requestQuit != nil {
						a.requestQuit()
					}
				} else if n.closing != nil {
					n.projects[n.closing.root].close(n.closing)
					open = false
				}
			})
			quietButton(c, bookLabel(c, "Save"), "").OnClick(func() {
				if n.quitPrompt {
					for _, p := range n.projects {
						for _, d := range p.docs {
							if d.dirty() && !a.saveNote(d) {
								open = false
								return
							}
						}
					}
					open = false
					if a.requestQuit != nil {
						a.requestQuit()
					}
				} else if n.closing != nil && a.saveNote(n.closing) {
					n.projects[n.closing.root].close(n.closing)
					open = false
				}
			})
		})
	})
	if !open {
		n.closing = nil
		n.quitPrompt = false
	}
}

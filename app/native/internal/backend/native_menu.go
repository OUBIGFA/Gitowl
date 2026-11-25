package backend

func (n *NativeOwner) menu() KernelMenuDTO {
	item := func(id, shortcut string, enabled bool) KernelMenuItem {
		return KernelMenuItem{ID: id, Type: "normal", LabelKey: "menu." + id, Visible: true, Enabled: enabled, Accelerator: shortcut}
	}
	group := func(id string, items ...KernelMenuItem) KernelMenuItem {
		return KernelMenuItem{ID: id, Type: "submenu", LabelKey: "menu." + id, Visible: true, Enabled: true, Items: items}
	}
	ready := n.state.Selected != nil
	quit := item("quit", "Ctrl+Q", true)
	quit.Role = "quit"
	role := func(id, shortcut string) KernelMenuItem {
		value := item(id, shortcut, true)
		value.Role = id
		return value
	}
	return KernelMenuDTO{Items: []KernelMenuItem{
		group("file", item("new-repository", "Ctrl+N", true), item("add-local-repository", "Ctrl+O", true), item("clone-repository", "Ctrl+Shift+O", true), item("preferences", "Ctrl+,", true), quit),
		group("edit", role("undo", "Ctrl+Z"), role("redo", "Ctrl+Y"), role("cut", "Ctrl+X"), role("copy", "Ctrl+C"), role("paste", "Ctrl+V"), role("selectAll", "Ctrl+A")),
		group("view", item("show-changes", "Ctrl+1", ready), item("show-history", "Ctrl+2", ready), item("show-repository-list", "Ctrl+T", true), item("show-branches-list", "Ctrl+B", ready), item("show-worktrees-list", "", ready), item("show-filter", "Ctrl+F", ready), item("go-to-commit-message", "Ctrl+G", ready)),
		group("repository", item("fetch", "Ctrl+Shift+F", ready), item("pull", "Ctrl+Shift+P", ready), item("push", "Ctrl+P", ready), item("refresh", "Ctrl+R", ready), item("open-working-directory", "Ctrl+Shift+E", ready), item("create-worktree", "", ready), item("stash-all-changes", "", ready), item("show-stashed-changes", "", ready), item("recover-commit", "", ready), item("show-repository-settings", "", ready)),
		group("branch", item("create-branch", "Ctrl+Shift+N", ready), item("merge-branch", "Ctrl+Shift+M", ready)),
	}}
}

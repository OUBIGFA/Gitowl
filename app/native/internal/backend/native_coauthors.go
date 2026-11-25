package backend

import (
	"context"
	"errors"
	"net/mail"
	"slices"
	"strings"
)

// Suggestions come from local Git authors. Adding a literal name/email also
// works offline; identity is never inferred from an unrelated remote account.
func (n *NativeOwner) coAuthors(ctx context.Context, command nativeCommand) error {
	view := n.state.Selected.CommitView
	switch command.Name {
	case "commit.coAuthors.show":
		view.ShowCoAuthors = command.Enabled
	case "commit.coAuthors.remove":
		view.CoAuthors = slices.DeleteFunc(view.CoAuthors, func(a KernelCoAuthor) bool { return a.ID == command.ID })
	case "commit.coAuthors.add":
		index := slices.IndexFunc(view.AuthorHits, func(a KernelAuthorHit) bool { return a.ID == command.ID })
		if index < 0 {
			return errors.New("author selection changed")
		}
		a := view.AuthorHits[index]
		if !slices.ContainsFunc(view.CoAuthors, func(c KernelCoAuthor) bool { return c.ID == a.ID }) {
			view.CoAuthors = append(view.CoAuthors, KernelCoAuthor{ID: a.ID, Kind: "git", Name: a.Name, Email: a.Email})
		}
		view.AuthorQuery = ""
		view.AuthorHits = nil
	case "commit.coAuthors.search":
		view.AuthorQuery = command.Query
		view.AuthorHits = []KernelAuthorHit{}
		if strings.TrimSpace(command.Query) == "" {
			return nil
		}
		seen := map[string]bool{}
		add := func(name, email string) {
			id := name + " <" + email + ">"
			if !seen[id] && !strings.ContainsAny(id, "\r\n\x00") && strings.Contains(strings.ToLower(id), strings.ToLower(command.Query)) {
				seen[id] = true
				view.AuthorHits = append(view.AuthorHits, KernelAuthorHit{ID: id, Username: name, Name: name, Email: email})
			}
		}
		if n.status.Head != "" {
			data, err := n.current.Run(ctx, nil, "log", "--max-count=500", "--format=%an%x00%ae", n.status.Head, "--")
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(data), "\n") {
				name, email, ok := strings.Cut(line, "\x00")
				if ok {
					add(name, email)
				}
			}
		}
		if address, err := mail.ParseAddress(command.Query); err == nil {
			if address.Name == "" {
				address.Name = address.Address
			}
			add(address.Name, address.Address)
		}
	}
	return nil
}

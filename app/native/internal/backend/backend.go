package backend

import (
	"context"
	"encoding/json"
	"fmt"
)

// Repository is a handle owned by the Node service, not a native Git handle.
type Repository struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
	Name string `json:"name"`
}

// File describes the working tree or a commit's changeset, including renames.
type File struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	OldPath     string `json:"oldPath,omitempty"`
	IsSubmodule bool   `json:"isSubmodule"`
}

// Status preserves nullable branch/upstream and the service's operation state.
type Status struct {
	Branch     *string `json:"branch"`
	Upstream   *string `json:"upstream"`
	Ahead      int     `json:"ahead"`
	Behind     int     `json:"behind"`
	Conflicted bool    `json:"conflicted"`
	Operation  *string `json:"operation"`
	Files      []File  `json:"files"`
}

// Commit is the service's real history entry. Dates remain ISO strings at the
// boundary and are formatted in the window's locale when displayed.
type Commit struct {
	SHA         string `json:"sha"`
	ShortSHA    string `json:"shortSha"`
	Summary     string `json:"summary"`
	Body        string `json:"body"`
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	Date        string `json:"date"`
	DateLabels  map[string]struct {
		Text     string `json:"text"`
		Absolute string `json:"absolute"`
	} `json:"dateLabels"`
	Tags []string `json:"tags"`
}

// Changeset counts only changes actually returned by the service.
type Changeset struct {
	Files        []File `json:"files"`
	LinesAdded   int    `json:"linesAdded"`
	LinesDeleted int    `json:"linesDeleted"`
}

// Blob decodes the protocol's base64 contents at the JSON boundary.
type Blob struct {
	MediaID      string `json:"mediaID,omitempty"`
	PreviewError string `json:"previewError,omitempty"`
	Contents     []byte `json:"contents"`
	MediaType    string `json:"mediaType"`
	Bytes        int    `json:"bytes"`
}

// Diff keeps image/binary/submodule cases distinct from unified text patches.
type Diff struct {
	Kind      string          `json:"kind"`
	Text      *string         `json:"text,omitempty"`
	Previous  *Blob           `json:"previous,omitempty"`
	Current   *Blob           `json:"current,omitempty"`
	Submodule json.RawMessage `json:"submodule,omitempty"`
}

// Info is the handshake; an incompatible protocol must not look connected.
type Info struct {
	Protocol     int      `json:"protocol"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// CommitResult is a confirmed local Git commit, never a push result.
type CommitResult struct {
	SHA string `json:"sha"`
}

// Backend is injectable for tests of the production view. Commit is available
// to the UI only after system.info advertises repository.commit; all other
// methods are read-only, with no pretend fetch/push/auth operations.
type Backend interface {
	Info(context.Context) (Info, error)
	Open(context.Context, string) (Repository, error)
	Status(context.Context, int) (Status, error)
	History(context.Context, int, int, int) ([]Commit, error)
	Changeset(context.Context, int, string) (Changeset, error)
	Diff(context.Context, int, string, string) (Diff, error)
	Commit(context.Context, int, []string, string, string) (CommitResult, error)
}

func (c *Client) invalid(method, why string) error {
	err := &ProtocolError{fmt.Sprintf("invalid %s result: %s", method, why)}
	c.fail(err)
	return err
}

func (c *Client) Info(ctx context.Context) (Info, error) {
	var out Info
	err := c.Call(ctx, "system.info", struct{}{}, &out)
	if err == nil && (out.Protocol != 1 || out.Version == "" || out.Capabilities == nil) {
		err = c.invalid("system.info", "expected protocol 1, version and capabilities")
	}
	return out, err
}

func (c *Client) Open(ctx context.Context, path string) (Repository, error) {
	var out Repository
	err := c.Call(ctx, "repository.open", struct {
		Path string `json:"path"`
	}{path}, &out)
	if err == nil && (out.ID <= 0 || out.Path == "" || out.Name == "") {
		err = c.invalid("repository.open", "expected id, path and name")
	}
	return out, err
}

func (c *Client) Status(ctx context.Context, id int) (Status, error) {
	var out Status
	err := c.Call(ctx, "repository.status", struct {
		ID int `json:"id"`
	}{id}, &out)
	if err == nil && (out.Files == nil || out.Ahead < 0 || out.Behind < 0) {
		err = c.invalid("repository.status", "expected files and nonnegative ahead/behind")
	}
	if err == nil {
		err = c.validateFiles("repository.status", out.Files)
	}
	return out, err
}

func (c *Client) History(ctx context.Context, id, skip, limit int) ([]Commit, error) {
	var out []Commit
	err := c.Call(ctx, "repository.history", struct {
		ID    int `json:"id"`
		Skip  int `json:"skip,omitempty"`
		Limit int `json:"limit,omitempty"`
	}{id, skip, limit}, &out)
	if err == nil {
		seen := make(map[string]bool, len(out))
		for _, commit := range out {
			if commit.SHA == "" || commit.ShortSHA == "" || commit.Date == "" || commit.Tags == nil || seen[commit.SHA] {
				err = c.invalid("repository.history", "expected unique commits with sha, shortSha, date and tags")
				break
			}
			seen[commit.SHA] = true
		}
	}
	return out, err
}

func (c *Client) Changeset(ctx context.Context, id int, commit string) (Changeset, error) {
	var out Changeset
	err := c.Call(ctx, "repository.changeset", struct {
		ID     int    `json:"id"`
		Commit string `json:"commit"`
	}{id, commit}, &out)
	if err == nil && (out.Files == nil || out.LinesAdded < 0 || out.LinesDeleted < 0) {
		err = c.invalid("repository.changeset", "expected files and nonnegative line counts")
	}
	if err == nil {
		err = c.validateFiles("repository.changeset", out.Files)
	}
	return out, err
}

func (c *Client) Diff(ctx context.Context, id int, path, commit string) (Diff, error) {
	var out Diff
	err := c.Call(ctx, "repository.diff", struct {
		ID     int    `json:"id"`
		Path   string `json:"path"`
		Commit string `json:"commit,omitempty"`
	}{id, path, commit}, &out)
	if err == nil {
		switch out.Kind {
		case "text", "large-text", "image", "binary", "submodule", "unrenderable":
		default:
			err = c.invalid("repository.diff", "unknown diff kind")
		}
		for _, blob := range []*Blob{out.Previous, out.Current} {
			if err == nil && blob != nil && (blob.Bytes != len(blob.Contents) || blob.MediaType == "") {
				err = c.invalid("repository.diff", "blob byte count or media type is invalid")
			}
		}
	}
	return out, err
}

// Commit sends whole file paths to the upstream-backed staging/hooks service.
// An absent confirmation is an error; callers must not clear drafts or retry
// automatically, because a lost response can leave the outcome unknown.
func (c *Client) Commit(ctx context.Context, id int, paths []string, summary, description string) (CommitResult, error) {
	var out CommitResult
	err := c.Call(ctx, "repository.commit", struct {
		ID          int      `json:"id"`
		Paths       []string `json:"paths"`
		Summary     string   `json:"summary"`
		Description string   `json:"description,omitempty"`
	}{id, paths, summary, description}, &out)
	if err == nil && out.SHA == "" {
		err = c.invalid("repository.commit", "missing confirmed commit sha")
	}
	return out, err
}

func (c *Client) validateFiles(method string, files []File) error {
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if file.Path == "" || file.Status == "" || seen[file.Path] {
			return c.invalid(method, "expected unique files with path and status")
		}
		seen[file.Path] = true
	}
	return nil
}

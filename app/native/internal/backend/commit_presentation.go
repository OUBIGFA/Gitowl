package backend

import "context"

// Metadata of the original commit form. It contains no callbacks, credential
// accounts, rules matcher functions, recovery capabilities or raw errors.
type KernelCommitView struct {
	Mode                 string              `json:"mode"`
	AllowEmpty           bool                `json:"allowEmpty"`
	SignOff              bool                `json:"signOff"`
	SkipHooks            bool                `json:"skipHooks"`
	ShowSkipHooks        bool                `json:"showSkipHooks"`
	Amending             bool                `json:"amending"`
	CanAmend             bool                `json:"canAmend"`
	DefaultSummary       *string             `json:"defaultSummary"`
	CanCommit            bool                `json:"canCommit"`
	RulesPending         bool                `json:"rulesPending"`
	RulesBlocked         bool                `json:"rulesBlocked"`
	SummaryLengthWarning bool                `json:"summaryLengthWarning"`
	Author               *KernelGitAuthor    `json:"author"`
	CoAuthorsAvailable   bool                `json:"coAuthorsAvailable"`
	ShowCoAuthors        bool                `json:"showCoAuthors"`
	CoAuthors            []KernelCoAuthor    `json:"coAuthors"`
	AuthorQuery          string              `json:"authorQuery"`
	AuthorSearching      bool                `json:"authorSearching"`
	AuthorHits           []KernelAuthorHit   `json:"authorHits"`
	AssistedAvailable    bool                `json:"assistedAvailable"`
	PushAfterAssisted    bool                `json:"pushAfterAssisted"`
	Assisted             KernelAssistedState `json:"assisted"`
}

type KernelGitAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
type KernelCoAuthor struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Username *string `json:"username"`
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	State    *string `json:"state"`
}
type KernelAuthorHit struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}
type KernelAssistedState struct {
	Kind       string  `json:"kind"`
	RunID      *string `json:"runID"`
	Busy       bool    `json:"busy"`
	CanCancel  bool    `json:"canCancel"`
	Retry      *string `json:"retry"`
	CanDismiss bool    `json:"canDismiss"`
	Title      string  `json:"title"`
	Index      int     `json:"index"`
	Total      int     `json:"total"`
}

type KernelAssistedReceipt struct {
	Kind    string   `json:"kind"`
	Commits []string `json:"commits"`
	HeadSHA *string  `json:"headSHA"`
}
type KernelCommitOptions struct {
	SkipHooks  *bool `json:"skipCommitHooks,omitempty"`
	SignOff    *bool `json:"signOffCommits,omitempty"`
	AllowEmpty *bool `json:"allowEmptyCommit,omitempty"`
}

type commitPresentationRequest struct {
	Name       string                    `json:"name"`
	Repository KernelRepositoryReference `json:"repository"`
	Enabled    *bool                     `json:"enabled,omitempty"`
	Options    *KernelCommitOptions      `json:"options,omitempty"`
	Mode       string                    `json:"mode,omitempty"`
	Query      *string                   `json:"query,omitempty"`
	ID         string                    `json:"id,omitempty"`
	RunID      string                    `json:"runID,omitempty"`
	Kind       string                    `json:"kind,omitempty"`
}

func (k *Kernel) SetCommitOptions(ctx context.Context, ref KernelRepositoryReference, options KernelCommitOptions) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.options", Repository: ref, Options: &options})
}
func (k *Kernel) AmendCommit(ctx context.Context, ref KernelRepositoryReference, enabled bool) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.amend", Repository: ref, Enabled: &enabled})
}
func (k *Kernel) SetCommitMode(ctx context.Context, ref KernelRepositoryReference, mode string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.mode", Repository: ref, Mode: mode})
}
func (k *Kernel) ShowCoAuthors(ctx context.Context, ref KernelRepositoryReference, show bool) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.coAuthors.show", Repository: ref, Enabled: &show})
}
func (k *Kernel) SearchCoAuthors(ctx context.Context, ref KernelRepositoryReference, query string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.coAuthors.search", Repository: ref, Query: &query})
}
func (k *Kernel) AddCoAuthor(ctx context.Context, ref KernelRepositoryReference, id string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.coAuthors.add", Repository: ref, ID: id})
}
func (k *Kernel) RemoveCoAuthor(ctx context.Context, ref KernelRepositoryReference, id string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.coAuthors.remove", Repository: ref, ID: id})
}
func (k *Kernel) PushAfterAssistedCommit(ctx context.Context, ref KernelRepositoryReference, enabled bool) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.assisted.pushAfter", Repository: ref, Enabled: &enabled})
}
func (k *Kernel) CreateAssistedCommits(ctx context.Context, ref KernelRepositoryReference) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.assisted.create", Repository: ref})
}
func (k *Kernel) CancelAssistedCommits(ctx context.Context, ref KernelRepositoryReference, runID string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.assisted.cancel", Repository: ref, RunID: runID})
}
func (k *Kernel) RetryAssistedCommits(ctx context.Context, ref KernelRepositoryReference, runID, kind string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.assisted.retry", Repository: ref, RunID: runID, Kind: kind})
}
func (k *Kernel) DismissAssistedCommitError(ctx context.Context, ref KernelRepositoryReference, runID string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.assisted.dismiss", Repository: ref, RunID: runID})
}
func (k *Kernel) InspectAssistedCommitError(ctx context.Context, ref KernelRepositoryReference, runID string) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.assisted.details", Repository: ref, RunID: runID})
}
func (k *Kernel) ShowCommitProgress(ctx context.Context, ref KernelRepositoryReference) (KernelResult, error) {
	return k.command(ctx, commitPresentationRequest{Name: "commit.progress", Repository: ref})
}

package backend

import "context"

// Menu identity belongs to the clicked project, which need not be selected.
type KernelRepositoryMenu struct {
	Repository KernelRepositoryReference `json:"repository"`
	Lease      string                    `json:"lease"`
	Items      []KernelWorkspaceMenuItem `json:"items"`
}

func (k *Kernel) ExecuteRepositoryMenu(ctx context.Context, repository KernelRepositoryReference, lease string, path []int) (KernelResult, error) {
	return k.command(ctx, map[string]any{"name": "repository.menu", "repository": repository, "lease": lease, "path": path})
}

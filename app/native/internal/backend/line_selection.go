package backend

import "context"

type KernelDiffSelection struct {
	Identity       string                 `json:"identity"`
	FileID         string                 `json:"fileID"`
	ShowCheckMarks bool                   `json:"showCheckMarks"`
	Lines          []KernelSelectableLine `json:"lines"`
}

type KernelSelectableLine struct {
	ID            int    `json:"id"`
	Kind          string `json:"kind"`
	OldBufferLine *int   `json:"oldBufferLine"`
	NewBufferLine *int   `json:"newBufferLine"`
	Selected      bool   `json:"selected"`
}

// Identity belongs to the original patch. A stale native range cannot select a
// different revision; the kernel verifies all IDs against its own DiffSelection.
func (k *Kernel) IncludeLines(ctx context.Context, repository KernelRepositoryReference, fileID, identity string, ids []int, included bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		FileID     string                    `json:"fileID"`
		Identity   string                    `json:"identity"`
		LineIDs    []int                     `json:"lineIDs"`
		Included   bool                      `json:"included"`
	}{"repository.lines.include", repository, fileID, identity, ids, included})
}

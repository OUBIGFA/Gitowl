package backend

import "context"

// Missing-repository facts come from Desktop's original Git path inspection.
// Trust's path is deliberately not a writable native command parameter.
type KernelMissingRepository struct {
	Repository KernelRepository `json:"repository"`
	Checking   bool             `json:"checking"`
	Busy       bool             `json:"busy"`
	UnsafePath *string          `json:"unsafePath"`
	CanClone   bool             `json:"canClone"`
	Error      *string          `json:"error"`
}

func (k *Kernel) RecoverMissingRepository(ctx context.Context, repository KernelRepositoryReference, action string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		Action     string                    `json:"action"`
	}{"repository.missing", repository, action})
}

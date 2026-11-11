package backend

import "context"

// KernelMenuItem carries only original menu presentation data, never closures
// or repository/account state. The kernel revalidates ID clicks against its
// current menu and original AppStore policy.
type KernelMenuItem struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	LabelKey    string           `json:"labelKey,omitempty"`
	LabelData   string           `json:"labelData,omitempty"`
	Enabled     bool             `json:"enabled"`
	Visible     bool             `json:"visible"`
	Checked     bool             `json:"checked,omitempty"`
	Accelerator string           `json:"accelerator,omitempty"`
	AccessKey   string           `json:"accessKey,omitempty"`
	Role        string           `json:"role,omitempty"`
	Items       []KernelMenuItem `json:"items,omitempty"`
}

type KernelMenuDTO struct {
	Items []KernelMenuItem `json:"items"`
}

func (k *Kernel) AcknowledgeRepositoryListFocus(ctx context.Context) (KernelResult, error) {
	return k.command(ctx, struct { Name string `json:"name"` }{"presentation.repository-list-focused"})
}

func (k *Kernel) ReportResizableFocus(ctx context.Context, active bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}{"presentation.resizable-focus", active})
}

func (k *Kernel) ExecuteMenu(ctx context.Context, id string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}{"menu.execute", id})
}

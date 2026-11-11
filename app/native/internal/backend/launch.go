package backend

import "context"

func (k *Kernel) OpenFromCLI(ctx context.Context, path string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name   string `json:"name"`
		Action struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		} `json:"action"`
	}{
		Name: "launch.cli", Action: struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		}{Kind: "open-repository", Path: path},
	})
}

func (k *Kernel) CloneFromCLI(ctx context.Context, url, branch string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name   string `json:"name"`
		Action struct {
			Kind   string `json:"kind"`
			URL    string `json:"url"`
			Branch string `json:"branch,omitempty"`
		} `json:"action"`
	}{
		Name: "launch.cli", Action: struct {
			Kind   string `json:"kind"`
			URL    string `json:"url"`
			Branch string `json:"branch,omitempty"`
		}{Kind: "clone-url", URL: url, Branch: branch},
	})
}

func (k *Kernel) DispatchApplicationURL(ctx context.Context, url string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}{"launch.url", url})
}

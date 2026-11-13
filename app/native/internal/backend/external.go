package backend

import "context"

// External URL opening belongs to the visible native host. OAuth state, URL
// creation and callback processing still belong to the original SignInStore.
type KernelExternalRequest struct {
	ID  int    `json:"id"`
	URL string `json:"url"`
}

// A browser reply cannot queue behind an operation waiting for that browser.
func (k *Kernel) ReplyExternal(ctx context.Context, id int, opened bool) error {
	var out struct {
		Received bool `json:"received"`
	}
	if err := k.client.Call(ctx, "kernel.externalResult", struct {
		ID     int  `json:"id"`
		Opened bool `json:"opened"`
	}{id, opened}, &out); err != nil {
		return err
	}
	if !out.Received {
		return k.client.invalid("kernel.externalResult", "browser result was not acknowledged")
	}
	return nil
}

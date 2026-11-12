package backend

import "context"

// File dialogs are presentation requests. Git validation and all follow-up
// operations remain with the original caller after it receives the selection.
type KernelFileDialog struct {
	ID      int    `json:"id"`
	Kind    string `json:"kind"`
	Options struct {
		Title          string   `json:"title"`
		DefaultPath    string   `json:"defaultPath"`
		ButtonLabel    string   `json:"buttonLabel"`
		Message        string   `json:"message"`
		NameFieldLabel string   `json:"nameFieldLabel"`
		Properties     []string `json:"properties"`
		Filters        []struct {
			Name       string   `json:"name"`
			Extensions []string `json:"extensions"`
		} `json:"filters"`
	} `json:"options"`
}

// ReplyFileDialog must not enter the UI's ordered command queue: a command in
// that queue may be waiting for this very picker to finish.
func (k *Kernel) ReplyFileDialog(ctx context.Context, id int, paths []string, dialogErr error) error {
	if paths == nil {
		paths = []string{}
	}
	var message *string
	if dialogErr != nil {
		text := dialogErr.Error()
		message = &text
	}
	var out struct {
		Received bool `json:"received"`
	}
	if err := k.client.Call(ctx, "kernel.dialogResult", struct {
		ID    int      `json:"id"`
		Paths []string `json:"paths"`
		Error *string  `json:"error"`
	}{id, paths, message}, &out); err != nil {
		return err
	}
	if !out.Received {
		return k.client.invalid("kernel.dialogResult", "dialog result was not acknowledged")
	}
	return nil
}

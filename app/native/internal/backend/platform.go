package backend

import (
	"context"
	"encoding/json"
)

// Platform requests are issued by the owned business process; the visible
// MyGo parent executes them and returns the real result on a separate lane.
type KernelPlatformRequest struct {
	ID        int               `json:"id"`
	Operation string            `json:"operation"`
	Args      []json.RawMessage `json:"args"`
}

func (k *Kernel) ReplyPlatform(ctx context.Context, id int, value any, operationErr error) error {
	if k.native!=nil{return k.native.answer(id,value,operationErr)}
	var message *string
	if operationErr != nil {
		text := operationErr.Error()
		message = &text
	}
	var result struct {
		Received bool `json:"received"`
	}
	if err := k.client.Call(ctx, "kernel.platformResult", struct {
		ID    int     `json:"id"`
		Value any     `json:"value"`
		Error *string `json:"error"`
	}{id, value, message}, &result); err != nil {
		return err
	}
	if !result.Received {
		return k.client.invalid("kernel.platformResult", "platform result was not acknowledged")
	}
	return nil
}

func (k *Kernel) PlatformEvent(ctx context.Context, name string, args ...any) error {
	if k.native!=nil{
		if name=="copilot-runtime-cancel"{k.native.cancelMu.Lock();if k.native.generationCancel!=nil{k.native.generationCancel()};k.native.cancelMu.Unlock()}
		return nil
	}
	var result struct {
		Received bool `json:"received"`
	}
	if args == nil {
		args = []any{}
	}
	if err := k.client.Call(ctx, "kernel.platformEvent", struct {
		Name string `json:"name"`
		Args []any  `json:"args"`
	}{name, args}, &result); err != nil {
		return err
	}
	if !result.Received {
		return k.client.invalid("kernel.platformEvent", "platform event was not acknowledged")
	}
	return nil
}

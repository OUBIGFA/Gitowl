package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type nativeAnswer struct {
	value any
	err   error
}

func (n *NativeOwner) request(ctx context.Context, event func(int) KernelEvent) (any, error) {
	n.interactionMu.Lock()
	n.interactionID++
	id := n.interactionID
	done := make(chan nativeAnswer, 1)
	n.answers[id] = done
	n.interactionMu.Unlock()
	defer func() { n.interactionMu.Lock(); delete(n.answers, id); n.interactionMu.Unlock() }()
	if n.onEvent == nil {
		return nil, errors.New("native interaction host is unavailable")
	}
	n.onEvent(event(id))
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-n.ctx.Done():
		return nil, n.ctx.Err()
	case answer := <-done:
		return answer.value, answer.err
	}
}
func (n *NativeOwner) answer(id int, value any, err error) error {
	n.interactionMu.Lock()
	defer n.interactionMu.Unlock()
	done := n.answers[id]
	if done == nil {
		return errors.New("interaction expired")
	}
	select {
	case done <- nativeAnswer{value, err}:
		return nil
	default:
		return errors.New("interaction already answered")
	}
}
func (n *NativeOwner) platform(ctx context.Context, operation string, args ...any) (any, error) {
	values := []json.RawMessage{}
	for _, arg := range args {
		data, err := json.Marshal(arg)
		if err != nil {
			return nil, err
		}
		values = append(values, data)
	}
	return n.request(ctx, func(id int) KernelEvent {
		return KernelEvent{Platform: &KernelPlatformRequest{ID: id, Operation: operation, Args: values}}
	})
}
func (n *NativeOwner) chooseDirectory(ctx context.Context) ([]string, error) {
	result, err := n.request(ctx, func(id int) KernelEvent {
		request := &KernelFileDialog{ID: id, Kind: "open"}
		request.Options.Properties = []string{"openDirectory"}
		return KernelEvent{Dialog: request}
	})
	if err != nil {
		return nil, err
	}
	paths, ok := result.([]string)
	if !ok {
		return nil, fmt.Errorf("invalid directory picker response")
	}
	return paths, nil
}
func (n *NativeOwner) openExternal(ctx context.Context, url string) error {
	result, err := n.request(ctx, func(id int) KernelEvent { return KernelEvent{External: &KernelExternalRequest{ID: id, URL: url}} })
	if err != nil {
		return err
	}
	if opened, ok := result.(bool); !ok || !opened {
		return errors.New("system browser could not open the address")
	}
	return nil
}

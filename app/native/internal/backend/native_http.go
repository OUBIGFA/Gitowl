package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Fixed diagnostics never include server response bodies, authorization headers
// or prompts. Redirects are rejected by the shared system-proxy HTTP client.
func nativeJSON(ctx context.Context, client *http.Client, method, address string, body any, headers http.Header, out any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, input)
	if err != nil {
		return errors.New("invalid service address")
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Gitowl/0.1.0")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("service connection failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("service request failed: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return errors.New("service response could not be read")
	}
	if len(data) > 8<<20 {
		return errors.New("service response exceeded the limit")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("service response was not valid JSON")
	}
	return nil
}

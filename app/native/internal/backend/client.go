// Package backend owns the newline JSON-RPC boundary to the Node service.
// It opens no sockets and contains no repository or staging implementation.
package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	ErrClosed       = errors.New("repository client closed")
	ErrDisconnected = errors.New("repository service disconnected")
	ErrBusy         = errors.New("original Desktop actions are still pending")
)

// RPCError is an application error returned by the service, not a broken pipe.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("%s (RPC %d)", e.Message, e.Code) }

// ProtocolError stops the connection: continuing after corrupt framing could
// associate repository data with the wrong request.
type ProtocolError struct{ Message string }

func (e *ProtocolError) Error() string { return "repository service protocol: " + e.Message }

type reply struct {
	result json.RawMessage
	err    error
}

type pendingCall struct {
	media   bool
	replies chan reply
	sent    bool
}

type request struct {
	id   uint64
	data []byte
}

// Client multiplexes positive integer request IDs over one pair of owned pipes.
// Call is safe concurrently. Close releases every pending call and both pipes.
type Client struct {
	reader io.ReadCloser
	writer io.WriteCloser
	mu     sync.Mutex
	nextID uint64
	calls  map[uint64]*pendingCall
	// Only sent, canceled requests may legitimately receive an unused reply.
	canceled map[uint64]bool
	err      error
	done     chan struct{}
	readDone chan struct{}
	outgoing chan request
	close    sync.Once
	closeErr error
	// A child process supplies its exit status after stdout reaches EOF.
	onEOF func() error
	// Native kernel notifications share framing with responses. The handler
	// must only schedule presentation; it must not wait for another RPC here.
	onNotification func(string, json.RawMessage) error
	cancelRequests bool
	stopAdmission  bool
}

// NewClient starts the real transport on pipes supplied by a caller or a test.
func NewClient(reader io.ReadCloser, writer io.WriteCloser) *Client {
	return newClient(reader, writer, nil)
}

func newClient(reader io.ReadCloser, writer io.WriteCloser, onEOF func() error) *Client {
	return newRPCClient(reader, writer, onEOF, nil, true)
}

func newRPCClient(reader io.ReadCloser, writer io.WriteCloser, onEOF func() error, notify func(string, json.RawMessage) error, cancelRequests bool) *Client {
	c := &Client{
		reader: reader, writer: writer, calls: make(map[uint64]*pendingCall),
		canceled: make(map[uint64]bool), done: make(chan struct{}),
		readDone: make(chan struct{}), outgoing: make(chan request), onEOF: onEOF,
		onNotification: notify, cancelRequests: cancelRequests,
	}
	go c.readReplies()
	go c.writeRequests()
	return c
}

// Done closes on local close, process exit, or a terminal protocol/pipe failure.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns the terminal error, or nil while the connection is usable.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// InFlight includes sent calls whose local wait was canceled: their original
// kernel actions have not necessarily finished, and must still guard shutdown.
func (c *Client) InFlight() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls) + len(c.canceled)
}

// CriticalInFlight excludes only explicitly read-only image-frame requests,
// including their canceled waiters. Every other original action still protects
// shutdown until its real response arrives.
func (c *Client) CriticalInFlight() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, pending := range c.calls {
		if !pending.media {
			count++
		}
	}
	for _, media := range c.canceled {
		if !media {
			count++
		}
	}
	return count
}

func (c *Client) beginShutdown() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	if len(c.calls) != 0 || len(c.canceled) != 0 {
		return ErrBusy
	}
	c.stopAdmission = true
	return nil
}

// Call waits for the matching result without blocking the UI thread. A canceled
// call leaves the shared transport usable; its eventual reply is deliberately
// discarded, unlike an unsolicited or duplicate response.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	return c.call(ctx, method, params, result, false, false)
}

func (c *Client) call(ctx context.Context, method string, params, result any, shutdown, media bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if params == nil {
		params = struct{}{}
	}
	p := &pendingCall{replies: make(chan reply, 1), media: media}
	c.mu.Lock()
	if c.stopAdmission && !shutdown {
		c.mu.Unlock()
		return ErrClosed
	}
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := c.nextID
	c.calls[id] = p
	c.mu.Unlock()
	data, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", id, method, params})
	if err != nil {
		c.cancel(id)
		return fmt.Errorf("encode %s: %w", method, err)
	}
	select {
	case c.outgoing <- request{id, append(data, '\n')}:
	case <-ctx.Done():
		c.cancel(id)
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
	var r reply
	select {
	case r = <-p.replies:
	case <-ctx.Done():
		c.cancel(id)
		return ctx.Err()
	}
	// fail delivers to every pending call, so a result immediately preceding
	// EOF is never lost to a competing Done notification.
	if r.err != nil {
		return r.err
	}
	if err := json.Unmarshal(r.result, result); err != nil {
		err = &ProtocolError{fmt.Sprintf("invalid %s result: %v", method, err)}
		c.fail(err)
		return err
	}
	return nil
}

func (c *Client) cancel(id uint64) {
	c.mu.Lock()
	sent := false
	if p := c.calls[id]; p != nil {
		sent = p.sent
		if sent {
			c.canceled[id] = p.media
		}
		delete(c.calls, id)
	}
	c.mu.Unlock()
	if sent && c.cancelRequests {
		// Research services support a cancellation notification. The original
		// Desktop kernel does not: canceling a waiter must not fabricate a Git
		// cancellation or choose a recovery-dialog result.
		// Queue it through the same writer so JSON lines cannot interleave.
		data := []byte(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"method\":\"$/cancelRequest\",\"params\":{\"id\":%d}}\n", id))
		go func() {
			select {
			case c.outgoing <- request{data: data}:
			case <-c.done:
			}
		}()
	}
}

func (c *Client) writeRequests() {
	for {
		select {
		case <-c.done:
			return
		case r := <-c.outgoing:
			if r.id != 0 { // id zero denotes a cancellation notification.
				c.mu.Lock()
				p := c.calls[r.id]
				if p != nil {
					p.sent = true
				}
				c.mu.Unlock()
				if p == nil {
					continue // canceled before the writer took the request
				}
			}
			n, err := c.writer.Write(r.data)
			if err == nil && n != len(r.data) {
				err = io.ErrShortWrite
			}
			if err != nil {
				c.fail(fmt.Errorf("write repository request: %w", err))
				return
			}
		}
	}
}

func (c *Client) readReplies() {
	defer close(c.readDone)
	scanner := bufio.NewScanner(c.reader)
	// Real patches and base64 images exceed Scanner's default 64 KiB limit.
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		if c.onNotification != nil && len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue // Electron native startup may emit an empty stdout line.
		}
		if err := c.receive(scanner.Bytes()); err != nil {
			c.fail(err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		c.fail(fmt.Errorf("read repository response: %w", err))
	} else if c.onEOF != nil {
		c.fail(c.onEOF())
	} else {
		c.fail(fmt.Errorf("%w: %w", ErrDisconnected, io.EOF))
	}
}

func (c *Client) receive(line []byte) error {
	var message struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	bad := func(why string) error { return &ProtocolError{why} }
	if err := json.Unmarshal(line, &message); err != nil {
		return bad("invalid JSON: " + err.Error())
	}
	if message.Method != "" {
		if message.JSONRPC != "2.0" || len(message.ID) != 0 || len(message.Result) != 0 || len(message.Error) != 0 || len(message.Params) == 0 || c.onNotification == nil {
			return bad("invalid or unsupported notification")
		}
		return c.onNotification(message.Method, message.Params)
	}
	var id uint64
	if message.JSONRPC != "2.0" || json.Unmarshal(message.ID, &id) != nil || id == 0 {
		return bad("expected JSON-RPC 2.0 and a positive integer id")
	}
	hasResult, hasError := len(message.Result) != 0, len(message.Error) != 0
	if hasResult == hasError || hasResult && bytes.Equal(bytes.TrimSpace(message.Result), []byte("null")) {
		return bad("expected exactly one non-null result or error")
	}
	r := reply{result: message.Result}
	if hasError {
		var e struct {
			Code    *int    `json:"code"`
			Message *string `json:"message"`
		}
		if err := json.Unmarshal(message.Error, &e); err != nil || e.Code == nil || e.Message == nil {
			return bad("invalid error object")
		}
		r.err = &RPCError{Code: *e.Code, Message: *e.Message}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p := c.calls[id]; p != nil {
		delete(c.calls, id)
		p.replies <- r
		return nil
	}
	if _, ok := c.canceled[id]; ok {
		delete(c.canceled, id)
		return nil
	}
	return bad(fmt.Sprintf("unsolicited or duplicate response id %d", id))
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
		for _, p := range c.calls {
			p.replies <- reply{err: err}
		}
		clear(c.calls)
		clear(c.canceled)
		close(c.done)
	}
	c.mu.Unlock()
	c.closeTransport()
}

func (c *Client) closeTransport() error {
	c.close.Do(func() {
		for _, pipe := range []io.Closer{c.writer, c.reader} {
			if err := pipe.Close(); err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
				c.closeErr = errors.Join(c.closeErr, err)
			}
		}
	})
	return c.closeErr
}

// Close is idempotent and reports pipe cleanup errors instead of hiding them.
func (c *Client) Close() error {
	c.fail(ErrClosed)
	return c.closeTransport()
}

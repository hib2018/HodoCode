package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// Commands act as wire-order barriers: their response follows all requested
// notifications, and create responses are checked before replying to the test.
func serveProgress(read func() message, send func(any)) {
	for {
		command := read()
		switch command.Method {
		case "test/create", "test/create-workspace", "test/bad-create":
			method := "window/workDoneProgress/create"
			if command.Method == "test/create-workspace" {
				method = "workspace/workDoneProgress/create"
			}
			send(map[string]any{"jsonrpc": "2.0", "id": "create", "method": method, "params": command.Params})
			response := read()
			if string(response.ID) != `"create"` || (command.Method == "test/bad-create") != (response.Error != nil) {
				os.Exit(21)
			}
			if response.Error != nil && response.Error.Code != -32602 {
				os.Exit(22)
			}
		case "test/progress":
			send(map[string]any{"jsonrpc": "2.0", "method": "$/progress", "params": command.Params})
		}
		send(map[string]any{"jsonrpc": "2.0", "id": command.ID, "result": nil})
	}
}

func progressCommand(c *Client, ctx context.Context, method string, params any) error {
	var result any
	return c.call(ctx, "test/"+method, params, &result)
}
func progressEvent(c *Client, ctx context.Context, token any, kind string) error {
	return progressCommand(c, ctx, "progress", map[string]any{"token": token, "value": map[string]any{"kind": kind, "title": "Loading"}})
}
func TestWaitReadyProgress(t *testing.T) {
	c, ctx := startServer(t, "progress-control")
	if err := progressCommand(c, ctx, "create", map[string]any{"token": "one"}); err != nil {
		t.Fatal(err)
	}
	if err := progressCommand(c, ctx, "create-workspace", map[string]any{"token": 2}); err != nil {
		t.Fatal(err)
	}
	if err := progressEvent(c, ctx, "one", "begin"); err != nil {
		t.Fatal(err)
	}
	if err := progressEvent(c, ctx, 2, "begin"); err != nil {
		t.Fatal(err)
	}
	if err := progressEvent(c, ctx, "one", "report"); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		ready, err := c.WaitReady(ctx, 0)
		if err == nil && !ready {
			err = errors.New("not ready")
		}
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("returned before end: %v", err)
	default:
	}
	if err := progressEvent(c, ctx, "one", "end"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		t.Fatalf("returned with active second token: %v", err)
	default:
	}
	if err := progressEvent(c, ctx, 2, "end"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Completion history survives token deletion and is immediately discoverable.
	ready, err := c.WaitReady(ctx, 0)
	if err != nil || !ready {
		t.Fatalf("completed: %v %v", ready, err)
	}
}
func TestWaitReadyLateRegistration(t *testing.T) {
	c, ctx := startServer(t, "progress-control")
	started := make(chan struct{})
	result := make(chan bool, 1)
	go func() { close(started); ready, err := c.WaitReady(ctx, 3*time.Second); result <- ready && err == nil }()
	<-started
	if err := progressCommand(c, ctx, "create", map[string]any{"token": "late"}); err != nil {
		t.Fatal(err)
	}
	if err := progressEvent(c, ctx, "late", "end"); err != nil {
		t.Fatal(err)
	}
	select {
	case ready := <-result:
		if !ready {
			t.Fatal("late progress missed")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestWaitReadyNoProgress(t *testing.T) {
	c, ctx := startServer(t, "progress-control")
	before := time.Now()
	ready, err := c.WaitReady(ctx, 20*time.Millisecond)
	if err != nil || ready {
		t.Fatalf("got %v %v", ready, err)
	}
	if time.Since(before) < 20*time.Millisecond {
		t.Fatal("returned before discovery grace")
	}
	// Timing out discovery must not terminate the client.
	if err := progressCommand(c, ctx, "barrier", nil); err != nil {
		t.Fatal(err)
	}
}
func TestWaitReadyCancellationAndClose(t *testing.T) {
	for _, mode := range []string{"cancel-discovery", "cancel-active", "close"} {
		t.Run(mode, func(t *testing.T) {
			c, serverCtx := startServer(t, "progress-control")
			if mode != "cancel-discovery" {
				if err := progressCommand(c, serverCtx, "create", map[string]any{"token": "pending"}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(serverCtx)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := c.WaitReady(ctx, time.Hour); result <- err }()
			if mode == "close" {
				c.Close()
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("wait succeeded after cancellation/close")
				}
				if mode != "close" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-serverCtx.Done():
				t.Fatal("wait did not stop")
			}
			select {
			case <-c.done:
			default:
				t.Fatal("client not terminated")
			}
		})
	}
}
func TestProgressValidation(t *testing.T) {
	for _, params := range []any{nil, map[string]any{}, map[string]any{"token": true}, map[string]any{"token": 1.5}} {
		t.Run("create", func(t *testing.T) {
			c, ctx := startServer(t, "progress-control")
			if err := progressCommand(c, ctx, "bad-create", params); err != nil {
				t.Fatal(err)
			}
			ready, err := c.WaitReady(ctx, 0)
			if ready || err != nil {
				t.Fatalf("invalid create registered: %v %v", ready, err)
			}
		})
	}
	for _, raw := range []string{`null`, `{}`, `{"token":null,"value":{"kind":"end"}}`, `{"token":[],"value":{"kind":"end"}}`, `{"token":"x","value":null}`, `{"token":"x","value":{"kind":"bad"}}`, `{"token":"x","value":{"kind":"begin"}}`, `{"token":"x","value":{"kind":"report","percentage":101}}`, `{"token":"x","value":{"kind":"end","message":3}}`} {
		t.Run("notification", func(t *testing.T) {
			c, ctx := startServer(t, "progress-control")
			if err := progressCommand(c, ctx, "progress", json.RawMessage(raw)); err == nil {
				t.Fatal("malformed notification accepted")
			}
			if _, err := c.WaitReady(ctx, time.Second); err == nil {
				t.Fatal("failed client reported ready")
			}
		})
	}
}
func TestWaitReadyConcurrentWaiters(t *testing.T) {
	c, ctx := startServer(t, "progress-control")
	if err := progressCommand(c, ctx, "create", map[string]any{"token": "shared"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready, err := c.WaitReady(ctx, 0)
			if !ready || err != nil {
				t.Errorf("%v %v", ready, err)
			}
		}()
	}
	if err := progressEvent(c, ctx, "shared", "end"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBlockedWriteCancellation(t *testing.T) {
	c, _ := startServer(t, "blocked-write")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := c.Open(ctx, "a.go", "go", []byte(strings.Repeat("x", 1<<20)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline exceeded", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.readDone:
	default:
		t.Fatal("reader did not stop")
	}
	select {
	case <-c.reaped:
	default:
		t.Fatal("child did not stop")
	}
}

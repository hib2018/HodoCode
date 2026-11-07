package explain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FX invokes fx directly (no shell) in a fresh empty temporary directory, removed
// on return. Instructions are advisory: fx and its configuration are trusted,
// not sandboxed. Diagnostics are retained up to 128 KiB per output stream.
func FX(ctx context.Context, prompt string) (answer string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := checkSize(prompt); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "hodocode-explain-")
	if err != nil {
		return "", fmt.Errorf("fx temporary directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			if err == nil {
				err = fmt.Errorf("fx cleanup: %w", cleanupErr)
			} else {
				err = fmt.Errorf("%w; fx cleanup: %v", err, cleanupErr)
			}
		}
	}()
	cmd := exec.CommandContext(ctx, "fx", "ask", "--no-save", "--no-color")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr limitedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if runErr := cmd.Run(); runErr != nil {
		if ctx.Err() != nil {
			runErr = fmt.Errorf("%w: %v", ctx.Err(), runErr)
		}
		return "", fmt.Errorf("fx ask: %w\nstdout: %s\nstderr: %s", runErr, stdout.String(), stderr.String())
	}
	if stdout.overflow || stderr.overflow {
		return "", fmt.Errorf("fx output exceeds %d bytes\nstderr: %s", maxPrompt, stderr.String())
	}
	return stdout.String(), nil
}

type limitedOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maxPrompt - b.buffer.Len()
	if n > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func (b *limitedOutput) String() string {
	if b.overflow {
		return b.buffer.String() + "\n[output truncated]"
	}
	return b.buffer.String()
}

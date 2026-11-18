package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// WaitReady discovers work-done progress for at most discoveryWait. With no
// observed progress it returns false, nil; otherwise it waits until all created
// or started tokens have ended. Previously completed progress counts as ready.
// Once progress is observed only ctx bounds the wait. Cancellation closes Client.
func (c *Client) WaitReady(ctx context.Context, discoveryWait time.Duration) (bool, error) {
	timer := time.NewTimer(discoveryWait)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			c.fail(err)
			return false, err
		}
		c.mu.Lock()
		err := c.err
		seen, idle, changed := c.progressSeen, len(c.progress) == 0, c.progressChanged
		c.mu.Unlock()
		if err != nil {
			return false, err
		}
		if seen && idle {
			return true, nil
		}
		var discovery <-chan time.Time
		if !seen {
			discovery = timer.C
		}
		select {
		case <-ctx.Done():
			c.fail(ctx.Err())
			return false, ctx.Err()
		case <-c.done:
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			c.mu.Lock()
			err = c.err
			c.mu.Unlock()
			return false, err
		case <-changed:
		case <-discovery:
			// Recheck under the same lock as registration to avoid losing progress
			// that arrived concurrently with the discovery deadline.
			c.mu.Lock()
			seen = c.progressSeen
			err = c.err
			c.mu.Unlock()
			if err != nil {
				return false, err
			}
			if !seen {
				return false, nil
			}
		}
	}
}

func progressToken(raw json.RawMessage) (string, error) {
	var text string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", err
		}
		return "s:" + text, nil
	}
	var number int64
	if len(raw) == 0 || string(raw) == "null" {
		return "", errors.New("missing progress token")
	}
	if err := json.Unmarshal(raw, &number); err != nil {
		return "", fmt.Errorf("invalid progress token: %w", err)
	}
	return "n:" + strconv.FormatInt(number, 10), nil
}

func (c *Client) createProgress(raw json.RawMessage) error {
	var params struct {
		Token json.RawMessage `json:"token"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	token, err := progressToken(params.Token)
	if err != nil {
		return err
	}
	return c.updateProgress(token, false)
}

func (c *Client) handleProgress(raw json.RawMessage) error {
	var params struct {
		Token json.RawMessage `json:"token"`
		Value struct {
			Kind        string  `json:"kind"`
			Title       *string `json:"title"`
			Message     *string `json:"message"`
			Cancellable *bool   `json:"cancellable"`
			Percentage  *int    `json:"percentage"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	token, err := progressToken(params.Token)
	if err != nil {
		return err
	}
	if params.Value.Percentage != nil && (*params.Value.Percentage < 0 || *params.Value.Percentage > 100) {
		return errors.New("invalid progress percentage")
	}
	switch params.Value.Kind {
	case "begin":
		if params.Value.Title == nil {
			return errors.New("missing progress title")
		}
		return c.updateProgress(token, false)
	case "end":
		return c.updateProgress(token, true)
	case "report":
		return c.updateProgress(token, false)
	default:
		return errors.New("invalid work-done progress kind")
	}
}

func (c *Client) updateProgress(token string, end bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !end {
		if _, exists := c.progress[token]; !exists && len(c.progress) >= 1024 {
			return errors.New("too many active progress tokens")
		}
		c.progress[token] = struct{}{}
	} else {
		delete(c.progress, token)
	}
	c.progressSeen = true
	close(c.progressChanged)
	c.progressChanged = make(chan struct{})
	return nil
}

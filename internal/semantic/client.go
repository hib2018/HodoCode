// Package semantic provides a small UTF-16 LSP client for explicitly supplied servers.
package semantic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Symbol represents either a DocumentSymbol or a flat SymbolInformation.
// Flat symbols have a zero SelectionRange: LSP provides no identifier span.
// Their ContainerName is preserved, but it is not proof of lexical ownership.
type Symbol struct {
	ContainerName  string   `json:"containerName,omitempty"`
	Name           string   `json:"name"`
	Kind           int      `json:"kind"`
	Range          Range    `json:"range"`
	SelectionRange Range    `json:"selectionRange"`
	Children       []Symbol `json:"children,omitempty"`
}

// URI returns an absolute, escaped local file URI.
func URI(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	slash := filepath.ToSlash(absolute)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

// Path accepts only absolute local file URIs (including file://localhost).
func Path(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !strings.HasPrefix(u.Path, "/") || strings.ContainsRune(u.Path, 0) {
		return "", fmt.Errorf("not a local file URI: %q", uri)
	}
	path := u.Path
	if runtime.GOOS == "windows" && len(path) > 2 && path[2] == ':' {
		path = path[1:]
	}
	p := filepath.FromSlash(path)
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("not an absolute local file URI: %q", uri)
	}
	return p, nil
}

const maxFrame = 8 << 20
const maxHeader = 8 << 10

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message) }

// Client owns one child process. Cancellation of Start's context, any operation,
// or a protocol failure terminates it. Close is idempotent and waits for cleanup.
type Client struct {
	cmd             *exec.Cmd
	in              io.WriteCloser
	out             io.ReadCloser
	writeMu         sync.Mutex
	mu              sync.Mutex
	next            int
	pending         map[string]chan message
	err             error
	done            chan struct{}
	reaped          chan struct{}
	readDone        chan struct{}
	settings        any
	progress        map[string]struct{}
	progressSeen    bool
	progressChanged chan struct{}
}

// Start executes command directly, without a shell, installation, or build step.
// workspace/configuration reads dotted sections from initializationOptions.settings
// when present, otherwise initializationOptions; missing sections return null.
// Omitted positionEncoding means UTF-16, as specified by LSP.
func Start(ctx context.Context, command []string, root string, initializationOptions any) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(command) == 0 || command[0] == "" {
		return nil, errors.New("empty LSP command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = root
	setupProcessGroup(cmd)
	// A nil Stderr is connected to the null device, without an unbounded buffer.
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	c := &Client{cmd: cmd, in: in, out: out, pending: make(map[string]chan message), done: make(chan struct{}), reaped: make(chan struct{}), readDone: make(chan struct{}), settings: initializationOptions, progress: make(map[string]struct{}), progressChanged: make(chan struct{})}
	if opts, ok := initializationOptions.(map[string]any); ok {
		if settings, exists := opts["settings"]; exists {
			c.settings = settings
		}
	}
	go func() {
		err := cmd.Wait()
		if err == nil {
			err = io.EOF
		}
		c.fail(err)
		close(c.reaped)
	}()
	go c.readLoop()
	go func() {
		select {
		case <-ctx.Done():
			c.fail(ctx.Err())
		case <-c.done:
		}
	}()
	var result struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	err = c.call(ctx, "initialize", map[string]any{"processId": os.Getpid(), "rootUri": URI(root), "capabilities": map[string]any{
		"general":   map[string]any{"positionEncodings": []string{"utf-16"}},
		"workspace": map[string]any{"configuration": true},
		"window":    map[string]any{"workDoneProgress": true},
		"textDocument": map[string]any{
			"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
			"definition":     map[string]any{"linkSupport": true},
		},
	}, "initializationOptions": initializationOptions}, &result)
	if err == nil && result.Capabilities.PositionEncoding != "" && result.Capabilities.PositionEncoding != "utf-16" {
		err = fmt.Errorf("unsupported position encoding %q", result.Capabilities.PositionEncoding)
	}
	if err == nil {
		err = c.notify(ctx, "initialized", map[string]any{})
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
	c.in.Close()
	c.out.Close()
	killProcessGroup(c.cmd)
}
func (c *Client) Close() error {
	c.fail(errors.New("LSP client closed"))
	<-c.reaped
	<-c.readDone
	return nil
}
func (c *Client) watch(ctx context.Context) func() {
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.fail(ctx.Err())
		case <-stop:
		case <-c.done:
		}
	}()
	return func() { close(stop) }
}
func (c *Client) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxFrame {
		return errors.New("LSP frame exceeds limit")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		c.mu.Lock()
		err = c.err
		c.mu.Unlock()
		return err
	default:
	}
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	if err != nil {
		c.fail(err)
	}
	return err
}
func (c *Client) notify(ctx context.Context, method string, params any) error {
	if err := ctx.Err(); err != nil {
		c.fail(err)
		return err
	}
	stop := c.watch(ctx)
	defer stop()
	err := c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		c.fail(err)
		return err
	}
	stop := c.watch(ctx)
	defer stop()
	c.mu.Lock()
	c.next++
	id := strconv.Itoa(c.next)
	ch := make(chan message, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if err := json.Unmarshal(m.Result, result); err != nil {
			c.fail(err)
			return err
		}
		return nil
	case <-ctx.Done():
		c.fail(ctx.Err())
		return ctx.Err()
	case <-c.done:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	total := 0
	for {
		line, err := r.ReadSlice('\n')
		if err != nil {
			return nil, err
		}
		total += len(line)
		if total > maxHeader {
			return nil, errors.New("LSP header exceeds limit")
		}
		if !bytes.HasSuffix(line, []byte("\r\n")) {
			return nil, errors.New("invalid LSP header line")
		}
		line = line[:len(line)-2]
		if len(line) == 0 {
			break
		}
		key, value, ok := strings.Cut(string(line), ":")
		if !ok {
			return nil, errors.New("invalid LSP header")
		}
		if strings.EqualFold(key, "Content-Length") {
			if length != -1 {
				return nil, errors.New("duplicate Content-Length")
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 || n > maxFrame {
				return nil, errors.New("invalid Content-Length")
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	b := make([]byte, length)
	_, err := io.ReadFull(r, b)
	return b, err
}
func (c *Client) readLoop() {
	defer close(c.readDone)
	r := bufio.NewReaderSize(c.out, maxHeader)
	for {
		b, err := readFrame(r)
		if err != nil {
			c.fail(err)
			return
		}
		var m message
		if err = json.Unmarshal(b, &m); err != nil || m.JSONRPC != "2.0" {
			c.fail(errors.New("invalid JSON-RPC message"))
			return
		}
		if len(m.ID) > 0 && m.ID[0] != '"' && m.ID[0] != '-' && (m.ID[0] < '0' || m.ID[0] > '9') {
			c.fail(errors.New("invalid JSON-RPC id"))
			return
		}
		if m.Method != "" {
			if len(m.Result) > 0 || m.Error != nil {
				c.fail(errors.New("invalid JSON-RPC request"))
				return
			}
			if len(m.ID) == 0 && m.Method == "$/progress" {
				if err = c.handleProgress(m.Params); err != nil {
					c.fail(err)
					return
				}
			}
			if len(m.ID) > 0 {
				if err = c.respond(m); err != nil {
					c.fail(err)
					return
				}
			}
			continue
		}
		if len(m.ID) == 0 || (len(m.Result) == 0 && m.Error == nil) || (len(m.Result) > 0 && m.Error != nil) {
			c.fail(errors.New("invalid JSON-RPC response"))
			return
		}
		c.mu.Lock()
		ch := c.pending[string(m.ID)]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- m:
			default:
			}
		}
	}
}
func (c *Client) respond(m message) error {
	response := map[string]any{"jsonrpc": "2.0", "id": m.ID}
	if m.Method == "window/workDoneProgress/create" || m.Method == "workspace/workDoneProgress/create" {
		if err := c.createProgress(m.Params); err != nil {
			response["error"] = &rpcError{-32602, err.Error()}
		} else {
			response["result"] = nil
		}
	} else if m.Method == "workspace/configuration" {
		var params struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		if err := json.Unmarshal(m.Params, &params); err != nil {
			response["error"] = &rpcError{-32602, "invalid configuration parameters"}
		} else {
			values := make([]any, len(params.Items))
			// Normalize typed initialization options into JSON objects for section lookup.
			b, _ := json.Marshal(c.settings)
			var settings any
			_ = json.Unmarshal(b, &settings)
			for i, item := range params.Items {
				v := settings
				if item.Section != "" {
					for _, key := range strings.Split(item.Section, ".") {
						obj, _ := v.(map[string]any)
						v = obj[key]
					}
				}
				values[i] = v
			}
			response["result"] = values
		}
	} else {
		response["error"] = &rpcError{-32601, "unsupported client method: " + m.Method}
	}
	return c.send(response)
}

// Open sends a full-text didOpen notification; callers should open each file once.
func (c *Client) Open(ctx context.Context, filename, language string, source []byte) error {
	return c.notify(ctx, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": URI(filename), "languageId": language, "version": 1, "text": string(source)}})
}
func (c *Client) Symbols(ctx context.Context, filename string) ([]Symbol, error) {
	requested, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	requested = symbolFilePath(requested)
	var raw []json.RawMessage
	if err := c.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": URI(filename)}}, &raw); err != nil {
		return nil, err
	}
	result := make([]Symbol, 0, len(raw))
	for _, b := range raw {
		var item struct {
			Symbol
			Location *Location `json:"location"`
		}
		if err := json.Unmarshal(b, &item); err != nil {
			c.fail(err)
			return nil, err
		}
		if item.Location != nil {
			file, err := Path(item.Location.URI)
			if err != nil {
				c.fail(err)
				return nil, err
			}
			if symbolFilePath(file) != requested {
				err := fmt.Errorf("document symbol belongs to %q, not requested file %q", item.Location.URI, filename)
				c.fail(err)
				return nil, err
			}
			item.Range = item.Location.Range
			item.SelectionRange = Range{}
		}
		result = append(result, item.Symbol)
	}
	return result, nil
}

// Resolve existing symlink aliases, but still support unsaved/nonexistent files.
func symbolFilePath(path string) string {
	path = filepath.Clean(path)
	if actual, err := filepath.EvalSymlinks(path); err == nil {
		return actual
	}
	return path
}

func (c *Client) Definitions(ctx context.Context, filename string, pos Position) ([]Location, error) {
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": URI(filename)}, "position": pos}, &raw); err != nil {
		return nil, err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var items []json.RawMessage
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			c.fail(err)
			return nil, err
		}
	} else {
		items = []json.RawMessage{raw}
	}
	result := make([]Location, 0, len(items))
	for _, b := range items {
		var item struct {
			Location
			TargetURI            string `json:"targetUri"`
			TargetSelectionRange Range  `json:"targetSelectionRange"`
		}
		if err := json.Unmarshal(b, &item); err != nil {
			c.fail(err)
			return nil, err
		}
		if item.TargetURI != "" {
			item.URI = item.TargetURI
			item.Range = item.TargetSelectionRange
		}
		result = append(result, item.Location)
	}
	return result, nil
}

package semantic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestServer is re-executed as the server, so tests require no installed LSP.
func TestServer(t *testing.T) {
	if os.Getenv("SEMANTIC_TEST_SERVER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	r := bufio.NewReader(os.Stdin)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	read := func() message {
		b, err := readFrame(r)
		if err != nil {
			os.Exit(0)
		}
		var m message
		if json.Unmarshal(b, &m) != nil {
			os.Exit(2)
		}
		return m
	}
	m := read()
	if m.Method != "initialize" {
		os.Exit(3)
	}
	var init struct {
		Capabilities struct {
			General struct {
				PositionEncodings []string `json:"positionEncodings"`
			} `json:"general"`
		} `json:"capabilities"`
		InitializationOptions map[string]any `json:"initializationOptions"`
	}
	if json.Unmarshal(m.Params, &init) != nil || len(init.Capabilities.General.PositionEncodings) != 1 || init.Capabilities.General.PositionEncodings[0] != "utf-16" {
		os.Exit(4)
	}
	var initObject map[string]any
	_ = json.Unmarshal(m.Params, &initObject)
	capabilities := initObject["capabilities"].(map[string]any)
	if capabilities["workspace"].(map[string]any)["configuration"] != true {
		os.Exit(11)
	}
	if capabilities["window"].(map[string]any)["workDoneProgress"] != true {
		os.Exit(13)
	}
	textDocument := capabilities["textDocument"].(map[string]any)
	if textDocument["documentSymbol"].(map[string]any)["hierarchicalDocumentSymbolSupport"] != true || textDocument["definition"].(map[string]any)["linkSupport"] != true {
		os.Exit(12)
	}
	if mode == "bad-envelope" {
		send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil, "error": map[string]any{"code": -1, "message": "bad"}})
		for {
			read()
		}
	}
	if mode == "bad-frame" {
		fmt.Fprint(os.Stdout, "Content-Length: 999999999\r\n\r\n")
		select {}
	}
	if mode == "bad-json" {
		fmt.Fprint(os.Stdout, "Content-Length: 1\r\n\r\nx")
		select {}
	}
	if mode == "hang-init" {
		for {
			read()
		}
	}
	encoding := "utf-16"
	if mode == "utf8" {
		encoding = "utf-8"
	}
	if mode == "default" {
		encoding = ""
	}
	send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"capabilities": map[string]any{"positionEncoding": encoding}}})
	initialized := read()
	if initialized.Method != "initialized" {
		os.Exit(5)
	}
	if mode == "progress-control" {
		serveProgress(read, send)
		return
	}
	if mode == "blocked-write" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	if mode == "stderr" {
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("x"), 1<<20))
	}
	opened := false
	for {
		m = read()
		switch m.Method {
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI, LanguageID, Text string
					Version               int
				}
			}
			if json.Unmarshal(m.Params, &p) != nil || p.TextDocument.LanguageID != "go" || p.TextDocument.Text != "package test\n" || p.TextDocument.Version != 1 || !strings.HasPrefix(p.TextDocument.URI, "file:///") {
				os.Exit(6)
			}
			opened = true
		case "textDocument/documentSymbol":
			if mode == "hang" {
				continue
			}
			if mode == "malformed" {
				fmt.Fprint(os.Stdout, "Content-Length: 0\r\n\r\n")
				continue
			}
			if mode == "exit" {
				os.Exit(0)
			}
			if mode == "config" {
				send(map[string]any{"jsonrpc": "2.0", "id": "cfg", "method": "workspace/configuration", "params": map[string]any{"items": []any{map[string]any{"section": "demo.enabled"}, map[string]any{"section": "missing"}, map[string]any{}}}})
				response := read()
				var values []any
				_ = json.Unmarshal(response.Result, &values)
				if string(response.ID) != `"cfg"` || len(values) != 3 || values[0] != true || values[1] != nil {
					os.Exit(7)
				}
				send(map[string]any{"jsonrpc": "2.0", "id": 91, "method": "workspace/applyEdit", "params": map[string]any{}})
				response = read()
				if response.Error == nil || response.Error.Code != -32601 {
					os.Exit(8)
				}
				// Notifications must not produce responses.
				send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{}})
			}
			if mode == "rpc-error" {
				send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "not supported"}})
				continue
			}
			if !opened && mode == "normal" {
				os.Exit(9)
			}
			rng := Range{Start: Position{1, 2}, End: Position{3, 4}}
			var result any = []Symbol{{Name: "parent", Kind: 5, Range: rng, SelectionRange: rng, Children: []Symbol{{Name: "child", Kind: 6, Range: rng, SelectionRange: rng}}}}
			if mode == "flat" || mode == "flat-wrong-uri" || mode == "flat-invalid-uri" {
				var params struct {
					TextDocument struct {
						URI string `json:"uri"`
					} `json:"textDocument"`
				}
				_ = json.Unmarshal(m.Params, &params)
				uri := params.TextDocument.URI
				if mode == "flat-wrong-uri" {
					uri = URI(filepath.Join(os.TempDir(), "unrelated-semantic-symbol.go"))
				}
				if mode == "flat-invalid-uri" {
					uri = "https://example.com/a.go"
				}
				result = []any{map[string]any{"name": "flat", "kind": 12, "containerName": "owner", "location": Location{URI: uri, Range: rng}}}
			}
			if mode == "null" {
				result = nil
			}
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		case "textDocument/definition":
			var p struct {
				Position Position `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			if p.Position != (Position{4, 6}) {
				os.Exit(10)
			}
			loc := Location{URI: "file:///a.go", Range: Range{Start: Position{1, 2}, End: Position{3, 4}}}
			var result any = loc
			switch mode {
			case "array":
				result = []Location{loc, loc}
			case "link":
				result = []any{map[string]any{"targetUri": loc.URI, "targetRange": Range{}, "targetSelectionRange": loc.Range}}
			case "null":
				result = nil
			}
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		}
	}
}

func serverCommand(t *testing.T, mode string) []string {
	t.Helper()
	t.Setenv("SEMANTIC_TEST_SERVER", "1")
	return []string{os.Args[0], "-test.run=^TestServer$", "--", mode}
}
func startServer(t *testing.T, mode string) (*Client, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, err := Start(ctx, serverCommand(t, mode), t.TempDir(), map[string]any{"settings": map[string]any{"demo": map[string]any{"enabled": true}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c, ctx
}
func TestClient(t *testing.T) {
	c, ctx := startServer(t, "normal")
	if err := c.Open(ctx, "a.go", "go", []byte("package test\n")); err != nil {
		t.Fatal(err)
	}
	symbols, err := c.Symbols(ctx, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || symbols[0].Name != "parent" || len(symbols[0].Children) != 1 || symbols[0].Children[0].Name != "child" {
		t.Fatalf("symbols: %+v", symbols)
	}
	locations, err := c.Definitions(ctx, "a.go", Position{4, 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 1 || locations[0].URI != "file:///a.go" || locations[0].Range.Start != (Position{1, 2}) {
		t.Fatalf("locations: %+v", locations)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Symbols(ctx, "a.go"); err == nil {
		t.Fatal("request after close succeeded")
	}
}
func TestSymbolVariants(t *testing.T) {
	for _, mode := range []string{"flat", "null", "config", "default", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			c, ctx := startServer(t, mode)
			symbols, err := c.Symbols(ctx, "a.go")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "null" {
				if len(symbols) != 0 {
					t.Fatal(symbols)
				}
				return
			}
			if len(symbols) != 1 {
				t.Fatal(symbols)
			}
			if mode == "flat" && (symbols[0].Name != "flat" || symbols[0].SelectionRange != (Range{}) || symbols[0].ContainerName != "owner" || symbols[0].Range.Start != (Position{1, 2})) {
				t.Fatal(symbols)
			}
		})
	}
}
func TestDefinitionVariants(t *testing.T) {
	for _, mode := range []string{"array", "link", "null"} {
		t.Run(mode, func(t *testing.T) {
			c, ctx := startServer(t, mode)
			locations, err := c.Definitions(ctx, "a.go", Position{4, 6})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "array" {
				want = 2
			}
			if mode == "null" {
				want = 0
			}
			if len(locations) != want {
				t.Fatal(locations)
			}
			if want > 0 && locations[0].Range.Start != (Position{1, 2}) {
				t.Fatal(locations)
			}
		})
	}
}
func TestStartFailures(t *testing.T) {
	for _, mode := range []string{"utf8", "bad-frame", "bad-json", "bad-envelope", "hang-init"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			c, err := Start(ctx, serverCommand(t, mode), t.TempDir(), nil)
			if err == nil {
				c.Close()
				t.Fatal("expected error")
			}
		})
	}
	if _, err := Start(context.Background(), nil, "", nil); err == nil {
		t.Fatal("empty command accepted")
	}
	if _, err := Start(context.Background(), []string{filepath.Join(t.TempDir(), "missing")}, "", nil); err == nil {
		t.Fatal("missing executable accepted")
	}
}
func TestOperationFailures(t *testing.T) {
	for _, mode := range []string{"hang", "malformed", "exit", "rpc-error"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := startServer(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if _, err := c.Symbols(ctx, "a.go"); err == nil {
				t.Fatal("expected error")
			}
			if mode != "rpc-error" {
				select {
				case <-c.done:
				default:
					t.Fatal("client not terminated")
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestLifetimeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, err := Start(ctx, serverCommand(t, "normal"), t.TempDir(), nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not terminate client")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.reaped:
	default:
		t.Fatal("child not reaped")
	}
	select {
	case <-c.readDone:
	default:
		t.Fatal("reader not stopped")
	}
}
func TestFrameValidation(t *testing.T) {
	for _, frame := range []string{
		"\r\n", "Content-Length: -1\r\n\r\n", "Content-Length: 8388609\r\n\r\n", "Content-Length: no\r\n\r\n", "Content-Length: 1\r\nContent-Length: 1\r\n\r\nx", "Content-Length: 2\r\n\r\nx", "Content-Length: 1\n\nx", strings.Repeat("x", maxHeader+1), "bad\r\n\r\n",
	} {
		if _, err := readFrame(bufio.NewReaderSize(strings.NewReader(frame), maxHeader)); err == nil {
			t.Fatalf("accepted %q", frame)
		}
	}
	body := `{"jsonrpc":"2.0","id":1,"result":null}`
	frame := fmt.Sprintf("content-length: %d\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n%s", len(body), body)
	b, err := readFrame(bufio.NewReader(strings.NewReader(frame)))
	if err != nil || !bytes.Equal(b, []byte(body)) {
		t.Fatalf("%q %v", b, err)
	}
}
func TestFileURI(t *testing.T) {
	name := filepath.Join(t.TempDir(), "space # 日本 %.go")
	uri := URI(name)
	if strings.Contains(uri, " ") || !strings.Contains(uri, "%23") {
		t.Fatal(uri)
	}
	path, err := Path(uri)
	if err != nil || path != name {
		t.Fatalf("%q %v", path, err)
	}
	local, err := Path("file://localhost/tmp/a%20b")
	if err != nil || local != "/tmp/a b" {
		t.Fatalf("%q %v", local, err)
	}
	for _, uri := range []string{"https://example.com/a", "file://remote/a", "file:relative", "file:///a?x=y", "file:///a#b", "file:///a%00", "file:///bad%zz"} {
		if _, err := Path(uri); err == nil {
			t.Fatalf("accepted %q", uri)
		}
	}
}

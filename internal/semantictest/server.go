// Package semantictest supplies fixture-only language servers for tests.
package semantictest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf16"
)

func init() {
	if os.Getenv("HODOCODE_TEST_LSP") == "1" {
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// Use installs temporary server launchers without enabling server mode in this process.
func Use(t testing.TB) {
	t.Helper()
	dir := t.TempDir()
	binary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "'"
	for _, name := range []string{"typescript-language-server", "rust-analyzer", "clangd", "pyright-langserver", "zls"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nHODOCODE_TEST_LSP=1 exec "+quoted+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type span struct {
	Start position `json:"start"`
	End   position `json:"end"`
}
type symbol struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	Range          span   `json:"range"`
	SelectionRange span   `json:"selectionRange"`
}
type document struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

// shortcut: these declarations and brace bodies cover fixtures only; add fixture cases, not a parser.
var declaration = regexp.MustCompile(`\b(?:function|def|fn|int)\s+([\pL_][\pL\pN_]*)\s*(?:<[^(){};\r\n]*>)?\s*\(`)

func at(source string, offset int) position {
	prefix := source[:offset]
	line := strings.Count(prefix, "\n")
	column := prefix[strings.LastIndex(prefix, "\n")+1:]
	return position{line, len(utf16.Encode([]rune(column)))}
}

func symbols(source string) []symbol {
	result := []symbol{}
	matches := declaration.FindAllStringSubmatchIndex(source, -1)
	for i, match := range matches {
		start, end := match[0], len(strings.TrimRight(source, "\r\n"))
		if strings.HasPrefix(source[start:], "def ") {
			if i+1 < len(matches) {
				end = len(strings.TrimRight(source[:matches[i+1][0]], " \t\r\n"))
			}
		} else {
			open := strings.IndexByte(source[match[1]:], '{')
			if open < 0 {
				continue
			}
			depth := 0
			for j := match[1] + open; j < len(source); j++ {
				if source[j] == '{' {
					depth++
				}
				if source[j] == '}' {
					depth--
					if depth == 0 {
						end = j + 1
						break
					}
				}
			}
		}
		result = append(result, symbol{
			Name: source[match[2]:match[3]], Kind: 12,
			Range:          span{at(source, start), at(source, end)},
			SelectionRange: span{at(source, match[2]), at(source, match[3])},
		})
	}
	return result
}

func wordAt(source string, pos position) string {
	lines := strings.Split(source, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) || pos.Character < 0 {
		return ""
	}
	runes := []rune(lines[pos.Line])
	index, units := 0, 0
	for index < len(runes) && units < pos.Character {
		units += len(utf16.Encode(runes[index : index+1]))
		index++
	}
	if units != pos.Character || index == len(runes) {
		return ""
	}
	identifier := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	if !identifier(runes[index]) {
		return ""
	}
	start, end := index, index
	for start > 0 && identifier(runes[start-1]) {
		start--
	}
	for end < len(runes) && identifier(runes[end]) {
		end++
	}
	return string(runes[start:end])
}

func serve() error {
	reader := bufio.NewReader(os.Stdin)
	documents := map[string]string{}
	buildPending := os.Getenv("HODOCODE_TEST_LSP_BUILD_PROGRESS") == "1"
	for {
		length := 0
		for {
			line, err := reader.ReadString('\n')
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if line == "\r\n" {
				break
			}
			if value, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				length, err = strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					return err
				}
			}
		}
		if length <= 0 || length > 8<<20 {
			return fmt.Errorf("invalid frame length %d", length)
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return err
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				TextDocument document `json:"textDocument"`
				Position     position `json:"position"`
			} `json:"params"`
		}
		if err := json.Unmarshal(body, &message); err != nil {
			return err
		}
		// Client responses to server requests must not generate another response.
		if message.Method == "" {
			continue
		}
		var result any
		emitBuildProgress := false
		switch message.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-16", "documentSymbolProvider": true, "definitionProvider": true}}
		case "textDocument/didOpen":
			documents[message.Params.TextDocument.URI] = message.Params.TextDocument.Text
		case "textDocument/documentSymbol":
			result = symbols(documents[message.Params.TextDocument.URI])
		case "textDocument/definition":
			if buildPending {
				buildPending = false
				emitBuildProgress = true
				break
			}
			locations := []any{}
			name := wordAt(documents[message.Params.TextDocument.URI], message.Params.Position)
			for uri, source := range documents {
				for _, sym := range symbols(source) {
					if sym.Name == name {
						locations = append(locations, map[string]any{"uri": uri, "range": sym.SelectionRange})
					}
				}
			}
			result = locations
		case "exit":
			return nil
		case "initialized", "shutdown":
		}
		if len(message.ID) == 0 {
			continue
		}
		response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result})
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(response), response); err != nil {
			return err
		}
		if emitBuildProgress {
			for _, notification := range []string{
				`{"jsonrpc":"2.0","id":"build-create","method":"window/workDoneProgress/create","params":{"token":"build"}}`,
				`{"jsonrpc":"2.0","method":"$/progress","params":{"token":"build","value":{"kind":"begin","title":"Build configuration"}}}`,
				`{"jsonrpc":"2.0","method":"$/progress","params":{"token":"build","value":{"kind":"end"}}}`,
			} {
				if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(notification), notification); err != nil {
					return err
				}
			}
		}
	}
}

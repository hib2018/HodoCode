package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
	"unicode"
)

type sourceModule struct {
	dir, path string
}

// directoryModule never follows a go.mod symlink, even when its target is local.
func directoryModule(dir string) (sourceModule, error) {
	filename := filepath.Join(dir, "go.mod")
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return sourceModule{}, nil
	}
	if err != nil {
		return sourceModule{}, fmt.Errorf("%s: %w", filename, err)
	}
	if !info.Mode().IsRegular() {
		return sourceModule{}, fmt.Errorf("%s: not a regular file", filename)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return sourceModule{}, fmt.Errorf("%s: %w", filename, err)
	}
	name, err := modulePath(string(data))
	if err != nil {
		return sourceModule{}, fmt.Errorf("%s: %w", filename, err)
	}
	// shortcut: require/replace and go.work are not interpreted; canonical imports
	// resolve indexed modules, but replace aliases with different module names do not.
	return sourceModule{dir: dir, path: name}, nil
}

// modulePath extracts only the module directive. The scanner preserves quoted
// paths and recognizes comments outside strings instead of stripping // blindly.
func modulePath(data string) (string, error) {
	var s scanner.Scanner
	s.Init(strings.NewReader(data))
	s.Mode = scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanComments | scanner.SkipComments
	s.Whitespace ^= 1 << '\n'
	var scanErr error
	s.Error = func(s *scanner.Scanner, message string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("line %d: %s", s.Pos().Line, message)
		}
	}
	var fields []string
	name := ""
	end := -1
	for {
		tok := s.Scan()
		if tok == '\n' || tok == scanner.EOF {
			if len(fields) > 0 && fields[0] == "module" {
				if name != "" || len(fields) != 2 {
					return "", fmt.Errorf("invalid or duplicate module directive")
				}
				name = fields[1]
				if strings.HasPrefix(name, "\"") || strings.HasPrefix(name, "`") {
					var err error
					name, err = strconv.Unquote(name)
					if err != nil {
						return "", fmt.Errorf("module path: %w", err)
					}
				}
				if !validModulePath(name) {
					return "", fmt.Errorf("invalid module path %q", name)
				}
			}
			fields = nil
			end = -1
			if tok == scanner.EOF {
				break
			}
			continue
		}
		text := s.TokenText()
		if s.Position.Offset == end {
			fields[len(fields)-1] += text
		} else {
			fields = append(fields, text)
		}
		end = s.Position.Offset + len(text)
	}
	if scanErr != nil {
		return "", scanErr
	}
	if name == "" {
		return "", fmt.Errorf("no module directive")
	}
	return name, nil
}

func validModulePath(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
		for _, ch := range part {
			if ch > unicode.MaxASCII || !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-._~", ch)) {
				return false
			}
		}
	}
	return true
}

package analyze

import (
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hib2018/HodoCode/internal/sourcelex"
)

// polyFunction keeps physical byte ranges in the immutable project snapshot.
type polyFunction struct {
	file, language, name, owner string
	semanticMethod              bool
	start, end, body, bodyEnd   int
	tokens                      []sourcelex.Token
	pairs                       map[int]int
}

func polyLanguage(language string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "auto":
		return "auto", nil
	case "go", "golang":
		return "go", nil
	case "zig":
		return "zig", nil
	case "ts", "typescript":
		return "typescript", nil
	case "rust", "rs":
		return "rust", nil
	case "c":
		return "c", nil
	case "js", "javascript":
		return "javascript", nil
	case "py", "python":
		return "python", nil
	}
	return "", fmt.Errorf("unsupported language %q", language)
}

func polyExtension(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".go":
		return "go"
	case ".zig":
		return "zig"
	case ".ts":
		return "typescript"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	}
	return ""
}

func loadPolyglot(root, moduleBoundary, language string) (*Project, error) {
	language, err := polyLanguage(language)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	var files []string
	hasModule := false
	err = filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if filename != root && (strings.HasPrefix(entry.Name(), ".") || polyExcludedDir(entry.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if entry.Name() == "go.mod" {
			hasModule = true
		}
		lang := polyExtension(filename)
		if lang != "" && lang != "go" && (language == "auto" || lang == language) {
			files = append(files, filename)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !hasModule && moduleBoundary != "" {
		for dir := root; ; dir = filepath.Dir(dir) {
			if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
				hasModule = true
				break
			}
			if dir == moduleBoundary || filepath.Dir(dir) == dir {
				break
			}
		}
	}
	var p *Project
	if language == "go" || (language == "auto" && (hasModule || len(files) == 0)) {
		p, err = load(root, moduleBoundary)
		if err != nil {
			return nil, err
		}
	} else {
		p = &Project{Root: root, Functions: map[string]Function{}, sources: map[string][]byte{}, local: map[string]bool{}}
	}
	p.Language = language
	p.polyglot = map[string]polyFunction{}
	for _, filename := range files {
		source, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return nil, err
		}
		relative = filepath.ToSlash(relative)
		lang := polyExtension(filename)
		tokens, err := sourcelex.Scan(lang, string(source))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", relative, err)
		}
		filtered := tokens[:0]
		for _, t := range tokens {
			if t.Kind != "comment" {
				filtered = append(filtered, t)
			}
		}
		tokens = filtered
		pairs := polyPairs(tokens)
		p.sources[filename] = source
		functions := polyParse(lang, string(source), tokens, pairs)
		for _, fn := range functions {
			fn.file, fn.language, fn.tokens, fn.pairs = filename, lang, tokens, pairs
			name := fn.name
			if fn.owner != "" {
				name = fn.owner + "." + name
			}
			id := relative + "." + name
			if _, exists := p.Functions[id]; exists {
				id += fmt.Sprintf("@%d", fn.start)
			}
			p.polyglot[id] = fn
			p.Functions[id] = Function{ID: id, File: relative, Line: polyLine(source, fn.start), End: polyLine(source, fn.end-1), Language: lang}
			p.local[id] = true
		}
		for _, target := range polyImports(lang, tokens, pairs) {
			p.Imports = append(p.Imports, Edge{From: relative, To: target})
		}
	}
	sortEdges(p.Imports)
	return p, nil
}

func polyExcludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", "target", "dist", "build", "__pycache__":
		return true
	}
	return false
}

func polyPairs(tokens []sourcelex.Token) map[int]int {
	pairs := map[int]int{}
	var stack []int
	for i, t := range tokens {
		if t.Kind == "string" || t.Kind == "comment" {
			continue
		}
		switch t.Text {
		case "(", "[", "{":
			stack = append(stack, i)
		case ")", "]", "}":
			if len(stack) == 0 {
				continue
			}
			j := stack[len(stack)-1]
			if (tokens[j].Text == "(" && t.Text == ")") || (tokens[j].Text == "[" && t.Text == "]") || (tokens[j].Text == "{" && t.Text == "}") {
				pairs[j], pairs[i] = i, j
				stack = stack[:len(stack)-1]
			}
		}
	}
	return pairs
}

func polyLine(source []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	return strings.Count(string(source[:offset]), "\n") + 1
}

func (p *Project) polySource(id string) (string, error) {
	fn, ok := p.polyglot[id]
	if !ok {
		return "", fmt.Errorf("function %q not found", id)
	}
	return string(p.sources[fn.file][fn.start:fn.end]), nil
}
func (p *Project) polyPosition(id string) (token.Position, error) {
	fn, ok := p.polyglot[id]
	if !ok {
		return token.Position{}, fmt.Errorf("function %q not found", id)
	}
	source := p.sources[fn.file]
	return token.Position{Filename: fn.file, Offset: fn.start, Line: polyLine(source, fn.start), Column: fn.start - strings.LastIndex(string(source[:fn.start]), "\n")}, nil
}
func (p *Project) polyMainFunctions() []string {
	var ids []string
	for id, fn := range p.polyglot {
		if fn.name == "main" && fn.owner == "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

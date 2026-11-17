package analyze

import (
	"github.com/hib2018/HodoCode/internal/sourcelex"
	"strings"
)

type polyScope struct {
	name       string
	start, end int
}

// shortcut: structural declarations only; use a full parser when language-specific type or macro expansion is required.
func polyParse(language, source string, ts []sourcelex.Token, pairs map[int]int) []polyFunction {
	text := func(i int) string {
		if i < 0 || i >= len(ts) {
			return ""
		}
		return ts[i].Text
	}
	var scopes []polyScope
	for i, t := range ts {
		zigStruct := language == "zig" && t.Text == "struct" && text(i-1) == "=" && i >= 3 && text(i-3) == "const"
		if t.Text != "class" && !(language == "rust" && t.Text == "impl") && !zigStruct {
			continue
		}
		name := text(i + 1)
		if zigStruct {
			name = text(i - 2)
		}
		if language == "rust" {
			// Trait implementations belong to the implementing type, not the trait.
			for j := i + 1; j < len(ts) && text(j) != "{"; j++ {
				if text(j) == "for" {
					name = text(j + 1)
					break
				}
			}
		}
		for j := i + 1; j < len(ts); j++ {
			if text(j) == "{" {
				if end, ok := pairs[j]; ok {
					scopes = append(scopes, polyScope{name, j, end})
				}
				break
			}
			if language == "python" && text(j) == ":" {
				scopes = append(scopes, polyScope{name, j, polyPythonEnd(source, ts, i, j+1)})
				break
			}
			if text(j) == ";" {
				break
			}
		}
	}
	var out []polyFunction
	add := func(name string, start, body, end int) {
		if name == "" || start < 0 || body < 0 || end < body || end >= len(ts) {
			return
		}
		owner := ""
		for _, scope := range scopes {
			if start > scope.start && start < scope.end {
				owner = scope.name
			}
		}
		out = append(out, polyFunction{name: name, owner: owner, start: ts[start].Start, end: ts[end].End, body: body, bodyEnd: end})
	}
	for i, t := range ts {
		switch {
		case (language == "rust" || language == "zig") && t.Text == "fn":
			if text(i+1) == "(" {
				continue
			}
			j := i + 2
			for j < len(ts) && text(j) != "{" && text(j) != ";" {
				j++
			}
			if end, ok := pairs[j]; ok && text(j) == "{" {
				add(text(i+1), i, j+1, end)
			}
		case language == "python" && t.Text == "def":
			j := i + 2
			for j < len(ts) {
				if text(j) == "(" || text(j) == "[" {
					if end, ok := pairs[j]; ok {
						j = end + 1
						continue
					}
				}
				if text(j) == ":" {
					break
				}
				j++
			}
			if j+1 < len(ts) {
				start := i
				if text(i-1) == "async" {
					start = i - 1
				}
				end := polyPythonEnd(source, ts, start, j+1)
				if end > j+1 {
					add(text(i+1), start, j+1, end-1)
				}
			}
		case (language == "javascript" || language == "typescript") && t.Text == "function":
			n := i + 1
			if text(n) == "*" {
				n++
			}
			if text(n) == "(" {
				continue
			}
			j := n + 1
			for j < len(ts) && text(j) != "{" && text(j) != ";" {
				if text(j) == "(" {
					if end, ok := pairs[j]; ok {
						j = end + 1
						continue
					}
				}
				j++
			}
			if end, ok := pairs[j]; ok && text(j) == "{" {
				add(text(n), i, j+1, end)
			}
		case (language == "javascript" || language == "typescript") && (t.Text == "const" || t.Text == "let" || t.Text == "var"):
			// Only statically named, directly assigned arrows are declarations.
			n := i + 1
			j := n + 1
			for j < len(ts) && text(j) != "=" && text(j) != ";" && text(j) != "{" {
				j++
			}
			if text(j) != "=" {
				continue
			}
			j++
			if text(j) == "async" {
				j++
			}
			if text(j) == "(" {
				end, ok := pairs[j]
				if !ok {
					continue
				}
				j = end + 1
			} else if j < len(ts) && ts[j].Kind == "identifier" {
				j++
			} else {
				continue
			}
			// Typed arrow return values can precede =>.
			for j < len(ts) && text(j) != "=>" && text(j) != ";" && text(j) != "{" {
				j++
			}
			if text(j) != "=>" {
				continue
			}
			j++
			if text(j) == "{" {
				if end, ok := pairs[j]; ok {
					add(text(n), i, j+1, end)
				}
				continue
			}
			end := j
			for end+1 < len(ts) && text(end+1) != ";" && text(end+1) != "}" && polyLine([]byte(source), ts[end+1].Start) == polyLine([]byte(source), ts[j].Start) {
				if close, ok := pairs[end+1]; ok && close > end+1 {
					end = close
				} else {
					end++
				}
			}
			add(text(n), i, j, end)
		case t.Kind == "identifier" && text(i+1) == "(" && (language == "c" || language == "javascript" || language == "typescript"):
			close, ok := pairs[i+1]
			if !ok || polyControl(t.Text) {
				continue
			}
			j := close + 1
			// Method return annotations are skipped up to the body.
			if text(j) == ":" {
				for j < len(ts) && text(j) != "{" && text(j) != ";" && text(j) != "=" {
					j++
				}
			}
			if text(j) != "{" {
				continue
			}
			end, ok := pairs[j]
			if !ok {
				continue
			}
			if language != "c" {
				inClass := false
				for _, s := range scopes {
					if i > s.start && i < s.end {
						inClass = true
					}
				}
				if !inClass {
					continue
				}
				if text(i-1) == "function" || text(i-1) == "." || text(i-1) == "new" {
					continue
				}
			}
			start := i
			if language == "c" {
				for start > 0 && text(start-1) != ";" && text(start-1) != "}" && text(start-1) != "{" && polyLine([]byte(source), ts[start-1].Start) == polyLine([]byte(source), t.Start) {
					start--
				}
			}
			add(t.Text, start, j+1, end)
		}
	}
	// A nested declaration belongs to its lexical owner, not the file namespace.
	for i := range out {
		for j := range out {
			if i != j && out[i].start > out[j].start && out[i].end <= out[j].end {
				out[i].owner = out[j].name
				break
			}
		}
	}
	return out
}

func polyPythonEnd(source string, ts []sourcelex.Token, start, body int) int {
	lineStart := strings.LastIndex(source[:ts[start].Start], "\n") + 1
	indent := 0
	for lineStart+indent < len(source) && (source[lineStart+indent] == ' ' || source[lineStart+indent] == '\t') {
		indent++
	}
	pairs := polyPairs(ts)
	declarationLine := polyLine([]byte(source), ts[start].Start)
	for j := body; j < len(ts); j++ {
		if end, ok := pairs[j]; ok && end > j {
			j = end
			continue
		}
		line := polyLine([]byte(source), ts[j].Start)
		if line <= declarationLine {
			continue
		}
		beginning := strings.LastIndex(source[:ts[j].Start], "\n") + 1
		prefix := source[beginning:ts[j].Start]
		if strings.TrimSpace(prefix) == "" && len(prefix) <= indent {
			return j
		}
	}
	return len(ts)
}

func polyControl(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "catch", "with", "sizeof", "alignof", "_Alignof", "typeof", "return", "function", "fn", "def", "match", "assert":
		return true
	}
	return false
}

func polyImports(language string, ts []sourcelex.Token, pairs map[int]int) []string {
	var imports []string
	pythonFromImports := map[int]bool{}
	literal := func(s string) string {
		if len(s) >= 2 {
			return s[1 : len(s)-1]
		}
		return s
	}
	for i, t := range ts {
		switch language {
		case "javascript", "typescript":
			if t.Text == "require" && i+3 < len(ts) && ts[i+1].Text == "(" && ts[i+2].Kind == "string" && ts[i+3].Text == ")" && (i == 0 || (ts[i-1].Text != "." && ts[i-1].Text != "?.")) {
				imports = append(imports, literal(ts[i+2].Text))
			}
			if t.Text != "import" && t.Text != "export" {
				continue
			}
			if i > 0 && (ts[i-1].Text == "." || ts[i-1].Text == "?.") {
				continue
			}
			j := i + 1
			if j >= len(ts) {
				continue
			}
			if t.Text == "import" && ts[j].Kind == "string" {
				imports = append(imports, literal(ts[j].Text))
				continue
			}
			if t.Text == "import" && ts[j].Text == "(" {
				if j+2 < len(ts) && ts[j+1].Kind == "string" && ts[j+2].Text == ")" {
					imports = append(imports, literal(ts[j+1].Text))
				}
				continue
			}
			if t.Text == "export" && ts[j].Text != "{" && ts[j].Text != "*" && ts[j].Text != "type" {
				continue
			}
			for ; j < len(ts); j++ {
				if ts[j].Text == "{" {
					if end, ok := pairs[j]; ok {
						j = end
						continue
					}
					break
				}
				if ts[j].Text == "from" {
					if j+1 < len(ts) && ts[j+1].Kind == "string" {
						imports = append(imports, literal(ts[j+1].Text))
					}
					break
				}
				if ts[j].Text == ";" || ts[j].Text == "=" || ts[j].Text == "function" || ts[j].Text == "const" || ts[j].Text == "class" || ts[j].Kind == "string" {
					break
				}
			}
		case "zig":
			if t.Text == "@" && i+3 < len(ts) && ts[i+1].Text == "import" && ts[i+3].Kind == "string" {
				imports = append(imports, literal(ts[i+3].Text))
			}
		case "c":
			if t.Text == "include" && i > 0 && ts[i-1].Text == "#" && i+1 < len(ts) {
				if ts[i+1].Kind == "string" {
					imports = append(imports, literal(ts[i+1].Text))
				} else if ts[i+1].Text == "<" {
					s := ""
					for j := i + 2; j < len(ts) && ts[j].Text != ">"; j++ {
						s += ts[j].Text
					}
					if s != "" {
						imports = append(imports, s)
					}
				}
			}
		case "python":
			if t.Text != "from" && t.Text != "import" {
				continue
			}
			if pythonFromImports[i] {
				continue
			}
			for j := i + 1; j < len(ts); {
				var name strings.Builder
				for j < len(ts) && (ts[j].Kind == "identifier" || ts[j].Text == ".") {
					if ts[j].Text == "import" || ts[j].Text == "as" {
						break
					}
					name.WriteString(ts[j].Text)
					j++
					if j < len(ts) && ts[j].Text != "." && ts[j-1].Text != "." {
						break
					}
				}
				if name.Len() > 0 {
					imports = append(imports, name.String())
				}
				if j < len(ts) && ts[j].Text == "import" {
					pythonFromImports[j] = true
					break
				}
				if j < len(ts) && ts[j].Text == "as" {
					j += 2
				}
				if j >= len(ts) || ts[j].Text != "," || t.Text == "from" {
					break
				}
				j++
			}
		case "rust":
			if t.Text == "import" || t.Text == "from" || t.Text == "use" || t.Text == "mod" {
				var name strings.Builder
				for j := i + 1; j < len(ts); j++ {
					s := ts[j].Text
					if ts[j].Kind != "identifier" && s != "." && s != "::" {
						break
					}
					if s == "import" || s == "as" {
						break
					}
					if j > i+1 && ts[j-1].Kind == "identifier" && ts[j].Kind == "identifier" {
						break
					}
					name.WriteString(s)
				}
				if name.Len() > 0 {
					imports = append(imports, name.String())
				}
			}
		}
	}
	return imports
}

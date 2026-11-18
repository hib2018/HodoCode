package analyze

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hib2018/HodoCode/internal/semantic"
	"github.com/hib2018/HodoCode/internal/sourcelex"
)

// Language servers are mandatory for non-Go languages. They must be installed
// explicitly; analysis never installs tools or downloads project dependencies.
func languageServer(language string) ([]string, any) {
	switch language {
	case "typescript", "javascript":
		return []string{"typescript-language-server", "--stdio"}, nil
	case "rust":
		settings := map[string]any{"cargo": map[string]any{"buildScripts": map[string]any{"enable": false}}, "procMacro": map[string]any{"enable": false}, "checkOnSave": false}
		return []string{"rust-analyzer"}, map[string]any{"cargo": settings["cargo"], "procMacro": settings["procMacro"], "checkOnSave": false, "settings": map[string]any{"rust-analyzer": settings}}
	case "c":
		return []string{"clangd", "--background-index=false", "--clang-tidy=false", "--enable-config=false"}, nil
	case "python":
		return []string{"pyright-langserver", "--stdio"}, nil
	case "zig":
		return []string{"zls"}, map[string]any{"enable_build_on_save": false, "dangerous_comptime_experiments_do_not_enable": false}
	}
	return nil, nil
}

type semanticDeclaration struct {
	id, file   string
	start, end int
	concrete   bool
}

// analyzeSemantics replaces non-Go declarations with server-provided symbols,
// resolves call positions against the same snapshot, then shuts down all tools.
func (p *Project) analyzeSemantics(ctx context.Context) error {
	filesByLanguage := map[string][]string{}
	for file := range p.sources {
		if language := FileLanguage(file); language != "" && language != "go" {
			// JS and TS share one semantic project and may import each other.
			if language == "javascript" {
				language = "typescript"
			}
			filesByLanguage[language] = append(filesByLanguage[language], file)
		}
	}
	languages := make([]string, 0, len(filesByLanguage))
	for language := range filesByLanguage {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	// Fail before starting any server if a required tool is missing.
	for _, language := range languages {
		command, _ := languageServer(language)
		if _, err := exec.LookPath(command[0]); err != nil {
			return fmt.Errorf("%s semantic analysis requires %s on PATH: %w", language, command[0], err)
		}
	}
	p.semanticCalls = map[string][]CallSite{}
	for _, language := range languages {
		files := filesByLanguage[language]
		sort.Strings(files)
		if err := p.analyzeLanguage(ctx, language, files); err != nil {
			return fmt.Errorf("%s semantic analysis: %w", language, err)
		}
	}
	return nil
}

func (p *Project) analyzeLanguage(ctx context.Context, language string, files []string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command, options := languageServer(language)
	client, err := semantic.Start(ctx, command, p.Root, options)
	if err != nil {
		return err
	}
	defer client.Close()
	for _, file := range files {
		if err := client.Open(ctx, file, FileLanguage(file), p.sources[file]); err != nil {
			return err
		}
	}
	var declarations []semanticDeclaration
	mutated := map[string]bool{}
	for _, file := range files {
		tokens, err := semanticTokens(FileLanguage(file), p.sources[file])
		if err != nil {
			return err
		}
		for i, token := range tokens {
			if token.Kind != "identifier" || i+1 >= len(tokens) {
				continue
			}
			switch tokens[i+1].Text {
			case "=", ":=", "+=", "-=":
				mutated[token.Text] = true
			}
		}
	}
	for _, file := range files {
		symbols, err := client.Symbols(ctx, file)
		if err != nil {
			return fmt.Errorf("symbols for %s: %w", file, err)
		}
		indexed, err := p.indexSemanticSymbols(file, FileLanguage(file), symbols)
		if err != nil {
			return err
		}
		declarations = append(declarations, indexed...)
	}
	zigBuildWaited := false
	for _, declaration := range declarations {
		fn := p.polyglot[declaration.id]
		sites, err := p.polyCallSites(declaration.id)
		if err != nil {
			return err
		}
		for i := range sites {
			site := &sites[i]
			// A lexical name match is not proof of semantic binding.
			site.Unknown = true
			site.To = "unknown: " + site.Name
			site.Reason = "言語サーバーで呼び出し先を確定できない"
			if site.Name == "" {
				site.To = "unknown: dynamic call"
				site.Reason = "間接呼び出しの対象を特定できない"
				continue
			}
			position, err := semanticPosition(p.sources[fn.file], fn.start+site.Offset)
			if err != nil {
				return err
			}
			targets, err := client.Definitions(ctx, fn.file, position)
			if err != nil {
				return fmt.Errorf("definition at %s:%d: %w", fn.file, site.Line, err)
			}
			if len(targets) == 0 && fn.language == "zig" && !zigBuildWaited {
				for i, token := range fn.tokens {
					if token.Start != fn.start+site.Offset || !zigImportedNamespace(fn, i) {
						continue
					}
					zigBuildWaited = true
					// ZLS loads named build imports asynchronously. Wait for its
					// progress completion, rather than caching a startup null result.
					ready, waitErr := client.WaitReady(ctx, 2*time.Second)
					if waitErr != nil {
						return fmt.Errorf("Zig build configuration: %w", waitErr)
					}
					if ready {
						targets, err = client.Definitions(ctx, fn.file, position)
						if err != nil {
							return fmt.Errorf("definition at %s:%d: %w", fn.file, site.Line, err)
						}
					}
					break
				}
			}
			candidates := map[string]bool{}
			unindexed := false
			for _, target := range targets {
				file, err := semantic.Path(target.URI)
				if err != nil {
					unindexed = true
					continue
				}
				file = filepath.Clean(file)
				if actual, err := filepath.EvalSymlinks(file); err == nil {
					file = actual
				}
				source, ok := p.sources[file]
				if !ok {
					unindexed = true
					continue
				}
				offset, err := semanticOffset(source, target.Range.Start)
				if err != nil {
					return fmt.Errorf("invalid definition from language server: %w", err)
				}
				found := false
				for _, candidate := range declarations {
					if candidate.concrete && candidate.file == file && offset >= candidate.start && offset < candidate.end {
						candidates[candidate.id], found = true, true
					}
				}
				if !found {
					unindexed = true
				}
			}
			for candidate := range candidates {
				site.Candidates = append(site.Candidates, candidate)
			}
			sort.Strings(site.Candidates)
			freeTargets := len(site.Candidates) > 0
			for _, id := range site.Candidates {
				if target := p.polyglot[id]; target.owner != "" || target.semanticMethod {
					freeTargets = false
				}
			}
			unsafe := mutated[site.Name] || semanticDynamicCall(fn, *site, freeTargets)
			for _, id := range site.Candidates {
				target := p.polyglot[id]
				if mutated[target.name] || semanticDynamicCall(target, CallSite{Name: target.name, Offset: -1}, false) {
					unsafe = true
				}
			}
			switch {
			case len(site.Candidates) > 0 && unsafe:
				site.Reason = "定義先候補はあるが、動的な呼び出し先は未確定"
			case len(site.Candidates) == 1 && !unindexed:
				site.To, site.Unknown, site.Reason = site.Candidates[0], false, ""
				site.Candidates = nil
			case len(site.Candidates) > 0:
				site.Reason = "呼び出し先を一意に確定できない"
			case unindexed:
				site.Reason = "定義先が関数ではないか、解析対象範囲外にある"
			}
		}
		p.semanticCalls[declaration.id] = sites
	}
	return nil
}

func (p *Project) indexSemanticSymbols(file, language string, symbols []semantic.Symbol) ([]semanticDeclaration, error) {
	var old []polyFunction
	for id, fn := range p.polyglot {
		if fn.file == file {
			old = append(old, fn)
			delete(p.polyglot, id)
			delete(p.Functions, id)
			delete(p.local, id)
		}
	}
	source := p.sources[file]
	// Tokenization remains responsible for source positions and call occurrences;
	// language-server symbols, not lexical guesses, decide which functions exist.
	tokens, err := semanticTokens(language, source)
	if err != nil {
		return nil, err
	}
	pairs := polyPairs(tokens)
	relative, err := filepath.Rel(p.Root, file)
	if err != nil {
		return nil, err
	}
	var declarations []semanticDeclaration
	var visit func([]semantic.Symbol, string) error
	visit = func(symbols []semantic.Symbol, owner string) error {
		for _, symbol := range symbols {
			callable := symbol.Kind == 6 || symbol.Kind == 9 || symbol.Kind == 12
			start, err := semanticOffset(source, symbol.Range.Start)
			if err != nil {
				return err
			}
			end, err := semanticOffset(source, symbol.Range.End)
			if err != nil {
				return err
			}
			selection, err := semanticOffset(source, symbol.SelectionRange.Start)
			if err != nil {
				return err
			}
			selectionEnd, err := semanticOffset(source, symbol.SelectionRange.End)
			if err != nil {
				return err
			}
			flat := symbol.SelectionRange == (semantic.Range{})
			if flat {
				// Flat symbols lack a name range: only inspect the declaration header.
				selection, selectionEnd = start, start
				for _, token := range tokens {
					if token.Start < start {
						continue
					}
					if token.Start >= end || token.Text == "{" {
						break
					}
					if token.Kind == "identifier" && token.Text == symbol.Name {
						selection, selectionEnd = token.Start, token.End
						break
					}
				}
			}
			if start > end || selection < start || selectionEnd > end || selectionEnd < selection {
				return fmt.Errorf("invalid symbol range for %s", symbol.Name)
			}
			var original *polyFunction
			for i := range old {
				candidate := &old[i]
				if candidate.name != symbol.Name || candidate.body >= len(tokens) {
					continue
				}
				for _, token := range tokens {
					if token.Start < candidate.start {
						continue
					}
					if token.Start >= tokens[candidate.body].Start {
						break
					}
					if token.Text == candidate.name {
						if token.Start == selection && token.End == selectionEnd {
							original = candidate
						}
						break
					}
				}
			}
			if flat && original != nil {
				// Flat locations may cover only the identifier. The server proves
				// the declaration; the existing parser supplies its source extent.
				start, end = original.start, original.end
			}
			if original != nil && (original.bodyEnd >= len(tokens) || tokens[original.body].Start < start || tokens[original.bodyEnd].End > end) {
				original = nil
			}
			// Only an actual arrow header proves a variable/property is callable;
			// getters and arbitrary same-named values must not become functions.
			if (symbol.Kind == 13 || symbol.Kind == 7 || symbol.Kind == 14) && original != nil {
				for _, token := range tokens {
					if token.Start >= tokens[original.body].Start {
						break
					}
					if token.Start >= original.start && token.Text == "=>" {
						callable = true
					}
				}
			}
			if callable && start < end {
				symbolOwner := owner
				if symbol.ContainerName != "" {
					symbolOwner = symbol.ContainerName
				}
				fn := polyFunction{file: file, language: language, name: symbol.Name, owner: symbolOwner, start: start, end: end, tokens: tokens, pairs: pairs, body: len(tokens), bodyEnd: len(tokens) - 1}
				if original != nil {
					if original.owner != "" {
						fn.owner = original.owner
					}
					fn.body, fn.bodyEnd = original.body, original.bodyEnd
				} else {
					// shortcut: call occurrence extraction uses lexical body boundaries;
					// upgrade to server call hierarchy for macro/generated bodies.
					for i, token := range tokens {
						if token.Start < selectionEnd || token.Start >= end {
							continue
						}
						if token.Text == "{" || (language == "python" && token.Text == ":") {
							fn.body = i + 1
							break
						}
					}
					for fn.bodyEnd >= 0 && tokens[fn.bodyEnd].Start >= end {
						fn.bodyEnd--
					}
				}
				name := fn.name
				if fn.owner != "" {
					name = fn.owner + "." + name
				}
				id := filepath.ToSlash(relative) + "." + name
				if _, exists := p.Functions[id]; exists {
					id += fmt.Sprintf("@%d", start)
				}
				fn.semanticMethod = symbol.Kind == 6 || symbol.Kind == 9
				p.polyglot[id] = fn
				p.Functions[id] = Function{ID: id, File: filepath.ToSlash(relative), Language: language, Line: polyLine(source, start), End: polyLine(source, end-1)}
				p.local[id] = true
				declarations = append(declarations, semanticDeclaration{id: id, file: file, start: selection, end: selectionEnd, concrete: fn.body <= fn.bodyEnd})
			}
			childOwner := owner
			if symbol.Kind == 5 || symbol.Kind == 23 || symbol.Kind == 11 {
				childOwner = symbol.Name
			}
			if err := visit(symbol.Children, childOwner); err != nil {
				return err
			}
		}
		return nil
	}
	err = visit(symbols, "")
	return declarations, err
}

// Definition navigation does not prove dynamic member dispatch or mutable values.
func semanticDynamicCall(fn polyFunction, site CallSite, freeTargets bool) bool {
	offset := fn.start + site.Offset
	for i, token := range fn.tokens {
		if token.Text == "@" && (fn.language == "python" || fn.language == "typescript" || fn.language == "javascript") {
			return true
		}
		if token.Start == offset && i > 0 {
			previous := fn.tokens[i-1].Text
			imported := zigImportedNamespace(fn, i) || (freeTargets && semanticImportedNamespace(fn, i))
			rustModule := freeTargets && rustNamespacePath(fn, i)
			if (previous == "." && !imported) || previous == "?." || previous == "->" || (previous == "::" && !rustModule) {
				return true
			}
		}
		if token.Text == site.Name && i+1 < len(fn.tokens) {
			next := fn.tokens[i+1].Text
			if next == "=" || next == ":=" || next == "+=" || next == "-=" || next == "!" {
				return true
			}
		}
	}
	return false
}

// zigImportedNamespace proves only const name = @import("literal") roots.
// shortcut: aliases and shadowed names stay unknown until binding-aware analysis exists.
func zigImportedNamespace(fn polyFunction, call int) bool {
	if fn.language != "zig" {
		return false
	}
	ts := fn.tokens
	root := call
	for root >= 2 && ts[root-1].Text == "." && ts[root-2].Kind == "identifier" {
		root -= 2
	}
	if root == call {
		return false
	}
	name := ts[root].Text
	declaration := -1
	for i, token := range ts {
		if token.Text != name || i+1 >= len(ts) {
			continue
		}
		previous := ""
		if i > 0 {
			previous = ts[i-1].Text
		}
		next := ts[i+1].Text
		if next == ":" || previous == "|" || previous == "*" || next == "|" || next == "," {
			return false
		}
		if next == "=" || previous == "const" || previous == "var" {
			if declaration >= 0 || previous != "const" || i+7 >= len(ts) || next != "=" || ts[i+2].Text != "@" || ts[i+3].Text != "import" || ts[i+4].Text != "(" || ts[i+5].Kind != "string" || ts[i+6].Text != ")" || ts[i+7].Text != ";" {
				return false
			}
			declaration = i
		}
	}
	if declaration < 0 {
		return false
	}
	for open, close := range fn.pairs {
		if open >= close || ts[open].Text != "{" {
			continue
		}
		if open < declaration && declaration < close && !(declaration < root && root < close) {
			return false
		}
	}
	return true
}

func semanticTokens(language string, source []byte) ([]sourcelex.Token, error) {
	tokens, err := sourcelex.Scan(language, string(source))
	if err != nil {
		return nil, err
	}
	filtered := tokens[:0]
	for _, token := range tokens {
		if token.Kind != "comment" {
			filtered = append(filtered, token)
		}
	}
	return filtered, nil
}

// LSP uses UTF-16 columns; all public analyzer positions remain byte offsets.
func semanticPosition(source []byte, offset int) (semantic.Position, error) {
	if offset < 0 || offset > len(source) || !utf8.Valid(source[:offset]) {
		return semantic.Position{}, fmt.Errorf("invalid byte offset %d", offset)
	}
	line := strings.Count(string(source[:offset]), "\n")
	start := strings.LastIndex(string(source[:offset]), "\n") + 1
	character := 0
	for _, r := range string(source[start:offset]) {
		character++
		if r > 0xffff {
			character++
		}
	}
	return semantic.Position{Line: line, Character: character}, nil
}

func semanticOffset(source []byte, position semantic.Position) (int, error) {
	if position.Line < 0 || position.Character < 0 {
		return 0, fmt.Errorf("negative LSP position")
	}
	offset := 0
	for line := 0; line < position.Line; line++ {
		next := strings.IndexByte(string(source[offset:]), '\n')
		if next < 0 {
			return 0, fmt.Errorf("LSP line out of range")
		}
		offset += next + 1
	}
	units := 0
	for units < position.Character {
		if offset >= len(source) || source[offset] == '\n' {
			return 0, fmt.Errorf("LSP column out of range")
		}
		r, size := utf8.DecodeRune(source[offset:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("invalid UTF-8 source")
		}
		units++
		if r > 0xffff {
			units++
		}
		if units > position.Character {
			return 0, fmt.Errorf("LSP position splits surrogate pair")
		}
		offset += size
	}
	return offset, nil
}

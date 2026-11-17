package analyze

import (
	"fmt"
	"strings"
)

// polyCallSites resolves only unshadowed file-local function declarations. Member
// dispatch, imported bindings and callable values stay unknown without type data.
func (p *Project) polyCallSites(id string) ([]CallSite, error) {
	fn, ok := p.polyglot[id]
	if !ok {
		return nil, fmt.Errorf("function %q not found", id)
	}
	ts := fn.tokens
	source := p.sources[fn.file]
	text := func(i int) string {
		if i < 0 || i >= len(ts) {
			return ""
		}
		return ts[i].Text
	}
	candidates := map[string]string{}
	ambiguous := map[string]bool{}
	for target, other := range p.polyglot {
		if other.file == fn.file && other.owner == "" {
			if _, exists := candidates[other.name]; exists {
				ambiguous[other.name] = true
			}
			candidates[other.name] = target
		}
	}
	shadow := map[string]bool{}
	// Conservatively treat every signature identifier except the declaration name
	// as a possible parameter, and every assigned name as a possible local binding.
	for i, t := range ts {
		inside := t.Start >= fn.start && t.Start < fn.end
		if inside && i < fn.body && t.Kind == "identifier" && t.Text != fn.name {
			shadow[t.Text] = true
		}
		assigned := t.Kind == "identifier" && (text(i+1) == "=" || text(i+1) == ":=" || text(i+1) == "+=" || text(i+1) == "-=")
		// An arrow's own declaration is not a reassignment of that binding.
		if target := candidates[t.Text]; assigned && target != "" {
			declaration := p.polyglot[target]
			if t.Start >= declaration.start && i < declaration.body {
				assigned = false
			}
		}
		if assigned || (t.Kind == "identifier" && text(i-1) == "*" && text(i+1) == ")") {
			shadow[t.Text] = true
		}
		if inside && (t.Text == "let" || t.Text == "const" || t.Text == "var" || t.Text == "for" || t.Text == "as") && i+1 < len(ts) {
			shadow[text(i+1)] = true
		}
		if (t.Text == "let" || t.Text == "const" || t.Text == "var") && (text(i+1) == "{" || text(i+1) == "[") {
			if end, ok := fn.pairs[i+1]; ok {
				for j := i + 2; j < end; j++ {
					if ts[j].Kind == "identifier" {
						shadow[text(j)] = true
					}
				}
			}
		}
		if t.Kind == "identifier" && candidates[t.Text] != "" && i > 0 && ts[i-1].Kind == "identifier" && (text(i+1) == ";" || text(i+1) == ",") {
			shadow[t.Text] = true
		}
		if t.Text == "catch" && text(i+1) == "(" {
			if end, ok := fn.pairs[i+1]; ok {
				for j := i + 2; j < end; j++ {
					if ts[j].Kind == "identifier" {
						shadow[text(j)] = true
					}
				}
			}
		}
	}
	// Imports can introduce aliases or shadow names; do not infer their bindings.
	for i, t := range ts {
		if t.Text != "import" && t.Text != "from" && t.Text != "use" {
			continue
		}
		line := polyLine(source, t.Start)
		for j := i + 1; j < len(ts) && polyLine(source, ts[j].Start) == line && text(j) != ";"; j++ {
			if ts[j].Kind == "identifier" {
				shadow[text(j)] = true
			}
		}
	}
	var sites []CallSite
	for i := fn.body; i <= fn.bodyEnd && i < len(ts); i++ {
		t := ts[i]
		nestedEnd := 0
		for _, other := range p.polyglot {
			if other.file == fn.file && other.start > fn.start && t.Start >= other.start && t.Start < other.end && other.end <= fn.end {
				if other.end > nestedEnd {
					nestedEnd = other.end
				}
			}
		}
		if nestedEnd != 0 {
			for i+1 < len(ts) && ts[i+1].Start < nestedEnd {
				i++
			}
			continue
		}
		// Anonymous function and arrow bodies are not calls of their enclosing owner.
		if t.Text == "function" || t.Text == "=>" || (fn.language == "rust" && (t.Text == "|" || t.Text == "||") && (text(i-1) == "=" || text(i-1) == "move" || text(i-1) == "(" || text(i-1) == ",")) {
			if t.Text == "=>" && text(i+1) != "{" {
				j := i + 1
				for j <= fn.bodyEnd && j < len(ts) && text(j) != "," && text(j) != ";" && text(j) != ")" && text(j) != "}" {
					if end, ok := fn.pairs[j]; ok && end > j {
						j = end + 1
					} else {
						j++
					}
				}
				i = j - 1
				continue
			}
			for j := i + 1; j <= fn.bodyEnd && j < len(ts); j++ {
				if text(j) == "{" {
					if end, ok := fn.pairs[j]; ok {
						i = end
					}
					break
				}
				if text(j) == ";" {
					i = j
					break
				}
			}
			continue
		}
		if t.Text == "(" && (text(i-1) == "]" || text(i-1) == ")") {
			opening, paired := fn.pairs[i-1]
			if paired && (polyControl(text(opening-1)) || (fn.language == "c" && polyCType(text(opening-1)))) {
				continue
			}
			position := ts[i-1].Start
			column := position - strings.LastIndex(string(source[:position]), "\n")
			sites = append(sites, CallSite{Edge: Edge{From: id, To: "unknown: dynamic call", Unknown: true}, Line: polyLine(source, position), Column: column, EndColumn: column, Offset: position - fn.start})
			continue
		}
		macro := fn.language == "rust" && text(i+1) == "!" && text(i+2) == "("
		if t.Kind != "identifier" || (text(i+1) != "(" && !macro) || polyControl(t.Text) || (fn.language == "c" && polyCType(t.Text)) {
			continue
		}
		if text(i-1) == "@" || text(i-1) == "#" {
			continue
		}
		label := t.Text
		member := text(i-1) == "." || text(i-1) == "::" || text(i-1) == "->" || text(i-1) == "?."
		if member && i >= 2 {
			label = text(i-2) + text(i-1) + label
		}
		edge := Edge{From: id, To: "unknown: " + label, Unknown: true}
		if target := candidates[t.Text]; target != "" && !member && !macro && !shadow[t.Text] && !ambiguous[t.Text] {
			edge.To, edge.Unknown = target, false
		}
		column := t.Start - strings.LastIndex(string(source[:t.Start]), "\n")
		sites = append(sites, CallSite{Edge: edge, Line: polyLine(source, t.Start), Column: column, EndColumn: column + len(t.Text), Offset: t.Start - fn.start, Name: t.Text})
	}
	return sites, nil
}

func polyCType(name string) bool {
	switch name {
	case "void", "int", "char", "long", "short", "signed", "unsigned", "float", "double", "bool", "_Bool":
		return true
	}
	return false
}

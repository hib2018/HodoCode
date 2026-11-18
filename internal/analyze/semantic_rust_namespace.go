package analyze

// rustNamespacePath recognizes module paths, not generic/associated type paths.
// The caller must additionally prove that all targets are free functions.
func rustNamespacePath(fn polyFunction, call int) bool {
	if fn.language != "rust" {
		return false
	}
	ts := fn.tokens
	root := call
	for root >= 2 && ts[root-1].Text == "::" && ts[root-2].Kind == "identifier" {
		root -= 2
	}
	if root == call {
		return false
	}
	name := ts[root].Text
	switch name {
	case "crate", "self", "super":
		return true
	case "Self":
		return false
	}
	module := false
	for i, token := range ts {
		if token.Text != name {
			continue
		}
		if i+1 < len(ts) && ts[i+1].Text == ":" {
			return false
		}
		if i > 0 && ts[i-1].Text == "mod" {
			module = true
		}
		if i > 0 && (ts[i-1].Text == "type" || ts[i-1].Text == "struct" || ts[i-1].Text == "trait" || ts[i-1].Text == "enum") {
			return false
		}
	}
	if module {
		return true
	}
	// A use binding can denote a module or type. The free-function target check
	// distinguishes modules; associated/default trait methods stay unknown.
	for i, token := range ts {
		if token.Text != "use" {
			continue
		}
		for j := i + 1; j < len(ts) && ts[j].Text != ";"; j++ {
			if ts[j].Text != name || j+1 >= len(ts) {
				continue
			}
			next := ts[j+1].Text
			if next == ";" || next == "," || next == "}" {
				return true
			}
		}
	}
	return false
}

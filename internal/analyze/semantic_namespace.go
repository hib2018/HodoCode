package analyze

// semanticImportedNamespace proves only explicit module-import receiver roots.
// call is the token index of the called member, not a source offset.
func semanticImportedNamespace(fn polyFunction, call int) bool {
	if fn.language != "javascript" && fn.language != "typescript" && fn.language != "python" {
		return false
	}
	ts := fn.tokens
	if call < 2 || call+1 >= len(ts) || ts[call].Kind != "identifier" || ts[call+1].Text != "(" {
		return false
	}
	root := call
	for root >= 2 && ts[root-1].Text == "." && ts[root-2].Kind == "identifier" {
		root -= 2
	}
	if root == call || (root > 0 && (ts[root-1].Text == "." || ts[root-1].Text == "?.")) {
		return false
	}
	name := ts[root].Text
	start, end := -1, -1
	for i := 0; i < len(ts); i++ {
		if ts[i].Text != "import" {
			continue
		}
		binding, last := -1, -1
		if fn.language != "python" {
			// Deliberately exclude type-only imports, require(), and aliases of values.
			if i+5 < len(ts) && ts[i+1].Text == "*" && ts[i+2].Text == "as" && ts[i+3].Kind == "identifier" && ts[i+4].Text == "from" && ts[i+5].Kind == "string" {
				literal := ts[i+5].Text
				if len(literal) >= 2 && (literal[0] == '\'' || literal[0] == '"') && literal[len(literal)-1] == literal[0] {
					binding, last = i+3, i+5
				}
			}
		} else if i+1 < len(ts) && ts[i+1].Kind == "identifier" {
			from := false
			for j := i - 1; j >= 0; j-- {
				if ts[j].Text == "from" {
					from = true
					break
				}
				if ts[j].Text == "import" || ts[j].Text == ";" || ts[j].Text == ":" {
					break
				}
			}
			if !from {
				binding, last = i+1, i+1
				for last+2 < len(ts) && ts[last+1].Text == "." && ts[last+2].Kind == "identifier" {
					last += 2
				}
				if last+2 < len(ts) && ts[last+1].Text == "as" && ts[last+2].Kind == "identifier" {
					binding, last = last+2, last+2
				}
				if last+1 < len(ts) && ts[last+1].Text == "," {
					binding = -1
				}
			}
		}
		if binding < 0 || ts[binding].Text != name {
			continue
		}
		receiverEnd := binding
		if fn.language == "python" {
			receiverEnd = last
		}
		// An imported module does not prove that its object-valued properties
		// are namespaces too. Match only the exact imported module or alias.
		if call-root-1 != receiverEnd-binding+1 {
			return false
		}
		for j := binding; j <= receiverEnd; j++ {
			if ts[j].Text != ts[root+j-binding].Text {
				return false
			}
		}
		if start >= 0 || last >= root {
			return false
		}
		start, end = i, last
	}
	if start < 0 || fn.body < 0 || root < fn.body || root > fn.bodyEnd {
		return false
	}
	if fn.language == "python" {
		// shortcut: tokens lack indentation; accept only module-prefix or straight-
		// line local imports, until indentation-aware binding scopes are available.
		if ts[start].Start >= fn.start && ts[start].Start < fn.end {
			if start < fn.body {
				return false
			}
		} else {
			for i := 0; i < start; i++ {
				if ts[i].Text == ":" {
					return false
				}
			}
		}
		for i := fn.body; i <= fn.bodyEnd && i < len(ts); i++ {
			if ts[i].Text == ":" || ts[i].Text == "def" || ts[i].Text == "class" || ts[i].Text == "lambda" {
				return false
			}
		}
	} else {
		for open, close := range fn.pairs {
			if open < start && start < close {
				return false // ESM imports must be at module scope.
			}
		}
		for i := fn.body; i <= fn.bodyEnd && i < len(ts); i++ {
			if ts[i].Text == "function" || ts[i].Text == "=>" || ts[i].Text == "class" {
				return false
			}
		}
	}
	for i, token := range ts {
		if token.Text != name || (i >= start && i <= end) {
			continue
		}
		// Member reads can share the import; bindings and bare escapes cannot.
		if i+1 >= len(ts) || ts[i+1].Text != "." {
			return false
		}
		if fn.language != "python" && i >= fn.body && i <= fn.bodyEnd {
			for open, close := range fn.pairs {
				if open >= fn.body && open < i && i < close && ts[open].Text == "{" {
					return false
				}
			}
		}
	}
	return true
}

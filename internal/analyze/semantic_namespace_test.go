package analyze

import "testing"

func TestSemanticImportedNamespace(t *testing.T) {
	for _, tc := range []struct {
		name, language, source string
		want                   bool
	}{
		{"js namespace", "javascript", `import * as ns from 'pkg'; function main() { ns.run(); }`, true},
		{"js shared namespace", "javascript", `import * as ns from 'pkg'; function before() { ns.run(); } function main() { ns.run(); } function after() { ns.run(); }`, true},
		{"ts shared namespace and type headers", "typescript", `import * as ns from 'pkg'; function before(value: ns.Options) { ns.run(); } function main(value: ns.Options): ns.Result { ns.run(); } function after() { ns.run(); }`, true},
		{"python shared namespace", "python", "import package as ns\ndef before():\n    ns.run()\ndef main():\n    ns.run()\ndef after():\n    ns.run()\n", true},
		{"python shared namespace and type headers", "python", "import package as ns\ndef before(value: ns.Options):\n    ns.run()\ndef main(value: ns.Options) -> ns.Result:\n    ns.run()\ndef after():\n    ns.run()\n", true},
		{"ts object chain", "typescript", `import * as ns from "pkg"; function main() { ns.cli.run(); }`, false},
		{"js object chain", "javascript", `import * as ns from 'pkg'; function main() { ns.handlers.run(); }`, false},
		{"ts ordinary namespace", "typescript", `import * as ns from 'pkg'; function main(): void { ns.run(); }`, true},
		{"python module", "python", "import package\ndef main():\n    package.run()\n", true},
		{"python dotted module", "python", "import package.module\ndef main():\n    package.module.run()\n", true},
		{"python alias", "python", "import package.module as ns\ndef main():\n    ns.run()\n", true},
		{"python object chain", "python", "import package\ndef main():\n    package.object.run()\n", false},
		{"python alias object chain", "python", "import package.module as ns\ndef main():\n    ns.handlers.run()\n", false},
		{"python wrong imported path", "python", "import package.module\ndef main():\n    package.other.run()\n", false},
		{"python parent module", "python", "import package.module\ndef main():\n    package.run()\n", false},
		{"python local module", "python", "def main():\n    import package\n    package.run()\n", true},
		{"python local alias", "python", "def main():\n    import package.module as ns\n    ns.run()\n", true},
		{"python unrelated import", "python", "from other import value\nimport package as ns\ndef main():\n    ns.run()\n", true},
		{"js object", "javascript", `const ns = {}; function main() { ns.run(); }`, false},
		{"js parameter", "javascript", `import * as ns from 'pkg'; function main(ns) { ns.run(); }`, false},
		{"js default parameter", "javascript", `import * as ns from 'pkg'; function main(ns = {}) { ns.run(); }`, false},
		{"js destructured parameter", "javascript", `import * as ns from 'pkg'; function main({ns}) { ns.run(); }`, false},
		{"ts object parameter", "typescript", `import * as ns from 'pkg'; function main(ns: any) { ns.run(); }`, false},
		{"python object parameter", "python", "import package as ns\ndef main(ns):\n    ns.run()\n", false},
		{"js reassign", "javascript", `import * as ns from 'pkg'; function main() { ns = other; ns.run(); }`, false},
		{"js sibling reassign", "javascript", `import * as ns from 'pkg'; function main() { ns.run(); } function after() { ns = other; }`, false},
		{"ts sibling binding", "typescript", `import * as ns from 'pkg'; function main() { ns.run(); } function after(ns: any) { ns.run(); }`, false},
		{"js sibling bare capture", "javascript", `import * as ns from 'pkg'; function main() { ns.run(); } const capture = () => ns;`, false},
		{"python sibling reassign", "python", "import package as ns\ndef main():\n    ns.run()\ndef after():\n    ns = other\n", false},
		{"python sibling binding", "python", "import package as ns\ndef main():\n    ns.run()\ndef after(ns):\n    ns.run()\n", false},
		{"js later reassign", "javascript", `import * as ns from 'pkg'; function main() { ns.run(); ns = other; }`, false},
		{"js redeclare", "javascript", `import * as ns from 'pkg'; function main() { const ns = other; ns.run(); }`, false},
		{"js nested shadow", "javascript", `import * as ns from 'pkg'; function main() { { let ns = other; ns.run(); } }`, false},
		{"js capture", "javascript", `import * as ns from 'pkg'; function main() { const f = () => ns.run(); }`, false},
		{"js method capture", "javascript", `import * as ns from 'pkg'; function main() { const o = { m() { ns.run(); } }; }`, false},
		{"js namespace escape", "javascript", `import * as ns from 'pkg'; function main() { consume(ns); ns.run(); }`, false},
		{"js default import", "javascript", `import ns from 'pkg'; function main() { ns.run(); }`, false},
		{"js named import", "javascript", `import { ns } from 'pkg'; function main() { ns.run(); }`, false},
		{"ts type-only", "typescript", `import type * as ns from 'pkg'; function main() { ns.run(); }`, false},
		{"js nonliteral", "javascript", "import * as ns from `pkg`; function main() { ns.run(); }", false},
		{"js sibling scope", "javascript", `function other() { import * as ns from 'pkg'; } function main() { ns.run(); }`, false},
		{"js import later", "javascript", `function main() { ns.run(); } import * as ns from 'pkg';`, false},
		{"js other receiver", "javascript", `import * as ns from 'pkg'; function main() { object.ns.run(); }`, false},
		{"js computed receiver", "javascript", `import * as ns from 'pkg'; function main() { object['x'].ns.run(); }`, false},
		{"js duplicate binding", "javascript", `import * as ns from 'pkg'; import * as ns from 'other'; function main() { ns.run(); }`, false},
		{"python from import", "python", "from package import ns\ndef main():\n    ns.run()\n", false},
		{"python reassign", "python", "import package as ns\ndef main():\n    ns = other\n    ns.run()\n", false},
		{"python later reassign", "python", "import package as ns\ndef main():\n    ns.run()\n    ns = other\n", false},
		{"python sibling import", "python", "def other():\n    import package as ns\ndef main():\n    ns.run()\n", false},
		{"python conditional import", "python", "def main():\n    if condition:\n        import package as ns\n    ns.run()\n", false},
		{"python capture", "python", "def outer():\n    import package as ns\n    def main():\n        ns.run()\n", false},
		{"python nested capture", "python", "import package as ns\ndef main():\n    def inner():\n        ns.run()\n", false},
		{"python import later", "python", "def main():\n    ns.run()\n    import package as ns\n", false},
		{"python namespace escape", "python", "import package as ns\ndef main():\n    consume(ns)\n    ns.run()\n", false},
		{"python comma import", "python", "import ns, other\ndef main():\n    ns.run()\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := semanticTokens(tc.language, []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			pairs := polyPairs(tokens)
			var selected *polyFunction
			functions := polyParse(tc.language, tc.source, tokens, pairs)
			for i := range functions {
				if functions[i].name == "main" {
					selected = &functions[i]
				}
			}
			if selected == nil {
				t.Fatal("main not found")
			}
			selected.language, selected.tokens, selected.pairs = tc.language, tokens, pairs
			call := -1
			for i := selected.body; i <= selected.bodyEnd && i+1 < len(tokens); i++ {
				if tokens[i].Text == "run" && tokens[i+1].Text == "(" {
					call = i
					break
				}
			}
			if call < 0 {
				t.Fatal("run call not found")
			}
			if got := semanticImportedNamespace(*selected, call); got != tc.want {
				t.Fatalf("semanticImportedNamespace = %v, want %v", got, tc.want)
			}
			if tc.want {
				for _, language := range []string{"zig", "rust", "c", "go"} {
					selected.language = language
					if semanticImportedNamespace(*selected, call) {
						t.Fatalf("unexpected proof for %s", language)
					}
				}
			}
		})
	}
}

func TestSemanticImportedNamespaceInvalidCall(t *testing.T) {
	for _, call := range []int{-1, 0, 1, 100} {
		if semanticImportedNamespace(polyFunction{language: "python"}, call) {
			t.Fatalf("proved invalid token index %d", call)
		}
	}
}

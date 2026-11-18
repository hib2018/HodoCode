package analyze

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantictest"
)

func TestSemanticImportedNamespaceCalls(t *testing.T) {
	for _, tc := range []struct {
		name, language, main, helper, caller, callee string
		unknown                                      bool
	}{
		{"javascript", "js", "main.js", "cli.js", "import * as cli from './cli.js';\nfunction main() { cli.run(); }\n", "export function run() {}\n", false},
		{"typescript", "ts", "main.ts", "cli.ts", "import * as cli from './cli';\nfunction main() { cli.run(); }\n", "export function run(): void {}\n", false},
		{"python", "python", "main.py", "cli.py", "import cli\ndef main():\n    cli.run()\n", "def run():\n    pass\n", false},
		{"python alias", "python", "main.py", "cli.py", "import cli as commands\ndef main():\n    commands.run()\n", "def run():\n    pass\n", false},
		{"rust module", "rust", "main.rs", "cli.rs", "mod cli;\nfn main() { cli::run(); }\n", "pub fn run() {}\n", false},
		{"rust crate path", "rust", "main.rs", "cli.rs", "mod cli;\nfn main() { crate::cli::run(); }\n", "pub fn run() {}\n", false},
		{"rust use alias", "rust", "main.rs", "cli.rs", "mod cli;\nuse crate::cli as commands;\nfn main() { commands::run(); }\n", "pub fn run() {}\n", false},
		{"javascript object", "js", "main.js", "cli.js", "import * as cli from './cli.js';\nfunction main() { cli.handlers.run(); }\n", "export function run() {}\n", true},
		{"javascript shadow", "js", "main.js", "cli.js", "import * as cli from './cli.js';\nfunction main(cli) { cli.run(); }\n", "export function run() {}\n", true},
		{"typescript shadow", "ts", "main.ts", "cli.ts", "import * as cli from './cli';\nfunction main(cli: any) { cli.run(); }\n", "export function run() {}\n", true},
		{"python shadow", "python", "main.py", "cli.py", "import cli\ndef main(cli):\n    cli.run()\n", "def run():\n    pass\n", true},
		{"python reassignment", "python", "main.py", "cli.py", "import cli\ncli = other\ndef main():\n    cli.run()\n", "def run():\n    pass\n", true},
		{"python nested object", "python", "main.py", "cli.py", "import cli\ndef main():\n    cli.handlers.run()\n", "def run():\n    pass\n", true},
		{"rust generic", "rust", "main.rs", "cli.rs", "mod cli;\nfn main<T: Trait>() { T::run(); }\n", "pub fn run() {}\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			semantictest.Use(t)
			root := t.TempDir()
			for file, source := range map[string]string{tc.main: tc.caller, tc.helper: tc.callee} {
				if err := os.WriteFile(filepath.Join(root, file), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := LoadLanguage(root, tc.language)
			if err != nil {
				t.Fatal(err)
			}
			sites, err := p.CallSites(tc.main + ".main")
			if err != nil || len(sites) != 1 || sites[0].Unknown != tc.unknown {
				t.Fatalf("namespace call: %+v, %v", sites, err)
			}
			if !tc.unknown && sites[0].To != tc.helper+".run" {
				t.Fatalf("target: %+v", sites[0])
			}
		})
	}
}

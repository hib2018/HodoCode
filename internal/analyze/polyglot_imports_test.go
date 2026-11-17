package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolyglotJavaScriptImportSources(t *testing.T) {
	root := t.TempDir()
	source := `export function entry() { return 'value'; }
export const x = 'not-a-dependency';
export {x} from 'reexport';
export * from 'all';
import {name} from 'named';
import 'side-effect';
const loaded = import('dynamic');
const common = require('common');
object.require('not-a-module');
object.import('not-an-import');
`
	if err := os.WriteFile(filepath.Join(root, "a.js"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "js")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"reexport": true, "all": true, "named": true, "side-effect": true, "dynamic": true, "common": true}
	if len(p.Imports) != len(want) {
		t.Fatalf("imports=%#v", p.Imports)
	}
	for _, edge := range p.Imports {
		if !want[edge.To] {
			t.Fatalf("false dependency: %#v", edge)
		}
	}
}

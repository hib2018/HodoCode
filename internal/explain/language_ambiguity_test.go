package explain

import (
	"strings"
	"testing"
)

func TestPrepareEveryLanguageRejectsUnsafeMask(t *testing.T) {
	for _, tt := range []struct{ language, name, valid, unsafe string }{
		{"javascript", "app.js", "function entry() { return 1; }", "function entry() { return 'LITERAL_SECRET; }"},
		{"typescript", "app.ts", "function entry() { return 1; }", "function entry() { return `LITERAL_SECRET${value}`; }"},
		{"python", "app.py", "def entry():\n    return 1\n", "def entry():\n    return f'LITERAL_SECRET{value}'\n"},
		{"rust", "app.rs", "fn entry() { }", "fn entry() { let value = r##\"LITERAL_SECRET; }"},
		{"zig", "app.zig", "fn entry() void { }", "fn entry() void { const value = \"LITERAL_SECRET; }"},
		{"c", "app.c", "int entry(void) { return 1; }", "int entry(void) { const char *value = \"LITERAL_SECRET; }"},
	} {
		t.Run(tt.language, func(t *testing.T) {
			p, id := languageFixture(t, tt.language, tt.name, tt.valid)
			write(t, p.Root, tt.name, tt.unsafe)
			r, err := Prepare(p, id, "explain", nil, nil)
			if r != nil || err == nil || !strings.Contains(err.Error(), "redact") {
				t.Fatalf("unsafe lexing yielded request or bypassed mask: %v, %v", r, err)
			}
		})
	}
}

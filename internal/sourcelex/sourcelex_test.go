package sourcelex

import (
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	tests := []struct {
		lang, source string
		kinds        []string
	}{
		{"ts", `const α = "hi"; // note`, []string{"identifier", "identifier", "punct", "string", "punct", "comment"}},
		{"JS", "let x = `plain`;", []string{"identifier", "identifier", "punct", "string", "punct"}},
		{"ts", "x => a?.b", []string{"identifier", "punct", "identifier", "punct", "identifier"}},
		{"rust", "a::b->c", []string{"identifier", "punct", "identifier", "punct", "identifier"}},
		{"python", "x = rb'''a\n#b''' # end", []string{"identifier", "punct", "string", "comment"}},
		{"rust", `let x = br##"a"#b"##; /* outer /* inner */ end */`, []string{"identifier", "identifier", "punct", "string", "punct", "comment"}},
		{"rs", `fn x<'a>(v: &'a str) { let c = 'é'; let b = b'\n'; }`, nil},
		{"zig", "const x = \\\\first\n \\\\second\n;", []string{"identifier", "identifier", "punct", "string", "string", "punct"}},
		{"c", "int x; /\\\n* hidden */ // continued\\\r\n still hidden\r\nx++;", []string{"identifier", "identifier", "punct", "comment", "comment", "identifier", "punct", "punct", "punct"}},
		{"go", "var x = `raw\ntext`; // comment", []string{"identifier", "identifier", "punct", "string", "punct", "comment"}},
		{"golang", "x := 0xFF + 1.25e-3", []string{"identifier", "punct", "punct", "number", "punct", "number"}},
	}
	for _, tt := range tests {
		t.Run(tt.lang+tt.source, func(t *testing.T) {
			tokens, err := Scan(tt.lang, tt.source)
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			end := 0
			for _, tok := range tokens {
				if tok.Start < end || tok.End <= tok.Start || tok.Text != tt.source[tok.Start:tok.End] {
					t.Fatalf("invalid token: %#v", tok)
				}
				end = tok.End
				kinds = append(kinds, tok.Kind)
			}
			if tt.kinds != nil && strings.Join(kinds, ",") != strings.Join(tt.kinds, ",") {
				t.Fatalf("kinds = %v, want %v", kinds, tt.kinds)
			}
		})
	}
}

func TestMask(t *testing.T) {
	tests := []struct{ lang, source, want string }{
		{"typescript", "const 名前 = '秘密'; // 隠す\r\nnext()", "const 名前 =         ;          \r\nnext()"},
		{"python", "x = r'''one\r\ntwo''' # hi\nnext", "x =        \r\n           \nnext"},
		{"rust", "x = r##\"a\n\"#b\"##; /* a /* b */ c */", "x =      \n      ;                  "},
		{"c", "x=\"a\\\nb\"; // hi\\\ncontinued\nx", "x=   \n  ;       \n         \nx"},
		{"zig", "x = \\\\hello\n    \\\\world\n;", "x =        \n           \n;"},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			got, err := Mask(tt.lang, tt.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.source) {
				t.Fatalf("length %d != %d", len(got), len(tt.source))
			}
			if got != tt.want {
				t.Fatalf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestRejectsUnsafeInput(t *testing.T) {
	tests := []struct{ lang, source string }{
		{"unknown", "x"}, {"go", string([]byte{0xff})},
		{"javascript", "<!-- hidden"}, {"js", "--> hidden"},
		{"c", "// continuation??/\nsecret"},
		{"c", `"unterminated`}, {"c", "/* unterminated"},
		{"c", "// okay\n\"bad\nquote\""}, {"c", `R"tag(raw)tag"`},
		{"javascript", `/unterminated`}, {"js", `if (x) /abc/g`},
		{"python", `'''unterminated`}, {"python", `r"ends\`},
		{"rust", `r###"wrong"##`}, {"rust", `/* a /* b */`},
		{"go", "`unterminated"}, {"zig", `"bad`},
	}
	for _, tt := range tests {
		t.Run(tt.lang+tt.source, func(t *testing.T) {
			got, err := Scan(tt.lang, tt.source)
			if err == nil || got != nil {
				t.Fatalf("Scan = %#v, %v", got, err)
			}
			masked, err := Mask(tt.lang, tt.source)
			if err == nil || masked != "" {
				t.Fatalf("Mask = %q, %v", masked, err)
			}
		})
	}
}

func TestFormattedOpaque(t *testing.T) {
	tests := []struct{ lang, literal string }{
		{"js", "`fake function hidden(){} ${name} tail`"},
		{"ts", "`outer ${`inner ${name}`} tail`"},
		{"js", "`outer ${ { key: \"}\" } } tail`"},
		{"python", `f"fake def hidden(): {name}"`},
		{"python", `rf"{lookup['key']} tail"`},
		{"python", `t"{value}"`},
		{"python", `f"{f'{value}'}"`},
		{"python", "f'''fake def hidden():\n{value!r:20}'''"},
	}
	for _, tt := range tests {
		source := tt.literal + "; real()"
		tokens, err := Scan(tt.lang, source)
		if err != nil {
			t.Fatalf("%q: %v", source, err)
		}
		if len(tokens) < 3 || tokens[0].Kind != "string" || tokens[0].Text != tt.literal || tokens[2].Text != "real" {
			t.Fatalf("unexpected tokens: %#v", tokens)
		}
		if masked, err := Mask(tt.lang, source); err == nil || masked != "" {
			t.Fatalf("strict Mask = %q, %v", masked, err)
		}
	}
	// Uncertain regex/division in interpolation must never expose following text.
	tokens, err := Scan("js", "`a${ /[}]/.test(x) } fake function hidden(){}`; real()")
	if err != nil || len(tokens) != 1 || tokens[0].Kind != "string" {
		t.Fatalf("opaque fallback: %#v, %v", tokens, err)
	}
}

func TestJavaScriptShebang(t *testing.T) {
	for _, prefix := range []string{"", "\uFEFF"} {
		source := prefix + "#!/usr/bin/env node\r\nreal()"
		tokens, err := Scan("js", source)
		if err != nil || tokens[0].Kind != "comment" || tokens[0].Start != len(prefix) || tokens[1].Text != "real" {
			t.Fatalf("tokens = %#v, %v", tokens, err)
		}
		got, err := Mask("javascript", source)
		want := prefix + strings.Repeat(" ", len("#!/usr/bin/env node")) + "\r\nreal()"
		if err != nil || got != want {
			t.Fatalf("Mask = %q, %v; want %q", got, err, want)
		}
	}
}

func TestJavaScriptSlash(t *testing.T) {
	for _, source := range []string{`a / b`, `call() / 2`, `x /= 2`, `x[0] / 2`} {
		got, err := Mask("js", source)
		if err != nil || got != source {
			t.Fatalf("division %q: %q, %v", source, got, err)
		}
	}
	for _, source := range []string{`/secret/g`, `const re = /se[cr/]et/i;`, `return /secret/`, `x = /escaped\\/secret/`} {
		got, err := Mask("typescript", source)
		if err != nil || strings.Contains(got, "secret") || strings.Contains(got, "se[cr") {
			t.Fatalf("regex %q: %q, %v", source, got, err)
		}
	}
}

func TestEscapesAndLiteralBoundaries(t *testing.T) {
	for _, tt := range []struct{ lang, source string }{
		{"js", `"escaped \" // inside"; real()`},
		{"python", `r"escaped \" # inside"; real()`},
		{"python", "'''escaped \\''' # inside\nend'''; real()"},
		{"rust", `b"/* inside */"; real()`},
		{"go", "`raw \\ // inside`; real()"},
	} {
		got, err := Mask(tt.lang, tt.source)
		if err != nil {
			t.Fatalf("%s: %v", tt.source, err)
		}
		if strings.Contains(got, "inside") || !strings.Contains(got, "real()") {
			t.Fatalf("unsafe mask %q", got)
		}
	}
}

func FuzzMask(f *testing.F) {
	for _, source := range []string{"", `x = "hi" // end`, "r##\"raw\"##", "f'{x}'", "/* nested */", "\\\\zig\n"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		for _, lang := range []string{"zig", "typescript", "rust", "c", "javascript", "python", "go"} {
			tokens, scanErr := Scan(lang, source)
			if scanErr == nil {
				end := 0
				for _, tok := range tokens {
					if tok.Start < end || tok.End <= tok.Start || tok.End > len(source) || tok.Text != source[tok.Start:tok.End] {
						t.Fatalf("invalid token: %#v", tok)
					}
					end = tok.End
				}
			}
			masked, err := Mask(lang, source)
			if err != nil {
				if masked != "" {
					t.Fatal("partial mask")
				}
				continue
			}
			if len(masked) != len(source) {
				t.Fatal("length changed")
			}
			for i := range source {
				if (source[i] == '\n' || source[i] == '\r') && masked[i] != source[i] {
					t.Fatal("newline changed")
				}
			}
		}
	})
}

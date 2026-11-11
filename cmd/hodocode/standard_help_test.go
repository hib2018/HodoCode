package main

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func TestStandardHelp(t *testing.T) {
	// Keep the required canonical targets independent of the catalog.
	targets := strings.Fields(`
		fmt.Print fmt.Println fmt.Printf fmt.Fprint fmt.Fprintln fmt.Fprintf
		fmt.Sprint fmt.Sprintln fmt.Sprintf fmt.Errorf
		errors.New errors.Is errors.As
		strings.Contains strings.HasPrefix strings.HasSuffix strings.TrimSpace
		strings.Split strings.Join strings.ReplaceAll
		strconv.Atoi strconv.Itoa strconv.ParseInt
		os.Open os.ReadFile os.WriteFile os.Stat
		io.WriteString io.ReadAll io.Copy
		context.Background context.WithCancel context.WithTimeout
		time.Now time.Sleep log.Print log.Println log.Printf
	`)
	if len(standardHelp) != len(targets) {
		t.Errorf("catalog has %d entries, want %d", len(standardHelp), len(targets))
	}
	for _, target := range targets {
		if _, ok := standardHelp[target]; !ok {
			t.Errorf("missing canonical target %q", target)
		}
	}

	imp := importer.Default()
	for target, help := range standardHelp {
		t.Run(target, func(t *testing.T) {
			for _, field := range []struct{ name, value string }{
				{"category", help.category}, {"title", help.title},
				{"description", help.description}, {"syntax", help.syntax},
				{"example", help.example}, {"caution", help.caution},
				{"reference", help.reference},
			} {
				if strings.TrimSpace(field.value) == "" {
					t.Errorf("empty %s", field.name)
				}
			}
			pkgPath, symbol, ok := strings.Cut(target, ".")
			if !ok || pkgPath == "" || symbol == "" {
				t.Fatalf("not a canonical package.Symbol target: %q", target)
			}
			if want := "https://pkg.go.dev/" + pkgPath + "#" + symbol; help.reference != want {
				t.Errorf("reference = %q, want %q", help.reference, want)
			}
			pkg, err := imp.Import(pkgPath)
			if err != nil {
				t.Fatalf("installed standard-library package unavailable: %v", err)
			}
			fn, ok := pkg.Scope().Lookup(symbol).(*types.Func)
			if !ok || !fn.Exported() {
				t.Fatalf("target %q is not an exported standard-library function", target)
			}
			// Compare the displayed signature with the installed API, qualifying
			// types from the target package too so standalone help is unambiguous.
			qualifier := func(p *types.Package) string { return p.Name() }
			wantSyntax := "func " + symbol + strings.TrimPrefix(types.TypeString(fn.Type(), qualifier), "func")
			if help.syntax != wantSyntax {
				t.Errorf("syntax = %q, want %q", help.syntax, wantSyntax)
			}
			if !strings.Contains(help.example, "\n") {
				t.Error("example must be multiline")
			}
			// Each example is its own temporary in-memory package. Never execute
			// examples: some intentionally demonstrate file writes or waiting.
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "example.go", help.example, parser.AllErrors)
			if err != nil {
				t.Fatalf("example does not parse: %v\n%s", err, help.example)
			}
			info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
			config := types.Config{Importer: imp, GoVersion: "go1.23"}
			if _, err := config.Check("standardhelp/example", fset, []*ast.File{file}, info); err != nil {
				t.Fatalf("example is not self-contained and well-typed: %v\n%s", err, help.example)
			}
			called := false
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && info.Uses[sel.Sel] == fn {
					called = true
				}
				return true
			})
			if !called {
				t.Errorf("example does not call resolved target %s", target)
			}
		})
	}
}

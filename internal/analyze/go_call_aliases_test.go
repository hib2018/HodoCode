package analyze

import (
	"reflect"
	"strings"
	"testing"
)

func TestGoCallAliases(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []string
	}{
		{"short", "f := Work; f(); f()", []string{"Work", "Work"}},
		{"var", "var f func() = (Work); (f)()", []string{"Work"}},
		{"incompatible definition", "var f func(int) = Work; f(1)", []string{"?f"}},
		{"named function type", "type Callback func(); var f Callback = Work; f()", []string{"Work"}},
		{"import", `f := strings.TrimSpace; f("x")`, []string{"strings.TrimSpace"}},
		{"defer and go", "f := Work; defer f(); go f()", []string{"Work", "Work"}},
		{"reassigned", "f := Work; f(); f = Other; f()", []string{"?f", "?f"}},
		{"same assignment", "f := Work; f = Work; f()", []string{"?f"}},
		{"address", "f := Work; _ = &(f); f()", []string{"?f"}},
		{"branch write", "f := Work; if yes { f = Other }; f()", []string{"?f"}},
		{"branch definition", "if yes { f := Work; f() }", []string{"Work"}},
		{"uninitialized", "var f func(); f(); f = Work; f()", []string{"?f", "?f"}},
		{"parameter", "dynamic()", []string{"?dynamic"}},
		{"parameter assigned", "dynamic = Work; dynamic()", []string{"?dynamic"}},
		{"global", "global()", []string{"?global"}},
		{"global alias", "f := global; f()", []string{"?f"}},
		{"chain", "f := Work; g := f; g(); f()", []string{"?g", "Work"}},
		{"shadow", "f := Work; { f := Other; f() }; f()", []string{"Other", "Work"}},
		{"shadow write", "f := Work; { f := Other; f = Work; f() }; f()", []string{"?f", "Work"}},
		{"parameter shadow", "dynamic(); { dynamic := Work; dynamic() }; dynamic()", []string{"?dynamic", "Work", "?dynamic"}},
		{"closure write", "f := Work; _ = func() { f = Other }; f()", []string{"?f"}},
		{"closure address", "f := Work; _ = func() { _ = &f }; f()", []string{"?f"}},
		{"closure calls omitted", "f := Work; _ = func() { f() }; f()", []string{"?f"}},
		{"unrelated closure", "f := Work; _ = func() { Other() }; f()", []string{"Work"}},
		{"range write", "f := Work; for _, f = range []func(){Other} {}; f()", []string{"?f"}},
		{"range shadow", "f := Work; for _, f := range []func(){Other} { f() }; f()", []string{"?f", "Work"}},
		{"interface", "i.Run(); f := i.Run; f()", []string{"?i.Run", "?f"}},
		{"method deferred", "f := v.Run; f()", []string{"?f"}},
		{"literal", "f := func() { Work() }; f()", []string{"?f"}},
		{"before short declaration", "f(); f := Work; f()", []string{"?f", "Work"}},
		{"before declaration with global", "global(); global := Work; global()", []string{"?global", "Work"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "go.mod", "module example.com/aliases\n")
			write(t, root, "main.go", `package aliases
import "strings"
var global = Work
func Work() {}
func Other() {}
type Value struct{}
func (Value) Run() {}
func Entry(dynamic func(), yes bool, i interface{ Run() }, v Value) {
`+tc.body+"\n}\n")
			p, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			id := "example.com/aliases.Entry"
			sites, err := p.CallSites(id)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, site := range sites {
				if site.Unknown {
					got = append(got, "?"+strings.TrimPrefix(site.To, "unknown: "))
				} else {
					got = append(got, strings.TrimPrefix(site.To, "example.com/aliases."))
				}
				if site.From != id || site.Name == "" || site.EndColumn-site.Column != len(site.Name) {
					t.Fatalf("invalid alias call location: %+v", site)
				}
				source, _ := p.Source(id)
				if source[site.Offset:site.Offset+len(site.Name)] != site.Name {
					t.Fatalf("invalid alias offset: %+v", site)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targets = %v, want %v", got, tc.want)
			}
		})
	}
}

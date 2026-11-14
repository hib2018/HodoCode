package analyze

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

func TestDefinitionsScopesAndSources(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/defs\n\ngo 1.23\n")
	model := "package model\n//line imaginary.go:800\ntype (\n Item struct { Name string } // 日本語\n Other int\n)\ntype Alias = Item\nvar Global = []Item{{Name: `original`}}\n"
	write(t, root, "model/model.go", model)
	write(t, root, "types.go", "package defs\ntype Local struct { N int }\nvar Global []Local\ntype Items []Local\n")
	entry := `func Entry(param []m.Alias, named Items) (result []Local) {
 // 日本語: offsets are bytes, not runes.
 var local = []Local{{N: 1}}
 local, extra := local, []Local{}
 _ = extra
 _ = local
 _ = Global
 _ = m.Global
 _ = param
 _ = named
 _ = result
 _ = m.Alias{}
 _ = new(Local)
 {
  local := []m.Item{}
  _ = local
 }
 closure := func(local []m.Item) {
  _ = local
  captured := param
  _ = captured
 }
 closure(nil)
 var holder struct { Values []Local }
 _ = holder.Values
 return
}`
	write(t, root, "entry.go", "package defs\nimport m \"example.com/defs/model\"\n"+entry+"\nfunc Other() { unavailable() }\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	const id = "example.com/defs.Entry"
	before, _ := p.CallSites(id)
	// Every snippet must use the snapshot, even for a different package.
	if err := os.WriteFile(filepath.Join(root, "model/model.go"), []byte("package changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := p.Definitions(id)
	if err != nil {
		t.Fatal(err)
	}
	group := "type (\n Item struct { Name string } // 日本語\n Other int\n)"
	localType := DefinitionSource{"types.go", 2, "type Local struct { N int }"}
	itemType := DefinitionSource{"model/model.go", 3, group}
	aliasType := DefinitionSource{"model/model.go", 7, "type Alias = Item"}
	check := func(marker, name string, want ...DefinitionSource) {
		t.Helper()
		offset := strings.Index(entry, marker)
		if offset < 0 {
			t.Fatalf("missing test marker %q", marker)
		}
		offset += strings.Index(marker, name)
		definition, exists := got[offset]
		if !exists || definition.Reason != "" || !reflect.DeepEqual(definition.Snippets, want) {
			t.Errorf("%q: got %+v (exists %v), want %+v", marker, definition, exists, want)
		}
	}
	check("param []m.Alias", "param", DefinitionSource{"entry.go", 3, "param []m.Alias"}, itemType)
	check("_ = param", "param", DefinitionSource{"entry.go", 3, "param []m.Alias"}, itemType)
	check("_ = named", "named", DefinitionSource{"entry.go", 3, "named Items"}, localType)
	check("_ = result", "result", DefinitionSource{"entry.go", 3, "result []Local"}, localType)
	check("_ = local\n _ = Global", "local", DefinitionSource{"entry.go", 5, "var local = []Local{{N: 1}}"}, localType)
	check("_ = extra", "extra", DefinitionSource{"entry.go", 6, "local, extra := local, []Local{}"}, localType)
	check("_ = Global", "Global", DefinitionSource{"types.go", 3, "var Global []Local"}, localType)
	check("_ = m.Global", "Global", DefinitionSource{"model/model.go", 8, "var Global = []Item{{Name: `original`}}"}, itemType)
	check("_ = m.Alias{}", "Alias", aliasType, itemType)
	check("new(Local)", "Local", localType)
	check("local := []m.Item", "local", DefinitionSource{"entry.go", 17, "local := []m.Item{}"}, itemType)
	check("local []m.Item", "local", DefinitionSource{"entry.go", 20, "local []m.Item"}, itemType)
	check("_ = captured", "captured", DefinitionSource{"entry.go", 22, "captured := param"}, itemType)
	check("holder.Values", "Values", DefinitionSource{"entry.go", 26, "Values []Local"}, localType)
	for offset, definition := range got {
		if offset < 0 || offset >= len(entry) || (definition.Reason != "" && !strings.Contains(definition.Reason, "組み込み")) {
			t.Errorf("unexpected offset/reason: %d %+v", offset, definition)
		}
	}
	for _, marker := range []string{"closure(nil)"} {
		if _, exists := got[strings.Index(entry, marker)]; exists {
			t.Errorf("unrelated identifier included: %q", marker)
		}
	}
	after, _ := p.CallSites(id)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("Definitions changed call analysis: before %+v, after %+v", before, after)
	}
	again, err := p.Definitions(id)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("repeat lookup changed: %+v, %v", again, err)
	}
}

func TestDefinitionsUnknownAndExternal(t *testing.T) {
	fakeSDK(t, "time", "package time\ntype Time struct { N int }\n")
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/defs\n")
	entry := `func Entry() {
 var values []time.Time
 _ = values
 _ = time.Time{}
 var missing unavailable.Type
 _ = missing
 type Bad = unavailable.Type
 var unresolved []Bad
 _ = unresolved
 absent()
 println(len(values))
 _ = true
}`
	write(t, root, "entry.go", "package defs\nimport \"time\"\n"+entry)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Definitions("example.com/defs.Entry")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"missing\n", "unavailable.Type", "Type\n", "Bad =", "Bad\n", "unresolved\n", "absent()", "println(", "len(", "true\n"} {
		offset := strings.Index(entry, marker)
		definition, ok := got[offset]
		if !ok || !strings.ContainsFunc(definition.Reason, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			t.Errorf("%q: missing Japanese reason: %+v", marker, definition)
		}
	}
	values := got[strings.Index(entry, "values\n")]
	if values.Reason != "" || len(values.Snippets) != 2 || values.Snippets[0].Source != "var values []time.Time" || values.Snippets[1].File != "GOROOT/src/time/sdk.go" {
		t.Fatalf("external slice element: %+v", values)
	}
	for _, marker := range []string{"Entry()", "time.Time"} {
		if _, ok := got[strings.Index(entry, marker)]; ok {
			t.Errorf("noise identifier included: %q", marker)
		}
	}
	if _, err := p.Definitions("missing"); err == nil {
		t.Fatal("unknown ID accepted")
	}
}

func TestDefinitionsLocalAliasesAndShadowedTypes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/defs\n")
	entry := `func Entry() {
 type Value struct { Outer int }
 type Alias = Value
 values := []*Alias{}
 _ = values
 {
  type Value struct { Inner string }
  var values []Value
  _ = values
 }
}`
	write(t, root, "entry.go", "package defs\n"+entry)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Definitions("example.com/defs.Entry")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		offset int
		want   []DefinitionSource
	}{
		{strings.Index(entry, "_ = values") + 4, []DefinitionSource{{"entry.go", 5, "values := []*Alias{}"}, {"entry.go", 3, "type Value struct { Outer int }"}}},
		{strings.LastIndex(entry, "_ = values") + 4, []DefinitionSource{{"entry.go", 9, "var values []Value"}, {"entry.go", 8, "type Value struct { Inner string }"}}},
	} {
		if definition := got[tc.offset]; definition.Reason != "" || !reflect.DeepEqual(definition.Snippets, tc.want) {
			t.Errorf("offset %d: got %+v, want %+v", tc.offset, definition, tc.want)
		}
	}
}

func TestDefinitionsRangeAndEmbeddedSlice(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/defs\n")
	write(t, root, "types.go", "package defs\ntype Item struct { Name string }\ntype Items []Item\ntype Holder struct { Items }\n")
	entry := `func Entry(rows [][]Item, holder Holder) {
 for _, values := range rows {
  _ = values
 }
 _ = holder.Items
}`
	write(t, root, "entry.go", "package defs\n"+entry)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := p.Definitions("example.com/defs.Entry")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		marker, name string
		want         DefinitionSource
	}{
		{"_ = values", "values", DefinitionSource{"entry.go", 3, "for _, values := range rows"}},
		{"holder.Items", "Items", DefinitionSource{"types.go", 4, "Items"}},
	} {
		offset := strings.Index(entry, tc.marker) + strings.Index(tc.marker, tc.name)
		got := definitions[offset]
		want := []DefinitionSource{tc.want, {"types.go", 2, "type Item struct { Name string }"}}
		if got.Reason != "" || !reflect.DeepEqual(got.Snippets, want) {
			t.Errorf("%s: got %+v, want %+v", tc.marker, got, want)
		}
	}
}

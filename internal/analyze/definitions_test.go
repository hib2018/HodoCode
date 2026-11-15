package analyze

import (
	"go/ast"
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
	check("closure(nil)", "closure", DefinitionSource{"entry.go", 20, "closure := func(local []m.Item) {\n  _ = local\n  captured := param\n  _ = captured\n }"})
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

func TestDefinitionsAllVariablesIdentityAndSnapshot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/defs\n\ngo 1.23\n")
	entry := `func (receiver *Thing) Entry(param int) (result string) {
 var scalar int
 var text = "snapshot"
 var pointer *Thing
 var record Thing
 var array [2]int
 var mapping map[string]int
 var channel chan int
 var dynamic any
 var broken Missing
 数字 := 1
 scalar, extra := scalar, true
 closure := func(inner int) int { return inner + param + scalar }
 for index, value := range array {
  _, _ = index, value
 }
 {
  scalar := 2
  _ = scalar
 }
 _, _, _, _, _, _, _, _, _, _, _, _, _ = receiver, result, scalar, text, pointer, record, mapping, channel, dynamic, broken, 数字, extra, closure
 _ = record.Field
 absent()
 println(param)
 return
}`
	write(t, root, "entry.go", "package defs\ntype Thing struct { Field int }\n"+entry)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	const id = "example.com/defs.Thing.Entry"
	fn := p.declarations[id]
	if fn == nil {
		t.Fatalf("missing method %s", id)
	}
	// Definitions, positions, and source bytes must not follow disk edits.
	write(t, root, "entry.go", "package changed\n")
	got, err := p.Definitions(id)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]string{}
	for _, tc := range []struct {
		name, declaration, reference, source string
	}{
		{"receiver", "receiver *Thing", "receiver, result", "receiver *Thing"},
		{"param", "param int", "param + scalar", "param int"},
		{"result", "result string", "result, scalar", "result string"},
		{"scalar", "scalar int", "scalar, text", "var scalar int"},
		{"text", "text =", "text, pointer", `var text = "snapshot"`},
		{"pointer", "pointer *Thing", "pointer, record", "var pointer *Thing"},
		{"record", "record Thing", "record.Field", "var record Thing"},
		{"array", "array [2]int", "array {", "var array [2]int"},
		{"mapping", "mapping map", "mapping, channel", "var mapping map[string]int"},
		{"channel", "channel chan", "channel, dynamic", "var channel chan int"},
		{"dynamic", "dynamic any", "dynamic, broken", "var dynamic any"},
		{"broken", "broken Missing", "broken, 数字", "var broken Missing"},
		{"数字", "数字 :=", "数字, extra", "数字 := 1"},
		{"extra", "extra :=", "extra, closure", "scalar, extra := scalar, true"},
		{"closure", "closure :=", "closure\n", "closure := func(inner int) int { return inner + param + scalar }"},
		{"inner", "inner int", "inner +", "inner int"},
		{"index", "index, value :=", "index, value\n", "for index, value := range array"},
		{"value", "value := range", "value\n", "for index, value := range array"},
		{"scalar", "scalar := 2", "scalar\n }", "scalar := 2"},
	} {
		declOffset := strings.Index(entry, tc.declaration)
		refOffset := strings.Index(entry, tc.reference)
		if declOffset < 0 || refOffset < 0 {
			t.Fatalf("bad markers: %+v", tc)
		}
		wantID := int(fn.Pos()) + declOffset
		if previous, exists := seen[wantID]; exists {
			t.Errorf("declarations %q and %q share ID %d", previous, tc.declaration, wantID)
		}
		seen[wantID] = tc.declaration
		for _, offset := range []int{declOffset, refOffset} {
			definition, exists := got[offset]
			if !exists || definition.VariableID != wantID || definition.Length != len(tc.name) || definition.Unresolved || !definition.PopupOnly || len(definition.Snippets) != 1 || definition.Snippets[0].Source != tc.source {
				t.Errorf("%q at %d: got %+v; want ID %d, length %d, source %q", tc.name, offset, definition, wantID, len(tc.name), tc.source)
			}
			if (definition.Reason != "") != (tc.name == "broken") {
				t.Errorf("%q: unexpected reason %q", tc.name, definition.Reason)
			}
		}
	}
	// Redeclaration reuses the outer variable, including inside a closure.
	outerID := got[strings.Index(entry, "scalar int")].VariableID
	for _, marker := range []string{"scalar, extra :=", "scalar, true", "scalar }"} {
		if definition := got[strings.Index(entry, marker)]; definition.VariableID != outerID {
			t.Errorf("%q changed variable identity: %+v", marker, definition)
		}
	}
	field := got[strings.Index(entry, "Field")]
	if field.VariableID == 0 || len(field.Snippets) != 1 || field.Snippets[0].Source != "Field int" {
		t.Errorf("field variable: %+v", field)
	}
	for _, tc := range []struct {
		marker     string
		unresolved bool
	}{
		{"absent()", true},
		{"Missing", true},
		{"println(", false},
		{"Thing", false},
	} {
		definition, exists := got[strings.Index(entry, tc.marker)]
		if !exists || definition.VariableID != 0 || definition.Unresolved != tc.unresolved {
			t.Errorf("%q: %+v (exists %v)", tc.marker, definition, exists)
		}
	}
	ast.Inspect(fn, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			if definition, exists := got[int(ident.Pos()-fn.Pos())]; exists && definition.Length != len(ident.Name) {
				t.Errorf("%s: length %d, want %d", ident.Name, definition.Length, len(ident.Name))
			}
		}
		return true
	})
	again, err := p.Definitions(id)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("snapshot lookup changed: %+v, %v", again, err)
	}
}

func TestDefinitionsPopupOnlyPreservesLegacyHelp(t *testing.T) {
	fakeSDK(t, "time", "package time\nvar Scalar int\nvar Values []int\n")
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/defs\n")
	entry := `func Entry(scalar int, values []int, named Values) {
 var invalid Missing
 var invalidSlice []Missing
 _, _, _, _, _, _, _ = scalar, values, named, invalid, invalidSlice, time.Scalar, time.Values
 absent()
 println(scalar)
}`
	write(t, root, "entry.go", "package defs\nimport \"time\"\ntype Values []int\n"+entry)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Definitions("example.com/defs.Entry")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		marker               string
		popupOnly, hasReason bool
	}{
		{"scalar, values", true, false},
		{"values, named", false, false},
		{"named, invalid", false, false},
		{"invalid, invalidSlice", true, true},
		{"invalidSlice, time", false, true},
		{"Scalar, time", false, false},
		{"Values\n", false, false},
	} {
		definition, exists := got[strings.Index(entry, tc.marker)]
		if !exists || definition.PopupOnly != tc.popupOnly || (definition.Reason != "") != tc.hasReason || len(definition.Snippets) != 1 {
			t.Errorf("%q: got %+v; want PopupOnly %v, reason %v, one snippet", tc.marker, definition, tc.popupOnly, tc.hasReason)
		}
	}
	for _, marker := range []string{"Missing", "absent()", "println(", "Values)"} {
		if definition := got[strings.Index(entry, marker)]; definition.PopupOnly {
			t.Errorf("nonvariable %q marked popup-only: %+v", marker, definition)
		}
	}
}

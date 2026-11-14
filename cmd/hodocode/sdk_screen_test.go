package main

import (
	"go/build"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func fakeScreenSDK(t *testing.T, files map[string]string) {
	t.Helper()
	previous := build.Default
	t.Cleanup(func() { build.Default = previous })
	build.Default.GOROOT = t.TempDir()
	for name, source := range files {
		path := filepath.Join(build.Default.GOROOT, "src", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Neither SDK nor dependency inspection may invoke Go or use the network.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
}

func sdkScreen(t *testing.T) *screen {
	t.Helper()
	fakeScreenSDK(t, map[string]string{"fakeio/sdk.go": `package fakeio
type Number int
type Alias = Number
type Item struct { Field int }
type Reader interface { Read() int }
const Answer Number = 42
var Scalar = 7
var Values []Item
func New() *Item { return &Item{Field: Scalar} }
func (i *Item) Read() int { return helper(i.Field) }
func helper(n int) int { return n + int(Answer) }
`})
	p := screenProject(t, map[string]string{"main.go": `package main
import "fakeio"
func main() {
 item := fakeio.New()
 _ = item.Read()
 _ = item.Field
 _ = fakeio.Answer
 _ = fakeio.Scalar
 _ = fakeio.Values
 _ = fakeio.Number(1)
 _ = fakeio.Alias(1)
 var reader fakeio.Reader
 _ = reader
}
`})
	s, err := newScreen(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSDKScreenAllObjectDefinitions(t *testing.T) {
	s := sdkScreen(t)
	for _, tc := range []struct{ marker, name, source string }{
		{"fakeio.New()", "New", "func New() *Item { return &Item{Field: Scalar} }"},
		{"item.Read()", "Read", "func (i *Item) Read() int { return helper(i.Field) }"},
		{"item.Field", "Field", "Field int"},
		{"fakeio.Answer", "Answer", "const Answer Number = 42"},
		{"fakeio.Scalar", "Scalar", "var Scalar = 7"},
		{"fakeio.Values", "Values", "var Values []Item"},
		{"fakeio.Number(1)", "Number", "type Number int"},
		{"fakeio.Alias(1)", "Alias", "type Alias = Number"},
		{"var reader fakeio.Reader", "Reader", "type Reader interface { Read() int }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pointDefinition(t, s, tc.marker, tc.name)
			before := s.screenPosition
			pane := strings.Join(s.explanationLines(200, 30), "\n")
			if !strings.Contains(pane, "GOROOT/src/fakeio/sdk.go") || !strings.Contains(pane, tc.source) || strings.Contains(pane, "索引範囲外") {
				t.Fatalf("missing SDK definition %q: %s", tc.source, pane)
			}
			if s.screenPosition != before || len(s.history) != 0 || len(s.flow.expanded) != 1 {
				t.Fatal("definition lookup navigated or expanded calls")
			}
		})
	}
}

func TestSDKScreenSourceEnterAndBack(t *testing.T) {
	s := sdkScreen(t)
	rootIDs := functionIDs(s.p)
	if !reflect.DeepEqual(rootIDs, []string{"example.com/screen.main"}) {
		t.Fatal(rootIDs)
	}
	for _, tc := range []struct{ marker, name, id, source string }{
		{"fakeio.New()", "New", "fakeio.New", "func New() *Item"},
		{"item.Read()", "Read", "fakeio.Item.Read", "func (i *Item) Read() int"},
	} {
		pointDefinition(t, s, tc.marker, tc.name)
		s.selected = s.callAtCursor()
		s.followCursor(6, 30)
		before := s.screenPosition
		if i := s.callAtCursor(); i < 0 || s.reason(s.sites[i]) != "" {
			t.Fatalf("SDK call %s unavailable", tc.id)
		}
		if !s.flow.nodes[tc.id] {
			t.Fatalf("direct SDK call %s missing from flow", tc.id)
		}
		press(t, s, "\r")
		if s.current != tc.id || len(s.history) != 1 || !strings.Contains(strings.Join(s.code, "\n"), tc.source) || !s.flow.expanded[tc.id] {
			t.Fatalf("Enter did not show SDK source: %+v", s.screenPosition)
		}
		if tc.id == "fakeio.Item.Read" {
			if len(s.sites) != 1 || s.sites[0].To != "fakeio.helper" || !s.flow.nodes["fakeio.helper"] {
				t.Fatalf("nested SDK call missing: %+v", s.sites)
			}
			method := s.screenPosition
			press(t, s, "\r")
			if s.current != "fakeio.helper" || len(s.history) != 2 {
				t.Fatal("nested SDK Enter failed")
			}
			press(t, s, "b")
			if s.screenPosition != method {
				t.Fatal("back did not restore SDK method")
			}
		}
		press(t, s, "b")
		if s.screenPosition != before || len(s.history) != 0 {
			t.Fatal("back did not restore root cursor and viewport")
		}
	}
	if !reflect.DeepEqual(functionIDs(s.p), rootIDs) || len(s.p.Functions) <= len(rootIDs) {
		t.Fatal("SDK navigation changed root-only function selection", functionIDs(s.p))
	}
}

func TestSDKScreenTreeSelectedAndNestedEnter(t *testing.T) {
	s := sdkScreen(t)
	before := s.screenPosition
	press(t, s, "t", "j") // New is selected, but do not enter it.
	if s.tree.selected.site.To != "fakeio.New" || s.tree.selected.loaded {
		t.Fatal("selection expanded SDK function")
	}
	press(t, s, "j", " ") // Expand Read, not the source cursor's New.
	method := s.tree.selected
	if method.site.To != "fakeio.Item.Read" || !method.open || len(method.children) != 1 || method.children[0].site.To != "fakeio.helper" || s.flow.expanded["fakeio.New"] {
		t.Fatal("tree did not lazily expand selected SDK method")
	}
	press(t, s, "\r")
	if s.current != "fakeio.Item.Read" || s.tree.visible || len(s.history) != 1 {
		t.Fatal("tree Enter followed source cursor instead of selected SDK method")
	}
	press(t, s, "b")
	if s.screenPosition != before {
		t.Fatal("tree jump back lost root position")
	}
	press(t, s, "t", "j", "j", "j", "\r") // New, Read, helper.
	if s.current != "fakeio.helper" || s.tree.visible || !s.flow.expanded["fakeio.helper"] || len(s.history) != 1 {
		t.Fatal("tree Enter did not navigate nested SDK function")
	}
	press(t, s, "b")
	if s.screenPosition != before || !reflect.DeepEqual(functionIDs(s.p), []string{"example.com/screen.main"}) {
		t.Fatal("nested tree jump changed root state or function list")
	}
}

func TestMissingSDKScreenStaysOpen(t *testing.T) {
	fakeScreenSDK(t, nil)
	p := screenProject(t, map[string]string{"main.go": "package main\nimport \"absentsdk\"\nfunc main() { absentsdk.Run() }\n"})
	s, err := newScreen(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := s.screenPosition
	if len(s.sites) != 1 || s.reason(s.sites[0]) == "" {
		t.Fatal("missing SDK must be unavailable")
	}
	press(t, s, "\r", "\n", "t", "j", " ", "\r")
	if !s.tree.visible || s.tree.selected.open || s.screenPosition != before || len(s.history) != 0 || len(s.flow.expanded) != 1 {
		t.Fatal("missing SDK closed popup, navigated, or expanded")
	}
	press(t, s, "esc", "j", "k")
	if s.screenPosition != before {
		t.Fatal("screen unusable after missing SDK Enter")
	}
}

func TestExternalReplacePaneWithoutNavigation(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	t.Setenv("GOTOOLCHAIN", "local")
	dep, root := t.TempDir(), t.TempDir()
	for path, source := range map[string]string{
		filepath.Join(dep, "go.mod"): "module example.com/dep\n\ngo 1.23\n",
		filepath.Join(dep, "dep.go"): `package dep
type Item struct { Field int }
var Values []Item
func Run() {}
`,
		filepath.Join(root, "go.mod"): "module example.com/app\n\ngo 1.23\nrequire example.com/dep v1.0.0\nreplace example.com/dep => " + filepath.ToSlash(dep) + "\n",
		filepath.Join(root, "main.go"): `package main
import "example.com/dep"
func main() {
 _ = dep.Item{}
 _ = dep.Values
 dep.Run()
}
`,
	} {
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newScreen(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ marker, name, source string }{
		{"dep.Item{}", "Item", "type Item struct { Field int }"},
		{"dep.Values", "Values", "var Values []Item"},
	} {
		pointDefinition(t, s, tc.marker, tc.name)
		pane := strings.Join(s.explanationLines(200, 30), "\n")
		if !strings.Contains(pane, "dep.go") || !strings.Contains(pane, tc.source) || strings.Contains(pane, "索引範囲外") {
			t.Fatalf("external definition missing: %s", pane)
		}
	}
	pointDefinition(t, s, "dep.Run()", "Run")
	before := s.screenPosition
	if len(s.sites) != 1 || !s.sites[0].Unknown || s.sites[0].To != "unknown: dep.Run" || s.reason(s.sites[0]) == "" || s.flow.nodes["example.com/dep.Run"] {
		t.Fatalf("external function escaped the structs/slices-only lookup: %+v", s.sites)
	}
	if pane := strings.Join(s.explanationLines(200, 30), "\n"); strings.Contains(pane, "func Run()") || !strings.Contains(pane, "意味") {
		t.Fatal("external function displayed source instead of fallback help", pane)
	}
	press(t, s, "\r", "t", "j", " ", "\r")
	if !s.tree.visible || s.tree.selected.open || s.screenPosition != before || len(s.history) != 0 || len(s.flow.expanded) != 1 {
		t.Fatal("external call navigated, expanded, or closed tree")
	}
	press(t, s, "esc")
	if !reflect.DeepEqual(functionIDs(p), []string{"example.com/app.main"}) {
		t.Fatal("external definitions entered function selection")
	}
}

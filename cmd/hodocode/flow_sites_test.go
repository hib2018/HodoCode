package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func TestFlowCachesSitesAndPreservesEdges(t *testing.T) {
	root := t.TempDir()
	for name, source := range map[string]string{
		"go.mod": "module example.com/flow\n",
		"main.go": `package sample
import "strings"
func Entry(dynamic func()) {
 Z(); Z(); A(); Z()
 dynamic(); dynamic()
 strings.TrimSpace("x"); strings.TrimSpace("y")
}
func A() {}
func Z() {}
func Unreachable() {}
`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "example.com/flow.Entry"
	wantSites, err := p.CallSites(id)
	if err != nil {
		t.Fatal(err)
	}
	wantEdges, err := p.Calls(id)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newFlow(p, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(wantSites) != 8 || len(wantEdges) != 4 {
		t.Fatalf("fixture: sites=%+v edges=%+v", wantSites, wantEdges)
	}
	if !reflect.DeepEqual(s.sites[id], wantSites) || !reflect.DeepEqual(s.edges, wantEdges) {
		t.Fatalf("sites/edges changed: sites=%+v edges=%+v", s.sites[id], s.edges)
	}
	if len(s.sites) != 1 || s.expanded["example.com/flow.Z"] {
		t.Fatal("callee was eagerly analyzed")
	}
	wantDiagram := analyze.MermaidNodes(wantEdges, []string{id, "example.com/flow.A", "example.com/flow.Z"})
	if got := s.diagram(); got != wantDiagram {
		t.Fatalf("diagram = %q, want %q", got, wantDiagram)
	}
	if err := s.expand(id); err != nil || !reflect.DeepEqual(s.sites[id], wantSites) || !reflect.DeepEqual(s.edges, wantEdges) {
		t.Fatalf("repeated expansion changed sites/edges: %+v, %v", s, err)
	}
	if err := s.expand("example.com/flow.Unreachable"); err == nil || len(s.sites) != 1 {
		t.Fatal("unreachable expansion accepted or cached")
	}
	if err := s.expand("example.com/flow.A"); err != nil {
		t.Fatal(err)
	}
	if sites, ok := s.sites["example.com/flow.A"]; !ok || len(sites) != 0 {
		t.Fatalf("leaf sites not cached: %v", s.sites)
	}
	if !reflect.DeepEqual(s.edges, wantEdges) || s.diagram() != wantDiagram {
		t.Fatal("leaf expansion changed edges/diagram")
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"flow", "--root", root, "--func", "Entry"}, strings.NewReader(""), &out, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != wantDiagram {
		t.Fatalf("flow output = %q, want %q", out.String(), wantDiagram)
	}
	out.Reset()
	if err := interactive(p, id, nil, strings.NewReader("d\nq\n"), &out); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		"├─ 1: A\n├─ 2: Z\n",
		"外部ソース: 1件、呼び出し先不明: 1件",
		"├─ strings.TrimSpace（外部ソース・選択不可）",
		"└─ unknown: dynamic（呼び出し先不明・選択不可）",
	} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("line UI missing %q: %s", text, out.String())
		}
	}
}

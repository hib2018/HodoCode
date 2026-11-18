package semantictest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hib2018/HodoCode/internal/semantic"
)

func TestFixtureGenericSymbols(t *testing.T) {
	const source = "fn main<T: Trait>() { T::run(); }\n"
	got := symbols(source)
	if len(got) != 1 || got[0].Name != "main" || got[0].SelectionRange.Start.Character != 3 || got[0].Range.End.Character != len(source)-1 {
		t.Fatalf("generic symbols = %+v", got)
	}
}

func TestFixtureServer(t *testing.T) {
	Use(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	root := t.TempDir()
	client, err := semantic.Start(ctx, []string{"typescript-language-server", "--stdio"}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	file := filepath.Join(root, "app.js")
	const source = "function entry() { return other(); } function other() { return 2; }\n"
	if err := client.Open(ctx, file, "javascript", []byte(source)); err != nil {
		t.Fatal(err)
	}
	got, err := client.Symbols(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "entry" || got[0].Range.End.Character != 36 || got[1].SelectionRange.Start.Character != 46 {
		t.Fatalf("symbols = %+v", got)
	}
	locations, err := client.Definitions(ctx, file, semantic.Position{Line: 0, Character: 26})
	if err != nil || len(locations) != 1 || locations[0].Range != got[1].SelectionRange {
		t.Fatalf("definition = %+v, %v", locations, err)
	}
	locations, err = client.Definitions(ctx, file, semantic.Position{Line: 0, Character: 20})
	if err != nil || len(locations) != 0 {
		t.Fatalf("unknown definition = %+v, %v", locations, err)
	}
}

func TestFixtureServerBuildProgress(t *testing.T) {
	Use(t)
	t.Setenv("HODOCODE_TEST_LSP_BUILD_PROGRESS", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	root := t.TempDir()
	client, err := semantic.Start(ctx, []string{"zls"}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	file := filepath.Join(root, "main.zig")
	const source = "fn run() void {}\nfn main() void { run(); }\n"
	if err := client.Open(ctx, file, "zig", []byte(source)); err != nil {
		t.Fatal(err)
	}
	pos := semantic.Position{Line: 1, Character: 17}
	locations, err := client.Definitions(ctx, file, pos)
	if err != nil || len(locations) != 0 {
		t.Fatalf("first definition must be null in build-progress mode: %+v, %v", locations, err)
	}
	ready, err := client.WaitReady(ctx, time.Second)
	if err != nil || !ready {
		t.Fatalf("build progress was not observed: ready=%v, err=%v", ready, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		locations, err = client.Definitions(ctx, file, pos)
		if err != nil || len(locations) != 1 || locations[0].URI != semantic.URI(file) || locations[0].Range.Start != (semantic.Position{Line: 0, Character: 3}) {
			t.Fatalf("definition after build progress (attempt %d): %+v, %v", attempt, locations, err)
		}
	}
}

func TestFixtureRanges(t *testing.T) {
	const source = "def entry():\r\n    value = '''secret\r\nmore'''\r\n    return value\r\n"
	got := symbols(source)
	if len(got) != 1 || got[0].Range != (span{position{0, 0}, position{3, 16}}) || got[0].SelectionRange != (span{position{0, 4}, position{0, 9}}) {
		t.Fatalf("Python ranges = %+v", got)
	}
	if got := at("😀名前", len("😀名")); got != (position{0, 3}) {
		t.Fatalf("UTF-16 position = %+v", got)
	}
}

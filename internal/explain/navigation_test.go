package explain

import (
	"context"
	"go/build"
	"reflect"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func TestPrepareAfterSDKNavigation(t *testing.T) {
	previous := build.Default
	t.Cleanup(func() { build.Default = previous })
	build.Default.GOROOT = t.TempDir()
	write(t, build.Default.GOROOT, "src/fakesdk/sdk.go", "package fakesdk\nfunc External() { SDK_BODY_MARKER() }\n")

	root := t.TempDir()
	write(t, root, "go.mod", "module example.org/navigation\n\ngo 1.23\n")
	write(t, root, "main.go", "package navigation\nfunc Local() {}\n")
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	const local = "example.org/navigation.Local"
	const sdk = "fakesdk.External"
	edges := []analyze.Edge{
		{From: local, To: sdk},
		{From: local, To: "external.PRIVATE_LABEL"},
		{From: local, To: "external.UNKNOWN_LABEL", Unknown: true},
		{From: sdk, To: local},
	}
	capture := func() []string {
		t.Helper()
		r, err := Prepare(p, local, "explain local", edges, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.candidates) != 1 || r.candidates[0].ID != local || len(r.snippets) != 1 {
			t.Fatalf("nonlocal candidates or snippets: %+v, %v", r.candidates, r.snippets)
		}
		if want := []link{{From: local, To: "unknown", Unknown: true}}; !reflect.DeepEqual(r.graph, want) {
			t.Fatalf("graph = %+v, want %+v", r.graph, want)
		}
		var prompts []string
		_, err = r.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
			prompts = append(prompts, prompt)
			for _, forbidden := range []string{sdk, "GOROOT", "SDK_BODY_MARKER", "PRIVATE_LABEL", "UNKNOWN_LABEL"} {
				if strings.Contains(prompt, forbidden) {
					t.Errorf("payload leaked %q", forbidden)
				}
			}
			if len(prompts) == 1 {
				return `["` + local + `"]`, nil
			}
			return "done", nil
		})
		if err != nil || len(prompts) != 2 {
			t.Fatalf("Explain: err=%v calls=%d", err, len(prompts))
		}
		return prompts
	}
	before := capture()
	if err := p.EnsureFunction(sdk); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Functions[sdk]; !ok || p.LocalFunction(sdk) {
		t.Fatal("SDK function was not lazily registered as nonlocal")
	}
	if after := capture(); !reflect.DeepEqual(before, after) {
		t.Fatal("outbound payload changed after SDK navigation")
	}
	if r, err := Prepare(p, sdk, "explain SDK", edges, nil); err == nil || r != nil {
		t.Fatalf("accepted SDK target: request=%v err=%v", r, err)
	}
}

package analyze

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDependencyAutoVendorConditions(t *testing.T) {
	cases := []struct {
		name, goVersion, flags                              string
		workspace, workspaceMarker, configFlags, wantVendor bool
	}{
		{name: "old-module", goVersion: "1.13"},
		{name: "no-go-directive"},
		{name: "auto-module", goVersion: "1.14", wantVendor: true},
		{name: "workspace-manifest-in-module", goVersion: "1.23", workspaceMarker: true},
		{name: "auto-workspace", goVersion: "1.23", workspace: true, workspaceMarker: true, wantVendor: true},
		{name: "module-manifest-in-workspace", goVersion: "1.23", workspace: true},
		{name: "readonly-disables-vendor", goVersion: "1.23", flags: "-mod=readonly"},
		{name: "mod-disables-vendor", goVersion: "1.23", flags: "-mod=mod"},
		{name: "quoted-mod-disables-vendor", goVersion: "1.23", flags: "'-mod=mod'"},
		{name: "config-disables-vendor", goVersion: "1.23", flags: "-mod=readonly", configFlags: true},
		{name: "explicit-vendor-old-module", goVersion: "1.13", flags: "-mod=vendor", wantVendor: true},
		{name: "workspace-readonly", goVersion: "1.23", workspace: true, workspaceMarker: true, flags: "-mod=readonly"},
		{name: "workspace-config-mod", goVersion: "1.23", workspace: true, workspaceMarker: true, flags: "-mod=mod", configFlags: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offlineTestEnv(t)
			base := t.TempDir()
			root := base
			if tc.workspace {
				root = filepath.Join(base, "app")
				write(t, base, "go.work", "go "+tc.goVersion+"\nuse ./app\n")
				t.Setenv("GOWORK", filepath.Join(base, "go.work"))
			}
			dep := t.TempDir()
			write(t, dep, "go.mod", "module example.com/dep\n\ngo 1.13\n")
			write(t, dep, "dep.go", "package dep\ntype Item struct { Actual int }\n")
			directive := ""
			if tc.goVersion != "" {
				directive = "go " + tc.goVersion + "\n"
			}
			write(t, root, "go.mod", "module example.com/app\n"+directive+"require example.com/dep v1.0.0\nreplace example.com/dep => "+filepath.ToSlash(dep)+"\n")
			write(t, root, "app.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{} }\n")
			manifest := "# example.com/dep v1.0.0 => " + filepath.ToSlash(dep) + "\n## explicit; go 1.13\nexample.com/dep\n# example.com/dep => " + filepath.ToSlash(dep) + "\n"
			if tc.workspaceMarker {
				manifest = "## workspace\n" + manifest
			}
			write(t, base, "vendor/modules.txt", manifest)
			write(t, base, "vendor/example.com/dep/dep.go", "package dep\ntype Item struct { Vendored int }\n")
			flags := tc.flags + " -overlay=/must-not-read.json"
			if tc.configFlags {
				cfg := t.TempDir()
				write(t, cfg, "goenv", "GOFLAGS="+flags+"\n")
				t.Setenv("GOENV", filepath.Join(cfg, "goenv"))
				t.Setenv("GOFLAGS", "")
			} else {
				t.Setenv("GOFLAGS", flags)
			}
			before, err := os.ReadFile(filepath.Join(root, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			p, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			def := externalDefinition(t, p, "example.com/app.Entry", "Item{}")
			want := "Actual int"
			if tc.wantVendor {
				want = "Vendored int"
			}
			if def.Reason != "" || len(def.Snippets) != 1 || !strings.Contains(def.Snippets[0].Source, want) {
				t.Fatalf("want %s, got %+v", want, def)
			}
			if strings.Contains(def.Snippets[0].File, ", vendor]") != tc.wantVendor {
				t.Fatalf("vendor origin: %+v", def)
			}
			after, err := os.ReadFile(filepath.Join(root, "go.mod"))
			if err != nil || string(after) != string(before) {
				t.Fatalf("module mutated: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "go.sum")); !os.IsNotExist(err) {
				t.Fatalf("created go.sum: %v", err)
			}
			if tc.workspace {
				if _, err := os.Stat(filepath.Join(base, "go.work.sum")); !os.IsNotExist(err) {
					t.Fatalf("created workspace sum: %v", err)
				}
			}
		})
	}
}

func TestDefinitionsResolveIndexedModuleFromCaller(t *testing.T) {
	for _, selection := range []string{"replacement", "workspace-outside", "workspace-indexed", "unavailable"} {
		t.Run(selection, func(t *testing.T) {
			offlineTestEnv(t)
			root := t.TempDir()
			outside := t.TempDir()
			write(t, outside, "go.mod", "module example.com/dep\n\ngo 1.23\n")
			write(t, outside, "dep.go", "package dep\ntype Item struct { SelectedOutside int }\nfunc Build() {}\n")
			write(t, root, "nested/go.mod", "module example.com/dep\n\ngo 1.23\n")
			write(t, root, "nested/dep.go", "package dep\ntype Item struct { Indexed int }\nfunc Build() {}\n")
			mod := "module example.com/app\n\ngo 1.23\nrequire example.com/dep v1.0.0\n"
			if selection == "replacement" {
				mod += "replace example.com/dep => " + filepath.ToSlash(outside) + "\n"
			}
			write(t, root, "go.mod", mod)
			if strings.HasPrefix(selection, "workspace-") {
				used := outside
				if selection == "workspace-indexed" {
					used = filepath.Join(root, "nested")
				}
				write(t, root, "go.work", "go 1.23\nuse (\n .\n "+filepath.ToSlash(used)+"\n)\n")
				t.Setenv("GOWORK", filepath.Join(root, "go.work"))
			}
			write(t, root, "app.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{}; dep.Build() }\n")
			p, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			id := "example.com/app.Entry"
			def := externalDefinition(t, p, id, "Item{}")
			if selection == "unavailable" {
				if def.Reason == "" || len(def.Snippets) != 0 {
					t.Fatalf("unselected index used: %+v", def)
				}
			} else {
				want := "SelectedOutside int"
				if selection == "workspace-indexed" {
					want = "Indexed int"
				}
				if def.Reason != "" || len(def.Snippets) != 1 || !strings.Contains(def.Snippets[0].Source, want) {
					t.Fatalf("wrong package selected: %+v", def)
				}
				if !strings.HasPrefix(def.Snippets[0].File, "example.com/dep@") {
					t.Fatalf("missing selected origin: %+v", def)
				}
			}
			// A proven external selection must not navigate to the same-ID root function.
			sites, err := p.CallSites(id)
			if err != nil || len(sites) != 1 {
				t.Fatalf("calls %+v, %v", sites, err)
			}
			wantUnknown := selection == "replacement" || selection == "workspace-outside"
			if sites[0].Unknown != wantUnknown {
				t.Fatalf("false root target: %+v", sites)
			}
			if !p.LocalFunction("example.com/dep.Build") || len(p.Functions) != 2 {
				t.Fatalf("original index changed: %+v", p.Functions)
			}
		})
	}
}

func TestDependencyFilenameLabelsAreContextual(t *testing.T) {
	offlineTestEnv(t)
	dep := t.TempDir()
	root := t.TempDir()
	original := "package dep\ntype Item struct { Snapshot int }\n"
	write(t, dep, "go.mod", "module example.com/dep\n\ngo 1.23\n")
	write(t, dep, "dep.go", original)
	for _, v := range []string{"v1.0.0", "v1.1.0"} {
		write(t, root, v+"/go.mod", "module example.com/"+v+"\n\ngo 1.23\nrequire example.com/dep "+v+"\nreplace example.com/dep => "+filepath.ToSlash(dep)+"\n")
		write(t, root, v+"/app.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{} }\n")
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"v1.0.0", "v1.1.0", "v1.0.0"} {
		def := externalDefinition(t, p, "example.com/"+v+".Entry", "Item{}")
		if def.Reason != "" || len(def.Snippets) != 1 || !strings.HasPrefix(def.Snippets[0].File, "example.com/dep@"+v+" [replace => ") || def.Snippets[0].Source != "type Item struct { Snapshot int }" {
			t.Fatalf("context label/snapshot: %+v", def)
		}
		if _, ok := p.sources[def.Snippets[0].File]; ok {
			t.Fatal("display label used as storage key")
		}
		write(t, dep, "dep.go", "package dep\ntype Item struct { Changed string; ExtraField bool }\n")
	}
	file, err := filepath.EvalSymlinks(filepath.Join(dep, "dep.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(p.sources[file]) != original {
		t.Fatal("stable source key/snapshot changed")
	}
}

func TestLocalFunctionManuallyConstructedProject(t *testing.T) {
	p := &Project{Functions: map[string]Function{"example.com/app.Entry": {File: "app.go"}, "sdk.Read": {File: "GOROOT/src/sdk/sdk.go"}}}
	if !p.LocalFunction("example.com/app.Entry") || p.LocalFunction("sdk.Read") || p.LocalFunction("unknown") {
		t.Fatal("nil original-root set fallback")
	}
	p.local = map[string]bool{}
	if p.LocalFunction("example.com/app.Entry") {
		t.Fatal("explicit original-root set ignored")
	}
}

func TestSDKImportedSignaturesCachedAndConservative(t *testing.T) {
	sdk := fakeSDK(t, "fakeio", `package fakeio
import "fakebase"
func New() *fakebase.Item { return nil }
func Bad() *Missing { return nil }
`)
	write(t, sdk, "src/fakebase/base.go", "package fakebase\ntype Item struct{}\nfunc (*Item) Run() {}\n")
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n")
	write(t, root, "app.go", "package app\nimport \"fakeio\"\nfunc Entry() { fakeio.New().Run(); fakeio.Bad().Run() }\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "example.com/app.Entry"
	first, err := p.CallSites(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 4 || first[1].Unknown || first[1].To != "fakebase.Item.Run" || !first[3].Unknown {
		t.Fatalf("transitive/invalid SDK types: %+v", first)
	}
	importer := p.newImporter(id, false)
	checked, base := importer.cache["fakeio"], importer.cache["fakebase"]
	if checked == nil || base == nil {
		t.Fatal("missing checked signatures")
	}
	for n := 0; n < 3; n++ {
		again, err := p.CallSites(id)
		if err != nil || !reflect.DeepEqual(first, again) {
			t.Fatalf("warm calls %+v %v", again, err)
		}
		if p.newImporter(id, false) != importer || importer.cache["fakeio"] != checked || importer.cache["fakebase"] != base {
			t.Fatal("rechecked transitive imports")
		}
	}
	if len(p.Functions) != 1 {
		t.Fatal("registered imported SDK targets eagerly")
	}
}

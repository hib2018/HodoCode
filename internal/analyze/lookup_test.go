package analyze

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeSDK(t *testing.T, pkg, source string) string {
	t.Helper()
	previous := build.Default
	t.Cleanup(func() { build.Default = previous })
	root := t.TempDir()
	build.Default.GOROOT = root
	write(t, root, "src/"+pkg+"/sdk.go", source)
	return root
}

func TestSDKAllObjectsAndLazyNavigation(t *testing.T) {
	sdk := fakeSDK(t, "fakeio", `package fakeio
type Number int
type Item struct { Field int }
const Answer Number = 42
var Scalar = 7
var Values []Item
func New() *Item { return &Item{Field: Scalar} }
func (i *Item) Read() int { return helper(i.Field) }
func helper(n int) int { return n + int(Answer) }
`)
	t.Setenv("PATH", t.TempDir()) // SDK inspection must not invoke the Go tool.
	write(t, sdk, "src/fakeio/excluded_test.go", "package fakeio\nfunc TestOnly() {}\n")
	write(t, sdk, "src/fakeio/excluded.go", "//go:build never_sdk_test\n\npackage fakeio\nfunc Excluded() {}\n")
	write(t, sdk, "src/fakeio/tagged.go", "//go:build sdk_test\n\npackage fakeio\nconst Tagged = 9\n")
	write(t, sdk, "src/fakeio/platform_"+build.Default.GOOS+".go", "package fakeio\nconst Platform = 1\n")
	otherOS := "windows"
	if build.Default.GOOS == otherOS {
		otherOS = "linux"
	}
	write(t, sdk, "src/fakeio/platform_"+otherOS+".go", "package fakeio\nconst Platform = 2\n")
	write(t, sdk, "src/fakeio/arch_"+build.Default.GOARCH+".go", "package fakeio\nconst Arch = 1\n")
	otherArch := "amd64"
	if build.Default.GOARCH == otherArch {
		otherArch = "arm64"
	}
	write(t, sdk, "src/fakeio/arch_"+otherArch+".go", "package fakeio\nconst Arch = 2\n")
	build.Default.BuildTags = []string{"sdk_test"}
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/nav\n")
	entry := `func Entry() {
 x := fakeio.New()
 _ = x.Read()
 _ = x.Field
 _ = fakeio.Answer
 _ = fakeio.Scalar
 _ = fakeio.Values
 _ = fakeio.Number(1)
 _ = fakeio.Tagged
 _ = fakeio.Platform
 _ = fakeio.Arch
 println(len(fakeio.Values))
}`
	write(t, root, "main.go", "package nav\nimport \"fakeio\"\n"+entry)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "example.com/nav.Entry"
	sites, err := p.CallSites(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 || sites[0].To != "fakeio.New" || sites[1].To != "fakeio.Item.Read" || sites[1].Unknown {
		t.Fatalf("calls: %+v", sites)
	}
	if len(p.Functions) != 1 {
		t.Fatal("CallSites eagerly registered functions")
	}
	defs, err := p.Definitions(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ marker, name, want string }{
		{"fakeio.New()", "New", "func New() *Item { return &Item{Field: Scalar} }"},
		{"x.Read()", "Read", "func (i *Item) Read() int { return helper(i.Field) }"},
		{"x.Field", "Field", "Field int"},
		{"fakeio.Answer", "Answer", "const Answer Number = 42"},
		{"fakeio.Scalar", "Scalar", "var Scalar = 7"},
		{"fakeio.Values", "Values", "var Values []Item"},
		{"fakeio.Number(1)", "Number", "type Number int"},
		{"fakeio.Tagged", "Tagged", "const Tagged = 9"},
		{"fakeio.Platform", "Platform", "const Platform = 1"},
		{"fakeio.Arch", "Arch", "const Arch = 1"},
	} {
		offset := strings.Index(entry, tc.marker) + strings.Index(tc.marker, tc.name)
		def := defs[offset]
		if def.Reason != "" || len(def.Snippets) == 0 || def.Snippets[0].Source != tc.want || !strings.HasPrefix(def.Snippets[0].File, "GOROOT/src/fakeio/") {
			t.Errorf("%s: %+v", tc.marker, def)
		}
	}
	if def := defs[strings.Index(entry, "println")]; !strings.Contains(def.Reason, "組み込み") {
		t.Fatalf("builtin: %+v", def)
	}
	if len(p.Functions) != 1 {
		t.Fatal("Definitions eagerly registered functions")
	}
	for _, target := range []string{id, "fakeio.New", "fakeio.Item.Read", "fakeio.helper"} {
		if err := p.EnsureFunction(target); err != nil {
			t.Fatal(err)
		}
	}
	if !p.LocalFunction(id) || p.LocalFunction("fakeio.New") || p.LocalFunction("missing") {
		t.Fatal("local send filter")
	}
	if len(p.Functions) != 4 {
		t.Fatal(p.Functions)
	}
	src, err := p.Source("fakeio.Item.Read")
	if err != nil || src != "func (i *Item) Read() int { return helper(i.Field) }" {
		t.Fatalf("source %q: %v", src, err)
	}
	calls, err := p.CallSites("fakeio.Item.Read")
	if err != nil || len(calls) != 1 || calls[0].To != "fakeio.helper" || calls[0].Unknown {
		t.Fatalf("SDK navigation %+v %v", calls, err)
	}
	if _, err := p.Definitions("fakeio.Item.Read"); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"fakeio.TestOnly", "fakeio.Excluded", "example.com/out.Func", "fakeio.Answer"} {
		if err := p.EnsureFunction(target); err == nil {
			t.Errorf("accepted %s", target)
		}
	}
}

func offlineTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "https://must-not-be-used.invalid")
	t.Setenv("GOSUMDB", "must-not-be-used.invalid")
	t.Setenv("GOTOOLCHAIN", "go999.0.0")
	t.Setenv("GOFLAGS", "-mod=mod -overlay=/must-not-read.json")
	t.Setenv("GOPATH", t.TempDir())
}

func externalDefinition(t *testing.T, p *Project, id, marker string) Definition {
	t.Helper()
	source, err := p.Source(id)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := p.Definitions(id)
	if err != nil {
		t.Fatal(err)
	}
	offset := strings.Index(source, marker)
	if offset < 0 {
		t.Fatalf("missing marker %s", marker)
	}
	return defs[offset]
}

func TestExternalReplaceContextsAndSubset(t *testing.T) {
	offlineTestEnv(t)
	root := t.TempDir()
	deps := t.TempDir()
	for _, version := range []string{"one", "two"} {
		write(t, deps, version+"/go.mod", "module example.com/dep\n\ngo 1.23\n")
		write(t, deps, version+"/dep.go", "package dep\ntype Item struct { "+strings.Title(version)+" int }\nvar Values []Item\nconst Ignored = 1\nvar Scalar = 2\nfunc Func() {}\n")
		module := "example.com/" + version
		write(t, root, version+"/go.mod", "module "+module+"\n\ngo 1.23\nrequire example.com/dep v1.0.0\nreplace example.com/dep => "+filepath.ToSlash(filepath.Join(deps, version))+"\n")
		write(t, root, version+"/main.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{}; _ = dep.Values; _ = dep.Ignored; _ = dep.Scalar; dep.Func() }\n")
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"one", "two", "one"} {
		id := "example.com/" + version + ".Entry"
		def := externalDefinition(t, p, id, "Item{}")
		if def.Reason != "" || len(def.Snippets) != 1 || !strings.Contains(def.Snippets[0].Source, strings.Title(version)+" int") {
			t.Fatalf("%s: %+v", version, def)
		}
		if !strings.Contains(def.Snippets[0].File, "example.com/dep@v1.0.0 [replace => ") || !strings.HasSuffix(def.Snippets[0].File, "/"+version+"]/dep.go") {
			t.Fatalf("missing replacement provenance: %q", def.Snippets[0].File)
		}
		values := externalDefinition(t, p, id, "Values")
		if values.Reason != "" || len(values.Snippets) != 2 {
			t.Fatalf("slice: %+v", values)
		}
		for _, marker := range []string{"Ignored", "Scalar", "Func()"} {
			def := externalDefinition(t, p, id, marker)
			if len(def.Snippets) != 0 {
				t.Fatalf("external unsupported object: %+v", def)
			}
		}
	}
	if len(p.Functions) != 2 {
		t.Fatal(p.Functions)
	}
	if err := p.EnsureFunction("example.com/dep.Func"); err == nil {
		t.Fatal("external function accepted")
	}
}

func TestExternalCachedModuleVersions(t *testing.T) {
	offlineTestEnv(t)
	cache := t.TempDir()
	t.Setenv("GOMODCACHE", "")
	configDir := t.TempDir()
	write(t, configDir, "goenv", "GOMODCACHE="+cache+"\nGOPROXY=https://must-not-use.invalid\n")
	t.Setenv("GOENV", filepath.Join(configDir, "goenv"))
	root := t.TempDir()
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		field := "First"
		if version == "v1.1.0" {
			field = "Second"
		}
		write(t, cache, "example.com/dep@"+version+"/go.mod", "module example.com/dep\n\ngo 1.23\n")
		write(t, cache, "example.com/dep@"+version+"/dep.go", "package dep\ntype Item struct { "+field+" int }\n")
		write(t, cache, "cache/download/example.com/dep/@v/"+version+".mod", "module example.com/dep\n\ngo 1.23\n")
		write(t, root, field+"/go.mod", "module example.com/"+field+"\n\ngo 1.23\nrequire example.com/dep "+version+"\n")
		hash := "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		write(t, cache, "cache/download/example.com/dep/@v/"+version+".ziphash", hash)
		modHash := sha256.Sum256([]byte(fmt.Sprintf("%x  go.mod\n", sha256.Sum256([]byte("module example.com/dep\n\ngo 1.23\n")))))
		write(t, root, field+"/go.sum", "example.com/dep "+version+" "+hash+"\nexample.com/dep "+version+"/go.mod h1:"+base64.StdEncoding.EncodeToString(modHash[:])+"\n")
		write(t, root, field+"/main.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{} }\n")
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"First", "Second", "First"} {
		def := externalDefinition(t, p, "example.com/"+field+".Entry", "Item{}")
		if def.Reason != "" || len(def.Snippets) != 1 || !strings.Contains(def.Snippets[0].Source, field+" int") {
			dir, lookupErr := dependencyDir(p.packageDirs[p.owners["example.com/"+field+".Entry"]], "example.com/dep")
			t.Fatalf("%s: %+v; dir=%+v err=%v cache=%+v", field, def, dir, lookupErr, p.lookup)
		}
		version := "v1.0.0"
		if field == "Second" {
			version = "v1.1.0"
		}
		if def.Snippets[0].File != "example.com/dep@"+version+"/dep.go" {
			t.Fatalf("cache version label: %+v", def)
		}
	}
}

func TestExternalWorkspaceAndReadonly(t *testing.T) {
	offlineTestEnv(t)
	base := t.TempDir()
	root := filepath.Join(base, "app")
	write(t, base, "go.work", "go 1.23\nuse ./app\nreplace example.com/dep => ./dep\n")
	write(t, base, "go.work.sum", "// unchanged workspace sentinel\n")
	t.Setenv("GOWORK", filepath.Join(base, "go.work"))
	write(t, base, "app/go.mod", "module example.com/app\n\ngo 1.23\nrequire example.com/dep v1.0.0\n")
	write(t, base, "app/go.sum", "// unchanged module sentinel\n")
	write(t, base, "dep/go.mod", "module example.com/dep\n\ngo 1.23\n")
	write(t, base, "dep/dep.go", "package dep\ntype Item struct { Workspace int }\n")
	write(t, base, "app/main.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{} }\n")
	names := []string{"go.work", "go.work.sum", "app/go.mod", "app/go.sum", "dep/go.mod"}
	before := map[string]string{}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = string(data)
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	def := externalDefinition(t, p, "example.com/app.Entry", "Item{}")
	if def.Reason != "" || len(def.Snippets) != 1 || !strings.Contains(def.Snippets[0].Source, "Workspace int") {
		t.Fatalf("workspace: %+v", def)
	}
	if !strings.HasPrefix(def.Snippets[0].File, "example.com/dep@v1.0.0 [replace => ") || !strings.HasSuffix(def.Snippets[0].File, ", workspace]/dep.go") {
		t.Fatalf("workspace replace label: %+v", def)
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil || string(data) != before[name] {
			t.Fatalf("mutated %s: %v", name, err)
		}
	}
	// Workspace use directives must select the local module without a replace.
	write(t, base, "go.work", "go 1.23\nuse (\n ./app\n ./dep\n)\n")
	p, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	def = externalDefinition(t, p, "example.com/app.Entry", "Item{}")
	if def.Reason != "" || len(def.Snippets) != 1 || !strings.Contains(def.Snippets[0].Source, "Workspace int") {
		t.Fatalf("workspace use: %+v", def)
	}
	if def.Snippets[0].File != "example.com/dep@workspace [workspace]/dep.go" {
		t.Fatalf("workspace label: %+v", def)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("unexpected files: %v", entries)
	}
}

func TestVendorAndSourceBounds(t *testing.T) {
	for _, workspace := range []bool{false, true} {
		t.Run(map[bool]string{false: "module", true: "workspace"}[workspace], func(t *testing.T) {
			offlineTestEnv(t)
			t.Setenv("GOFLAGS", "-overlay=/must-not-read.json")
			base := t.TempDir()
			root := base
			manifest := "# example.com/dep v1.0.0\n## explicit; go 1.23\nexample.com/dep\n"
			if workspace {
				root = filepath.Join(base, "app")
				write(t, base, "go.work", "go 1.23\nuse ./app\n")
				t.Setenv("GOWORK", filepath.Join(base, "go.work"))
				manifest = "## workspace\n" + manifest
			}
			write(t, root, "go.mod", "module example.com/app\n\ngo 1.23\nrequire example.com/dep v1.0.0\n")
			write(t, root, "main.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{} }\n")
			write(t, base, "vendor/modules.txt", manifest)
			write(t, base, "vendor/example.com/dep/dep.go", "package dep\ntype Item struct { Vendor int }\n")
			outside := t.TempDir()
			write(t, outside, "leak.go", "package dep\ntype Leaked struct { Secret int }\n")
			if err := os.Symlink(filepath.Join(outside, "leak.go"), filepath.Join(base, "vendor/example.com/dep/leak.go")); err != nil {
				t.Fatal(err)
			}
			p, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			def := externalDefinition(t, p, "example.com/app.Entry", "Item{}")
			if def.Reason != "" || len(def.Snippets) != 1 || !strings.Contains(def.Snippets[0].Source, "Vendor int") {
				t.Fatalf("vendor: %+v", def)
			}
			origin := "vendor"
			if workspace {
				origin = "workspace, vendor"
			}
			if def.Snippets[0].File != "example.com/dep@v1.0.0 ["+origin+"]/dep.go" {
				t.Fatalf("vendor label: %+v", def)
			}
			for _, source := range p.lookup {
				for _, file := range source.files {
					if strings.HasSuffix(p.fset.Position(file.Pos()).Filename, "leak.go") {
						t.Fatal("followed escaping file")
					}
				}
			}
			if len(p.Functions) != 1 {
				t.Fatal(p.Functions)
			}
		})
	}
}

func TestLookupMissingAndSDKSymlinkBoundary(t *testing.T) {
	sdk := fakeSDK(t, "fakeio", "package fakeio\nfunc Read() {}\n")
	outside := t.TempDir()
	write(t, outside, "outside.go", "package leak\nfunc Read() {}\n")
	if err := os.Symlink(outside, filepath.Join(sdk, "src", "leak")); err != nil {
		t.Fatal(err)
	}
	if _, ok := standardDir("leak"); ok {
		t.Fatal("SDK boundary escaped")
	}
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n")
	write(t, root, "main.go", "package app\nimport \"example.com/unavailable\"\nfunc Entry() { _ = unavailable.Item{} }\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	def := externalDefinition(t, p, "example.com/app.Entry", "Item{}")
	if def.Reason == "" || len(def.Snippets) != 0 {
		t.Fatalf("missing go: %+v", def)
	}
	before := p.Functions
	if err := p.EnsureFunction("leak.Read"); err == nil {
		t.Fatal("SDK symlink accepted")
	}
	if !reflect.DeepEqual(before, p.Functions) {
		t.Fatal("failed lookup registered funcs")
	}
}

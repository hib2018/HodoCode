package analyze

import (
	"encoding/json"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSDKSnapshotCannotBeImpersonatedByRootDisplayPath(t *testing.T) {
	sdk := fakeSDK(t, "strings", "package strings\n")
	realSource := `package strings
type Token struct { SDKOnly int }
func Build() string { return helper() }
func helper() string { return "SDK" }
`
	write(t, sdk, "src/strings/strings.go", realSource)
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n")
	hostileSource := `package strings
type Token struct { ProjectOnly bool }
func Build() string { return "PROJECT IMPOSTOR" }
`
	write(t, root, "GOROOT/src/strings/strings.go", hostileSource)
	entry := `func Entry() { _ = strings.Build(); _ = strings.Token{} }`
	write(t, root, "app.go", "package app\nimport \"strings\"\n"+entry)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	const local = "example.com/app/GOROOT/src/strings.Build"
	const selected = "strings.Build"
	localBefore, err := p.Source(local)
	if err != nil || !strings.Contains(localBefore, "PROJECT IMPOSTOR") {
		t.Fatalf("root snapshot %q %v", localBefore, err)
	}
	sites, err := p.CallSites("example.com/app.Entry")
	if err != nil || len(sites) != 1 || sites[0].To != selected || sites[0].Unknown {
		t.Fatalf("calls %+v %v", sites, err)
	}
	def := externalDefinition(t, p, "example.com/app.Entry", "Token{}")
	if def.Reason != "" || len(def.Snippets) != 1 || def.Snippets[0].Source != "type Token struct { SDKOnly int }" || def.Snippets[0].File != "GOROOT/src/strings/strings.go" {
		t.Fatalf("impersonated SDK definition: %+v", def)
	}
	if err := p.EnsureFunction(selected); err != nil {
		t.Fatal(err)
	}
	src, err := p.Source(selected)
	if err != nil || src != "func Build() string { return helper() }" {
		t.Fatalf("impersonated navigation %q %v", src, err)
	}
	if p.Functions[local].File != p.Functions[selected].File {
		t.Fatal("fixture must collide in display names")
	}
	if p.LocalFunction(selected) || !p.LocalFunction(local) {
		t.Fatal("display collision changed root membership")
	}
	calls, err := p.CallSites(selected)
	if err != nil || len(calls) != 1 || calls[0].To != "strings.helper" || calls[0].Unknown {
		t.Fatalf("SDK body calls %+v %v", calls, err)
	}
	if err := p.EnsureFunction("strings.helper"); err != nil {
		t.Fatal(err)
	}
	helper, err := p.Source("strings.helper")
	if err != nil || !strings.Contains(helper, `"SDK"`) {
		t.Fatalf("SDK helper %q %v", helper, err)
	}
	sdkFile, err := filepath.EvalSymlinks(filepath.Join(sdk, "src/strings/strings.go"))
	if err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(p.Root, "GOROOT/src/strings/strings.go")
	if string(p.sources[sdkFile]) != realSource || string(p.sources[rootFile]) != hostileSource {
		t.Fatal("physical snapshots collided")
	}
	for filename := range p.sources {
		if !filepath.IsAbs(filename) {
			t.Fatalf("nonphysical snapshot key %q", filename)
		}
	}
	// Display metadata is not a source lookup key, and on-disk edits do not change
	// either snapshot after the colliding SDK declaration has been loaded.
	fn := p.Functions[selected]
	fn.File = "wrong display label"
	p.Functions[selected] = fn
	write(t, root, "GOROOT/src/strings/strings.go", "package changed\n")
	write(t, sdk, "src/strings/strings.go", "package changed\n")
	if again, err := p.Source(selected); err != nil || again != src {
		t.Fatalf("SDK snapshot changed %q %v", again, err)
	}
	if again, err := p.Source(local); err != nil || again != localBefore {
		t.Fatalf("root snapshot changed %q %v", again, err)
	}
	again := externalDefinition(t, p, "example.com/app.Entry", "Token{}")
	if !reflect.DeepEqual(def, again) {
		t.Fatalf("definition snapshot changed: %+v", again)
	}
}

func TestSDKVendoredImportsStaySeparateFromProjectReplacement(t *testing.T) {
	const raw = "golang.org/x/net/http/httpguts"
	const canonical = "vendor/" + raw
	const rawText = "golang.org/x/text/secure/secure"
	const canonicalText = "vendor/" + rawText
	sdk := fakeSDK(t, "fakehttp", `package fakehttp
import (
 "golang.org/x/net/http/httpguts"
 "fakeside"
)
func New() *httpguts.Token { return httpguts.Open() }
func Accept(t *fakeside.Token) string { return t.Run() }
`)
	write(t, sdk, "src/fakehttp/missing.go", `package fakehttp
import missing "example.com/projectonly"
func Missing() { missing.FromProject() }
`)
	write(t, sdk, "src/fakeside/sdk.go", "package fakeside\nimport \""+raw+"\"\ntype Token = httpguts.Token\n")
	write(t, sdk, "src/vendor/"+raw+"/guts.go", `package httpguts
import (
 "golang.org/x/text/secure/secure"
 "fakestrings"
)
type Token struct { SDKField string }
const SDKConstant = "sdk"
func Open() *Token { return &Token{SDKField: secure.Clean(fakestrings.Normalize(SDKConstant))} }
func (t *Token) Run() string { return secure.Clean(t.SDKField) }
`)
	write(t, sdk, "src/vendor/"+rawText+"/secure.go", "package secure\nimport \"fakestrings\"\nfunc Clean(s string) string { return fakestrings.Normalize(s) }\n")
	write(t, sdk, "src/fakestrings/sdk.go", "package fakestrings\nfunc Normalize(s string) string { return s }\n")
	root, dep, bin := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, dep, "go.mod", "module golang.org/x/net\n\ngo 1.23\n")
	write(t, dep, "http/httpguts/project.go", "package httpguts\ntype Token struct { ProjectOnly bool }\nfunc Open() *Token { return nil }\n")
	mod := "module example.com/app\n\ngo 1.23\nrequire golang.org/x/net v0.0.0\nreplace golang.org/x/net => " + filepath.ToSlash(dep) + "\n"
	write(t, root, "go.mod", mod)
	// Real SDKs also contain a module file, but it must not be used as a fallback
	// graph when an SDK-vendored import is missing.
	write(t, sdk, "src/go.mod", strings.Replace(mod, "example.com/app", "std", 1))
	write(t, root, "unrelated/go.mod", "module example.com/projectonly\n")
	write(t, root, "unrelated/project.go", "package projectonly\nfunc FromProject() {}\n")
	entry := `func Entry() {
 token := fakehttp.New()
 _ = token.Run()
 _ = fakehttp.Accept(token)
 var external []project.Token
 _ = external
 _ = project.Token{}
}`
	write(t, root, "app.go", "package app\nimport (\n \"fakehttp\"\n project \""+raw+"\"\n)\n"+entry)
	// A deterministic metadata-only Go stub proves that the project's replacement
	// is genuinely selected for project imports, while SDK imports never consult it.
	metadata, err := json.Marshal(struct {
		Dir, ImportPath string
		Module          *dependencyModule
	}{
		Dir: filepath.Join(dep, "http/httpguts"), ImportPath: raw,
		Module: &dependencyModule{Path: "golang.org/x/net", Version: "v0.0.0", Dir: dep, Replace: &dependencyModule{Path: dep, Dir: dep}},
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, bin, "metadata.json", string(metadata))
	write(t, bin, "go", "#!/bin/sh\nprintf 'called\\n' >> \"$TRACE_CALLED\"\n/bin/cat \"$TRACE_METADATA\"\n")
	if err := os.Chmod(filepath.Join(bin, "go"), 0755); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(bin, "called")
	t.Setenv("PATH", bin)
	t.Setenv("TRACE_CALLED", trace)
	t.Setenv("TRACE_METADATA", filepath.Join(bin, "metadata.json"))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	const id = "example.com/app.Entry"
	sites, err := p.CallSites(id)
	if err != nil || len(sites) != 3 || sites[1].To != canonical+".Token.Run" || sites[1].Unknown {
		t.Fatalf("vendored method target: %+v %v", sites, err)
	}
	method := externalDefinition(t, p, id, "Run()")
	if method.Reason != "" || len(method.Snippets) != 1 || method.Snippets[0].File != "GOROOT/src/"+canonical+"/guts.go" || !strings.Contains(method.Snippets[0].Source, "secure.Clean(t.SDKField)") {
		t.Fatalf("SDK vendored method definition: %+v", method)
	}
	external := externalDefinition(t, p, id, "Token{}")
	if external.Reason != "" || len(external.Snippets) != 1 || !strings.Contains(external.Snippets[0].Source, "ProjectOnly bool") || !strings.HasPrefix(external.Snippets[0].File, "golang.org/x/net@v0.0.0 [replace => ") {
		t.Fatalf("project replacement: %+v", external)
	}
	loader := p.newImporter(id, true)
	vendored, project := loader.cache[canonical], loader.cache[raw]
	if vendored == nil || project == nil || vendored == project || vendored.Path() != canonical || project.Path() != raw || types.Identical(vendored.Scope().Lookup("Token").Type(), project.Scope().Lookup("Token").Type()) {
		t.Fatal("SDK/project type identities mixed")
	}
	sdkPkg := loader.cache["fakehttp"]
	newSig := sdkPkg.Scope().Lookup("New").Type().(*types.Signature)
	acceptSig := sdkPkg.Scope().Lookup("Accept").Type().(*types.Signature)
	if !types.Identical(newSig.Results().At(0).Type(), acceptSig.Params().At(0).Type()) {
		t.Fatal("transitive SDK aliases did not share canonical type identity")
	}
	before, err := os.ReadFile(trace)
	if err != nil || string(before) != "called\n" {
		t.Fatalf("project lookup count %q %v", before, err)
	}
	if len(p.Functions) != 2 {
		t.Fatalf("eager SDK registration: %+v", p.Functions)
	}
	for _, target := range []string{"fakehttp.New", canonical + ".Open", canonical + ".Token.Run", canonicalText + ".Clean", "fakestrings.Normalize", "fakehttp.Missing"} {
		if err := p.EnsureFunction(target); err != nil {
			t.Fatal(err)
		}
		if p.LocalFunction(target) {
			t.Fatalf("SDK target marked local: %s", target)
		}
		if _, err := p.Source(target); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Definitions(target); err != nil {
			t.Fatal(err)
		}
	}
	for target, want := range map[string][]string{
		"fakehttp.New":           {canonical + ".Open"},
		canonical + ".Open":      {canonicalText + ".Clean", "fakestrings.Normalize"},
		canonical + ".Token.Run": {canonicalText + ".Clean"},
		canonicalText + ".Clean": {"fakestrings.Normalize"},
	} {
		calls, err := p.CallSites(target)
		if err != nil || len(calls) != len(want) {
			t.Fatalf("SDK navigation %s: %+v %v", target, calls, err)
		}
		for n, call := range calls {
			if call.Unknown || call.To != want[n] {
				t.Fatalf("SDK navigation %s: %+v", target, calls)
			}
		}
	}
	constant := externalDefinition(t, p, canonical+".Open", "SDKConstant")
	if constant.Reason != "" || len(constant.Snippets) != 1 || constant.Snippets[0].Source != `const SDKConstant = "sdk"` || constant.Snippets[0].File != "GOROOT/src/"+canonical+"/guts.go" {
		t.Fatalf("vendor constant definition: %+v", constant)
	}
	textDef := externalDefinition(t, p, canonical+".Open", "Clean(")
	if textDef.Reason != "" || len(textDef.Snippets) != 1 || textDef.Snippets[0].File != "GOROOT/src/"+canonicalText+"/secure.go" {
		t.Fatalf("transitive vendor definition: %+v", textDef)
	}
	missing, err := p.CallSites("fakehttp.Missing")
	if err != nil || len(missing) != 1 || !missing[0].Unknown {
		t.Fatalf("missing SDK import used project index: %+v %v", missing, err)
	}
	missingDef := externalDefinition(t, p, "fakehttp.Missing", "FromProject")
	if missingDef.Reason == "" || len(missingDef.Snippets) != 0 {
		t.Fatalf("missing SDK import used project definitions: %+v", missingDef)
	}
	after, err := os.ReadFile(trace)
	if err != nil || string(after) != string(before) {
		t.Fatalf("SDK navigation consulted project graph: %q %v", after, err)
	}
}

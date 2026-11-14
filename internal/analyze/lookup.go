package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// sourcePackage is cached by origin directory and import path, never path alone.
// Keeping parsed ASTs preserves object positions and snapshot bytes across lookups.
type sourcePackage struct {
	files  []*ast.File
	dir    string
	err    error
	labels map[string]string
}

type dependencyLocation struct {
	dir, label string
}

type dependencyModule struct {
	Path, Version, Dir string
	Main               bool
	Replace            *dependencyModule
}

// sdkDirectory identifies the importing source, not the original navigation root.
// This also covers imports made transitively from GOROOT/src/vendor.
func sdkDirectory(dir string) bool {
	base, err := filepath.EvalSymlinks(filepath.Join(build.Default.GOROOT, "src"))
	if err != nil || dir == "" {
		return false
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, real)
	return err == nil && !escapes(rel)
}

func standardDir(pkg string) (string, bool) {
	if !validModulePath(pkg) || strings.Contains(strings.Split(pkg, "/")[0], ".") {
		return "", false
	}
	base, err := filepath.EvalSymlinks(filepath.Join(build.Default.GOROOT, "src"))
	if err != nil {
		return "", false
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(pkg)))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(base, dir)
	if err != nil || escapes(rel) {
		return "", false
	}
	stat, err := os.Stat(dir)
	return dir, err == nil && stat.IsDir()
}

// LocalFunction reports membership in the original root index, even after SDK
// navigation. Explanation/send callers must use this rather than Functions alone.
// Manually constructed Projects without an original-root set treat existing
// Functions entries as local, except explicitly labelled SDK entries.
func (p *Project) LocalFunction(id string) bool {
	if p.local != nil {
		return p.local[id]
	}
	fn, ok := p.Functions[id]
	return ok && !strings.HasPrefix(fn.File, "GOROOT/src/")
}

// EnsureFunction registers only the requested SDK declaration, including canonical
// vendor/... SDK IDs returned by CallSites. Root functions are a no-op;
// dependency functions are never navigation or explanation targets.
func (p *Project) EnsureFunction(id string) error {
	if _, ok := p.Functions[id]; ok {
		return nil
	}
	for dot := strings.LastIndexByte(id, '.'); dot >= 0; dot = strings.LastIndexByte(id[:dot], '.') {
		pkg := id[:dot]
		if _, ok := standardDir(pkg); ok {
			source := p.sourcePackage(pkg, "", true)
			if source.err != nil {
				return source.err
			}
			name := id[dot+1:]
			for _, file := range source.files {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok {
						continue
					}
					candidate := fn.Name.Name
					if fn.Recv != nil {
						candidate = receiverName(fn.Recv.List[0].Type) + "." + candidate
					}
					if candidate != name || candidate == "init" || candidate == "_" {
						continue
					}
					pos := p.fset.PositionFor(fn.Pos(), false)
					label := p.sourceName(pos.Filename)
					p.Functions[id] = Function{ID: id, File: label, Line: pos.Line, End: p.fset.PositionFor(fn.End(), false).Line}
					p.declarations[id], p.owners[id] = fn, pkg
					p.packages[pkg] = source.files
					p.packageDirs[pkg] = source.dir
					return nil
				}
			}
			return fmt.Errorf("SDK function %q not found", id)
		}
	}
	return fmt.Errorf("function %q is not an indexed root or local SDK function", id)
}

// sourceName is for local display only; snapshot storage uses physical filenames.
func (p *Project) sourceName(filename string) string {
	base, _ := filepath.EvalSymlinks(filepath.Join(build.Default.GOROOT, "src"))
	if rel, err := filepath.Rel(base, filename); base != "" && err == nil && !escapes(rel) {
		return "GOROOT/src/" + filepath.ToSlash(rel)
	}
	if rel, err := filepath.Rel(p.Root, filename); err == nil && !escapes(rel) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(filename)
}

func (p *Project) sourcePackage(pkg, origin string, standardOnly bool) *sourcePackage {
	dir, standard := standardDir(pkg)
	key := origin + "\x00" + pkg
	if standard {
		key = "GOROOT\x00" + build.Default.GOROOT + "\x00" + pkg
	}
	if cached := p.lookup[key]; cached != nil {
		return cached
	}
	result := &sourcePackage{labels: map[string]string{}}
	if p.lookup == nil {
		p.lookup = map[string]*sourcePackage{}
	}
	p.lookup[key] = result
	label := ""
	if !standard {
		if standardOnly {
			result.err = fmt.Errorf("SDK source unavailable: %s", pkg)
			return result
		}
		if !validModulePath(pkg) {
			result.err = fmt.Errorf("invalid import path")
			return result
		}
		location, err := dependencyDir(origin, pkg)
		if err != nil {
			result.err = err
			return result
		}
		dir, label = location.dir, location.label
	}
	dir, result.err = filepath.EvalSymlinks(dir)
	if result.err != nil {
		return result
	}
	if !standard {
		home, _ := os.UserHomeDir()
		home, _ = filepath.EvalSymlinks(home)
		if filepath.Dir(dir) == dir || dir == home {
			result.err = fmt.Errorf("refusing root/home source directory")
			return result
		}
	}
	result.dir = dir
	if !standard && dir == p.packageDirs[pkg] && len(p.packages[pkg]) != 0 {
		result.files = p.packages[pkg]
		for _, file := range result.files {
			filename := p.fset.PositionFor(file.Pos(), false).Filename
			result.labels[filename] = label + "/" + filepath.Base(filename)
		}
		return result
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		result.err = err
		return result
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filename := filepath.Join(dir, name)
		real, err := filepath.EvalSymlinks(filename)
		// No following a file symlink outside this exact package directory.
		if err != nil || filepath.Dir(real) != dir {
			continue
		}
		stat, err := os.Stat(real)
		if err != nil || !stat.Mode().IsRegular() {
			continue
		}
		matches, err := build.Default.MatchFile(dir, name)
		if err != nil {
			result.err = err
			return result
		}
		if !matches {
			continue
		}
		data := p.sources[real]
		if data == nil {
			data, err = os.ReadFile(real)
			if err != nil {
				result.err = err
				return result
			}
		}
		file, err := parser.ParseFile(p.fset, real, data, parser.SkipObjectResolution)
		if err != nil {
			result.err = err
			return result
		}
		p.sources[real] = data
		if label != "" {
			result.labels[real] = label + "/" + name
		}
		result.files = append(result.files, file)
	}
	if len(result.files) == 0 {
		result.err = fmt.Errorf("source unavailable: %s", pkg)
	}
	return result
}

// Only metadata is requested: no -deps, export data, builds or downloads.
// Read-only Go config still supplies custom cache paths; dangerous settings and
// GOFLAGS are explicitly overridden, never written back to that config.
func offlineGo(dir string, extra []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	forced := []string{
		"GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off",
		"GONOPROXY=none", "GOPRIVATE=", "GONOSUMDB=none", "GOINSECURE=",
		"GOVCS=*:off", "GO111MODULE=on", "GOTELEMETRY=off", "GOFLAGS= ",
		"GOROOT=" + build.Default.GOROOT, "GOOS=" + build.Default.GOOS, "GOARCH=" + build.Default.GOARCH,
	}
	blocked := map[string]bool{"GOWORK": true}
	for _, setting := range forced {
		key, _, _ := strings.Cut(setting, "=")
		blocked[key] = true
	}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !blocked[key] {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, forced...)
	cmd.Env = append(cmd.Env, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("offline go metadata: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

func workspaceFile(origin string) string {
	if value := os.Getenv("GOWORK"); value != "" {
		if value == "off" {
			return ""
		}
		if filepath.IsAbs(value) {
			return value
		}
		return ""
	}
	for dir := origin; ; dir = filepath.Dir(dir) {
		name := filepath.Join(dir, "go.work")
		if stat, err := os.Stat(name); err == nil && stat.Mode().IsRegular() {
			return name
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// A private workspace copy prevents even implicit go.work.sum writes. Relative
// use/replace paths are rebased; the user's workspace and config stay untouched.
func dependencyDir(origin, pkg string) (dependencyLocation, error) {
	var result dependencyLocation
	moduleRoot := moduleDirectory(origin)
	if moduleRoot == "" {
		return result, fmt.Errorf("missing caller module context")
	}
	work := workspaceFile(origin)
	env := []string{"GOWORK=off"}
	vendorBase, versionFile, workCopyDir := moduleRoot, filepath.Join(moduleRoot, "go.mod"), ""
	if work != "" {
		data, err := offlineGo(origin, env, "work", "edit", "-json", work)
		if err != nil {
			return result, err
		}
		var meta struct {
			Go      string
			Use     []struct{ DiskPath string }
			Replace []struct {
				Old, New struct{ Path, Version string }
			}
		}
		if err = json.Unmarshal(data, &meta); err != nil {
			return result, err
		}
		temp, err := os.MkdirTemp("", "hodocode-work-")
		if err != nil {
			return result, err
		}
		defer os.RemoveAll(temp)
		workCopyDir = temp
		base := filepath.Dir(work)
		vendorBase, versionFile = base, work
		absolute := func(name string) string {
			if !filepath.IsAbs(name) {
				return filepath.Join(base, name)
			}
			return name
		}
		var text strings.Builder
		if meta.Go != "" {
			fmt.Fprintf(&text, "go %s\n", meta.Go)
		}
		for _, use := range meta.Use {
			fmt.Fprintf(&text, "use %s\n", strconv.Quote(absolute(use.DiskPath)))
		}
		for _, rep := range meta.Replace {
			newPath := rep.New.Path
			if rep.New.Version == "" {
				newPath = absolute(newPath)
			}
			fmt.Fprintf(&text, "replace %s %s => %s %s\n", strconv.Quote(rep.Old.Path), rep.Old.Version, strconv.Quote(newPath), rep.New.Version)
		}
		copy := filepath.Join(temp, "go.work")
		if err = os.WriteFile(copy, []byte(text.String()), 0600); err != nil {
			return result, err
		}
		env = []string{"GOWORK=" + copy}
	}
	mode, err := dependencyMode(origin, versionFile, vendorBase, work != "", env)
	if err != nil {
		return result, err
	}
	if mode == "vendor" && workCopyDir != "" {
		if err := os.Symlink(filepath.Join(vendorBase, "vendor"), filepath.Join(workCopyDir, "vendor")); err != nil {
			return result, err
		}
	}
	args := []string{"list", "-e", "-json", "-mod=" + mode}
	if len(build.Default.BuildTags) > 0 {
		args = append(args, "-tags="+strings.Join(build.Default.BuildTags, ","))
	}
	args = append(args, pkg)
	data, err := offlineGo(origin, env, args...)
	if err != nil {
		return result, err
	}
	var meta struct {
		Dir, ImportPath string
		Module          *dependencyModule
		Error           *struct{ Err string }
	}
	if err = json.Unmarshal(data, &meta); err != nil {
		return result, err
	}
	if meta.Dir == "" || meta.ImportPath != pkg || meta.Module == nil {
		if meta.Error != nil {
			return result, fmt.Errorf("dependency source unavailable: %s: %s", pkg, meta.Error.Err)
		}
		return result, fmt.Errorf("dependency source unavailable: %s", pkg)
	}
	dir, err := filepath.EvalSymlinks(meta.Dir)
	if err != nil {
		return result, err
	}
	boundary := ""
	vendored := false
	if mode == "vendor" {
		vendor := filepath.Join(vendorBase, "vendor")
		if rel, e := filepath.Rel(vendor, meta.Dir); e == nil && !escapes(rel) {
			boundary = vendor
		}
		if workCopyDir != "" {
			if rel, e := filepath.Rel(filepath.Join(workCopyDir, "vendor"), meta.Dir); e == nil && !escapes(rel) {
				boundary = vendor
			}
		}
		vendored = boundary != ""
	}
	if boundary == "" {
		boundary = meta.Module.Dir
	}
	if boundary == "" {
		return result, fmt.Errorf("missing dependency source boundary: %s", pkg)
	}
	boundary, err = filepath.EvalSymlinks(boundary)
	if err != nil {
		return result, err
	}
	if rel, e := filepath.Rel(boundary, dir); e != nil || escapes(rel) {
		return result, fmt.Errorf("dependency source escapes boundary: %s", pkg)
	}
	label, err := dependencyLabel(pkg, meta.Module, vendored, work != "")
	if err != nil {
		return result, err
	}
	return dependencyLocation{dir: dir, label: label}, nil
}

// Package analyze indexes Go source and lazily inspects one function at a time.
//
// All modules physically inside Root are indexed without fetching dependencies.
// Build constraints follow go/build.Default; _test.go, vendor, hidden
// directories/files and symlinks are excluded. SDK navigation and restricted
// dependency definitions load local source lazily, without builds or downloads.
package analyze

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type Function struct {
	ID, File  string
	Line, End int
}

type Edge struct {
	From, To string
	Unknown  bool
}

// CallSite records one call occurrence. Positions are physical 1-based byte
// columns, with an exclusive EndColumn, covering only the function/method name.
// Calls without a name have Name == "" and an empty range at the callee's start.
// Offset is the 0-based byte offset of that range from the start of Source(id).
type CallSite struct {
	Edge
	Line, Column, EndColumn int
	Offset                  int
	Name                    string
}

// Project owns a source snapshot and mutable lazy caches. Serialize analyzer
// operations and Functions reads while navigation or lookups are loading source.
// Reload after build, module or workspace configuration changes.
type Project struct {
	Root string
	// Remote snapshot metadata; RepositorySubdir is slash-separated relative to the repository root.
	RepositoryURL, CommitSHA, RepositorySubdir string
	// Functions starts with root declarations only. EnsureFunction may add SDK
	// functions for local navigation; send/explain code must filter LocalFunction.
	Functions map[string]Function
	Imports   []Edge

	fset         *token.FileSet
	packages     map[string][]*ast.File
	declarations map[string]*ast.FuncDecl
	owners       map[string]string
	sources      map[string][]byte // Absolute physical filenames, never display labels.
	local        map[string]bool
	packageDirs  map[string]string
	lookup       map[string]*sourcePackage
	importers    map[string]*signatureImporter
}

// Load records declarations and imports without type-checking function bodies.
// Root must contain at least one go.mod, possibly in a subdirectory. Only source
// beneath a go.mod is indexed, using the nearest module's canonical import path.
// File paths and import sources are slash-separated, relative to Root; method IDs
// are import/path.Receiver.Method (also for *Receiver).
func Load(root string) (*Project, error) {
	return load(root, "")
}

func load(root, moduleBoundary string) (*Project, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("root: %w", err)
	}
	p := &Project{Root: root, fset: token.NewFileSet(), Functions: map[string]Function{}, packages: map[string][]*ast.File{}, declarations: map[string]*ast.FuncDecl{}, owners: map[string]string{}, sources: map[string][]byte{}}
	directories := map[string]sourceModule{}
	modules := map[string]string{}
	if moduleBoundary != "" && root != moduleBoundary {
		rootModule, err := directoryModule(root)
		if err != nil {
			return nil, err
		}
		for dir := filepath.Dir(root); rootModule.path == ""; dir = filepath.Dir(dir) {
			module, err := directoryModule(dir)
			if err != nil {
				return nil, err
			}
			if module.path != "" {
				directories[filepath.Dir(root)] = module
				modules[module.path] = dir
				break
			}
			if dir == moduleBoundary {
				break
			}
		}
	}
	packageDirs := map[string]string{}
	p.packageDirs = packageDirs
	p.local = map[string]bool{}
	p.lookup = map[string]*sourcePackage{}
	err = filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if filename != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			module, moduleErr := directoryModule(filename)
			if moduleErr != nil {
				return moduleErr
			}
			if module.path != "" {
				if previous, exists := modules[module.path]; exists {
					return fmt.Errorf("duplicate module path %q in %s and %s", module.path, previous, filename)
				}
				modules[module.path] = filename
			} else {
				module = directories[filepath.Dir(filename)]
			}
			directories[filename] = module
			return nil
		}
		module := directories[filepath.Dir(filename)]
		if module.path == "" || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(filename, ".go") || strings.HasSuffix(filename, "_test.go") {
			return nil
		}
		matches, matchErr := build.Default.MatchFile(filepath.Dir(filename), entry.Name())
		if matchErr != nil {
			return fmt.Errorf("%s: %w", filename, matchErr)
		}
		if !matches {
			return nil
		}
		source, readErr := os.ReadFile(filename)
		if readErr != nil {
			return readErr
		}
		file, parseErr := parser.ParseFile(p.fset, filename, source, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relative, relErr := filepath.Rel(root, filename)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		p.sources[filename] = source
		dir := filepath.Dir(filename)
		moduleRelative, relErr := filepath.Rel(module.dir, dir)
		if relErr != nil {
			return relErr
		}
		pkg := module.path
		if moduleRelative != "." {
			pkg += "/" + filepath.ToSlash(moduleRelative)
		}
		if previous, exists := packageDirs[pkg]; exists && previous != dir {
			return fmt.Errorf("duplicate package path %q in %s and %s", pkg, previous, dir)
		}
		packageDirs[pkg] = dir
		if files := p.packages[pkg]; len(files) > 0 && files[0].Name.Name != file.Name.Name {
			return fmt.Errorf("multiple packages in %s", path.Dir(relative))
		}
		p.packages[pkg] = append(p.packages[pkg], file)
		for _, spec := range file.Imports {
			target, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			p.Imports = append(p.Imports, Edge{From: relative, To: target})
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name == "_" {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil {
				receiver := receiverName(fn.Recv.List[0].Type)
				if receiver == "" {
					return fmt.Errorf("unsupported receiver in %s", relative)
				}
				name = receiver + "." + name
			}
			id := pkg + "." + name
			// init declarations are not callable by name and can occur more than once.
			if fn.Recv == nil && name == "init" {
				id += "@" + relative + ":" + strconv.Itoa(p.fset.PositionFor(fn.Pos(), false).Line)
			}
			if _, exists := p.Functions[id]; exists {
				return fmt.Errorf("duplicate function %s", id)
			}
			p.Functions[id] = Function{ID: id, File: relative, Line: p.fset.PositionFor(fn.Pos(), false).Line, End: p.fset.PositionFor(fn.End(), false).Line}
			p.local[id] = true
			p.declarations[id] = fn
			p.owners[id] = pkg
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("load %s: no go.mod found", root)
	}
	sortEdges(p.Imports)
	return p, nil
}

// Source returns the original declaration from the indexed snapshot, without
// reformatting or redaction. It is for local display, never explanation payloads.
func (p *Project) Source(id string) (string, error) {
	fn, ok := p.declarations[id]
	if !ok {
		return "", fmt.Errorf("function %q not found", id)
	}
	pos := p.fset.PositionFor(fn.Pos(), false)
	source := p.sources[pos.Filename]
	start := pos.Offset
	end := p.fset.PositionFor(fn.End(), false).Offset
	return string(source[start:end]), nil
}

// SourcePosition returns the physical start position of an indexed declaration,
// with an absolute filename and a 1-based byte column, ignoring //line directives.
// SDK declarations must first be registered with EnsureFunction, as with Source.
func (p *Project) SourcePosition(id string) (token.Position, error) {
	fn, ok := p.declarations[id]
	if !ok {
		return token.Position{}, fmt.Errorf("function %q not found", id)
	}
	return p.fset.PositionFor(fn.Pos(), false), nil
}

func receiverName(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.IndexExpr:
		return receiverName(x.X)
	case *ast.IndexListExpr:
		return receiverName(x.X)
	}
	return ""
}

// Resolve accepts an exact ID or a unique suffix starting after '.' or '/'.
func (p *Project) Resolve(name string) (string, error) {
	if _, ok := p.Functions[name]; ok {
		return name, nil
	}
	if name == "" {
		return "", fmt.Errorf("empty function name")
	}
	var matches []string
	for id := range p.Functions {
		if strings.HasSuffix(id, "."+name) || strings.HasSuffix(id, "/"+name) {
			matches = append(matches, id)
		}
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("function %q not found", name)
	}
	return "", fmt.Errorf("ambiguous function %q: %s", name, strings.Join(matches, ", "))
}

// Calls returns deduplicated one-level edges for an exact function ID. It never
// traverses callees or nested function literal bodies. Concrete methods resolve
// to root or SDK IDs without registering SDK targets; interface calls, function
// values and unresolved symbols are conservative unknowns.
func (p *Project) Calls(id string) ([]Edge, error) {
	sites, err := p.CallSites(id)
	if err != nil {
		return nil, err
	}
	var edges []Edge
	seen := map[Edge]bool{}
	for _, site := range sites {
		if !seen[site.Edge] {
			seen[site.Edge] = true
			edges = append(edges, site.Edge)
		}
	}
	sortEdges(edges)
	return edges, nil
}

// MainFunctions returns sorted IDs of receiver-less main functions in package main.
func (p *Project) MainFunctions() []string {
	var ids []string
	for id, fn := range p.declarations {
		if p.LocalFunction(id) && fn.Recv == nil && fn.Name.Name == "main" && p.packages[p.owners[id]][0].Name.Name == "main" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// CallSites returns all one-level call occurrences in source order, including
// duplicates, external targets and unknowns, but not function literal bodies.
// Type conversions and builtins are omitted, as in Calls.
func (p *Project) CallSites(id string) ([]CallSite, error) {
	fn, ok := p.declarations[id]
	if !ok {
		return nil, fmt.Errorf("function %q not found", id)
	}
	if fn.Body == nil {
		return nil, nil
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	loader := p.newImporter(id, false)
	config := types.Config{Importer: loader, Sizes: types.SizesFor(build.Default.Compiler, build.Default.GOARCH), Error: func(error) {}}
	// Type errors (e.g. unavailable third-party imports) leave partial information;
	// calls without a proven static target remain unknown rather than being guessed.
	files, body := p.typeFiles(p.owners[id], fn)
	_, _ = config.Check(p.owners[id], p.fset, files, info)
	var sites []CallSite
	sourceStart := p.fset.PositionFor(fn.Pos(), false).Offset
	ast.Inspect(body, func(node ast.Node) bool {
		if _, literal := node.(*ast.FuncLit); literal {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		expr := unparen(call.Fun)
		if info.Types[expr].IsType() {
			return true
		}
		// Generic instantiation is a direct call only when its base is a function.
		base := expr
		switch x := expr.(type) {
		case *ast.IndexExpr:
			base = unparen(x.X)
		case *ast.IndexListExpr:
			base = unparen(x.X)
		}
		var object types.Object
		switch x := base.(type) {
		case *ast.Ident:
			object = info.Uses[x]
		case *ast.SelectorExpr:
			object = info.Uses[x.Sel]
		}
		edge := Edge{From: id, Unknown: true}
		switch obj := object.(type) {
		case *types.Builtin, *types.TypeName:
			return true
		case *types.Func:
			signature, _ := obj.Type().(*types.Signature)
			if signature != nil && obj.Pkg() != nil {
				if signature.Recv() == nil {
					edge.To = obj.Pkg().Path() + "." + obj.Name()
					edge.Unknown = false
				} else if selector, ok := base.(*ast.SelectorExpr); ok && info.Selections[selector] != nil {
					receiver := types.Unalias(signature.Recv().Type())
					if pointer, ok := receiver.(*types.Pointer); ok {
						receiver = types.Unalias(pointer.Elem())
					}
					if named, ok := receiver.(*types.Named); ok {
						// A concrete selector may promote an embedded interface method.
						if _, isInterface := named.Underlying().(*types.Interface); !isInterface {
							target := obj.Pkg().Path() + "." + named.Obj().Name() + "." + obj.Name()
							_, indexed := p.Functions[target]
							_, standard := standardDir(obj.Pkg().Path())
							if indexed || standard {
								edge.To = target
								edge.Unknown = false
							}
						}
					}
				}
			}
		}
		if edge.Unknown {
			var label bytes.Buffer
			if _, literal := expr.(*ast.FuncLit); literal {
				edge.To = "func literal"
			} else if err := printer.Fprint(&label, p.fset, call.Fun); err != nil {
				edge.To = "unknown"
			} else {
				edge.To = label.String()
			}
			// Keep unknown labels out of the static ID namespace.
			edge.To = "unknown: " + edge.To
		}
		var name *ast.Ident
		switch x := base.(type) {
		case *ast.Ident:
			name = x
		case *ast.SelectorExpr:
			name = x.Sel
		}
		pos := p.fset.PositionFor(expr.Pos(), false)
		site := CallSite{Edge: edge, Line: pos.Line, Column: pos.Column, EndColumn: pos.Column}
		if name != nil {
			pos = p.fset.PositionFor(name.Pos(), false)
			site.Line, site.Column = pos.Line, pos.Column
			site.Name = name.Name
			site.EndColumn = pos.Column + len(name.Name)
		}
		site.Offset = pos.Offset - sourceStart
		sites = append(sites, site)
		return true
	})
	// A selector's receiver may contain calls that precede its method name.
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].Line != sites[j].Line {
			return sites[i].Line < sites[j].Line
		}
		return sites[i].Column < sites[j].Column
	})
	return sites, nil
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		x, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = x.X
	}
}

// typeFiles exposes only the selected declaration's body, never closure bodies.
// CallSites must inspect the returned body so copied expressions match types.Info.
func (p *Project) typeFiles(pkg string, selected *ast.FuncDecl) ([]*ast.File, *ast.BlockStmt) {
	var files []*ast.File
	var body *ast.BlockStmt
	for _, original := range p.packages[pkg] {
		file := omitBodies(original, selected).(*ast.File)
		files = append(files, file)
		for _, declaration := range file.Decls {
			if fn, ok := declaration.(*ast.FuncDecl); ok && selected != nil && fn.Pos() == selected.Pos() {
				body = fn.Body
			}
		}
	}
	return files, body
}

// Copy only paths to omitted bodies; all other nodes retain their identity.
// Reflection covers every AST child field, including expressions in types.
func omitBodies(node ast.Node, selected *ast.FuncDecl) ast.Node {
	switch n := node.(type) {
	case *ast.FuncDecl:
		if n != selected {
			copy := *n
			copy.Body = nil
			node = &copy
		}
	case *ast.FuncLit:
		copy := *n
		// FuncLit requires a non-nil body, even when it is not checked.
		copy.Body = &ast.BlockStmt{Lbrace: n.Body.Lbrace, Rbrace: n.Body.Rbrace}
		copy.Type = omitBodies(n.Type, selected).(*ast.FuncType)
		return &copy
	}
	original := reflect.ValueOf(node).Elem()
	var copy reflect.Value
	set := func(index int, value reflect.Value) {
		if !copy.IsValid() {
			copy = reflect.New(original.Type())
			copy.Elem().Set(original)
		}
		copy.Elem().Field(index).Set(value)
	}
	for i := 0; i < original.NumField(); i++ {
		field := original.Field(i)
		if child, ok := field.Interface().(ast.Node); ok && !reflect.ValueOf(child).IsNil() {
			if replacement := omitBodies(child, selected); replacement != child {
				set(i, reflect.ValueOf(replacement))
			}
		} else if field.Kind() == reflect.Slice {
			var slice reflect.Value
			for j := 0; j < field.Len(); j++ {
				child, ok := field.Index(j).Interface().(ast.Node)
				if !ok || reflect.ValueOf(child).IsNil() {
					continue
				}
				if replacement := omitBodies(child, selected); replacement != child {
					if !slice.IsValid() {
						slice = reflect.MakeSlice(field.Type(), field.Len(), field.Len())
						reflect.Copy(slice, field)
					}
					slice.Index(j).Set(reflect.ValueOf(replacement))
				}
			}
			if slice.IsValid() {
				set(i, slice)
			}
		}
	}
	if copy.IsValid() {
		return copy.Interface().(ast.Node)
	}
	return node
}

type signatureImporter struct {
	project *Project
	cache   map[string]*types.Package
	loading map[string]bool
	origin  string
	source  bool
	files   map[string][]*ast.File
	labels  map[string]string
	errors  map[string]error
}

// Keep checked import graphs for this snapshot, isolated by caller and policy.
// The selected function is still checked separately on each navigation request.
func (p *Project) newImporter(id string, source bool) *signatureImporter {
	origin := p.packageDirs[p.owners[id]]
	key := origin + "\x00" + p.owners[id] + "\x00" + strconv.FormatBool(source)
	if cached := p.importers[key]; cached != nil {
		return cached
	}
	loader := &signatureImporter{project: p, cache: map[string]*types.Package{}, loading: map[string]bool{}, origin: origin, source: source, files: map[string][]*ast.File{}, labels: map[string]string{}, errors: map[string]error{}}
	if p.importers == nil {
		p.importers = map[string]*signatureImporter{}
	}
	p.importers[key] = loader
	return loader
}

func (i *signatureImporter) Import(pkg string) (*types.Package, error) {
	return i.ImportFrom(pkg, i.origin, 0)
}

func (i *signatureImporter) ImportFrom(pkg, dir string, _ types.ImportMode) (*types.Package, error) {
	if pkg == "unsafe" {
		return types.Unsafe, nil
	}
	if sdkDirectory(dir) {
		if _, standard := standardDir(pkg); !standard {
			canonical := "vendor/" + pkg
			if _, vendored := standardDir(canonical); !vendored {
				return nil, fmt.Errorf("SDK import source unavailable: %s", pkg)
			}
			pkg = canonical
		}
	}
	return i.importPackage(pkg)
}

func (i *signatureImporter) importPackage(pkg string) (result *types.Package, err error) {
	if previous := i.errors[pkg]; previous != nil {
		return nil, previous
	}
	defer func() {
		if err != nil {
			i.errors[pkg] = err
		}
	}()
	if cached := i.cache[pkg]; cached != nil {
		return cached, nil
	}
	files := i.project.packages[pkg]
	_, standard := standardDir(pkg)
	if standard {
		files = nil
	} // SDK imports never use a same-named root package.
	// Canonical root indexing is retained within a module. A separately indexed
	// module is not proof that the caller's build selects it.
	indexedDir := i.project.packageDirs[pkg]
	if len(files) != 0 && !standard && moduleDirectory(indexedDir) != moduleDirectory(i.origin) {
		if i.source {
			files = nil // Resolve definitions from the caller's actual build graph.
		} else {
			selected, err := dependencyDir(i.origin, pkg)
			if err == nil && selected.dir != indexedDir {
				return nil, fmt.Errorf("indexed package %s is not selected by caller", pkg)
			}
			// shortcut: retain canonical Calls when offline selection is unavailable;
			// remove if repository-wide canonical navigation is retired. Definitions
			// never use this fallback.
		}
	}
	if len(files) == 0 {
		if !i.source {
			if !standard {
				return nil, fmt.Errorf("type information unavailable: %s", pkg)
			}
		}
		source := i.project.sourcePackage(pkg, i.origin, !i.source)
		if source.err != nil {
			return nil, source.err
		}
		files = source.files
		for filename, label := range source.labels {
			i.labels[filename] = label
		}
	}
	i.files[pkg] = files
	if i.loading[pkg] {
		return nil, fmt.Errorf("import cycle: %s", pkg)
	}
	i.loading[pkg] = true
	defer delete(i.loading, pkg)
	config := types.Config{Importer: i, IgnoreFuncBodies: true, Sizes: types.SizesFor(build.Default.Compiler, build.Default.GOARCH), Error: func(error) {}}
	result, err = config.Check(pkg, i.project.fset, files, nil)
	if result != nil {
		i.cache[pkg] = result
		return result, nil
	}
	return nil, err
}

// Dependencies selects import edges by source directory, not import-graph depth.
// dir is root-relative or an absolute directory inside Root; -1 means unlimited.
func (p *Project) Dependencies(dir string, depth int) ([]Edge, error) {
	if depth < -1 {
		return nil, fmt.Errorf("depth must be -1 or nonnegative")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(p.Root, dir)
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(p.Root, absolute)
	if err != nil || escapes(relative) {
		return nil, fmt.Errorf("directory escapes project root")
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("directory: %w", err)
	}
	relative, err = filepath.Rel(p.Root, absolute)
	if err != nil || escapes(relative) {
		return nil, fmt.Errorf("directory escapes project root")
	}
	stat, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}
	var edges []Edge
	for _, edge := range p.Imports {
		source := filepath.Dir(filepath.FromSlash(edge.From))
		child, relErr := filepath.Rel(relative, source)
		if relErr != nil || escapes(child) {
			continue
		}
		distance := 0
		if child != "." {
			distance = len(strings.Split(child, string(filepath.Separator)))
		}
		if depth == -1 || distance <= depth {
			edges = append(edges, edge)
		}
	}
	sortEdges(edges)
	return edges, nil
}

func escapes(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sortEdges(edges []Edge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return !edges[i].Unknown && edges[j].Unknown
	})
}

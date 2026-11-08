// Package analyze indexes Go source and lazily inspects one function at a time.
//
// All modules physically inside Root are indexed without fetching dependencies.
// Build constraints follow go/build.Default; _test.go, vendor, hidden
// directories/files and symlinks are excluded. Dependencies outside Root are
// not loaded from source; unavailable type information yields unknown calls.
package analyze

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
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

type Project struct {
	Root      string
	Functions map[string]Function
	Imports   []Edge

	fset         *token.FileSet
	packages     map[string][]*ast.File
	declarations map[string]*ast.FuncDecl
	owners       map[string]string
	sources      map[string][]byte
}

// Load records declarations and imports without type-checking function bodies.
// Root must contain at least one go.mod, possibly in a subdirectory. Only source
// beneath a go.mod is indexed, using the nearest module's canonical import path.
// File paths and import sources are slash-separated, relative to Root; method IDs
// are import/path.Receiver.Method (also for *Receiver).
func Load(root string) (*Project, error) {
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
	packageDirs := map[string]string{}
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
		p.sources[relative] = source
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
	source := p.sources[p.Functions[id].File]
	start := p.fset.PositionFor(fn.Pos(), false).Offset
	end := p.fset.PositionFor(fn.End(), false).Offset
	return string(source[start:end]), nil
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
// to indexed declarations; interface calls, function values and unresolved symbols
// are conservative unknowns.
func (p *Project) Calls(id string) ([]Edge, error) {
	fn, ok := p.declarations[id]
	if !ok {
		return nil, fmt.Errorf("function %q not found", id)
	}
	if fn.Body == nil {
		return nil, nil
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	loader := &signatureImporter{project: p, fallback: importer.Default(), cache: map[string]*types.Package{}, loading: map[string]bool{}}
	config := types.Config{Importer: loader, Error: func(error) {}}
	// Type errors (e.g. unavailable third-party imports) leave partial information;
	// calls without a proven static target remain unknown rather than being guessed.
	files, body := p.typeFiles(p.owners[id], fn)
	_, _ = config.Check(p.owners[id], p.fset, files, info)
	var edges []Edge
	seen := map[Edge]bool{}
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
							if _, indexed := p.Functions[target]; indexed {
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
		if !seen[edge] {
			seen[edge] = true
			edges = append(edges, edge)
		}
		return true
	})
	sortEdges(edges)
	return edges, nil
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
// Calls must inspect the returned body so copied expressions match types.Info.
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
	project  *Project
	fallback types.Importer
	cache    map[string]*types.Package
	loading  map[string]bool
}

func (i *signatureImporter) Import(pkg string) (*types.Package, error) {
	if cached := i.cache[pkg]; cached != nil {
		return cached, nil
	}
	if _, local := i.project.packages[pkg]; !local {
		return i.fallback.Import(pkg)
	}
	if i.loading[pkg] {
		return nil, fmt.Errorf("import cycle: %s", pkg)
	}
	i.loading[pkg] = true
	defer delete(i.loading, pkg)
	config := types.Config{Importer: i, IgnoreFuncBodies: true, Error: func(error) {}}
	files, _ := i.project.typeFiles(pkg, nil)
	result, err := config.Check(pkg, i.project.fset, files, nil)
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

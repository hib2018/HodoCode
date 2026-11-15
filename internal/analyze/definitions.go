package analyze

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
)

// Definition contains static declaration sources, never inferred runtime values.
// Reason explains unavailable definitions in Japanese; partial results may have both.
type Definition struct {
	Snippets   []DefinitionSource
	Reason     string
	VariableID int  // Declaration position within the snapshot; zero for nonvariables or unresolved identifiers.
	Length     int  // Identifier length in bytes.
	Unresolved bool // No object was resolved for this identifier.
	PopupOnly  bool // Snippets newly exposed for popup; existing definition pane keeps generic help.
}

// DefinitionSource preserves indexed bytes and a physical, 1-based line number.
// File is slash-separated: root-relative, GOROOT/src/... for SDK source, or
// a module@version label with replace/workspace/vendor origin for dependencies.
// Display labels never replace the source snapshot's stable storage keys.
type DefinitionSource struct {
	File   string
	Line   int
	Source string
}

// Definitions looks up SDK objects, root/dependency struct type names, and all
// variables, including the selected function's signature and closures.
// Keys are identifier byte offsets from Source(id). Builtins and unavailable
// sources have a Reason; resolved unrelated root/dependency identifiers are omitted.
// Variable declarations precede any existing slice-element type help.
// All snippets preserve parsed snapshot bytes and source object positions.
func (p *Project) Definitions(id string) (map[int]Definition, error) {
	fn, ok := p.declarations[id]
	if !ok {
		return nil, fmt.Errorf("function %q not found", id)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	loader := p.newImporter(id, true)
	config := types.Config{Importer: loader, Sizes: types.SizesFor(build.Default.Compiler, build.Default.GOARCH), Error: func(error) {}}
	files, _ := p.typeFiles(p.owners[id], fn)
	// Restore only the selected declaration, including closures, without mutating
	// either the index or typeFiles' body-omission policy for call analysis.
	for i, original := range files {
		for j, decl := range original.Decls {
			if decl.Pos() == fn.Pos() {
				file := *original
				file.Decls = append([]ast.Decl(nil), original.Decls...)
				file.Decls[j] = fn
				files[i] = &file
				break
			}
		}
	}
	_, _ = config.Check(p.owners[id], p.fset, files, info)

	declarations := p.definitionNodes()
	for _, files := range loader.files {
		addDefinitionNodes(declarations, files)
	}
	result := map[int]Definition{}
	start := p.fset.PositionFor(fn.Pos(), false).Offset
	ast.Inspect(fn, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name == "_" {
			return true
		}
		obj := info.Uses[ident]
		if obj == nil {
			obj = info.Defs[ident]
		}
		definition := Definition{Length: len(ident.Name)}
		appendObject := func(obj types.Object) {
			_, builtin := obj.(*types.Builtin)
			if obj.Pkg() == nil || builtin {
				definition.Reason = "組み込み識別子には Go ソース定義がありません"
				return
			}
			node := declarations[obj.Pos()]
			if node == nil {
				definition.Reason = "定義元のソースを特定できません"
				return
			}
			snippet := p.definitionSource(node)
			filename := p.fset.PositionFor(node.Pos(), false).Filename
			if label := loader.labels[filename]; label != "" {
				snippet.File = label
			}
			for _, previous := range definition.Snippets {
				if previous == snippet {
					return
				}
			}
			definition.Snippets = append(definition.Snippets, snippet)
		}
		appendStruct := func(t types.Type) {
			t = types.Unalias(t)
			if pointer, ok := t.(*types.Pointer); ok {
				t = types.Unalias(pointer.Elem())
			}
			if t.Underlying() == types.Typ[types.Invalid] {
				definition.Reason = "識別子の型を解決できません"
			}
			if named, ok := t.(*types.Named); ok {
				if _, ok := named.Underlying().(*types.Struct); ok {
					appendObject(named.Obj())
				}
			}
		}
		// SDK identifiers expose every object kind. Builtins are explained
		// rather than invented.
		if obj != nil {
			_, packageName := obj.(*types.PkgName)
			if obj.Pkg() != nil && !packageName {
				if _, standard := standardDir(obj.Pkg().Path()); standard {
					appendObject(obj)
				}
			} else if obj.Parent() == types.Universe {
				appendObject(obj)
			}
		}
		switch obj := obj.(type) {
		case nil:
			definition.Unresolved = true
			definition.Reason = "識別子の定義または型を解決できません"
		case *types.TypeName:
			if _, ok := obj.Type().Underlying().(*types.Struct); ok {
				appendObject(obj)
				appendStruct(obj.Type())
			} else if obj.Type().Underlying() == types.Typ[types.Invalid] {
				definition.Reason = "識別子の型を解決できません"
			}
		case *types.Var:
			definition.VariableID = int(obj.Pos())
			slice, isSlice := obj.Type().Underlying().(*types.Slice)
			definition.PopupOnly = len(definition.Snippets) == 0 && !isSlice
			appendObject(obj)
			if isSlice {
				appendStruct(slice.Elem())
			} else if obj.Type().Underlying() == types.Typ[types.Invalid] {
				definition.Reason = "識別子の型を解決できません"
			}
		}
		if len(definition.Snippets) != 0 || definition.Reason != "" {
			result[p.fset.PositionFor(ident.Pos(), false).Offset-start] = definition
		}
		return true
	})
	return result, nil
}

// Map object declaration positions to complete syntactic declaration sources.
func (p *Project) definitionNodes() map[token.Pos]ast.Node {
	nodes := map[token.Pos]ast.Node{}
	for _, files := range p.packages {
		addDefinitionNodes(nodes, files)
	}
	return nodes
}

func addDefinitionNodes(nodes map[token.Pos]ast.Node, files []*ast.File) {
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.FuncDecl:
				nodes[n.Name.Pos()] = n
			case *ast.GenDecl:
				for _, spec := range n.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						nodes[s.Name.Pos()] = n
					case *ast.ValueSpec:
						for _, name := range s.Names {
							nodes[name.Pos()] = n
						}
					}
				}
			case *ast.AssignStmt:
				if n.Tok == token.DEFINE {
					for _, lhs := range n.Lhs {
						if name, ok := lhs.(*ast.Ident); ok {
							nodes[name.Pos()] = n
						}
					}
				}
			case *ast.RangeStmt:
				if n.Tok == token.DEFINE {
					for _, expr := range []ast.Expr{n.Key, n.Value} {
						if name, ok := expr.(*ast.Ident); ok {
							nodes[name.Pos()] = n
						}
					}
				}
			case *ast.Field:
				for _, name := range n.Names {
					nodes[name.Pos()] = n
				}
				if len(n.Names) == 0 {
					ast.Inspect(n.Type, func(node ast.Node) bool {
						if name, ok := node.(*ast.Ident); ok {
							nodes[name.Pos()] = n
						}
						return true
					})
				}
			}
			return true
		})
	}
}

func (p *Project) definitionSource(node ast.Node) DefinitionSource {
	pos := p.fset.PositionFor(node.Pos(), false)
	name := p.sourceName(pos.Filename)
	end := p.fset.PositionFor(node.End(), false).Offset
	if loop, ok := node.(*ast.RangeStmt); ok {
		end = p.fset.PositionFor(loop.X.End(), false).Offset
	}
	return DefinitionSource{File: name, Line: pos.Line, Source: string(p.sources[pos.Filename][pos.Offset:end])}
}

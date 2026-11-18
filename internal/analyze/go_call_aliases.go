package analyze

import (
	"go/ast"
	"go/token"
	"go/types"
)

// goCallAliases proves only local definitions initialized with a direct function.
// Reassignment anywhere invalidates the alias, even before the first call.
func goCallAliases(body, original *ast.BlockStmt, info *types.Info) map[types.Object]*types.Func {
	aliases := map[types.Object]*types.Func{}
	define := func(name *ast.Ident, value ast.Expr) {
		variable, ok := info.Defs[name].(*types.Var)
		if !ok || variable.IsField() || name.Name == "_" {
			return
		}
		if _, ok := variable.Type().Underlying().(*types.Signature); !ok {
			return
		}
		var object types.Object
		switch value := unparen(value).(type) {
		case *ast.Ident:
			object = info.Uses[value]
		case *ast.SelectorExpr:
			object = info.Uses[value.Sel]
		}
		if fn, ok := object.(*types.Func); ok && fn.Pkg() != nil {
			if signature, ok := fn.Type().(*types.Signature); ok && signature.Recv() == nil && types.AssignableTo(fn.Type(), variable.Type()) {
				aliases[variable] = fn
			}
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if node.Tok == token.DEFINE && len(node.Lhs) == 1 && len(node.Rhs) == 1 {
				if name, ok := node.Lhs[0].(*ast.Ident); ok {
					define(name, node.Rhs[0])
				}
			}
		case *ast.ValueSpec:
			if len(node.Names) == 1 && len(node.Values) == 1 {
				define(node.Names[0], node.Values[0])
			}
		}
		return true
	})
	invalidate := func(expr ast.Expr) {
		if expr == nil {
			return
		}
		if name, ok := unparen(expr).(*ast.Ident); ok {
			delete(aliases, info.Uses[name])
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				invalidate(lhs)
			}
		case *ast.RangeStmt:
			invalidate(node.Key)
			invalidate(node.Value)
		case *ast.UnaryExpr:
			if node.Op == token.AND {
				invalidate(node.X)
			}
		}
		return true
	})
	// shortcut: closure bodies are not type-checked; reject same-name references
	// there until closure-aware object information is available.
	ast.Inspect(original, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		ast.Inspect(literal, func(child ast.Node) bool {
			if name, ok := child.(*ast.Ident); ok {
				for variable := range aliases {
					if variable.Name() == name.Name {
						delete(aliases, variable)
					}
				}
			}
			return true
		})
		return false
	})
	return aliases
}

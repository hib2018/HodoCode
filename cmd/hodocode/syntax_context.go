package main

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
)

// Classify syntax, not runtime types; malformed snippets retain token-level help.
func sourceTopics(source string) map[int]string {
	const prefix = "package syntaxhelp\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", prefix+source, 0)
	topics := map[int]string{}
	if err != nil {
		return topics
	}
	mark := func(pos token.Pos, topic string) {
		if !pos.IsValid() {
			return
		}
		offset := fset.PositionFor(pos, false).Offset - len(prefix)
		if offset >= 0 && offset < len(source) {
			topics[offset] = topic
		}
	}
	var markType func(ast.Expr)
	markType = func(expr ast.Expr) {
		switch e := expr.(type) {
		case *ast.Ident:
			if e.Obj == nil && syntaxTopics["type_"+e.Name].title != "" {
				mark(e.Pos(), "type_"+e.Name)
			}
		case *ast.StarExpr:
			mark(e.Star, "pointer_type")
			markType(e.X)
		case *ast.ArrayType:
			topic := "array_type"
			if e.Len == nil {
				topic = "slice_type"
			}
			mark(e.Lbrack, topic)
			markType(e.Elt)
		case *ast.MapType:
			markType(e.Key)
			markType(e.Value)
		case *ast.ChanType:
			markType(e.Value)
		case *ast.ParenExpr:
			markType(e.X)
		case *ast.Ellipsis:
			mark(e.Ellipsis, "variadic_param")
			markType(e.Elt)
		case *ast.UnaryExpr:
			if e.Op == token.TILDE {
				markType(e.X)
			}
		case *ast.BinaryExpr:
			if e.Op == token.OR {
				markType(e.X)
				markType(e.Y)
			}
		case *ast.IndexExpr:
			mark(e.Lbrack, "type_arguments")
			mark(e.Rbrack, "type_arguments")
		case *ast.IndexListExpr:
			mark(e.Lbrack, "type_arguments")
			mark(e.Rbrack, "type_arguments")
		}
	}
	var knownType func(ast.Expr) bool
	knownType = func(expr ast.Expr) bool {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Obj != nil && e.Obj.Kind == ast.Typ || e.Obj == nil && syntaxTopics["type_"+e.Name].title != ""
		case *ast.StarExpr:
			return knownType(e.X)
		case *ast.ParenExpr:
			return knownType(e.X)
		case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.StructType, *ast.InterfaceType:
			return true
		}
		return false
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			if n.Obj != nil {
				mark(n.Pos(), "identifier")
			}
		case *ast.SelectorExpr:
			mark(n.Sel.Pos(), "identifier")
		case *ast.Field:
			for _, name := range n.Names {
				mark(name.Pos(), "identifier")
			}
			markType(n.Type)
		case *ast.KeyValueExpr:
			if name, ok := n.Key.(*ast.Ident); ok && (syntaxTopics["builtin_"+name.Name].title != "" || syntaxTopics["type_"+name.Name].title != "") {
				mark(name.Pos(), "identifier")
			}
		case *ast.ValueSpec:
			markType(n.Type)
		case *ast.TypeSpec:
			markType(n.Type)
			if n.Assign.IsValid() {
				mark(n.Assign, "type_alias")
			}
		case *ast.CompositeLit:
			markType(n.Type)
			mark(n.Lbrace, "composite_literal")
			mark(n.Rbrace, "composite_literal")
		case *ast.StarExpr:
			offset := fset.PositionFor(n.Star, false).Offset - len(prefix)
			if topics[offset] == "" {
				mark(n.Star, "dereference")
			}
		case *ast.BinaryExpr:
			switch n.Op {
			case token.MUL:
				mark(n.OpPos, "multiply")
			case token.AND:
				mark(n.OpPos, "bitwise_and")
			}
		case *ast.UnaryExpr:
			switch n.Op {
			case token.AND:
				mark(n.OpPos, "address_of")
			case token.ARROW:
				mark(n.OpPos, "receive")
			case token.ADD:
				mark(n.OpPos, "unary_plus")
			case token.SUB:
				mark(n.OpPos, "unary_minus")
			case token.XOR:
				mark(n.OpPos, "bitwise_not")
			}
		case *ast.SendStmt:
			mark(n.Arrow, "send")
		case *ast.ChanType:
			if n.Dir == ast.SEND {
				mark(n.Begin, "chan_send")
				mark(n.Arrow, "chan_send")
			}
			if n.Dir == ast.RECV {
				mark(n.Begin, "chan_receive")
				mark(n.Arrow, "chan_receive")
			}
		case *ast.ForStmt:
			topic := "for_clause"
			if n.Init == nil && n.Post == nil {
				if n.Cond == nil {
					topic = "for_infinite"
				} else {
					topic = "for_condition"
				}
			}
			mark(n.For, topic)
		case *ast.RangeStmt:
			mark(n.For, "range_loop")
		case *ast.SwitchStmt:
			mark(n.Switch, "expression_switch")
		case *ast.TypeSwitchStmt:
			mark(n.Switch, "type_switch")
		case *ast.CaseClause:
			if n.List != nil {
				mark(n.Case, "switch_case")
			}
		case *ast.CommClause:
			if n.Comm != nil {
				mark(n.Case, "select_case")
			}
		case *ast.FuncDecl:
			topic := "func_decl"
			if n.Recv != nil {
				topic = "method_decl"
			}
			mark(n.Type.Func, topic)
		case *ast.FuncLit:
			mark(n.Type.Func, "func_literal")
		case *ast.FuncType:
			offset := fset.PositionFor(n.Func, false).Offset - len(prefix)
			if topics[offset] == "" {
				mark(n.Func, "func_type")
			}
		case *ast.ArrayType:
			markType(n)
		case *ast.IndexExpr:
			offset := fset.PositionFor(n.Lbrack, false).Offset - len(prefix)
			if topics[offset] == "" {
				mark(n.Lbrack, "index")
				mark(n.Rbrack, "index")
			}
		case *ast.IndexListExpr:
			mark(n.Lbrack, "type_arguments")
			mark(n.Rbrack, "type_arguments")
		case *ast.SliceExpr:
			topic := "slice"
			if n.Slice3 {
				topic = "full_slice"
			}
			mark(n.Lbrack, topic)
			mark(n.Rbrack, topic)
		case *ast.TypeAssertExpr:
			if n.Type != nil {
				mark(n.Lparen-1, "type_assertion")
				mark(n.Lparen, "type_assertion")
				mark(n.Rparen, "type_assertion")
				markType(n.Type)
			} else {
				start := fset.PositionFor(n.Lparen, false).Offset - len(prefix)
				end := fset.PositionFor(n.Rparen, false).Offset - len(prefix)
				if start >= 0 && end < len(source) {
					fragment := []byte(source[start : end+1])
					positions := token.NewFileSet().AddFile("", -1, len(fragment))
					var scan scanner.Scanner
					scan.Init(positions, fragment, nil, 0)
					for {
						pos, tok, _ := scan.Scan()
						if tok == token.EOF {
							break
						}
						if tok == token.TYPE {
							topics[start+positions.Offset(pos)] = "type_switch"
						}
					}
				}
			}
		case *ast.CallExpr:
			if knownType(n.Fun) {
				markType(n.Fun)
			}
			if n.Ellipsis.IsValid() {
				mark(n.Ellipsis, "argument_spread")
			}
			if name, ok := n.Fun.(*ast.Ident); ok && name.Obj == nil && syntaxTopics["builtin_"+name.Name].title != "" {
				mark(name.Pos(), "builtin_"+name.Name)
			}
		case *ast.Ellipsis:
			offset := fset.PositionFor(n.Ellipsis, false).Offset - len(prefix)
			if topics[offset] == "" {
				mark(n.Ellipsis, "array_length")
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				topic := "interpreted_string"
				if len(n.Value) > 0 && n.Value[0] == '`' {
					topic = "raw_string"
				}
				mark(n.Pos(), topic)
			}
		}
		return true
	})
	return topics
}

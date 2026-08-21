package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
)

type stringBuffer struct {
	producer string
	term     string
}

func checkCStringArgument(c CType, expr ast.Expr, body *ast.BlockStmt) (checked, clean bool, detail string) {
	if !isCCharPointer(c.Raw) {
		return false, false, ""
	}
	buffer := cStringBuffer(expr, body, 0)
	if buffer.producer == "" {
		return false, false, ""
	}
	if buffer.term != "" {
		return true, true, ""
	}
	return true, false, fmt.Sprintf("C %s requires a NUL-terminated buffer, but %s is built from %s without a terminator",
		c.Raw, goExpression(expr), buffer.producer)
}

func isCCharPointer(raw string) bool {
	typ := strings.Join(strings.Fields(strings.NewReplacer("const", " ", "volatile", " ").Replace(normalizeType(raw))), " ")
	return strings.Count(typ, "*") == 1 && strings.TrimSpace(strings.TrimSuffix(typ, "*")) == "char"
}

func cStringBuffer(expr ast.Expr, body *ast.BlockStmt, depth int) stringBuffer {
	if body == nil || depth > 6 {
		return stringBuffer{}
	}
	expr = unwrapGoPointer(unparenExpr(expr))
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return stringBuffer{}
	}
	return cStringPointer(unary.X, body, depth+1)
}

func cStringPointer(expr ast.Expr, body *ast.BlockStmt, depth int) stringBuffer {
	if depth > 6 {
		return stringBuffer{}
	}
	switch x := unparenExpr(expr).(type) {
	case *ast.Ident:
		return mergeStringBuffers(assignedExpressions(x.Name, body), body, depth+1, cStringPointer)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if index, ok := unparenExpr(x.X).(*ast.IndexExpr); ok && goExpression(index.Index) == "0" {
				return cByteSlice(index.X, body, depth+1)
			}
		}
	case *ast.CallExpr:
		callee := goExpression(x.Fun)
		if callee == "utils.BytePtrFromString" {
			return stringBuffer{producer: goExpression(x), term: "BytePtrFromString contract"}
		}
		if callee == "unsafe.SliceData" && len(x.Args) == 1 {
			return cByteSlice(x.Args[0], body, depth+1)
		}
	}
	return stringBuffer{}
}

func cByteSlice(expr ast.Expr, body *ast.BlockStmt, depth int) stringBuffer {
	if depth > 6 {
		return stringBuffer{}
	}
	switch x := unparenExpr(expr).(type) {
	case *ast.Ident:
		return mergeStringBuffers(assignedExpressions(x.Name, body), body, depth+1, cByteSlice)
	case *ast.CallExpr:
		if array, ok := x.Fun.(*ast.ArrayType); ok && array.Len == nil && len(x.Args) == 1 {
			if id, ok := array.Elt.(*ast.Ident); ok && (id.Name == "byte" || id.Name == "uint8") {
				return stringBuffer{producer: goExpression(x), term: terminatingString(x.Args[0], body, depth+1)}
			}
		}
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "append" && len(x.Args) >= 2 {
			last := x.Args[len(x.Args)-1]
			if goExpression(last) == "0" || goExpression(last) == "'\\x00'" {
				return stringBuffer{producer: goExpression(x), term: "append ends in zero"}
			}
		}
	case *ast.CompositeLit:
		if len(x.Elts) > 0 {
			last := goExpression(x.Elts[len(x.Elts)-1])
			buffer := stringBuffer{producer: goExpression(x)}
			if last == "0" || last == "'\\x00'" || last == "0x00" {
				buffer.term = "byte literal ends in zero"
			}
			return buffer
		}
	}
	return stringBuffer{}
}

func terminatingString(expr ast.Expr, body *ast.BlockStmt, depth int) string {
	if depth > 6 {
		return ""
	}
	switch x := unparenExpr(expr).(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if value, err := strconv.Unquote(x.Value); err == nil && strings.HasSuffix(value, "\x00") {
				return "string ends in NUL"
			}
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return terminatingString(x.Y, body, depth+1)
		}
	case *ast.Ident:
		for _, assigned := range assignedExpressions(x.Name, body) {
			if term := terminatingString(assigned, body, depth+1); term != "" {
				return term
			}
		}
	}
	return ""
}

func assignedExpressions(name string, body *ast.BlockStmt) []ast.Expr {
	var result []ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name != name {
					continue
				}
				if i < len(x.Rhs) {
					result = append(result, x.Rhs[i])
				} else if len(x.Rhs) == 1 {
					result = append(result, x.Rhs[0])
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if id.Name == name && i < len(x.Values) {
					result = append(result, x.Values[i])
				}
			}
		}
		return true
	})
	return result
}

func mergeStringBuffers(expressions []ast.Expr, body *ast.BlockStmt, depth int, classify func(ast.Expr, *ast.BlockStmt, int) stringBuffer) stringBuffer {
	var found stringBuffer
	for _, expr := range expressions {
		buffer := classify(expr, body, depth+1)
		if buffer.producer == "" {
			continue
		}
		if buffer.term == "" {
			return buffer
		}
		found = buffer
	}
	return found
}

func unparenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func goExpression(expr ast.Expr) string {
	var out bytes.Buffer
	_ = printer.Fprint(&out, token.NewFileSet(), expr)
	return out.String()
}

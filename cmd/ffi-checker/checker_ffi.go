package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

type FFIType struct {
	Name    string
	Kind    CKind
	Size    int
	Align   int
	Fields  []FFIType
	Offsets []int
}

type FFIBinding struct {
	CName      string
	GoVar      string
	Return     FFIType
	Parameters []FFIType
	Variadic   bool
	NFixed     int
	File       string
	Line       int
}

type FFIIssue struct {
	File   string
	Line   int
	Detail string
}

type FFICatalog struct {
	Types    map[string]FFIType
	Bindings map[string][]FFIBinding
	Issues   []FFIIssue
}

type FFIViolation struct {
	Symbol string
	File   string
	Line   int
	Detail string
}

type FFIReport struct {
	Bindings   int
	Matched    int
	Clean      int
	Required   int
	Covered    int
	Missing    []CFunction
	Violations []FFIViolation
	Signedness []FFIViolation
	Unverified []string
}

var ffiScalars = map[string]FFIType{
	"TypeVoid":    {Name: "TypeVoid", Kind: CVoid, Size: 0, Align: 1},
	"TypeUint8":   {Name: "TypeUint8", Kind: CUnsigned, Size: 1, Align: 1},
	"TypeSint8":   {Name: "TypeSint8", Kind: CSigned, Size: 1, Align: 1},
	"TypeUint16":  {Name: "TypeUint16", Kind: CUnsigned, Size: 2, Align: 2},
	"TypeSint16":  {Name: "TypeSint16", Kind: CSigned, Size: 2, Align: 2},
	"TypeUint32":  {Name: "TypeUint32", Kind: CUnsigned, Size: 4, Align: 4},
	"TypeSint32":  {Name: "TypeSint32", Kind: CSigned, Size: 4, Align: 4},
	"TypeUint64":  {Name: "TypeUint64", Kind: CUnsigned, Size: 8, Align: 8},
	"TypeSint64":  {Name: "TypeSint64", Kind: CSigned, Size: 8, Align: 8},
	"TypeFloat":   {Name: "TypeFloat", Kind: CFloat, Size: 4, Align: 4},
	"TypeDouble":  {Name: "TypeDouble", Kind: CDouble, Size: 8, Align: 8},
	"TypePointer": {Name: "TypePointer", Kind: CPointer, Size: 8, Align: 8},
}

type ffiParser struct {
	fset     *token.FileSet
	catalog  *FFICatalog
	pending  map[string]ast.Expr
	handled  map[*ast.CallExpr]bool
	callVars map[*ast.CallExpr]string
}

func ParseFFI(dir string) (*FFICatalog, error) {
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax,
		Dir:  dir,
	}, ".")
	if err != nil {
		return nil, err
	}
	if len(loaded) != 1 || loaded[0].Name != "whisper" {
		return nil, fmt.Errorf("package whisper not found in %s", dir)
	}
	pkg := loaded[0]
	if len(pkg.Errors) > 0 {
		return nil, fmt.Errorf("parse package whisper: %s", pkg.Errors[0])
	}
	fset := pkg.Fset

	catalog := &FFICatalog{Types: map[string]FFIType{}, Bindings: map[string][]FFIBinding{}}
	p := ffiParser{
		fset: fset, catalog: catalog, pending: map[string]ast.Expr{},
		handled: map[*ast.CallExpr]bool{}, callVars: map[*ast.CallExpr]string{},
	}
	for name, typ := range ffiScalars {
		catalog.Types[name] = typ
	}

	for _, file := range pkg.Syntax {
		p.collectTypeDeclarations(file)
		p.collectBindingVars(file)
	}
	for range len(p.pending) + 1 {
		for name, expr := range p.pending {
			if typ, ok := p.resolveType(expr); ok {
				typ.Name = name
				catalog.Types[name] = typ
				delete(p.pending, name)
			}
		}
	}
	for name, expr := range p.pending {
		pos := fset.Position(expr.Pos())
		catalog.Issues = append(catalog.Issues, FFIIssue{File: pos.Filename, Line: pos.Line, Detail: "cannot resolve FFI type " + name})
	}

	for _, file := range pkg.Syntax {
		p.collectRangeBindings(file)
	}
	for _, file := range pkg.Syntax {
		p.collectDirectBindings(file)
	}
	return catalog, nil
}

func (p *ffiParser) collectBindingVars(file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := prepCall(assign.Rhs[0])
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if ok && id.Name != "_" && id.Name != "err" && id.Name != "perr" {
				p.callVars[call] = bindingAlias(file, id.Name)
				break
			}
		}
		return true
	})
}

func bindingAlias(file *ast.File, name string) string {
	alias := name
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		rhs, ok := assign.Rhs[0].(*ast.Ident)
		if !ok || rhs.Name != name {
			return true
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if ok && strings.HasSuffix(lhs.Name, "Func") {
			alias = lhs.Name
		}
		return true
	})
	return alias
}

func (p *ffiParser) collectTypeDeclarations(file *ast.File) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if i < len(value.Values) && isFFITypeExpr(value.Values[i]) {
					p.pending[name.Name] = value.Values[i]
				}
			}
		}
	}
}

func isFFITypeExpr(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && id.Name == "ffi" && strings.HasPrefix(x.Sel.Name, "Type")
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == "ffi" && sel.Sel.Name == "NewType"
	}
	return false
}

func (p *ffiParser) resolveType(expr ast.Expr) (FFIType, bool) {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		return p.resolveType(unary.X)
	}
	switch x := expr.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok || id.Name != "ffi" {
			return FFIType{}, false
		}
		typ, ok := ffiScalars[x.Sel.Name]
		return typ, ok
	case *ast.Ident:
		typ, ok := p.catalog.Types[x.Name]
		return typ, ok
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewType" {
			return FFIType{}, false
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != "ffi" {
			return FFIType{}, false
		}
		return p.buildStruct(x.Args)
	}
	return FFIType{}, false
}

func (p *ffiParser) buildStruct(exprs []ast.Expr) (FFIType, bool) {
	result := FFIType{Kind: CStructValue, Align: 1}
	offset := 0
	for _, expr := range exprs {
		field, ok := p.resolveType(expr)
		if !ok || field.Size <= 0 {
			return FFIType{}, false
		}
		align := field.Align
		if align <= 0 {
			align = min(field.Size, 8)
		}
		if offset%align != 0 {
			offset += align - offset%align
		}
		result.Fields = append(result.Fields, field)
		result.Offsets = append(result.Offsets, offset)
		offset += field.Size
		result.Align = max(result.Align, align)
	}
	if offset%result.Align != 0 {
		offset += result.Align - offset%result.Align
	}
	result.Size = offset
	return result, true
}

func (p *ffiParser) collectRangeBindings(file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		types := localStructTypes(fn.Body)
		tables := localTables(fn.Body, types)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			rng, ok := node.(*ast.RangeStmt)
			if !ok {
				return true
			}
			name, ok := rng.X.(*ast.Ident)
			if !ok {
				return true
			}
			rows := tables[name.Name]
			if len(rows) == 0 {
				return true
			}
			value, ok := rng.Value.(*ast.Ident)
			if !ok {
				return true
			}
			ast.Inspect(rng.Body, func(node ast.Node) bool {
				call, ok := prepCall(node)
				if !ok || !usesRow(call.Args[0], value.Name) {
					return true
				}
				p.handled[call] = true
				for _, row := range rows {
					p.addBinding(call, row, value.Name, row["@pos"], rowBindingVar(row))
				}
				return true
			})
			return true
		})
		return false
	})
}

func rowBindingVar(row map[string]ast.Expr) string {
	for _, key := range []string{"out", "fn"} {
		unary, ok := row[key].(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			continue
		}
		if id, ok := unary.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func localStructTypes(body *ast.BlockStmt) map[string][]string {
	result := map[string][]string{}
	for _, stmt := range body.List {
		decl, ok := stmt.(*ast.DeclStmt)
		if !ok {
			continue
		}
		gen, ok := decl.Decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec := spec.(*ast.TypeSpec)
			if st, ok := typeSpec.Type.(*ast.StructType); ok {
				result[typeSpec.Name.Name] = structFieldNames(st)
			}
		}
	}
	return result
}

func localTables(body *ast.BlockStmt, types map[string][]string) map[string][]map[string]ast.Expr {
	result := map[string][]map[string]ast.Expr{}
	for _, stmt := range body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		name, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			continue
		}
		list, ok := assign.Rhs[0].(*ast.CompositeLit)
		if !ok {
			continue
		}
		array, ok := list.Type.(*ast.ArrayType)
		if !ok {
			continue
		}
		var fields []string
		switch element := array.Elt.(type) {
		case *ast.StructType:
			fields = structFieldNames(element)
		case *ast.Ident:
			fields = types[element.Name]
		}
		if len(fields) == 0 {
			continue
		}
		for _, element := range list.Elts {
			rowLit, ok := element.(*ast.CompositeLit)
			if !ok {
				continue
			}
			row := map[string]ast.Expr{"@pos": rowLit}
			for i, value := range rowLit.Elts {
				if pair, ok := value.(*ast.KeyValueExpr); ok {
					if key, ok := pair.Key.(*ast.Ident); ok {
						row[key.Name] = pair.Value
					}
				} else if i < len(fields) {
					row[fields[i]] = value
				}
			}
			result[name.Name] = append(result[name.Name], row)
		}
	}
	return result
}

func structFieldNames(st *ast.StructType) []string {
	var result []string
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			result = append(result, name.Name)
		}
	}
	return result
}

func usesRow(expr ast.Expr, row string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == row
}

func prepCall(node ast.Node) (*ast.CallExpr, bool) {
	call, ok := node.(*ast.CallExpr)
	if !ok || len(call.Args) < 2 {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Prep", "MustPrep", "PrepVar", "MustPrepVar":
		return call, true
	}
	return nil, false
}

func (p *ffiParser) collectDirectBindings(file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := prepCall(node)
		if !ok || p.handled[call] {
			return true
		}
		if _, ok := call.Args[0].(*ast.BasicLit); !ok {
			pos := p.fset.Position(call.Pos())
			p.catalog.Issues = append(p.catalog.Issues, FFIIssue{File: pos.Filename, Line: pos.Line, Detail: "cannot resolve dynamic ffi.Prep binding"})
			return true
		}
		p.addBinding(call, nil, "", call, p.callVars[call])
		return true
	})
}

func (p *ffiParser) addBinding(call *ast.CallExpr, row map[string]ast.Expr, rowName string, position ast.Expr, goVar string) {
	resolve := func(expr ast.Expr) ast.Expr {
		if sel, ok := expr.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == rowName {
				return row[sel.Sel.Name]
			}
		}
		return expr
	}
	nameExpr := resolve(call.Args[0])
	literal, ok := nameExpr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		p.issue(position, "cannot resolve ffi.Prep symbol name")
		return
	}
	name, err := strconv.Unquote(literal.Value)
	if err != nil {
		p.issue(position, "cannot decode ffi.Prep symbol name")
		return
	}

	sel := call.Fun.(*ast.SelectorExpr)
	start := 1
	binding := FFIBinding{CName: name, GoVar: goVar, NFixed: -1}
	if sel.Sel.Name == "PrepVar" || sel.Sel.Name == "MustPrepVar" {
		binding.Variadic = true
		start = 2
		if n, ok := integerLiteral(resolve(call.Args[1])); ok {
			binding.NFixed = n
		}
	}
	var typeExprs []ast.Expr
	for _, expr := range call.Args[start:] {
		expr = resolve(expr)
		if list, ok := expr.(*ast.CompositeLit); ok && call.Ellipsis.IsValid() {
			typeExprs = append(typeExprs, list.Elts...)
			continue
		}
		typeExprs = append(typeExprs, expr)
	}
	if len(typeExprs) == 1 && call.Ellipsis.IsValid() {
		if id, ok := typeExprs[0].(*ast.Ident); ok {
			if list, ok := row[id.Name].(*ast.CompositeLit); ok {
				typeExprs = append([]ast.Expr(nil), list.Elts...)
			}
		}
	}
	if len(typeExprs) == 0 {
		p.issue(position, "ffi.Prep has no return descriptor")
		return
	}
	ret, ok := p.resolveType(typeExprs[0])
	if !ok {
		p.issue(position, "cannot resolve return descriptor for "+name)
		return
	}
	binding.Return = ret
	for _, expr := range typeExprs[1:] {
		typ, ok := p.resolveType(expr)
		if !ok {
			p.issue(position, "cannot resolve argument descriptor for "+name)
			return
		}
		binding.Parameters = append(binding.Parameters, typ)
	}
	pos := p.fset.Position(position.Pos())
	binding.File, binding.Line = pos.Filename, pos.Line
	p.catalog.Bindings[name] = append(p.catalog.Bindings[name], binding)
}

func integerLiteral(expr ast.Expr) (int, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	n, err := strconv.Atoi(lit.Value)
	return n, err == nil
}

func (p *ffiParser) issue(expr ast.Expr, detail string) {
	pos := p.fset.Position(expr.Pos())
	p.catalog.Issues = append(p.catalog.Issues, FFIIssue{File: pos.Filename, Line: pos.Line, Detail: detail})
}

func CheckFFI(api *CAPI, ffi *FFICatalog) FFIReport {
	var report FFIReport
	functionNames := make([]string, 0, len(api.Functions))
	for name, fn := range api.Functions {
		if fn.File == "whisper.h" {
			functionNames = append(functionNames, name)
		}
	}
	sort.Strings(functionNames)
	for _, name := range functionNames {
		fn := api.Functions[name]
		report.Required++
		if len(ffi.Bindings[name]) == 0 {
			report.Missing = append(report.Missing, fn)
			continue
		}
		report.Covered++
	}

	names := make([]string, 0, len(ffi.Bindings))
	for name := range ffi.Bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bindings := ffi.Bindings[name]
		for _, binding := range bindings {
			report.Bindings++
			fn, ok := api.Functions[name]
			if !ok {
				report.Violations = append(report.Violations, ffiViolation(binding, "no matching C declaration"))
				continue
			}
			report.Matched++
			var problems, unverified []string
			switch {
			case binding.Variadic != fn.Variadic:
				problems = append(problems, "variadic convention differs")
			case binding.Variadic && binding.NFixed < 0:
				unverified = append(unverified, "PrepVar fixed argument count is unresolved")
			case binding.Variadic && binding.NFixed != len(fn.Parameters):
				problems = append(problems, fmt.Sprintf("PrepVar has %d fixed arguments; C has %d", binding.NFixed, len(fn.Parameters)))
			case !binding.Variadic && len(binding.Parameters) != len(fn.Parameters):
				problems = append(problems, fmt.Sprintf("arity: FFI has %d arguments; C has %d", len(binding.Parameters), len(fn.Parameters)))
			}

			compareFFIType(api, binding.Return, fn.ReturnType, "return", &problems, &unverified)
			for _, detail := range ffiSignedness(api, binding.Return, fn.ReturnType, "return") {
				report.Signedness = append(report.Signedness, ffiViolation(binding, detail))
			}
			n := min(len(binding.Parameters), len(fn.Parameters))
			for i := range n {
				compareFFIType(api, binding.Parameters[i], fn.Parameters[i].Type, fmt.Sprintf("arg%d", i), &problems, &unverified)
				for _, detail := range ffiSignedness(api, binding.Parameters[i], fn.Parameters[i].Type, fmt.Sprintf("arg%d", i)) {
					report.Signedness = append(report.Signedness, ffiViolation(binding, detail))
				}
			}
			for _, detail := range problems {
				report.Violations = append(report.Violations, ffiViolation(binding, detail))
			}
			for _, detail := range unverified {
				report.Unverified = append(report.Unverified, fmt.Sprintf("%s (%s:%d): %s", name, filepath.Base(binding.File), binding.Line, detail))
			}
			if len(problems) == 0 && len(unverified) == 0 {
				report.Clean++
			}
		}
	}
	return report
}

func ffiViolation(binding FFIBinding, detail string) FFIViolation {
	return FFIViolation{Symbol: binding.CName, File: binding.File, Line: binding.Line, Detail: detail}
}

func compareFFIType(api *CAPI, ffi FFIType, c CType, slot string, problems, unverified *[]string) {
	if c.Kind == CUnknown || c.Size < 0 {
		*unverified = append(*unverified, fmt.Sprintf("%s C type %q is unresolved", slot, c.Raw))
		return
	}
	if ffi.Kind == CUnknown || ffi.Size < 0 {
		*unverified = append(*unverified, fmt.Sprintf("%s FFI descriptor %q is unresolved", slot, ffi.Name))
		return
	}
	if c.Kind == CStructValue {
		if ffi.Kind != CStructValue {
			*problems = append(*problems, fmt.Sprintf("%s: C passes %s by value but FFI uses %s", slot, c.Raw, ffi.Name))
			return
		}
		if c.Size != ffi.Size {
			*problems = append(*problems, fmt.Sprintf("%s: C %s is %dB but FFI %s is %dB", slot, c.Raw, c.Size, ffi.Name, ffi.Size))
		}
		cStruct := api.Structs[structTypeName(api, c.Raw)]
		cLeaves, cOK := flattenCStruct(api, cStruct, 0, 0)
		ffiLeaves, ffiOK := flattenFFIType(ffi, 0, 0)
		if !cOK || !ffiOK {
			*unverified = append(*unverified, fmt.Sprintf("%s struct member layout is unresolved", slot))
			return
		}
		if difference := compareLeaves(ffiLeaves, cLeaves); difference != "" {
			*problems = append(*problems, slot+": struct layout differs: "+difference)
		}
		return
	}
	if ffi.Size != c.Size {
		*problems = append(*problems, fmt.Sprintf("%s: C %s is %dB but FFI %s is %dB", slot, c.Raw, c.Size, ffi.Name, ffi.Size))
		return
	}
	if !ffiKindsCompatible(ffi.Kind, c.Kind) {
		*problems = append(*problems, fmt.Sprintf("%s: C %s is %s but FFI %s is %s", slot, c.Raw, c.Kind, ffi.Name, ffi.Kind))
	}
}

func ffiKindsCompatible(left, right CKind) bool {
	if left == right {
		return true
	}
	return (left == CSigned || left == CUnsigned) && (right == CSigned || right == CUnsigned)
}

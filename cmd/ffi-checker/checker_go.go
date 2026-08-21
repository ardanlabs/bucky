package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type GoCallReport struct {
	Calls             int
	Clean             int
	EnumArgs          int
	CleanEnumArgs     int
	StringArgs        int
	CleanStrings      int
	UnverifiedStrings int
	Deprecated        int
	CleanDeprecated   int
	Violations        []FFIViolation
	Signedness        []FFIViolation
	Deprecation       []FFIViolation
	Unverified        []string
	seen              map[string]bool
	deprecatedSeen    map[string]bool
}

type goValue struct {
	Type types.Type
	Kind CKind
	Size int
	Nil  bool
}

type callTemplate struct {
	ReceiverParam int
	Call          *ast.CallExpr
	Pkg           *packages.Package
	Body          *ast.BlockStmt
}

var goTargets = []struct {
	name  string
	sizes types.Sizes
}{
	{"arm64", types.SizesFor("gc", "arm64")},
	{"amd64", types.SizesFor("gc", "amd64")},
}

func CheckGoCalls(api *CAPI, ffi *FFICatalog, enumTypes map[string]string, dir string) (GoCallReport, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir: dir,
	}
	loaded, err := packages.Load(cfg, ".")
	if err != nil {
		return GoCallReport{}, err
	}
	if packages.PrintErrors(loaded) > 0 {
		return GoCallReport{}, fmt.Errorf("cannot type-check Go bindings")
	}
	if len(loaded) != 1 {
		return GoCallReport{}, fmt.Errorf("expected one Go package in %s, found %d", dir, len(loaded))
	}
	pkg := loaded[0]

	byVar := map[string][]FFIBinding{}
	for _, bindings := range ffi.Bindings {
		for _, binding := range bindings {
			if binding.GoVar != "" {
				byVar[binding.GoVar] = append(byVar[binding.GoVar], binding)
			}
		}
	}

	report := GoCallReport{seen: map[string]bool{}, deprecatedSeen: map[string]bool{}}
	for _, bindings := range ffi.Bindings {
		for _, binding := range bindings {
			if binding.GoVar == "" {
				report.Unverified = append(report.Unverified,
					fmt.Sprintf("%s (%s:%d): binding variable is unresolved", binding.CName, filepath.Base(binding.File), binding.Line))
			}
		}
	}

	helpers := collectCallHelpers(pkg)
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if receiver, ok := callReceiver(call); ok {
					for _, binding := range byVar[receiver] {
						checkGoCall(api, binding, pkg, call, fn.Body, fn, enumTypes, &report)
					}
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				for _, helper := range helpers[id.Name] {
					if helper.ReceiverParam >= len(call.Args) {
						continue
					}
					bindingVar, ok := call.Args[helper.ReceiverParam].(*ast.Ident)
					if !ok {
						continue
					}
					for _, binding := range byVar[bindingVar.Name] {
						checkGoCall(api, binding, helper.Pkg, helper.Call, helper.Body, fn, enumTypes, &report)
					}
				}
				return true
			})
		}
	}
	for _, bindings := range ffi.Bindings {
		for _, binding := range bindings {
			if binding.GoVar != "" && !report.seen[bindingKey(binding)] {
				report.Unverified = append(report.Unverified,
					fmt.Sprintf("%s (%s:%d): no Go call site found for %s", binding.CName, filepath.Base(binding.File), binding.Line, binding.GoVar))
			}
		}
	}

	sort.Strings(report.Unverified)
	report.seen = nil
	report.deprecatedSeen = nil
	return report, nil
}

func bindingKey(binding FFIBinding) string {
	return fmt.Sprintf("%s:%d:%s", binding.File, binding.Line, binding.CName)
}

func collectCallHelpers(pkg *packages.Package) map[string][]callTemplate {
	result := map[string][]callTemplate{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Type.Params == nil {
				continue
			}
			params := map[string]int{}
			index := 0
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					params[name.Name] = index
					index++
				}
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				receiver, ok := callReceiver(call)
				if !ok {
					return true
				}
				if parameter, ok := params[receiver]; ok {
					result[fn.Name.Name] = append(result[fn.Name.Name], callTemplate{parameter, call, pkg, fn.Body})
				}
				return true
			})
		}
	}
	return result
}

func callReceiver(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Call" {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

func checkGoCall(api *CAPI, binding FFIBinding, pkg *packages.Package, call *ast.CallExpr, body *ast.BlockStmt, wrapper *ast.FuncDecl, enumTypes map[string]string, report *GoCallReport) {
	report.seen[bindingKey(binding)] = true
	report.Calls++
	pos := pkg.Fset.Position(call.Pos())
	var problems, unverified []string
	if len(call.Args) == 0 {
		problems = append(problems, "Call has no return-buffer argument")
	} else {
		checkGoReturn(api, binding, pkg, call.Args[0], &problems, &unverified, report)
	}

	var args []ast.Expr
	if len(call.Args) > 0 {
		args = call.Args[1:]
	}
	if !binding.Variadic && len(args) != len(binding.Parameters) {
		problems = append(problems, fmt.Sprintf("Call passes %d arguments; FFI expects %d", len(args), len(binding.Parameters)))
	}
	n := min(len(args), len(binding.Parameters))
	for i := range n {
		value, note := goPointee(pkg, args[i], goTargets[0].sizes)
		if note != "" {
			unverified = append(unverified, fmt.Sprintf("arg%d %s", i, note))
			continue
		}
		compareGoValue(binding.Parameters[i], value, fmt.Sprintf("arg%d", i), &problems, &unverified)
		if fn, ok := api.Functions[binding.CName]; ok && i < len(fn.Parameters) {
			if detail := goValueSignedness(fn.Parameters[i].Type, value.Type, value.Kind, fmt.Sprintf("arg%d", i)); detail != "" {
				appendSignedness(report, binding, pos, []string{detail})
			}
			if value.Kind == CStructValue && binding.Parameters[i].Kind == CStructValue {
				compareGoLayouts(api, fn.Parameters[i].Type, binding.Parameters[i], value.Type, fmt.Sprintf("arg%d", i), &problems, &unverified)
				appendSignedness(report, binding, pos, goStructSignedness(api, fn.Parameters[i].Type, value.Type, fmt.Sprintf("arg%d", i)))
			}
			comparePointerTarget(api, fn.Parameters[i].Type, value.Type, fmt.Sprintf("arg%d", i), &problems, &unverified, &report.Signedness, binding, pos)
			compareEnumArgument(api, fn.Parameters[i].Type, value.Type, enumTypes, fmt.Sprintf("arg%d", i), &problems, report)
			if isCCharPointer(fn.Parameters[i].Type.Raw) {
				report.StringArgs++
				checked, clean, detail := checkCStringArgument(fn.Parameters[i].Type, args[i], body)
				switch {
				case !checked:
					report.UnverifiedStrings++
					unverified = append(unverified, fmt.Sprintf("arg%d C string buffer source is unresolved", i))
				case clean:
					report.CleanStrings++
				default:
					problems = append(problems, fmt.Sprintf("arg%d: %s", i, detail))
				}
			}
		}
	}
	checkDeprecatedWrapper(api, binding, wrapper, pkg, report)

	for _, detail := range problems {
		report.Violations = append(report.Violations, FFIViolation{
			Symbol: binding.CName, File: pos.Filename, Line: pos.Line, Detail: detail,
		})
	}
	for _, detail := range unverified {
		report.Unverified = append(report.Unverified,
			fmt.Sprintf("%s (%s:%d): %s", binding.CName, filepath.Base(pos.Filename), pos.Line, detail))
	}
	if len(problems) == 0 && len(unverified) == 0 {
		report.Clean++
	}
}

func compareEnumArgument(api *CAPI, cType CType, goType types.Type, enumTypes map[string]string, slot string, problems *[]string, report *GoCallReport) {
	want := cEnumName(api, cType.Raw)
	if want == "" || goType == nil {
		return
	}
	report.EnumArgs++
	named, ok := goType.(*types.Named)
	if !ok {
		*problems = append(*problems, fmt.Sprintf("%s: C takes enum %s but Go passes %s; a semantic enum type is required", slot, want, goType))
		return
	}
	got := enumTypes[named.Obj().Name()]
	if got == "" {
		*problems = append(*problems, fmt.Sprintf("%s: C takes enum %s but Go type %s does not mirror a C enum", slot, want, goType))
		return
	}
	if got != want {
		*problems = append(*problems, fmt.Sprintf("%s: C takes enum %s but Go %s mirrors enum %s", slot, want, goType, got))
		return
	}
	report.CleanEnumArgs++
}

func comparePointerTarget(api *CAPI, cType CType, goType types.Type, slot string, problems, unverified *[]string, signedness *[]FFIViolation, binding FFIBinding, pos token.Position) {
	if cType.Kind != CPointer || goType == nil {
		return
	}
	goPointer, ok := goType.Underlying().(*types.Pointer)
	if !ok {
		return
	}
	raw := normalizeType(cType.Raw)
	star := strings.LastIndex(raw, "*")
	if star < 0 {
		return
	}
	cTarget := classifyType(api, strings.TrimSpace(raw[:star]), nil)
	if cTarget.Kind == CVoid {
		return
	}
	if cTarget.Kind == CUnknown || cTarget.Size < 0 {
		*unverified = append(*unverified, fmt.Sprintf("%s C pointer target %q is unresolved", slot, cType.Raw))
		return
	}
	goKind, goSize := classifyGoType(goPointer.Elem(), goTargets[0].sizes)
	if goKind == CUnknown || goSize < 0 {
		*unverified = append(*unverified, fmt.Sprintf("%s Go pointer target %s is unresolved", slot, goPointer.Elem()))
		return
	}
	if cTarget.Size != goSize || !ffiKindsCompatible(cTarget.Kind, goKind) {
		*problems = append(*problems, fmt.Sprintf("%s pointer target: C %s is %s/%dB; Go points to %s/%dB",
			slot, cType.Raw, cTarget.Kind, cTarget.Size, goKind, goSize))
	}
	if signednessMismatch(cTarget.Kind, goKind, cTarget.Size) {
		*signedness = append(*signedness, FFIViolation{Symbol: binding.CName, File: pos.Filename, Line: pos.Line,
			Detail: fmt.Sprintf("%s pointer target: C %s is %s but Go %s is %s; ABI-compatible, different numeric meaning",
				slot, cType.Raw, cTarget.Kind, goPointer.Elem(), goKind)})
	}
}

func checkGoReturn(api *CAPI, binding FFIBinding, pkg *packages.Package, expr ast.Expr, problems, unverified *[]string, report *GoCallReport) {
	value, note := goPointee(pkg, expr, goTargets[0].sizes)
	switch binding.Return.Kind {
	case CVoid:
		if !value.Nil {
			*problems = append(*problems, "FFI return is void but Call passes a return buffer")
		}
	case CSigned, CUnsigned, CPointer:
		if value.Nil {
			return
		}
		if note != "" {
			*unverified = append(*unverified, "return buffer "+note)
			return
		}
		if fn, ok := api.Functions[binding.CName]; ok {
			if detail := goValueSignedness(fn.ReturnType, value.Type, value.Kind, "return buffer"); detail != "" {
				appendSignedness(report, binding, pkg.Fset.Position(expr.Pos()), []string{detail})
			}
		}
		if value.Size < 8 {
			*problems = append(*problems, fmt.Sprintf("return buffer %s is %dB; integer and pointer returns require an 8B ffi.Arg-sized buffer", value.Type, value.Size))
			return
		}
		if !ffiKindsCompatible(binding.Return.Kind, value.Kind) {
			*problems = append(*problems, fmt.Sprintf("return buffer kind is %s; FFI returns %s", value.Kind, binding.Return.Kind))
		}
	case CFloat, CDouble:
		if value.Nil {
			return
		}
		if note != "" {
			*unverified = append(*unverified, "return buffer "+note)
			return
		}
		compareGoValue(binding.Return, value, "return buffer", problems, unverified)
	case CStructValue:
		if value.Nil {
			return
		}
		if note != "" {
			*unverified = append(*unverified, "return buffer "+note)
			return
		}
		compareGoValue(binding.Return, value, "return buffer", problems, unverified)
		if fn, ok := api.Functions[binding.CName]; ok {
			compareGoLayouts(api, fn.ReturnType, binding.Return, value.Type, "return buffer", problems, unverified)
			appendSignedness(report, binding, pkg.Fset.Position(expr.Pos()), goStructSignedness(api, fn.ReturnType, value.Type, "return buffer"))
		}
	}
}

func appendSignedness(report *GoCallReport, binding FFIBinding, pos token.Position, details []string) {
	for _, detail := range details {
		report.Signedness = append(report.Signedness, FFIViolation{Symbol: binding.CName, File: pos.Filename, Line: pos.Line, Detail: detail})
	}
}

func checkDeprecatedWrapper(api *CAPI, binding FFIBinding, wrapper *ast.FuncDecl, pkg *packages.Package, report *GoCallReport) {
	fn, ok := api.Functions[binding.CName]
	if !ok || !fn.Deprecated || wrapper == nil || !wrapper.Name.IsExported() {
		return
	}
	key := binding.CName + ":" + wrapper.Name.Name
	if report.deprecatedSeen[key] {
		return
	}
	report.deprecatedSeen[key] = true
	report.Deprecated++
	if hasDeprecatedDocumentation(wrapper.Doc) {
		report.CleanDeprecated++
		return
	}
	pos := pkg.Fset.Position(wrapper.Pos())
	report.Deprecation = append(report.Deprecation, FFIViolation{
		Symbol: binding.CName, File: pos.Filename, Line: pos.Line,
		Detail: fmt.Sprintf("exported wrapper %s binds deprecated C API %s but has no Deprecated: documentation", wrapper.Name.Name, binding.CName),
	})
}

func compareGoValue(ffi FFIType, value goValue, slot string, problems, unverified *[]string) {
	if ffi.Size < 0 || ffi.Kind == CUnknown {
		*unverified = append(*unverified, slot+" FFI type is unresolved")
		return
	}
	if value.Type == nil || value.Size < 0 || value.Kind == CUnknown {
		*unverified = append(*unverified, slot+" Go type is unresolved")
		return
	}
	wrongSize := value.Size != ffi.Size
	if ffi.Kind == CStructValue && value.Kind == CStructValue {
		wrongSize = value.Size < ffi.Size
	}
	if wrongSize {
		*problems = append(*problems, fmt.Sprintf("%s: FFI %s is %dB; Go %s is %dB", slot, ffi.Name, ffi.Size, value.Type, value.Size))
		return
	}
	if !ffiKindsCompatible(ffi.Kind, value.Kind) {
		*problems = append(*problems, fmt.Sprintf("%s: FFI kind is %s; Go %s is %s", slot, ffi.Kind, value.Type, value.Kind))
	}
}

func goPointee(pkg *packages.Package, expr ast.Expr, sizes types.Sizes) (goValue, string) {
	expr = unwrapGoPointer(expr)
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return goValue{Nil: true}, ""
	}
	typ := pkg.TypesInfo.TypeOf(expr)
	if typ == nil {
		return goValue{}, "type is unresolved"
	}
	pointer, ok := typ.Underlying().(*types.Pointer)
	if !ok {
		return goValue{}, "is not a pointer"
	}
	typ = pointer.Elem()
	kind, size := classifyGoType(typ, sizes)
	return goValue{Type: typ, Kind: kind, Size: size}, ""
}

func unwrapGoPointer(expr ast.Expr) ast.Expr {
	for {
		switch x := expr.(type) {
		case *ast.ParenExpr:
			expr = x.X
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Pointer" || len(x.Args) != 1 {
				return expr
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "unsafe" {
				return expr
			}
			expr = x.Args[0]
		default:
			return expr
		}
	}
}

func classifyGoType(typ types.Type, sizes types.Sizes) (CKind, int) {
	if typ == nil || sizes == nil {
		return CUnknown, -1
	}
	size := int(sizes.Sizeof(typ))
	switch underlying := typ.Underlying().(type) {
	case *types.Basic:
		switch {
		case underlying.Kind() == types.UnsafePointer || underlying.Kind() == types.Uintptr:
			return CPointer, size
		case underlying.Info()&types.IsUnsigned != 0 || underlying.Kind() == types.Bool:
			return CUnsigned, size
		case underlying.Info()&types.IsInteger != 0:
			return CSigned, size
		case underlying.Kind() == types.Float32:
			return CFloat, size
		case underlying.Kind() == types.Float64:
			return CDouble, size
		}
	case *types.Pointer, *types.Signature, *types.Map, *types.Chan:
		return CPointer, size
	case *types.Struct:
		return CStructValue, size
	}
	return CUnknown, size
}

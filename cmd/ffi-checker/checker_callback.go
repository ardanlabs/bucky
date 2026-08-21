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

type CallbackReport struct {
	Callbacks     int
	Clean         int
	PointerFields int
	TracedFields  int
	Violations    []FFIViolation
	Unverified    []string
}

type goCallback struct {
	name     string
	typedef  string
	sig      *types.Signature
	position token.Position
}

type callbackStore struct {
	structType types.Type
	field      string
	source     string
	position   token.Position
}

func CheckCallbacks(api *CAPI, dir string) (CallbackReport, error) {
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir: dir,
	}, ".")
	if err != nil {
		return CallbackReport{}, err
	}
	if packages.PrintErrors(loaded) > 0 || len(loaded) != 1 {
		return CallbackReport{}, fmt.Errorf("cannot type-check Go callbacks in %s", dir)
	}
	pkg := loaded[0]

	callbacks, stores := collectGoCallbacks(pkg, api)
	report := CallbackReport{}
	byName := make(map[string]goCallback, len(callbacks))
	for _, callback := range callbacks {
		report.Callbacks++
		byName[callback.name] = callback
		if callback.typedef == "" {
			report.Unverified = append(report.Unverified,
				fmt.Sprintf("%s (%s:%d): callback typedef is unresolved", callback.name, filepath.Base(callback.position.Filename), callback.position.Line))
			continue
		}
		c := api.Callbacks[callback.typedef]
		problems := compareCallbackSignature(c, callback.sig)
		if len(problems) == 0 {
			report.Clean++
		}
		for _, detail := range problems {
			report.Violations = append(report.Violations, FFIViolation{
				Symbol: callback.typedef, File: callback.position.Filename, Line: callback.position.Line, Detail: detail,
			})
		}
	}

	checkCallbackPointerFields(api, pkg, byName, stores, &report)
	sort.Strings(report.Unverified)
	sort.Slice(report.Violations, func(i, j int) bool {
		if report.Violations[i].File != report.Violations[j].File {
			return report.Violations[i].File < report.Violations[j].File
		}
		return report.Violations[i].Line < report.Violations[j].Line
	})
	return report, nil
}

func collectGoCallbacks(pkg *packages.Package, api *CAPI) ([]goCallback, []callbackStore) {
	var callbacks []goCallback
	var stores []callbackStore
	for _, file := range pkg.Syntax {
		var currentFunction string
		ast.Inspect(file, func(node ast.Node) bool {
			switch x := node.(type) {
			case *ast.FuncDecl:
				currentFunction = x.Name.Name
			case *ast.CallExpr:
				if !isPackageCall(x, "purego", "NewCallback") || len(x.Args) != 1 {
					return true
				}
				sig, _ := pkg.TypesInfo.TypeOf(x.Args[0]).(*types.Signature)
				callbacks = append(callbacks, goCallback{
					name: currentFunction, typedef: linkCallbackName(currentFunction, api), sig: sig,
					position: pkg.Fset.Position(x.Pos()),
				})
			case *ast.AssignStmt:
				if len(x.Lhs) != len(x.Rhs) {
					return true
				}
				for i, lhs := range x.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					typ := pkg.TypesInfo.TypeOf(sel.X)
					if pointer, ok := underlyingPointer(typ); ok {
						typ = pointer.Elem()
					}
					if typ == nil {
						continue
					}
					if _, ok := typ.Underlying().(*types.Struct); !ok {
						continue
					}
					if source := callbackSource(x.Rhs[i]); source != "" {
						stores = append(stores, callbackStore{typ, sel.Sel.Name, source, pkg.Fset.Position(x.Pos())})
					}
				}
			}
			return true
		})
	}
	return callbacks, stores
}

func isPackageCall(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

func underlyingPointer(typ types.Type) (*types.Pointer, bool) {
	if typ == nil {
		return nil, false
	}
	pointer, ok := typ.Underlying().(*types.Pointer)
	return pointer, ok
}

func callbackSource(expr ast.Expr) string {
	for {
		switch x := expr.(type) {
		case *ast.ParenExpr:
			expr = x.X
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok {
				if len(x.Args) == 1 && (id.Name == "uintptr" || id.Name == "unsafe.Pointer") {
					expr = x.Args[0]
					continue
				}
				return id.Name
			}
			return ""
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

func linkCallbackName(goName string, api *CAPI) string {
	// LogSilent is Bucky's public name for the ggml callback accepted by
	// whisper_log_set; unlike the constructors below, its name cannot be
	// matched mechanically to the C typedef.
	if goName == "LogSilent" {
		if _, ok := api.Callbacks["ggml_log_callback"]; ok {
			return "ggml_log_callback"
		}
	}
	want := normalizedCallbackName(goName)
	match := ""
	for name := range api.Callbacks {
		if normalizedCallbackName(name) != want {
			continue
		}
		if match != "" {
			return ""
		}
		match = name
	}
	return match
}

func normalizedCallbackName(name string) string {
	name = normalizeMemberName(name)
	name = strings.TrimPrefix(name, "whisper")
	name = strings.TrimPrefix(name, "ggml")
	return strings.TrimSuffix(name, "callback")
}

func compareCallbackSignature(c CCallback, sig *types.Signature) []string {
	if sig == nil {
		return []string{"Go callback signature is unresolved"}
	}
	var problems []string
	if sig.Params().Len() != len(c.Parameters) {
		problems = append(problems, fmt.Sprintf("Go callback takes %d parameters; C passes %d", sig.Params().Len(), len(c.Parameters)))
	}
	for i := range min(sig.Params().Len(), len(c.Parameters)) {
		goKind, goSize := classifyGoType(sig.Params().At(i).Type(), goTargets[0].sizes)
		cType := c.Parameters[i].Type
		if goSize != cType.Size || !ffiKindsCompatible(goKind, cType.Kind) {
			problems = append(problems, fmt.Sprintf("arg%d: C %s is %s/%dB; Go %s is %s/%dB",
				i, cType.Raw, cType.Kind, cType.Size, sig.Params().At(i).Type(), goKind, goSize))
		}
	}
	if c.ReturnType.Kind != CVoid {
		if sig.Results().Len() != 1 {
			problems = append(problems, fmt.Sprintf("Go callback returns %d values; C returns one", sig.Results().Len()))
		} else {
			goKind, goSize := classifyGoType(sig.Results().At(0).Type(), goTargets[0].sizes)
			if goSize < c.ReturnType.Size || !ffiKindsCompatible(goKind, c.ReturnType.Kind) {
				problems = append(problems, fmt.Sprintf("return: C %s is %s/%dB; Go %s is %s/%dB",
					c.ReturnType.Raw, c.ReturnType.Kind, c.ReturnType.Size, sig.Results().At(0).Type(), goKind, goSize))
			}
		}
	}
	return problems
}

func checkCallbackPointerFields(api *CAPI, pkg *packages.Package, callbacks map[string]goCallback, stores []callbackStore, report *CallbackReport) {
	goStructs := packageStructTypes(pkg)
	for _, cStruct := range api.Structs {
		if cStruct.File != "whisper.h" || cStruct.LayoutError != "" {
			continue
		}
		goType := matchingGoStruct(cStruct.Name, goStructs)
		if goType == nil {
			continue
		}
		cLeaves, cOK := flattenCStruct(api, cStruct, 0, 0)
		goLeaves, goOK := flattenGoType(goType, goTargets[0].sizes, 0, 0)
		if !cOK || !goOK {
			continue
		}
		for i, cLeaf := range cLeaves {
			typedef := callbackTypedef(api, cLeaf.Type.Raw)
			if typedef == "" || i >= len(goLeaves) {
				continue
			}
			report.PointerFields++
			goLeaf := goLeaves[i]
			if _, isFunc := goLeaf.typ.Underlying().(*types.Signature); isFunc {
				report.Violations = append(report.Violations, FFIViolation{
					Symbol: cStruct.Name + "." + cLeaf.Path, File: cStruct.File, Line: cStruct.Line,
					Detail: fmt.Sprintf("Go field %s is a func value, not a C code pointer", goLeaf.path),
				})
			}
			traced := false
			for _, store := range stores {
				if !types.Identical(store.structType, goType) || store.field != goLeaf.path {
					continue
				}
				callback, ok := callbacks[store.source]
				if !ok || callback.typedef == "" {
					continue
				}
				traced = true
				report.TracedFields++
				if callback.typedef != typedef {
					report.Violations = append(report.Violations, FFIViolation{
						Symbol: cStruct.Name + "." + cLeaf.Path, File: store.position.Filename, Line: store.position.Line,
						Detail: fmt.Sprintf("stores %s implementing %s; C calls this field as %s", store.source, callback.typedef, typedef),
					})
				}
			}
			if !traced {
				report.Unverified = append(report.Unverified,
					fmt.Sprintf("%s.%s: Go field %s has no traced callback assignment", cStruct.Name, cLeaf.Path, goLeaf.path))
			}
		}
	}
}

func callbackTypedef(api *CAPI, raw string) string {
	name := strings.TrimSpace(normalizeType(raw))
	if _, ok := api.Callbacks[name]; ok {
		return name
	}
	return ""
}

func packageStructTypes(pkg *packages.Package) map[string]types.Type {
	result := map[string]types.Type{}
	for _, name := range pkg.Types.Scope().Names() {
		obj, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		if _, ok := obj.Type().Underlying().(*types.Struct); ok {
			result[name] = obj.Type()
		}
	}
	return result
}

func matchingGoStruct(cName string, goStructs map[string]types.Type) types.Type {
	want := strings.TrimPrefix(normalizeMemberName(cName), "whisper")
	var match types.Type
	for name, typ := range goStructs {
		got := strings.TrimPrefix(normalizeMemberName(name), "whisper")
		if got != want {
			continue
		}
		if match != nil {
			return nil
		}
		match = typ
	}
	return match
}

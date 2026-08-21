package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
)

type EnumCoverage struct {
	Name     string
	Members  int
	Mirrored int
	Missing  []CEnumMember
}

type EnumReport struct {
	Constants  int
	Clean      int
	Enums      int
	Complete   int
	Violations []FFIViolation
	Unverified []string
	Partial    []EnumCoverage
	GoTypes    map[string]string
}

type goConstant struct {
	Name  string
	Type  string
	Value int64
	File  string
	Line  int
}

func CheckGoEnums(api *CAPI, dir string) (EnumReport, error) {
	pkg, err := loadEnumPackage(dir)
	if err != nil {
		return EnumReport{}, err
	}

	report := EnumReport{GoTypes: map[string]string{}}
	conflictedTypes := map[string]bool{}
	byKey := map[string][]CConstant{}
	for _, c := range api.Constants {
		if c.File != "whisper.h" {
			continue
		}
		key := constantKey(c.Name)
		byKey[key] = append(byKey[key], c)
	}

	mirrored := map[string]bool{}
	for _, g := range collectGoConstants(pkg) {
		matches := byKey[constantKey(g.Name)]
		if len(matches) == 0 {
			continue
		}
		if len(matches) > 1 {
			names := make([]string, len(matches))
			for i, c := range matches {
				names[i] = c.Name
			}
			sort.Strings(names)
			report.Unverified = append(report.Unverified,
				fmt.Sprintf("%s (%s:%d): name matches multiple C constants: %s", g.Name, filepath.Base(g.File), g.Line, strings.Join(names, ", ")))
			continue
		}

		c := matches[0]
		mirrored[c.Name] = true
		if g.Type != "" && c.Enum != "" && !conflictedTypes[g.Type] {
			if old := report.GoTypes[g.Type]; old == "" || old == c.Enum {
				report.GoTypes[g.Type] = c.Enum
			} else {
				report.Unverified = append(report.Unverified,
					fmt.Sprintf("Go type %s maps to both enum %s and enum %s", g.Type, old, c.Enum))
				delete(report.GoTypes, g.Type)
				conflictedTypes[g.Type] = true
			}
		}
		if !c.Resolved {
			report.Unverified = append(report.Unverified,
				fmt.Sprintf("%s (%s:%d): C constant %s is unresolved: %s", g.Name, filepath.Base(g.File), g.Line, c.Name, c.Reason))
			continue
		}

		report.Constants++
		if g.Value == c.Value {
			report.Clean++
			continue
		}
		report.Violations = append(report.Violations, FFIViolation{
			Symbol: c.Name,
			File:   g.File,
			Line:   g.Line,
			Detail: fmt.Sprintf("Go %s = %d; C %s = %d (%s:%d)", g.Name, g.Value, c.Name, c.Value, c.File, c.Line),
		})
	}

	enumNames := make([]string, 0, len(api.Enums))
	for name, enum := range api.Enums {
		if enum.File == "whisper.h" {
			enumNames = append(enumNames, name)
		}
	}
	sort.Strings(enumNames)
	for _, name := range enumNames {
		enum := api.Enums[name]
		coverage := EnumCoverage{Name: name, Members: len(enum.Members)}
		for _, member := range enum.Members {
			if mirrored[member.Name] {
				coverage.Mirrored++
			} else {
				coverage.Missing = append(coverage.Missing, member)
			}
		}
		if coverage.Mirrored > 0 {
			report.Enums++
			if len(coverage.Missing) == 0 {
				report.Complete++
			} else {
				report.Partial = append(report.Partial, coverage)
			}
		}
	}

	sort.Strings(report.Unverified)
	return report, nil
}

func loadEnumPackage(dir string) (*packages.Package, error) {
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir: dir,
	}, ".")
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(loaded) > 0 {
		return nil, fmt.Errorf("cannot type-check Go constants")
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("expected one Go package in %s, found %d", dir, len(loaded))
	}
	return loaded[0], nil
}

func collectGoConstants(pkg *packages.Package) []goConstant {
	var result []goConstant
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, specification := range group.Specs {
				values := specification.(*ast.ValueSpec)
				for _, name := range values.Names {
					object, ok := pkg.TypesInfo.Defs[name].(*types.Const)
					if !ok || name.Name == "_" {
						continue
					}
					value, exact := constant.Int64Val(constant.ToInt(object.Val()))
					if !exact {
						continue
					}
					position := pkg.Fset.Position(name.Pos())
					item := goConstant{Name: name.Name, Value: value, File: position.Filename, Line: position.Line}
					if named, ok := object.Type().(*types.Named); ok {
						item.Type = named.Obj().Name()
					}
					result = append(result, item)
				}
			}
		}
	}
	return result
}

func constantKey(name string) string {
	var normalized strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.TrimPrefix(normalized.String(), "whisper")
}

func cEnumName(api *CAPI, raw string) string {
	typeName := normalizeType(raw)
	for range 12 {
		if name, ok := strings.CutPrefix(typeName, "enum "); ok {
			return strings.TrimSpace(name)
		}
		typedef, ok := api.Typedefs[typeName]
		if !ok {
			return ""
		}
		typeName = normalizeType(typedef.Type.Raw)
	}
	return ""
}

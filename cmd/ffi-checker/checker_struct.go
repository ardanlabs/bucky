package main

import (
	"fmt"
	"go/types"
	"strings"
	"unicode"
)

type ffiLeaf struct {
	Offset int
	Size   int
	Kind   CKind
	Path   string
	Type   CType
}

type goLeaf struct {
	offset int
	size   int
	kind   CKind
	path   string
	typ    types.Type
}

func compareGoLayouts(api *CAPI, cType CType, ffi FFIType, typ types.Type, slot string, problems, unverified *[]string) {
	ffiLayout, ok := flattenFFIType(ffi, 0, 0)
	if !ok {
		*unverified = append(*unverified, slot+" FFI struct layout is unresolved")
		return
	}
	var cLayout []ffiLeaf
	if cType.Kind == CStructValue {
		cStruct := api.Structs[structTypeName(api, cType.Raw)]
		cLayout, ok = flattenCStruct(api, cStruct, 0, 0)
		if !ok {
			*unverified = append(*unverified, slot+" C struct member names are unresolved")
		}
	}
	for _, target := range goTargets {
		goLayout, ok := flattenGoType(typ, target.sizes, 0, 0)
		if !ok {
			*unverified = append(*unverified, fmt.Sprintf("%s Go struct layout is unresolved on %s", slot, target.name))
			continue
		}
		if detail := compareGoLeaves(ffiLayout, goLayout, ffi.Size); detail != "" {
			*problems = append(*problems, fmt.Sprintf("%s %s layout: %s", slot, target.name, detail))
		}
		if target.name == "arm64" && len(cLayout) > 0 {
			transposed, unmatched := compareMemberNames(cLayout, goLayout)
			for _, detail := range transposed {
				*problems = append(*problems, slot+": "+detail)
			}
			for _, detail := range unmatched {
				*unverified = append(*unverified, slot+": "+detail)
			}
		}
	}
}

func flattenCStruct(api *CAPI, st CStruct, base, depth int) ([]ffiLeaf, bool) {
	return flattenCStructPath(api, st, base, depth, "")
}

func flattenCStructPath(api *CAPI, st CStruct, base, depth int, prefix string) ([]ffiLeaf, bool) {
	if st.Name == "" || st.LayoutError != "" || depth > 8 {
		return nil, false
	}
	var leaves []ffiLeaf
	for _, field := range st.Fields {
		if field.Count <= 0 || field.Type.Size <= 0 {
			return nil, false
		}
		for i := range field.Count {
			offset := base + field.Offset + i*field.Type.Size
			path := joinMemberPath(prefix, field.Name)
			if field.Count > 1 {
				path += fmt.Sprintf("[%d]", i)
			}
			if field.Type.Kind == CStructValue {
				nested := api.Structs[structTypeName(api, field.Type.Raw)]
				more, ok := flattenCStructPath(api, nested, offset, depth+1, path)
				if !ok {
					return nil, false
				}
				leaves = append(leaves, more...)
			} else {
				leaves = append(leaves, ffiLeaf{Offset: offset, Size: field.Type.Size, Kind: field.Type.Kind, Path: path, Type: field.Type})
			}
		}
	}
	return leaves, true
}

func flattenFFIType(typ FFIType, base, depth int) ([]ffiLeaf, bool) {
	if typ.Kind != CStructValue || len(typ.Fields) != len(typ.Offsets) || depth > 8 {
		return nil, false
	}
	var leaves []ffiLeaf
	for i, field := range typ.Fields {
		offset := base + typ.Offsets[i]
		if field.Kind == CStructValue {
			more, ok := flattenFFIType(field, offset, depth+1)
			if !ok {
				return nil, false
			}
			leaves = append(leaves, more...)
		} else if field.Kind == CUnknown || field.Size <= 0 {
			return nil, false
		} else {
			leaves = append(leaves, ffiLeaf{Offset: offset, Size: field.Size, Kind: field.Kind})
		}
	}
	return leaves, true
}

func flattenGoType(typ types.Type, sizes types.Sizes, base, depth int) ([]goLeaf, bool) {
	return flattenGoTypePath(typ, sizes, base, depth, "")
}

func flattenGoTypePath(typ types.Type, sizes types.Sizes, base, depth int, prefix string) ([]goLeaf, bool) {
	if typ == nil || depth > 8 {
		return nil, false
	}
	switch underlying := typ.Underlying().(type) {
	case *types.Struct:
		fields := make([]*types.Var, underlying.NumFields())
		for i := range fields {
			fields[i] = underlying.Field(i)
		}
		offsets := sizes.Offsetsof(fields)
		var leaves []goLeaf
		for i, field := range fields {
			if field.Name() == "_" || strings.HasPrefix(field.Name(), "_pad") {
				continue
			}
			more, ok := flattenGoTypePath(field.Type(), sizes, base+int(offsets[i]), depth+1, joinMemberPath(prefix, field.Name()))
			if !ok {
				return nil, false
			}
			leaves = append(leaves, more...)
		}
		return leaves, true
	case *types.Array:
		var leaves []goLeaf
		stride := int(sizes.Sizeof(underlying.Elem()))
		for i := range int(underlying.Len()) {
			more, ok := flattenGoTypePath(underlying.Elem(), sizes, base+i*stride, depth+1, fmt.Sprintf("%s[%d]", prefix, i))
			if !ok {
				return nil, false
			}
			leaves = append(leaves, more...)
		}
		return leaves, true
	default:
		kind, size := classifyGoType(typ, sizes)
		if size <= 0 {
			return nil, false
		}
		return []goLeaf{{offset: base, size: size, kind: kind, path: prefix, typ: typ}}, true
	}
}

func compareLeaves(ffi, c []ffiLeaf) string {
	if len(ffi) != len(c) {
		return fmt.Sprintf("FFI has %d members; C has %d", len(ffi), len(c))
	}
	for i := range ffi {
		if ffi[i].Offset != c[i].Offset || ffi[i].Size != c[i].Size || !ffiKindsCompatible(ffi[i].Kind, c[i].Kind) {
			return fmt.Sprintf("member %d: FFI +%d %s/%dB; C +%d %s/%dB", i, ffi[i].Offset, ffi[i].Kind, ffi[i].Size, c[i].Offset, c[i].Kind, c[i].Size)
		}
	}
	return ""
}

func compareGoLeaves(ffi []ffiLeaf, goSide []goLeaf, ffiSize int) string {
	if len(goSide) < len(ffi) {
		return fmt.Sprintf("Go has %d members; FFI has %d", len(goSide), len(ffi))
	}
	for i := range ffi {
		if ffi[i].Offset != goSide[i].offset || ffi[i].Size != goSide[i].size || !ffiKindsCompatible(ffi[i].Kind, goSide[i].kind) {
			return fmt.Sprintf("member %d: Go +%d %s/%dB; FFI +%d %s/%dB",
				i, goSide[i].offset, goSide[i].kind, goSide[i].size, ffi[i].Offset, ffi[i].Kind, ffi[i].Size)
		}
	}
	for _, leaf := range goSide[len(ffi):] {
		if leaf.offset < ffiSize {
			return fmt.Sprintf("Go member at +%d falls inside the %dB FFI struct", leaf.offset, ffiSize)
		}
	}
	return ""
}

func compareMemberNames(c []ffiLeaf, goSide []goLeaf) (problems, unverified []string) {
	positions := make(map[string]int, len(c))
	for i, leaf := range c {
		key := normalizeMemberName(leaf.Path)
		if _, exists := positions[key]; exists {
			positions[key] = -1
		} else {
			positions[key] = i
		}
	}
	for i, leaf := range goSide {
		if i >= len(c) {
			break
		}
		j, exists := positions[normalizeMemberName(leaf.path)]
		switch {
		case !exists:
			unverified = append(unverified, fmt.Sprintf("Go member %s has no name match in C", leaf.path))
		case j < 0:
			unverified = append(unverified, fmt.Sprintf("Go member %s has an ambiguous name match in C", leaf.path))
		case j != i:
			problems = append(problems, fmt.Sprintf("Go member %s is C member %d (%s), but occupies member %d (%s): fields are transposed",
				leaf.path, j, c[j].Path, i, c[i].Path))
		}
	}
	return problems, unverified
}

func normalizeMemberName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	// WhisperFullParams flattens C's vad_params member into Vad-prefixed Go
	// fields. Keep this one reviewed spelling difference explicit rather than
	// using fuzzy matching that could hide a real transposition.
	return strings.ReplaceAll(b.String(), "vadparams", "vad")
}

func joinMemberPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

package main

import (
	"fmt"
	"go/types"
	"strings"
)

func signednessMismatch(left, right CKind, size int) bool {
	if size < 2 {
		return false
	}
	return left == CSigned && right == CUnsigned || left == CUnsigned && right == CSigned
}

func goValueSignedness(c CType, typ types.Type, goKind CKind, slot string) string {
	if typ == nil || strings.HasSuffix(typ.String(), "ffi.Arg") || !signednessMismatch(c.Kind, goKind, c.Size) {
		return ""
	}
	return fmt.Sprintf("%s: C %s is %s but Go %s is %s; ABI-compatible, different numeric meaning",
		slot, c.Raw, c.Kind, typ, goKind)
}

func ffiSignedness(api *CAPI, ffi FFIType, c CType, slot string) []string {
	if c.Kind == CStructValue && ffi.Kind == CStructValue {
		cLeaves, cOK := flattenCStruct(api, api.Structs[structTypeName(api, c.Raw)], 0, 0)
		ffiLeaves, ffiOK := flattenFFIType(ffi, 0, 0)
		if !cOK || !ffiOK {
			return nil
		}
		var findings []string
		for i := range min(len(cLeaves), len(ffiLeaves)) {
			if signednessMismatch(cLeaves[i].Kind, ffiLeaves[i].Kind, cLeaves[i].Size) {
				findings = append(findings, fmt.Sprintf("%s member %s: C is %s but FFI is %s; ABI-compatible, different numeric meaning",
					slot, cLeaves[i].Path, cLeaves[i].Kind, ffiLeaves[i].Kind))
			}
		}
		return findings
	}
	if signednessMismatch(c.Kind, ffi.Kind, c.Size) {
		return []string{fmt.Sprintf("%s: C %s is %s but FFI %s is %s; ABI-compatible, different numeric meaning",
			slot, c.Raw, c.Kind, ffi.Name, ffi.Kind)}
	}
	return nil
}

func goStructSignedness(api *CAPI, c CType, typ types.Type, slot string) []string {
	if c.Kind != CStructValue || typ == nil {
		return nil
	}
	cLeaves, cOK := flattenCStruct(api, api.Structs[structTypeName(api, c.Raw)], 0, 0)
	goLeaves, goOK := flattenGoType(typ, goTargets[0].sizes, 0, 0)
	if !cOK || !goOK {
		return nil
	}
	var findings []string
	for i := range min(len(cLeaves), len(goLeaves)) {
		if signednessMismatch(cLeaves[i].Kind, goLeaves[i].kind, cLeaves[i].Size) {
			findings = append(findings, fmt.Sprintf("%s member %s: C is %s but Go %s is %s; ABI-compatible, different numeric meaning",
				slot, cLeaves[i].Path, cLeaves[i].Kind, goLeaves[i].path, goLeaves[i].kind))
		}
	}
	return findings
}

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixtureHeader = `
#define WHISPER_API
#define WHISPER_SAMPLE_RATE 16000
#define WHISPER_SHIFTED (1 << 4)
#define WHISPER_ALIAS WHISPER_SAMPLE_RATE
#define WHISPER_TEXT "not an integer"
#define WHISPER_FN(x) (x)

typedef int32_t whisper_token;
typedef struct whisper_pair {
    int left, right;
    const float * values;
    uint32_t dimensions[2];
} whisper_pair;

enum whisper_mode {
    WHISPER_MODE_NONE = -1,
    WHISPER_MODE_FAST,
    WHISPER_MODE_SHIFTED = WHISPER_SHIFTED,
};

typedef bool (*whisper_progress_callback)(
    int progress,
    void * user_data);

WHISPER_API int whisper_n_len(struct whisper_context * ctx);
WHISPER_API const char * whisper_name(
    whisper_token token,
    const struct whisper_pair * pair);

WHISPER_DEPRECATED(
    WHISPER_API void whisper_old(void),
    "use whisper_new");
`

func fixtureAPI(t *testing.T) *CAPI {
	t.Helper()
	api, err := ParseCAPI([]Header{{Name: "whisper.h", Source: fixtureHeader, APIMacros: []string{"WHISPER_API"}}})
	if err != nil {
		t.Fatalf("ParseCAPI: %v", err)
	}
	return api
}

func TestCatalogsFunctions(t *testing.T) {
	api := fixtureAPI(t)
	if api.ExportedDeclarations != 3 || len(api.Issues) != 0 {
		t.Fatalf("declaration accounting = %d declarations, %#v", api.ExportedDeclarations, api.Issues)
	}

	fn, ok := api.Functions["whisper_n_len"]
	if !ok {
		t.Fatal("whisper_n_len not cataloged")
	}
	if fn.ReturnType.Raw != "int" || len(fn.Parameters) != 1 {
		t.Fatalf("whisper_n_len = %#v", fn)
	}
	if got := fn.Parameters[0].Type.Raw; got != "struct whisper_context *" {
		t.Fatalf("parameter type = %q", got)
	}
	if fn.ReturnType.Kind != CSigned || fn.ReturnType.Size != 4 || fn.Parameters[0].Type.Kind != CPointer {
		t.Fatalf("resolved function types = %#v", fn)
	}

	name := api.Functions["whisper_name"]
	if name.ReturnType.Raw != "const char *" || len(name.Parameters) != 2 {
		t.Fatalf("whisper_name = %#v", name)
	}
	if old := api.Functions["whisper_old"]; !old.Deprecated {
		t.Fatal("whisper_old was not marked deprecated")
	}
}

func TestCatalogsStructsEnumsConstantsAndCallbacks(t *testing.T) {
	api := fixtureAPI(t)

	st, ok := api.Structs["whisper_pair"]
	if !ok || len(st.Fields) != 4 {
		t.Fatalf("whisper_pair = %#v", st)
	}
	if st.Fields[0].Name != "left" || st.Fields[1].Name != "right" || st.Fields[3].ArrayCount != "2" {
		t.Fatalf("whisper_pair fields = %#v", st.Fields)
	}
	if st.Size != 24 || st.Align != 8 {
		t.Fatalf("whisper_pair layout = size %d align %d: %#v", st.Size, st.Align, st.Fields)
	}
	wantOffsets := []int{0, 4, 8, 16}
	for i, want := range wantOffsets {
		if st.Fields[i].Offset != want {
			t.Errorf("field %s offset = %d, want %d", st.Fields[i].Name, st.Fields[i].Offset, want)
		}
	}
	if st.Fields[3].Count != 2 {
		t.Fatalf("dimensions count = %d", st.Fields[3].Count)
	}

	en, ok := api.Enums["whisper_mode"]
	if !ok || len(en.Members) != 3 {
		t.Fatalf("whisper_mode = %#v", en)
	}
	want := map[string]int64{
		"WHISPER_MODE_NONE": -1, "WHISPER_MODE_FAST": 0, "WHISPER_MODE_SHIFTED": 16,
		"WHISPER_SAMPLE_RATE": 16000, "WHISPER_ALIAS": 16000,
	}
	for name, value := range want {
		got, ok := api.Constants[name]
		if !ok || !got.Resolved || got.Value != value {
			t.Errorf("constant %s = %#v, want %d", name, got, value)
		}
	}
	if c := api.Constants["WHISPER_TEXT"]; c.Resolved {
		t.Fatalf("string macro resolved as an integer: %#v", c)
	}
	if _, ok := api.Constants["WHISPER_FN"]; ok {
		t.Fatal("function-like macro was cataloged as a constant")
	}

	cb, ok := api.Callbacks["whisper_progress_callback"]
	if !ok || cb.ReturnType.Raw != "bool" || len(cb.Parameters) != 2 {
		t.Fatalf("callback = %#v", cb)
	}
	if td := api.Typedefs["whisper_token"]; td.Type.Raw != "int32_t" {
		t.Fatalf("whisper_token typedef = %#v", td)
	} else if td.Type.Kind != CSigned || td.Type.Size != 4 {
		t.Fatalf("whisper_token resolved type = %#v", td.Type)
	}
}

func TestCatalogsAnonymousAndNestedAggregates(t *testing.T) {
	const header = `
typedef uint16_t word;
typedef struct { word value; } inner;
typedef struct outer { uint8_t tag; inner nested; uint32_t values[2]; } outer;
typedef union { int integer; float decimal; } number;
typedef struct flags { unsigned int active : 1; } flags;
`
	api, err := ParseCAPI([]Header{{Name: "types.h", Source: header}})
	if err != nil {
		t.Fatal(err)
	}
	if inner := api.Structs["inner"]; inner.Size != 2 || inner.Align != 2 {
		t.Fatalf("inner = %#v", inner)
	}
	if outer := api.Structs["outer"]; outer.Size != 12 || outer.Align != 4 || outer.Fields[1].Offset != 2 || outer.Fields[2].Offset != 4 {
		t.Fatalf("outer = %#v", outer)
	}
	if union := api.Structs["number"]; !union.Union || union.LayoutError == "" || union.Size != -1 {
		t.Fatalf("number union = %#v", union)
	}
	if bits := api.Structs["flags"]; bits.LayoutError != "contains a bitfield" || bits.Size != -1 {
		t.Fatalf("flags = %#v", bits)
	}
}

func TestReportsUnparseableDeclarations(t *testing.T) {
	const header = `
WHISPER_API int;
typedef void (*broken_callback);
`
	api, err := ParseCAPI([]Header{{Name: "bad.h", Source: header, APIMacros: []string{"WHISPER_API"}}})
	if err != nil {
		t.Fatal(err)
	}
	if api.ExportedDeclarations != 1 || len(api.Issues) != 2 {
		t.Fatalf("accounting = %d declarations, issues %#v", api.ExportedDeclarations, api.Issues)
	}
}

func TestReportsMissingWhisperBindings(t *testing.T) {
	api := fixtureAPI(t)
	ffi := &FFICatalog{Bindings: map[string][]FFIBinding{
		"whisper_n_len": {{CName: "whisper_n_len"}},
	}}

	report := CheckFFI(api, ffi)
	if report.Required != 3 || report.Covered != 1 {
		t.Fatalf("coverage = %d/%d", report.Covered, report.Required)
	}
	if len(report.Missing) != 2 || report.Missing[0].Name != "whisper_name" || report.Missing[1].Name != "whisper_old" {
		t.Fatalf("missing = %#v", report.Missing)
	}
}

func TestCoverageIgnoresNonWhisperHeaders(t *testing.T) {
	api := &CAPI{Functions: map[string]CFunction{
		"whisper_version": {Name: "whisper_version", File: "whisper.h"},
		"ggml_init":       {Name: "ggml_init", File: "ggml.h"},
	}}
	ffi := &FFICatalog{Bindings: map[string][]FFIBinding{}}

	report := CheckFFI(api, ffi)
	if report.Required != 1 || len(report.Missing) != 1 || report.Missing[0].Name != "whisper_version" {
		t.Fatalf("coverage report = %#v", report)
	}
}

func TestEnumCheckingFindsValuePartialAndSemanticMismatches(t *testing.T) {
	const header = `
enum whisper_expected {
    WHISPER_EXPECTED_A = 0,
    WHISPER_EXPECTED_B = 1,
};
enum whisper_other {
    WHISPER_OTHER_A = 0,
};
#define WHISPER_MAGIC 3
WHISPER_API void fixture_enum(enum whisper_expected value);
WHISPER_API void fixture_enum_ok(enum whisper_expected value);
`
	api, err := ParseCAPI([]Header{{Name: "whisper.h", Source: header, APIMacros: []string{"WHISPER_API"}}})
	if err != nil {
		t.Fatal(err)
	}
	enums, err := CheckGoEnums(api, "testdata/enumfixture")
	if err != nil {
		t.Fatal(err)
	}
	if enums.Constants != 3 || enums.Clean != 1 || len(enums.Violations) != 2 {
		t.Fatalf("constant report = %#v", enums)
	}
	violations := map[string]bool{}
	for _, violation := range enums.Violations {
		violations[violation.Symbol] = true
	}
	if !violations["WHISPER_EXPECTED_A"] || !violations["WHISPER_MAGIC"] {
		t.Fatalf("constant violations = %#v", enums.Violations)
	}
	if enums.Enums != 2 || enums.Complete != 1 {
		t.Fatalf("enum coverage = %d enums, %d complete", enums.Enums, enums.Complete)
	}
	if len(enums.Partial) != 1 || enums.Partial[0].Name != "whisper_expected" || len(enums.Partial[0].Missing) != 1 || enums.Partial[0].Missing[0].Name != "WHISPER_EXPECTED_B" {
		t.Fatalf("partial enums = %#v", enums.Partial)
	}

	ffi := &FFICatalog{Bindings: map[string][]FFIBinding{
		"fixture_enum": {{
			CName: "fixture_enum", GoVar: "semanticFunc",
			Return:     FFIType{Name: "TypeVoid", Kind: CVoid, Size: 0},
			Parameters: []FFIType{{Name: "TypeSint32", Kind: CSigned, Size: 4}},
		}},
		"fixture_enum_ok": {{
			CName: "fixture_enum_ok", GoVar: "semanticOKFunc",
			Return:     FFIType{Name: "TypeVoid", Kind: CVoid, Size: 0},
			Parameters: []FFIType{{Name: "TypeSint32", Kind: CSigned, Size: 4}},
		}},
	}}
	calls, err := CheckGoCalls(api, ffi, enums.GoTypes, "testdata/enumfixture")
	if err != nil {
		t.Fatal(err)
	}
	if calls.EnumArgs != 2 || calls.CleanEnumArgs != 1 || len(calls.Violations) != 1 {
		t.Fatalf("semantic enum report = %#v", calls)
	}
	if calls.Violations[0].Symbol != "fixture_enum" || !strings.Contains(calls.Violations[0].Detail, "enumfixture.Other mirrors enum whisper_other") {
		t.Fatalf("semantic enum violation = %#v", calls.Violations[0])
	}
}

func TestConflictingConstantDefinitionsRemainUnresolved(t *testing.T) {
	const header = "#define VALUE 1\n#define VALUE 2\n"
	api, err := ParseCAPI([]Header{{Name: "constants.h", Source: header}})
	if err != nil {
		t.Fatal(err)
	}
	c := api.Constants["VALUE"]
	if c.Resolved || c.Reason != "multiple conditional definitions have different values" {
		t.Fatalf("VALUE = %#v", c)
	}
}

func TestStructMemberNamesDetectTransposition(t *testing.T) {
	c := []ffiLeaf{
		{Offset: 0, Size: 4, Kind: CSigned, Path: "left"},
		{Offset: 4, Size: 4, Kind: CSigned, Path: "right"},
	}
	clean := []goLeaf{
		{offset: 0, size: 4, kind: CSigned, path: "Left"},
		{offset: 4, size: 4, kind: CSigned, path: "Right"},
	}
	if problems, unverified := compareMemberNames(c, clean); len(problems) != 0 || len(unverified) != 0 {
		t.Fatalf("clean member names = problems %#v, unverified %#v", problems, unverified)
	}

	swapped := []goLeaf{
		{offset: 0, size: 4, kind: CSigned, path: "Right"},
		{offset: 4, size: 4, kind: CSigned, path: "Left"},
	}
	problems, unverified := compareMemberNames(c, swapped)
	if len(problems) != 2 || len(unverified) != 0 || !strings.Contains(problems[0], "transposed") {
		t.Fatalf("swapped member names = problems %#v, unverified %#v", problems, unverified)
	}
}

func TestCallbackCheckingFindsSignatureAndStoredPointerMismatches(t *testing.T) {
	const header = `
typedef void (*whisper_good_callback)(int32_t value, void * data);
typedef bool (*whisper_bad_callback)(void * data);
typedef struct whisper_params {
    whisper_good_callback good_callback;
    whisper_bad_callback bad_callback;
} whisper_params;
`
	api, err := ParseCAPI([]Header{{Name: "whisper.h", Source: header}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := CheckCallbacks(api, "testdata/callbackfixture")
	if err != nil {
		t.Fatal(err)
	}
	if report.Callbacks != 2 || report.Clean != 1 {
		t.Fatalf("callback signature report = %#v", report)
	}
	if report.PointerFields != 2 || report.TracedFields != 2 {
		t.Fatalf("callback field report = %#v", report)
	}
	if len(report.Violations) < 2 {
		t.Fatalf("callback violations = %#v", report.Violations)
	}
	var signature, store bool
	for _, violation := range report.Violations {
		signature = signature || (violation.Symbol == "whisper_bad_callback" && strings.Contains(violation.Detail, "arg0"))
		store = store || (violation.Symbol == "whisper_params.good_callback" && strings.Contains(violation.Detail, "whisper_bad_callback"))
	}
	if !signature || !store {
		t.Fatalf("callback violations = %#v", report.Violations)
	}
}

func TestStringSignednessAndDeprecationChecking(t *testing.T) {
	const header = `
#define WHISPER_API
WHISPER_API void fixture_string_bad(const char * value);
WHISPER_API void fixture_string_good(const char * value);
WHISPER_DEPRECATED(WHISPER_API void fixture_deprecated_bad(void), "use another function");
WHISPER_DEPRECATED(WHISPER_API void fixture_deprecated_good(void), "use another function");
WHISPER_API void fixture_signed_pointer(int32_t * value);
`
	api, err := ParseCAPI([]Header{{Name: "whisper.h", Source: header, APIMacros: []string{"WHISPER_API"}}})
	if err != nil {
		t.Fatal(err)
	}
	void := FFIType{Name: "TypeVoid", Kind: CVoid, Size: 0}
	pointer := FFIType{Name: "TypePointer", Kind: CPointer, Size: 8}
	ffi := &FFICatalog{Bindings: map[string][]FFIBinding{
		"fixture_string_bad":      {{CName: "fixture_string_bad", GoVar: "stringBadFunc", Return: void, Parameters: []FFIType{pointer}}},
		"fixture_string_good":     {{CName: "fixture_string_good", GoVar: "stringGoodFunc", Return: void, Parameters: []FFIType{pointer}}},
		"fixture_deprecated_bad":  {{CName: "fixture_deprecated_bad", GoVar: "deprecatedBadFunc", Return: void}},
		"fixture_deprecated_good": {{CName: "fixture_deprecated_good", GoVar: "deprecatedGoodFunc", Return: void}},
		"fixture_signed_pointer":  {{CName: "fixture_signed_pointer", GoVar: "signedPointerFunc", Return: void, Parameters: []FFIType{pointer}}},
	}}
	report, err := CheckGoCalls(api, ffi, nil, "testdata/gocheckfixture")
	if err != nil {
		t.Fatal(err)
	}
	if report.StringArgs != 2 || report.CleanStrings != 1 {
		t.Fatalf("C string report = %#v", report)
	}
	var unterminated bool
	for _, violation := range report.Violations {
		unterminated = unterminated || violation.Symbol == "fixture_string_bad" && strings.Contains(violation.Detail, "without a terminator")
	}
	if !unterminated {
		t.Fatalf("C string violations = %#v", report.Violations)
	}
	if report.Deprecated != 2 || report.CleanDeprecated != 1 || len(report.Deprecation) != 1 || report.Deprecation[0].Symbol != "fixture_deprecated_bad" {
		t.Fatalf("deprecation report = %#v", report)
	}
	if len(report.Signedness) != 1 || report.Signedness[0].Symbol != "fixture_signed_pointer" {
		t.Fatalf("signedness report = %#v", report.Signedness)
	}
}

func TestFFISignednessIsReportedWithoutBecomingABIMismatch(t *testing.T) {
	api := &CAPI{
		Functions: map[string]CFunction{
			"fixture_sign": {
				Name: "fixture_sign", File: "whisper.h",
				ReturnType: CType{Raw: "int32_t", Kind: CSigned, Size: 4},
				Parameters: []CParameter{{Type: CType{Raw: "uint32_t", Kind: CUnsigned, Size: 4}}},
			},
		},
	}
	ffi := &FFICatalog{Bindings: map[string][]FFIBinding{
		"fixture_sign": {{
			CName:      "fixture_sign",
			Return:     FFIType{Name: "TypeUint32", Kind: CUnsigned, Size: 4},
			Parameters: []FFIType{{Name: "TypeSint32", Kind: CSigned, Size: 4}},
		}},
	}}
	report := CheckFFI(api, ffi)
	if len(report.Violations) != 0 || len(report.Signedness) != 2 {
		t.Fatalf("FFI signedness report = %#v", report)
	}
}

func TestFetchVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	defer server.Close()

	version, err := fetchVersion(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v2.0.0" {
		t.Fatalf("version = %q", version)
	}
}

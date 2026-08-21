package main

import (
	"net/http"
	"net/http/httptest"
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

// ffi-checker catalogs the C API exposed by the whisper.cpp version Bucky uses.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	version := flag.String("version", "", "whisper.cpp version to catalog (default: current bucky-builder release)")
	flag.Parse()

	var err error
	if *version == "" {
		*version, err = resolveVersion()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}

	headers, source, err := obtainHeaders(*version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "obtain whisper.cpp headers: %v\n", err)
		os.Exit(2)
	}

	api, err := ParseCAPI(headers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse C API: %v\n", err)
		os.Exit(1)
	}
	ffi, err := ParseFFI("../../pkg/whisper")
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse FFI bindings: %v\n", err)
		os.Exit(2)
	}
	report := CheckFFI(api, ffi)
	goReport, err := CheckGoCalls(api, ffi, "../../pkg/whisper")
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse Go call sites: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("whisper.cpp C API: %s (%s)\n", *version, source)
	fmt.Printf("  functions:  %d\n", len(api.Functions))
	fmt.Printf("  declarations: %d exported, %d parse issues\n", api.ExportedDeclarations, len(api.Issues))
	fmt.Printf("  aggregates: %d (%d layouts resolved)\n", len(api.Structs), api.ResolvedLayouts())
	fmt.Printf("  enums:      %d\n", len(api.Enums))
	fmt.Printf("  constants:  %d resolved, %d unresolved\n", api.ResolvedConstants(), api.UnresolvedConstants())
	fmt.Printf("  callbacks:  %d\n", len(api.Callbacks))
	fmt.Printf("  typedefs:   %d\n", len(api.Typedefs))
	fmt.Printf("FFI bindings: %d (%d matched, %d clean, %d not verified, %d violations, %d parse issues)\n",
		report.Bindings, report.Matched, report.Clean, len(report.Unverified), len(report.Violations), len(ffi.Issues))
	fmt.Printf("whisper.h coverage: %d/%d functions bound (%d missing)\n",
		report.Covered, report.Required, len(report.Missing))
	for _, fn := range report.Missing {
		fmt.Printf("  MISSING %s (%s:%d)\n", fn.Name, fn.File, fn.Line)
	}
	for _, issue := range ffi.Issues {
		fmt.Printf("  NOT PARSED %s:%d: %s\n", issue.File, issue.Line, issue.Detail)
	}
	for _, item := range report.Unverified {
		fmt.Printf("  NOT VERIFIED %s\n", item)
	}
	for _, violation := range report.Violations {
		fmt.Printf("  MISMATCH %s (%s:%d): %s\n", violation.Symbol, violation.File, violation.Line, violation.Detail)
	}
	fmt.Printf("Go call mappings: %d (%d clean, %d not verified, %d violations)\n",
		goReport.Calls, goReport.Clean, len(goReport.Unverified), len(goReport.Violations))
	for _, item := range goReport.Unverified {
		fmt.Printf("  NOT VERIFIED %s\n", item)
	}
	for _, violation := range goReport.Violations {
		fmt.Printf("  MISMATCH %s (%s:%d): %s\n", violation.Symbol, violation.File, violation.Line, violation.Detail)
	}
	if len(ffi.Issues) > 0 {
		os.Exit(2)
	}
	if len(report.Missing) > 0 || len(report.Violations) > 0 || len(goReport.Violations) > 0 {
		os.Exit(1)
	}
}

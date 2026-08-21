// ffi-checker catalogs the C API exposed by the whisper.cpp version Bucky uses.
package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
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
	enumReport, err := CheckGoEnums(api, "../../pkg/whisper")
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse Go constants: %v\n", err)
		os.Exit(2)
	}
	goReport, err := CheckGoCalls(api, ffi, enumReport.GoTypes, "../../pkg/whisper")
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse Go call sites: %v\n", err)
		os.Exit(2)
	}
	callbackReport, err := CheckCallbacks(api, "../../pkg/whisper")
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse Go callbacks: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("whisper.cpp %s (%s)\n\n", *version, source)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CHECK\tSTATUS\tSUMMARY")
	fmt.Fprintln(w, "-----\t------\t-------")
	tableRow(w, "Headers", checkStatus(0, len(api.Issues)), "%d declarations, %d parse issues", api.ExportedDeclarations, len(api.Issues))
	tableRow(w, "API coverage", checkStatus(len(report.Missing), 0), "%d/%d functions bound (%d missing)", report.Covered, report.Required, len(report.Missing))
	tableRow(w, "FFI bindings", checkStatus(len(report.Violations)+len(report.Signedness)+len(ffi.Issues), len(report.Unverified)), "%d/%d ABI clean; %d ABI, %d signedness mismatches; %d not verified", report.Clean, report.Bindings, len(report.Violations), len(report.Signedness), len(report.Unverified))
	tableRow(w, "Constants and enums", checkStatus(len(enumReport.Violations)+len(enumReport.Partial), len(enumReport.Unverified)), "%d/%d constants clean, %d/%d enums complete", enumReport.Clean, enumReport.Constants, enumReport.Complete, enumReport.Enums)
	unsafeStrings := goReport.StringArgs - goReport.CleanStrings - goReport.UnverifiedStrings
	enumMismatches := goReport.EnumArgs - goReport.CleanEnumArgs
	otherMismatches := max(0, len(goReport.Violations)-enumMismatches-unsafeStrings)
	tableRow(w, "Go calls and structs", checkStatus(len(goReport.Violations)+len(goReport.Signedness), len(goReport.Unverified)), "%d/%d clean; %d enum, %d C string, %d signedness, %d other mismatches; %d not verified", goReport.Clean, goReport.Calls, enumMismatches, unsafeStrings, len(goReport.Signedness), otherMismatches, len(goReport.Unverified))
	tableRow(w, "Callbacks", checkStatus(len(callbackReport.Violations), len(callbackReport.Unverified)), "%d/%d signatures clean, %d/%d struct fields traced", callbackReport.Clean, callbackReport.Callbacks, callbackReport.TracedFields, callbackReport.PointerFields)
	tableRow(w, "Deprecated APIs", checkStatus(len(goReport.Deprecation), 0), "%d/%d wrappers documented", goReport.CleanDeprecated, goReport.Deprecated)
	_ = w.Flush()
	if len(ffi.Issues) > 0 {
		os.Exit(2)
	}
	if len(report.Missing) > 0 || len(report.Violations) > 0 || len(report.Signedness) > 0 || len(enumReport.Violations) > 0 || len(enumReport.Partial) > 0 || len(goReport.Violations) > 0 || len(goReport.Signedness) > 0 || len(goReport.Deprecation) > 0 || len(callbackReport.Violations) > 0 {
		os.Exit(1)
	}
}

func checkStatus(failures, warnings int) string {
	if failures > 0 {
		return "FAIL"
	}
	if warnings > 0 {
		return "WARN"
	}
	return "PASS"
}

func tableRow(w *tabwriter.Writer, check, status, format string, args ...any) {
	fmt.Fprintf(w, "%s\t%s\t%s\n", check, status, fmt.Sprintf(format, args...))
}

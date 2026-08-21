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

	fmt.Printf("whisper.cpp C API: %s (%s)\n", *version, source)
	fmt.Printf("  functions:  %d\n", len(api.Functions))
	fmt.Printf("  declarations: %d exported, %d parse issues\n", api.ExportedDeclarations, len(api.Issues))
	fmt.Printf("  aggregates: %d (%d layouts resolved)\n", len(api.Structs), api.ResolvedLayouts())
	fmt.Printf("  enums:      %d\n", len(api.Enums))
	fmt.Printf("  constants:  %d resolved, %d unresolved\n", api.ResolvedConstants(), api.UnresolvedConstants())
	fmt.Printf("  callbacks:  %d\n", len(api.Callbacks))
	fmt.Printf("  typedefs:   %d\n", len(api.Typedefs))
}

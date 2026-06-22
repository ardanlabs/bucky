// Package cmd implements the bucky CLI subcommands (install, system,
// model, whisper, info) wired into urfave/cli.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ardanlabs/bucky/pkg/download"
	"github.com/urfave/cli/v2"
)

// InstallCmd installs whisper.cpp shared libraries into a local directory.
var InstallCmd = &cli.Command{
	Name:  "install",
	Usage: "Install whisper.cpp libraries used by bucky",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "version",
			Aliases: []string{"v"},
			Usage:   "version of whisper.cpp to install (e.g. v1.9.1; default is the bucky-pinned version, pass \"latest\" to query the GitHub releases API)",
			Value:   "",
		},
		&cli.StringFlag{
			Name:    "lib",
			Aliases: []string{"l"},
			Usage:   "path to whisper.cpp compiled library files",
			EnvVars: []string{"BUCKY_LIB"},
		},
		&cli.StringFlag{
			Name:    "processor",
			Aliases: []string{"p"},
			Usage:   "processor to use (cpu, cuda, metal, vulkan)",
			Value:   "",
		},
		&cli.StringFlag{
			Name:  "os",
			Usage: "operating system to use (linux, windows, darwin)",
			Value: runtime.GOOS,
		},
		&cli.BoolFlag{
			Name:    "upgrade",
			Aliases: []string{"u"},
			Usage:   "upgrade existing installation",
			Value:   false,
		},
		&cli.BoolFlag{
			Name:    "quiet",
			Aliases: []string{"q"},
			Usage:   "suppress output during installation",
			Value:   false,
		},
	},
	Action: func(c *cli.Context) error {
		return runInstall(c)
	},
}

func runInstall(c *cli.Context) error {
	libPath := c.String("lib")
	version := c.String("version")
	processor := c.String("processor")
	osInstall := c.String("os")
	upgrade := c.Bool("upgrade")

	if libPath == "" {
		return fmt.Errorf("missing lib flag or BUCKY_LIB env var")
	}

	if !upgrade {
		if _, err := os.Stat(filepath.Join(libPath, download.LibraryName(runtime.GOOS))); !os.IsNotExist(err) {
			fmt.Println("whisper.cpp already installed at", libPath)
			return nil
		}
	}

	switch version {
	case "":
		// Use the bucky-pinned default. Avoids hitting the GitHub
		// releases API for first installs and CI runs.
		version = download.DefaultWhisperVersion
	case "latest":
		var err error
		version, err = download.WhisperLatestVersion()
		if err != nil {
			return fmt.Errorf("could not obtain latest version: %w", err)
		}
	}

	quiet := c.Bool("quiet")
	if !quiet {
		fmt.Println("installing whisper.cpp version", version, "to", libPath)
	} else {
		download.ProgressTracker = nil
	}

	if processor == "" {
		processor = defaultProcessor(osInstall, quiet)
	}

	if err := download.Get(runtime.GOARCH, osInstall, processor, version, libPath); err != nil {
		return fmt.Errorf("failed to download whisper.cpp: %w", err)
	}

	if !quiet {
		fmt.Println("done.")
		showInstallRequirements(libPath)
	}

	return nil
}

// defaultProcessor picks a sensible default backend per OS, matching how
// whisper.cpp / bucky-builder ship release artifacts. CUDA is auto-selected
// on linux + windows when nvidia-smi is on PATH; vulkan is never picked
// automatically (users opt in with `-p vulkan`).
func defaultProcessor(osInstall string, quiet bool) string {
	switch osInstall {
	case "darwin":
		// The xcframework includes Metal so cpu and metal both resolve to it.
		return "metal"
	case "windows", "linux":
		if cudaInstalled, cudaVersion := download.HasCUDA(); cudaInstalled {
			if !quiet {
				fmt.Printf("CUDA detected (version %s), using CUDA build\n", cudaVersion)
			}
			return "cuda"
		}
		return "cpu"
	default:
		return "cpu"
	}
}

func showInstallRequirements(libPath string) {
	if os.Getenv("BUCKY_LIB") == libPath {
		return
	}

	switch runtime.GOOS {
	case "linux":
		fmt.Println(`
You may want to set the BUCKY_LIB environment variable to the directory with your whisper.cpp library files. For example:

    export BUCKY_LIB=` + libPath)
	case "windows":
		fmt.Println(`
You may want to set the BUCKY_LIB environment variable to the directory with your whisper.cpp library files. For example:

    set BUCKY_LIB=` + libPath)
	case "darwin":
		fmt.Println(`
You may want to set the BUCKY_LIB environment variable to the directory with your whisper.cpp library files. For example:

    export BUCKY_LIB=` + libPath)
	}
}

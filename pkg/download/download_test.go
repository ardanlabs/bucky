package download

import (
	"errors"
	"testing"
)

func TestVersionIsValid(t *testing.T) {
	tests := []struct {
		version string
		wantErr bool
	}{
		{"v1.9.1", false},
		{"v1.7.0", false},
		{"1.8.4", true},
		{"v1", true},
		{"", true},
		{"latest", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			err := VersionIsValid(tt.version)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tt.version)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error for %q, got %v", tt.version, err)
			}
		})
	}
}

func TestLibraryName(t *testing.T) {
	tests := []struct {
		os   string
		want string
	}{
		{"linux", "libwhisper.so"},
		{"darwin", "libwhisper.dylib"},
		{"windows", "whisper.dll"},
		{"plan9", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.os, func(t *testing.T) {
			got := LibraryName(tt.os)
			if got != tt.want {
				t.Errorf("LibraryName(%q) = %q, want %q", tt.os, got, tt.want)
			}
		})
	}
}

func TestGetDownloadLocationAndFilename(t *testing.T) {
	const buckyBuilder = "https://github.com/ardanlabs/bucky-builder/releases/download/v1.9.1"
	const upstream = "https://github.com/ggml-org/whisper.cpp/releases/download/v1.9.1"

	tests := []struct {
		name         string
		arch         Arch
		os           OS
		proc         Processor
		version      string
		wantLocation string
		wantFile     string
		wantErr      error
	}{
		{
			name:         "darwin arm64 cpu uses bucky-builder xcframework",
			arch:         ARM64,
			os:           Darwin,
			proc:         CPU,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-darwin-metal-universal.zip",
		},
		{
			name:         "darwin arm64 metal uses bucky-builder xcframework",
			arch:         ARM64,
			os:           Darwin,
			proc:         Metal,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-darwin-metal-universal.zip",
		},
		{
			name:         "windows amd64 cpu uses bucky-builder",
			arch:         AMD64,
			os:           Windows,
			proc:         CPU,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-windows-cpu-x64.zip",
		},
		{
			name:         "windows amd64 cuda still uses upstream",
			arch:         AMD64,
			os:           Windows,
			proc:         CUDA,
			version:      "v1.9.1",
			wantLocation: upstream,
			wantFile:     "whisper-cublas-12.4.0-bin-x64.zip",
		},
		{
			name:         "linux amd64 cpu",
			arch:         AMD64,
			os:           Linux,
			proc:         CPU,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-ubuntu-cpu-x64.tar.gz",
		},
		{
			name:         "linux amd64 cuda",
			arch:         AMD64,
			os:           Linux,
			proc:         CUDA,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-ubuntu-cuda-x64.tar.gz",
		},
		{
			name:         "linux amd64 vulkan",
			arch:         AMD64,
			os:           Linux,
			proc:         Vulkan,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-ubuntu-vulkan-x64.tar.gz",
		},
		{
			name:         "linux arm64 cpu",
			arch:         ARM64,
			os:           Linux,
			proc:         CPU,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-ubuntu-cpu-arm64.tar.gz",
		},
		{
			name:         "linux arm64 cuda",
			arch:         ARM64,
			os:           Linux,
			proc:         CUDA,
			version:      "v1.9.1",
			wantLocation: buckyBuilder,
			wantFile:     "whisper-v1.9.1-bin-ubuntu-cuda-arm64.tar.gz",
		},
		{
			name:    "linux metal unsupported",
			arch:    AMD64,
			os:      Linux,
			proc:    Metal,
			version: "v1.9.1",
			wantErr: ErrUnknownProcessor,
		},
		{
			name:    "windows arm64 unsupported",
			arch:    ARM64,
			os:      Windows,
			proc:    CPU,
			version: "v1.9.1",
			wantErr: ErrUnsupportedPlatform,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocation, gotFile, err := getDownloadLocationAndFilename(tt.arch, tt.os, tt.proc, tt.version)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotFile != tt.wantFile {
				t.Errorf("filename: got %q, want %q", gotFile, tt.wantFile)
			}
			if gotLocation != tt.wantLocation {
				t.Errorf("location: got %q, want %q", gotLocation, tt.wantLocation)
			}
		})
	}
}

func TestParseHelpers(t *testing.T) {
	if _, err := ParseArch("amd64"); err != nil {
		t.Errorf("ParseArch(amd64): %v", err)
	}
	if _, err := ParseArch("nope"); err == nil {
		t.Error("ParseArch(nope) should fail")
	}
	if _, err := ParseOS("darwin"); err != nil {
		t.Errorf("ParseOS(darwin): %v", err)
	}
	if _, err := ParseOS("nope"); err == nil {
		t.Error("ParseOS(nope) should fail")
	}
	if _, err := ParseProcessor("cpu"); err != nil {
		t.Errorf("ParseProcessor(cpu): %v", err)
	}
	if _, err := ParseProcessor("nope"); err == nil {
		t.Error("ParseProcessor(nope) should fail")
	}
}

package cmd

import "testing"

func TestCUDAProcessor(t *testing.T) {
	tests := []struct {
		os      string
		version string
		want    string
	}{
		{"linux", "12.9", "cuda12"},
		{"linux", "13.0", "cuda13"},
		{"linux", "14.1", "cuda13"},
		{"linux", "", "cuda12"},
		{"linux", "unknown", "cuda12"},
		{"windows", "13.0", "cuda12"},
		{"windows", "12.4", "cuda12"},
	}
	for _, tt := range tests {
		t.Run(tt.os+"/"+tt.version, func(t *testing.T) {
			if got := cudaProcessor(tt.os, tt.version); got != tt.want {
				t.Errorf("processor: got %q, want %q", got, tt.want)
			}
		})
	}
}

package whisper

import (
	"testing"
)

// TestVersion verifies the expected whisper.cpp version string crosses the
// FFI boundary. The builder compiles tagged source with upstream's default
// WHISPER_BUILD_IS_DEV setting, which appends the -dev suffix.
func TestVersion(t *testing.T) {
	testSetup(t)

	const want = "1.9.4-dev"

	if got := Version(); got != want {
		t.Errorf("Version() = %q, want %q", got, want)
	}
}

// TestPrintSystemInfo verifies the system-info string is returned intact.
func TestPrintSystemInfo(t *testing.T) {
	testSetup(t)

	info := PrintSystemInfo()
	if info == "" {
		t.Fatal("PrintSystemInfo returned empty string")
	}
	t.Logf("whisper.PrintSystemInfo = %q", info)
}

// NOTE: there is intentionally no concurrent-access test for Context.
// Upstream whisper.cpp documents whisper_full as "Not thread safe for
// same context" (see the doc comment on Context in whisper.go). Callers
// that want parallel transcription must allocate one Context per
// goroutine; that pattern is exercised by the existing single-instance
// tests, so a multi-instance test would only burn CI minutes loading N
// copies of the same model without verifying any new contract.

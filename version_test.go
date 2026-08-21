package main

import (
	"testing"
)

func TestVersion(t *testing.T) {
	const want = "1.0.9"
	if got := Version(); got != want {
		t.Errorf("Version() = %q, want %q", got, want)
	}
}

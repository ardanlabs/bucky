package whisper

import (
	"testing"
	"unsafe"
)

// TestStringRefs_SettersPopulateSlot verifies each setter on StringRefs
// writes a non-zero pointer into the expected WhisperFullParams slot when
// given a non-empty Go string. The byte-level content of the C string is
// covered transitively by utils.BytePtrFromString's own contract; here we
// pin the FFI plumbing — that the right *byte lands in the right slot.
func TestStringRefs_SettersPopulateSlot(t *testing.T) {
	testSetup(t)

	cases := []struct {
		name   string
		input  string
		assign func(refs *StringRefs, p *WhisperFullParams, s string) error
		read   func(p *WhisperFullParams) uintptr
	}{
		{
			name:  "SetLanguage",
			input: "en",
			assign: func(refs *StringRefs, p *WhisperFullParams, s string) error {
				return refs.SetLanguage(p, s)
			},
			read: func(p *WhisperFullParams) uintptr { return p.Language },
		},
		{
			name:  "SetInitialPrompt",
			input: "transcribe in English",
			assign: func(refs *StringRefs, p *WhisperFullParams, s string) error {
				return refs.SetInitialPrompt(p, s)
			},
			read: func(p *WhisperFullParams) uintptr { return p.InitialPrompt },
		},
		{
			name:  "SetSuppressRegex",
			input: `\d+`,
			assign: func(refs *StringRefs, p *WhisperFullParams, s string) error {
				return refs.SetSuppressRegex(p, s)
			},
			read: func(p *WhisperFullParams) uintptr { return p.SuppressRegex },
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var p WhisperFullParams
			var refs StringRefs

			if err := tt.assign(&refs, &p, tt.input); err != nil {
				t.Fatalf("assign: %v", err)
			}
			defer refs.KeepAlive()

			if ptr := tt.read(&p); ptr == 0 {
				t.Errorf("slot is zero after assign(%q), want non-zero", tt.input)
			}
		})
	}
}

// TestStringRefs_EmptyClearsSlot verifies that passing an empty string
// zeros the params slot (instead of allocating a one-byte NUL buffer). The
// C library interprets a NULL Language / InitialPrompt / SuppressRegex
// pointer as "unset"; if the helpers silently substituted an empty string
// for NULL, whisper would treat "" as an explicit value and skip the
// auto-detect / no-suppress fast paths.
func TestStringRefs_EmptyClearsSlot(t *testing.T) {
	testSetup(t)

	cases := []struct {
		name   string
		assign func(refs *StringRefs, p *WhisperFullParams) error
		read   func(p *WhisperFullParams) uintptr
	}{
		{
			name:   "SetLanguage",
			assign: func(refs *StringRefs, p *WhisperFullParams) error { return refs.SetLanguage(p, "") },
			read:   func(p *WhisperFullParams) uintptr { return p.Language },
		},
		{
			name:   "SetInitialPrompt",
			assign: func(refs *StringRefs, p *WhisperFullParams) error { return refs.SetInitialPrompt(p, "") },
			read:   func(p *WhisperFullParams) uintptr { return p.InitialPrompt },
		},
		{
			name:   "SetSuppressRegex",
			assign: func(refs *StringRefs, p *WhisperFullParams) error { return refs.SetSuppressRegex(p, "") },
			read:   func(p *WhisperFullParams) uintptr { return p.SuppressRegex },
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Pre-populate with a sentinel pointer so we can detect that
			// empty-string assignment actually zeros the slot (rather than
			// silently no-oping).
			var p WhisperFullParams
			var seed StringRefs
			if err := seed.SetLanguage(&p, "sentinel"); err != nil {
				t.Fatalf("seed SetLanguage: %v", err)
			}
			defer seed.KeepAlive()

			var refs StringRefs
			if err := tt.assign(&refs, &p); err != nil {
				t.Fatalf("assign empty: %v", err)
			}
			if got := tt.read(&p); got != 0 {
				t.Errorf("slot = %#x after assign(\"\"), want 0", got)
			}
		})
	}
}

// TestStringRefs_SetPromptTokens verifies SetPromptTokens populates both
// the pointer and count slots from a non-empty token slice, copies the
// caller's data (so later mutation of the source does not corrupt the FFI
// buffer), and clears both slots for an empty slice. The helper is pure Go,
// so this test runs without the whisper library loaded.
func TestStringRefs_SetPromptTokens(t *testing.T) {
	t.Run("populates", func(t *testing.T) {
		var p WhisperFullParams
		var refs StringRefs

		src := []Token{10, 20, 30}
		refs.SetPromptTokens(&p, src)
		defer refs.KeepAlive()

		if p.PromptTokens == 0 {
			t.Error("PromptTokens slot is zero after assign, want non-zero")
		}
		if p.PromptNTokens != int32(len(src)) {
			t.Errorf("PromptNTokens: got %d, want %d", p.PromptNTokens, len(src))
		}

		// The retained buffer must point at the same memory the params
		// slot does, so whisper.cpp reads exactly what we kept alive.
		buf := refs.keepTokens[0]
		if p.PromptTokens != uintptr(unsafe.Pointer(&buf[0])) {
			t.Error("PromptTokens slot does not point at the retained buffer")
		}

		// Mutating the source must not change the retained buffer,
		// proving SetPromptTokens copied the data.
		src[0] = 999
		if buf[0] != 10 {
			t.Errorf("buffer aliased source: got %d after mutation, want 10", buf[0])
		}
	})

	t.Run("empty clears", func(t *testing.T) {
		var p WhisperFullParams
		var refs StringRefs

		// Seed with real tokens, then clear with an empty slice.
		refs.SetPromptTokens(&p, []Token{1, 2})
		refs.SetPromptTokens(&p, nil)
		defer refs.KeepAlive()

		if p.PromptTokens != 0 {
			t.Errorf("PromptTokens slot = %#x after empty assign, want 0", p.PromptTokens)
		}
		if p.PromptNTokens != 0 {
			t.Errorf("PromptNTokens: got %d after empty assign, want 0", p.PromptNTokens)
		}
	})
}

// TestStringRefs_EmbeddedNul verifies the setters surface the underlying
// utils.BytePtrFromString error when the Go string contains an embedded
// NUL byte (a C-string violation that must not silently truncate the
// prompt or language code).
func TestStringRefs_EmbeddedNul(t *testing.T) {
	testSetup(t)

	var p WhisperFullParams
	var refs StringRefs

	if err := refs.SetInitialPrompt(&p, "before\x00after"); err == nil {
		t.Fatal("SetInitialPrompt with embedded NUL: expected error, got nil")
	}
	if p.InitialPrompt != 0 {
		t.Errorf("slot = %#x after failed assign, want 0", p.InitialPrompt)
	}
}

package whisper

import (
	"unsafe"

	"github.com/jupiterrider/ffi"
)

// Timings mirrors struct whisper_timings exactly.
type Timings struct {
	SampleMs float32
	EncodeMs float32
	DecodeMs float32
	BatchdMs float32
	PromptMs float32
}

var (
	// WHISPER_API struct whisper_timings * whisper_get_timings(struct whisper_context * ctx);
	getTimingsFunc ffi.Fun

	// WHISPER_API void whisper_print_timings(struct whisper_context * ctx);
	printTimingsFunc ffi.Fun

	// WHISPER_API void whisper_reset_timings(struct whisper_context * ctx);
	resetTimingsFunc ffi.Fun
)

func loadTimingsFuncs(lib ffi.Lib) error {
	var err error

	if getTimingsFunc, err = lib.Prep("whisper_get_timings", &ffi.TypePointer, &ffi.TypePointer); err != nil {
		return loadError("whisper_get_timings", err)
	}

	if printTimingsFunc, err = lib.Prep("whisper_print_timings", &ffi.TypeVoid, &ffi.TypePointer); err != nil {
		return loadError("whisper_print_timings", err)
	}

	if resetTimingsFunc, err = lib.Prep("whisper_reset_timings", &ffi.TypeVoid, &ffi.TypePointer); err != nil {
		return loadError("whisper_reset_timings", err)
	}

	return nil
}

// GetTimings returns performance information from ctx's default state, or nil
// when that state has not been initialized. whisper.cpp allocates a new native
// object and transfers ownership to the caller, but exposes no function with
// which Go callers can free it. The pointer does not borrow ctx; each call
// allocates another object, so avoid repeatedly requesting snapshots.
func GetTimings(ctx Context) *Timings {
	if ctx == 0 {
		return nil
	}

	var timings *Timings
	getTimingsFunc.Call(unsafe.Pointer(&timings), unsafe.Pointer(&ctx))
	return timings
}

// PrintTimings writes performance information from ctx's default state using
// whisper.cpp's configured logger. The context must remain valid for the call.
func PrintTimings(ctx Context) {
	if ctx == 0 {
		return
	}
	printTimingsFunc.Call(nil, unsafe.Pointer(&ctx))
}

// ResetTimings clears performance counters in ctx's default state. The
// context must remain valid for the call.
func ResetTimings(ctx Context) {
	if ctx == 0 {
		return
	}
	resetTimingsFunc.Call(nil, unsafe.Pointer(&ctx))
}

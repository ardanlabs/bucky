package whisper

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	"github.com/jupiterrider/ffi"
)

var (
	pcmToMelFunc           ffi.Fun
	pcmToMelWithStateFunc  ffi.Fun
	setMelFunc             ffi.Fun
	setMelWithStateFunc    ffi.Fun
	encodeFunc             ffi.Fun
	encodeWithStateFunc    ffi.Fun
	decodeFunc             ffi.Fun
	decodeWithStateFunc    ffi.Fun
	getLogitsFunc          ffi.Fun
	getLogitsFromStateFunc ffi.Fun
	nLenFromStateFunc      ffi.Fun
)

func loadLowLevelFuncs(lib ffi.Lib) error {
	specs := []struct {
		name string
		out  *ffi.Fun
		ret  *ffi.Type
		args []*ffi.Type
	}{
		{"whisper_pcm_to_mel", &pcmToMelFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_pcm_to_mel_with_state", &pcmToMelWithStateFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_set_mel", &setMelFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_set_mel_with_state", &setMelWithStateFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_encode", &encodeFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_encode_with_state", &encodeWithStateFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_decode", &decodeFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_decode_with_state", &decodeWithStateFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypeSint32}},
		{"whisper_get_logits", &getLogitsFunc, &ffi.TypePointer, []*ffi.Type{&ffi.TypePointer}},
		{"whisper_get_logits_from_state", &getLogitsFromStateFunc, &ffi.TypePointer, []*ffi.Type{&ffi.TypePointer}},
		{"whisper_n_len_from_state", &nLenFromStateFunc, &ffi.TypeSint32, []*ffi.Type{&ffi.TypePointer}},
	}

	for _, spec := range specs {
		fn, err := lib.Prep(spec.name, spec.ret, spec.args...)
		if err != nil {
			return loadError(spec.name, err)
		}
		*spec.out = fn
	}
	return nil
}

// PcmToMel converts 16 kHz mono PCM samples into the default state's mel spectrogram.
func PcmToMel(ctx Context, samples []float32, nThreads int32) error {
	if ctx == 0 {
		return errors.New("whisper.PcmToMel: nil context")
	}
	if len(samples) == 0 {
		return errors.New("whisper.PcmToMel: empty samples")
	}
	if len(samples) > math.MaxInt32 {
		return errors.New("whisper.PcmToMel: too many samples")
	}
	ptr := unsafe.Pointer(unsafe.SliceData(samples))
	n := int32(len(samples))
	var result ffi.Arg
	pcmToMelFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&ptr), unsafe.Pointer(&n), unsafe.Pointer(&nThreads))
	return lowLevelResult("whisper_pcm_to_mel", result)
}

// PcmToMelWithState converts PCM samples into the supplied state's mel spectrogram.
func PcmToMelWithState(ctx Context, state State, samples []float32, nThreads int32) error {
	if ctx == 0 || state == 0 {
		return errors.New("whisper.PcmToMelWithState: nil context or state")
	}
	if len(samples) == 0 || len(samples) > math.MaxInt32 {
		return errors.New("whisper.PcmToMelWithState: invalid samples")
	}
	ptr := unsafe.Pointer(unsafe.SliceData(samples))
	n := int32(len(samples))
	var result ffi.Arg
	pcmToMelWithStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&state), unsafe.Pointer(&ptr), unsafe.Pointer(&n), unsafe.Pointer(&nThreads))
	return lowLevelResult("whisper_pcm_to_mel_with_state", result)
}

// SetMel installs a custom, row-major nLen by nMel mel spectrogram in the default state.
func SetMel(ctx Context, data []float32, nLen, nMel int32) error {
	if ctx == 0 {
		return errors.New("whisper.SetMel: nil context")
	}
	if err := validateMel(data, nLen, nMel); err != nil {
		return fmt.Errorf("whisper.SetMel: %w", err)
	}
	ptr := unsafe.Pointer(unsafe.SliceData(data))
	var result ffi.Arg
	setMelFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&ptr), unsafe.Pointer(&nLen), unsafe.Pointer(&nMel))
	return lowLevelResult("whisper_set_mel", result)
}

// SetMelWithState installs a custom mel spectrogram in state.
func SetMelWithState(ctx Context, state State, data []float32, nLen, nMel int32) error {
	if ctx == 0 || state == 0 {
		return errors.New("whisper.SetMelWithState: nil context or state")
	}
	if err := validateMel(data, nLen, nMel); err != nil {
		return fmt.Errorf("whisper.SetMelWithState: %w", err)
	}
	ptr := unsafe.Pointer(unsafe.SliceData(data))
	var result ffi.Arg
	setMelWithStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&state), unsafe.Pointer(&ptr), unsafe.Pointer(&nLen), unsafe.Pointer(&nMel))
	return lowLevelResult("whisper_set_mel_with_state", result)
}

// Encode runs the encoder over the default state's current mel spectrogram.
func Encode(ctx Context, offset, nThreads int32) error {
	if ctx == 0 {
		return errors.New("whisper.Encode: nil context")
	}
	var result ffi.Arg
	encodeFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&offset), unsafe.Pointer(&nThreads))
	return lowLevelResult("whisper_encode", result)
}

// EncodeWithState runs the encoder over state's current mel spectrogram.
func EncodeWithState(ctx Context, state State, offset, nThreads int32) error {
	if ctx == 0 || state == 0 {
		return errors.New("whisper.EncodeWithState: nil context or state")
	}
	var result ffi.Arg
	encodeWithStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&state), unsafe.Pointer(&offset), unsafe.Pointer(&nThreads))
	return lowLevelResult("whisper_encode_with_state", result)
}

// Decode runs the decoder using tokens as its context and stores logits in the default state.
func Decode(ctx Context, tokens []Token, nPast, nThreads int32) error {
	if ctx == 0 {
		return errors.New("whisper.Decode: nil context")
	}
	if len(tokens) == 0 || len(tokens) > math.MaxInt32 {
		return errors.New("whisper.Decode: invalid tokens")
	}
	ptr := unsafe.Pointer(unsafe.SliceData(tokens))
	n := int32(len(tokens))
	var result ffi.Arg
	decodeFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&ptr), unsafe.Pointer(&n), unsafe.Pointer(&nPast), unsafe.Pointer(&nThreads))
	return lowLevelResult("whisper_decode", result)
}

// DecodeWithState runs the decoder using the supplied state.
func DecodeWithState(ctx Context, state State, tokens []Token, nPast, nThreads int32) error {
	if ctx == 0 || state == 0 {
		return errors.New("whisper.DecodeWithState: nil context or state")
	}
	if len(tokens) == 0 || len(tokens) > math.MaxInt32 {
		return errors.New("whisper.DecodeWithState: invalid tokens")
	}
	ptr := unsafe.Pointer(unsafe.SliceData(tokens))
	n := int32(len(tokens))
	var result ffi.Arg
	decodeWithStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&state), unsafe.Pointer(&ptr), unsafe.Pointer(&n), unsafe.Pointer(&nPast), unsafe.Pointer(&nThreads))
	return lowLevelResult("whisper_decode_with_state", result)
}

// GetLogits returns a borrowed pointer to the C-owned logits from the last Decode call.
// It is invalidated by subsequent inference on ctx and by Free. The API does not expose
// the row count, so callers must not construct a slice without independently knowing it.
func GetLogits(ctx Context) *float32 {
	if ctx == 0 {
		return nil
	}
	var result *float32
	getLogitsFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx))
	return result
}

// GetLogitsFromState returns a borrowed pointer to the C-owned logits from the last
// DecodeWithState call. It is invalidated by later inference on state or FreeState.
func GetLogitsFromState(state State) *float32 {
	if state == 0 {
		return nil
	}
	var result *float32
	getLogitsFromStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&state))
	return result
}

// NLenFromState returns the mel length stored in state.
func NLenFromState(state State) int32 {
	if state == 0 {
		return 0
	}
	var result ffi.Arg
	nLenFromStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&state))
	return int32(result)
}

func validateMel(data []float32, nLen, nMel int32) error {
	if nLen <= 0 || nMel <= 0 || int64(nLen)*int64(nMel) != int64(len(data)) {
		return errors.New("data length must equal positive nLen * nMel")
	}
	return nil
}

func lowLevelResult(name string, result ffi.Arg) error {
	if rc := int32(result); rc != 0 {
		return fmt.Errorf("%s returned %d", name, rc)
	}
	return nil
}

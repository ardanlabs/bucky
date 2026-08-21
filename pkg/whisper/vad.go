package whisper

import (
	"errors"
	"os"
	"unsafe"

	"github.com/ardanlabs/bucky/pkg/utils"
	"github.com/jupiterrider/ffi"
)

// VadSegments is an opaque handle to a whisper_vad_segments object owned by
// the C library. Release it with VadFreeSegments when done.
type VadSegments uintptr

// ffiTypeVadParams describes whisper_vad_params to libffi. Padding bytes are
// not listed; libffi computes alignment from the field types just as the C
// compiler does.
//
// Layout (24 bytes, all 4-byte aligned, no padding):
//
//	float threshold;
//	int   min_speech_duration_ms;
//	int   min_silence_duration_ms;
//	float max_speech_duration_s;
//	int   speech_pad_ms;
//	float samples_overlap;
var ffiTypeVadParams = ffi.NewType(
	&ffi.TypeFloat,  // threshold
	&ffi.TypeSint32, // min_speech_duration_ms
	&ffi.TypeSint32, // min_silence_duration_ms
	&ffi.TypeFloat,  // max_speech_duration_s
	&ffi.TypeSint32, // speech_pad_ms
	&ffi.TypeFloat,  // samples_overlap
)

// ffiTypeVadContextParams describes whisper_vad_context_params to libffi.
//
// Layout (12 bytes on darwin/arm64):
//
//	int  n_threads;     // 0..4
//	bool use_gpu;       // 4
//	[3]byte _pad        // 5..7 (compiler-inserted)
//	int  gpu_device;    // 8..12
var ffiTypeVadContextParams = ffi.NewType(
	&ffi.TypeSint32, // n_threads
	&ffi.TypeUint8,  // use_gpu
	&ffi.TypeSint32, // gpu_device (libffi pads automatically)
)

var (
	// WHISPER_API int whisper_full_n_vad_segments(struct whisper_context * ctx);
	fullNVadSegmentsFunc ffi.Fun

	// WHISPER_API int whisper_full_n_vad_segments_from_state(struct whisper_state * state);
	fullNVadSegmentsFromStateFunc ffi.Fun

	// WHISPER_API int64_t whisper_full_get_vad_segment_t0(struct whisper_context * ctx, int i);
	fullGetVadSegmentT0Func ffi.Fun

	// WHISPER_API int64_t whisper_full_get_vad_segment_t0_from_state(struct whisper_state * state, int i);
	fullGetVadSegmentT0FromStateFunc ffi.Fun

	// WHISPER_API int64_t whisper_full_get_vad_segment_t1(struct whisper_context * ctx, int i);
	fullGetVadSegmentT1Func ffi.Fun

	// WHISPER_API int64_t whisper_full_get_vad_segment_t1_from_state(struct whisper_state * state, int i);
	fullGetVadSegmentT1FromStateFunc ffi.Fun

	// WHISPER_API struct whisper_vad_params whisper_vad_default_params(void);
	vadDefaultParamsFunc ffi.Fun

	// WHISPER_API struct whisper_vad_context_params whisper_vad_default_context_params(void);
	vadDefaultContextParamsFunc ffi.Fun

	// WHISPER_API struct whisper_vad_context * whisper_vad_init_from_file_with_params(
	//             const char * path_model,
	//             struct whisper_vad_context_params params);
	vadInitFromFileWithParamsFunc ffi.Fun

	// WHISPER_API struct whisper_vad_context * whisper_vad_init_with_params(
	//             struct whisper_model_loader * loader,
	//             struct whisper_vad_context_params params);
	vadInitWithParamsFunc ffi.Fun

	// WHISPER_API bool whisper_vad_detect_speech(
	//             struct whisper_vad_context * vctx,
	//                            const float * samples,
	//                                    int   n_samples);
	vadDetectSpeechFunc ffi.Fun

	// WHISPER_API bool whisper_vad_detect_speech_no_reset(
	//             struct whisper_vad_context * vctx,
	//                            const float * samples,
	//                                    int   n_samples);
	vadDetectSpeechNoResetFunc ffi.Fun

	// WHISPER_API void whisper_vad_reset_state(struct whisper_vad_context * vctx);
	vadResetStateFunc ffi.Fun

	// WHISPER_API int     whisper_vad_n_probs(struct whisper_vad_context * vctx);
	vadNProbsFunc ffi.Fun

	// WHISPER_API float * whisper_vad_probs  (struct whisper_vad_context * vctx);
	vadProbsFunc ffi.Fun

	// WHISPER_API struct whisper_vad_segments * whisper_vad_segments_from_probs(
	//             struct whisper_vad_context * vctx,
	//             struct whisper_vad_params    params);
	vadSegmentsFromProbsFunc ffi.Fun

	// WHISPER_API struct whisper_vad_segments * whisper_vad_segments_from_samples(
	//             struct whisper_vad_context * vctx,
	//             struct whisper_vad_params    params,
	//                            const float * samples,
	//                                    int   n_samples);
	vadSegmentsFromSamplesFunc ffi.Fun

	// WHISPER_API int whisper_vad_segments_n_segments(struct whisper_vad_segments * segments);
	vadSegmentsNSegmentsFunc ffi.Fun

	// WHISPER_API float whisper_vad_segments_get_segment_t0(struct whisper_vad_segments * segments, int i_segment);
	vadSegmentsGetSegmentT0Func ffi.Fun

	// WHISPER_API float whisper_vad_segments_get_segment_t1(struct whisper_vad_segments * segments, int i_segment);
	vadSegmentsGetSegmentT1Func ffi.Fun

	// WHISPER_API void whisper_vad_free_segments(struct whisper_vad_segments * segments);
	vadFreeSegmentsFunc ffi.Fun

	// WHISPER_API void whisper_vad_free(struct whisper_vad_context * ctx);
	vadFreeFunc ffi.Fun
)

func loadVadFuncs(lib ffi.Lib) error {
	var err error

	if fullNVadSegmentsFunc, err = lib.Prep("whisper_full_n_vad_segments", &ffi.TypeSint32, &ffi.TypePointer); err != nil {
		return loadError("whisper_full_n_vad_segments", err)
	}

	if fullNVadSegmentsFromStateFunc, err = lib.Prep("whisper_full_n_vad_segments_from_state", &ffi.TypeSint32, &ffi.TypePointer); err != nil {
		return loadError("whisper_full_n_vad_segments_from_state", err)
	}

	if fullGetVadSegmentT0Func, err = lib.Prep("whisper_full_get_vad_segment_t0", &ffi.TypeSint64, &ffi.TypePointer, &ffi.TypeSint32); err != nil {
		return loadError("whisper_full_get_vad_segment_t0", err)
	}

	if fullGetVadSegmentT0FromStateFunc, err = lib.Prep("whisper_full_get_vad_segment_t0_from_state", &ffi.TypeSint64, &ffi.TypePointer, &ffi.TypeSint32); err != nil {
		return loadError("whisper_full_get_vad_segment_t0_from_state", err)
	}

	if fullGetVadSegmentT1Func, err = lib.Prep("whisper_full_get_vad_segment_t1", &ffi.TypeSint64, &ffi.TypePointer, &ffi.TypeSint32); err != nil {
		return loadError("whisper_full_get_vad_segment_t1", err)
	}

	if fullGetVadSegmentT1FromStateFunc, err = lib.Prep("whisper_full_get_vad_segment_t1_from_state", &ffi.TypeSint64, &ffi.TypePointer, &ffi.TypeSint32); err != nil {
		return loadError("whisper_full_get_vad_segment_t1_from_state", err)
	}

	if vadDefaultParamsFunc, err = lib.Prep("whisper_vad_default_params", &ffiTypeVadParams); err != nil {
		return loadError("whisper_vad_default_params", err)
	}

	if vadDefaultContextParamsFunc, err = lib.Prep("whisper_vad_default_context_params", &ffiTypeVadContextParams); err != nil {
		return loadError("whisper_vad_default_context_params", err)
	}

	if vadInitFromFileWithParamsFunc, err = lib.Prep("whisper_vad_init_from_file_with_params",
		&ffi.TypePointer, &ffi.TypePointer, &ffiTypeVadContextParams,
	); err != nil {
		return loadError("whisper_vad_init_from_file_with_params", err)
	}

	if vadInitWithParamsFunc, err = lib.Prep("whisper_vad_init_with_params",
		&ffi.TypePointer, &ffi.TypePointer, &ffiTypeVadContextParams,
	); err != nil {
		return loadError("whisper_vad_init_with_params", err)
	}

	if vadDetectSpeechFunc, err = lib.Prep("whisper_vad_detect_speech",
		&ffi.TypeUint8, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32,
	); err != nil {
		return loadError("whisper_vad_detect_speech", err)
	}

	if vadDetectSpeechNoResetFunc, err = lib.Prep("whisper_vad_detect_speech_no_reset",
		&ffi.TypeUint8, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32,
	); err != nil {
		return loadError("whisper_vad_detect_speech_no_reset", err)
	}

	if vadResetStateFunc, err = lib.Prep("whisper_vad_reset_state", &ffi.TypeVoid, &ffi.TypePointer); err != nil {
		return loadError("whisper_vad_reset_state", err)
	}

	if vadNProbsFunc, err = lib.Prep("whisper_vad_n_probs", &ffi.TypeSint32, &ffi.TypePointer); err != nil {
		return loadError("whisper_vad_n_probs", err)
	}

	if vadProbsFunc, err = lib.Prep("whisper_vad_probs", &ffi.TypePointer, &ffi.TypePointer); err != nil {
		return loadError("whisper_vad_probs", err)
	}

	if vadSegmentsFromProbsFunc, err = lib.Prep("whisper_vad_segments_from_probs",
		&ffi.TypePointer, &ffi.TypePointer, &ffiTypeVadParams,
	); err != nil {
		return loadError("whisper_vad_segments_from_probs", err)
	}

	if vadSegmentsFromSamplesFunc, err = lib.Prep("whisper_vad_segments_from_samples",
		&ffi.TypePointer, &ffi.TypePointer, &ffiTypeVadParams, &ffi.TypePointer, &ffi.TypeSint32,
	); err != nil {
		return loadError("whisper_vad_segments_from_samples", err)
	}

	if vadSegmentsNSegmentsFunc, err = lib.Prep("whisper_vad_segments_n_segments", &ffi.TypeSint32, &ffi.TypePointer); err != nil {
		return loadError("whisper_vad_segments_n_segments", err)
	}

	if vadSegmentsGetSegmentT0Func, err = lib.Prep("whisper_vad_segments_get_segment_t0",
		&ffi.TypeFloat, &ffi.TypePointer, &ffi.TypeSint32,
	); err != nil {
		return loadError("whisper_vad_segments_get_segment_t0", err)
	}

	if vadSegmentsGetSegmentT1Func, err = lib.Prep("whisper_vad_segments_get_segment_t1",
		&ffi.TypeFloat, &ffi.TypePointer, &ffi.TypeSint32,
	); err != nil {
		return loadError("whisper_vad_segments_get_segment_t1", err)
	}

	if vadFreeSegmentsFunc, err = lib.Prep("whisper_vad_free_segments", &ffi.TypeVoid, &ffi.TypePointer); err != nil {
		return loadError("whisper_vad_free_segments", err)
	}

	if vadFreeFunc, err = lib.Prep("whisper_vad_free", &ffi.TypeVoid, &ffi.TypePointer); err != nil {
		return loadError("whisper_vad_free", err)
	}

	return nil
}

// FullNVadSegments returns the number of speech segments detected by the
// internal VAD for the context's default state.
func FullNVadSegments(ctx Context) int32 {
	if ctx == 0 {
		return 0
	}
	var result ffi.Arg
	fullNVadSegmentsFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx))
	return int32(result)
}

// FullNVadSegmentsFromState returns the number of speech segments detected by
// the internal VAD for state.
func FullNVadSegmentsFromState(state State) int32 {
	if state == 0 {
		return 0
	}
	var result ffi.Arg
	fullNVadSegmentsFromStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&state))
	return int32(result)
}

// FullGetVadSegmentT0 returns the VAD segment start time in centiseconds.
func FullGetVadSegmentT0(ctx Context, i int32) int64 {
	if ctx == 0 {
		return 0
	}
	var result int64
	fullGetVadSegmentT0Func.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&i))
	return result
}

// FullGetVadSegmentT0FromState returns the VAD segment start time in
// centiseconds for state.
func FullGetVadSegmentT0FromState(state State, i int32) int64 {
	if state == 0 {
		return 0
	}
	var result int64
	fullGetVadSegmentT0FromStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&state), unsafe.Pointer(&i))
	return result
}

// FullGetVadSegmentT1 returns the VAD segment end time in centiseconds.
func FullGetVadSegmentT1(ctx Context, i int32) int64 {
	if ctx == 0 {
		return 0
	}
	var result int64
	fullGetVadSegmentT1Func.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx), unsafe.Pointer(&i))
	return result
}

// FullGetVadSegmentT1FromState returns the VAD segment end time in
// centiseconds for state.
func FullGetVadSegmentT1FromState(state State, i int32) int64 {
	if state == 0 {
		return 0
	}
	var result int64
	fullGetVadSegmentT1FromStateFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&state), unsafe.Pointer(&i))
	return result
}

// VadDefaultParams returns the default VadParams populated by the C library.
func VadDefaultParams() VadParams {
	var p VadParams
	vadDefaultParamsFunc.Call(unsafe.Pointer(&p))
	return p
}

// VadDefaultContextParams returns the default VadContextParams populated by
// the C library.
func VadDefaultContextParams() VadContextParams {
	var p VadContextParams
	vadDefaultContextParamsFunc.Call(unsafe.Pointer(&p))
	return p
}

// VadInitFromFileWithParams loads a VAD model from a file path. The returned
// VadContext must be released with VadFree when no longer needed.
func VadInitFromFileWithParams(pathModel string, params VadContextParams) (VadContext, error) {
	var vctx VadContext
	if _, err := os.Stat(pathModel); errors.Is(err, os.ErrNotExist) {
		return vctx, err
	}

	cpath, err := utils.BytePtrFromString(pathModel)
	if err != nil {
		return vctx, err
	}

	vadInitFromFileWithParamsFunc.Call(
		unsafe.Pointer(&vctx),
		unsafe.Pointer(&cpath),
		unsafe.Pointer(&params),
	)
	if vctx == 0 {
		return vctx, errors.New("whisper_vad_init_from_file_with_params returned NULL")
	}
	return vctx, nil
}

// VadInitWithParams loads a VAD model through loader. The returned VadContext
// must be released with VadFree when no longer needed.
func VadInitWithParams(loader *ModelLoader, params VadContextParams) (VadContext, error) {
	if loader == nil {
		return 0, errors.New("whisper.VadInitWithParams: nil loader")
	}
	var vctx VadContext
	vadInitWithParamsFunc.Call(
		unsafe.Pointer(&vctx),
		unsafe.Pointer(&loader),
		unsafe.Pointer(&params),
	)
	if vctx == 0 {
		return 0, errors.New("whisper_vad_init_with_params returned NULL")
	}
	return vctx, nil
}

// VadDetectSpeech runs the VAD model over the provided 16 kHz mono float32
// samples. Returns true on success. The probabilities can be retrieved via
// VadNProbs/VadProbs and turned into segments via VadSegmentsFromProbs.
func VadDetectSpeech(vctx VadContext, samples []float32) bool {
	if vctx == 0 || len(samples) == 0 {
		return false
	}
	samplesPtr := unsafe.Pointer(unsafe.SliceData(samples))
	nSamples := int32(len(samples))

	var result ffi.Arg
	vadDetectSpeechFunc.Call(
		unsafe.Pointer(&result),
		unsafe.Pointer(&vctx),
		unsafe.Pointer(&samplesPtr),
		unsafe.Pointer(&nSamples),
	)
	return result.Bool()
}

// VadDetectSpeechNoReset runs the VAD model without resetting its LSTM state.
// Use VadResetState between streaming utterances.
func VadDetectSpeechNoReset(vctx VadContext, samples []float32) bool {
	if vctx == 0 || len(samples) == 0 {
		return false
	}
	samplesPtr := unsafe.Pointer(unsafe.SliceData(samples))
	nSamples := int32(len(samples))

	var result ffi.Arg
	vadDetectSpeechNoResetFunc.Call(
		unsafe.Pointer(&result),
		unsafe.Pointer(&vctx),
		unsafe.Pointer(&samplesPtr),
		unsafe.Pointer(&nSamples),
	)
	return result.Bool()
}

// VadResetState resets the VAD context's LSTM hidden and cell states.
func VadResetState(vctx VadContext) {
	if vctx == 0 {
		return
	}
	vadResetStateFunc.Call(nil, unsafe.Pointer(&vctx))
}

// VadNProbs returns the number of speech probabilities computed by the most
// recent VadDetectSpeech call.
func VadNProbs(vctx VadContext) int32 {
	if vctx == 0 {
		return 0
	}
	var result ffi.Arg
	vadNProbsFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&vctx))
	return int32(result)
}

// VadProbs returns the underlying speech-probability array produced by the
// most recent VadDetectSpeech call. The returned slice aliases C-owned memory
// that lives for the lifetime of vctx; copy it before VadFree.
func VadProbs(vctx VadContext) []float32 {
	if vctx == 0 {
		return nil
	}
	var ptr *float32
	vadProbsFunc.Call(unsafe.Pointer(&ptr), unsafe.Pointer(&vctx))
	if ptr == nil {
		return nil
	}
	n := VadNProbs(vctx)
	if n <= 0 {
		return nil
	}
	return unsafe.Slice(ptr, int(n))
}

// VadSegmentsFromProbs builds speech segments from the probabilities computed
// by the most recent VadDetectSpeech call. Release the result with
// VadFreeSegments.
func VadSegmentsFromProbs(vctx VadContext, params VadParams) (VadSegments, error) {
	if vctx == 0 {
		return 0, errors.New("whisper.VadSegmentsFromProbs: nil context")
	}
	var segs VadSegments
	vadSegmentsFromProbsFunc.Call(
		unsafe.Pointer(&segs),
		unsafe.Pointer(&vctx),
		unsafe.Pointer(&params),
	)
	if segs == 0 {
		return 0, errors.New("whisper_vad_segments_from_probs returned NULL")
	}
	return segs, nil
}

// VadSegmentsFromSamples runs detection and builds segments in one call.
// Release the result with VadFreeSegments.
func VadSegmentsFromSamples(vctx VadContext, params VadParams, samples []float32) (VadSegments, error) {
	if vctx == 0 {
		return 0, errors.New("whisper.VadSegmentsFromSamples: nil context")
	}
	if len(samples) == 0 {
		return 0, errors.New("whisper.VadSegmentsFromSamples: empty samples")
	}
	samplesPtr := unsafe.Pointer(unsafe.SliceData(samples))
	nSamples := int32(len(samples))

	var segs VadSegments
	vadSegmentsFromSamplesFunc.Call(
		unsafe.Pointer(&segs),
		unsafe.Pointer(&vctx),
		unsafe.Pointer(&params),
		unsafe.Pointer(&samplesPtr),
		unsafe.Pointer(&nSamples),
	)
	if segs == 0 {
		return 0, errors.New("whisper_vad_segments_from_samples returned NULL")
	}
	return segs, nil
}

// VadSegmentsNSegments returns the number of speech segments.
func VadSegmentsNSegments(segs VadSegments) int32 {
	if segs == 0 {
		return 0
	}
	var result ffi.Arg
	vadSegmentsNSegmentsFunc.Call(unsafe.Pointer(&result), unsafe.Pointer(&segs))
	return int32(result)
}

// VadSegmentsGetSegmentT0 returns the start time of the iSegment'th VAD
// segment in centisecond (10 ms) units, matching whisper.cpp's segment
// timestamp convention. Divide by 100 for seconds.
func VadSegmentsGetSegmentT0(segs VadSegments, iSegment int32) float32 {
	if segs == 0 {
		return 0
	}
	var result float32
	vadSegmentsGetSegmentT0Func.Call(unsafe.Pointer(&result), unsafe.Pointer(&segs), unsafe.Pointer(&iSegment))
	return result
}

// VadSegmentsGetSegmentT1 returns the end time of the iSegment'th VAD
// segment in centisecond (10 ms) units. Divide by 100 for seconds.
func VadSegmentsGetSegmentT1(segs VadSegments, iSegment int32) float32 {
	if segs == 0 {
		return 0
	}
	var result float32
	vadSegmentsGetSegmentT1Func.Call(unsafe.Pointer(&result), unsafe.Pointer(&segs), unsafe.Pointer(&iSegment))
	return result
}

// VadFreeSegments releases a VadSegments handle.
func VadFreeSegments(segs VadSegments) {
	if segs == 0 {
		return
	}
	vadFreeSegmentsFunc.Call(nil, unsafe.Pointer(&segs))
}

// VadFree releases a VadContext handle.
func VadFree(vctx VadContext) {
	if vctx == 0 {
		return
	}
	vadFreeFunc.Call(nil, unsafe.Pointer(&vctx))
}

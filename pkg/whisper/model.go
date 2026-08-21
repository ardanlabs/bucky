package whisper

import (
	"errors"
	"os"
	"unsafe"

	"github.com/ardanlabs/bucky/pkg/utils"
	"github.com/jupiterrider/ffi"
)

// ModelLoader mirrors struct whisper_model_loader. Callback fields hold C ABI
// function pointers with the read, eof, and close signatures from whisper.h.
type ModelLoader struct {
	Context unsafe.Pointer
	Read    unsafe.Pointer
	EOF     unsafe.Pointer
	Close   unsafe.Pointer
}

var (
	// WHISPER_API struct whisper_context * whisper_init_from_file_with_params(
	//                              const char * path_model,
	//                              struct whisper_context_params params);
	initFromFileWithParamsFunc          ffi.Fun
	initFromBufferWithParamsFunc        ffi.Fun
	initWithParamsFunc                  ffi.Fun
	initFromFileWithParamsNoStateFunc   ffi.Fun
	initFromBufferWithParamsNoStateFunc ffi.Fun
	initWithParamsNoStateFunc           ffi.Fun
	initFromFileFunc                    ffi.Fun
	initFromBufferFunc                  ffi.Fun
	initFunc                            ffi.Fun
	initFromFileNoStateFunc             ffi.Fun
	initFromBufferNoStateFunc           ffi.Fun
	initNoStateFunc                     ffi.Fun

	// WHISPER_API int whisper_model_n_vocab      (struct whisper_context * ctx);
	modelNVocabFunc ffi.Fun
	// WHISPER_API int whisper_model_n_audio_ctx  (struct whisper_context * ctx);
	modelNAudioCtxFunc ffi.Fun
	// WHISPER_API int whisper_model_n_audio_state(struct whisper_context * ctx);
	modelNAudioStateFunc ffi.Fun
	// WHISPER_API int whisper_model_n_audio_head (struct whisper_context * ctx);
	modelNAudioHeadFunc ffi.Fun
	// WHISPER_API int whisper_model_n_audio_layer(struct whisper_context * ctx);
	modelNAudioLayerFunc ffi.Fun
	// WHISPER_API int whisper_model_n_text_ctx   (struct whisper_context * ctx);
	modelNTextCtxFunc ffi.Fun
	// WHISPER_API int whisper_model_n_text_state (struct whisper_context * ctx);
	modelNTextStateFunc ffi.Fun
	// WHISPER_API int whisper_model_n_text_head  (struct whisper_context * ctx);
	modelNTextHeadFunc ffi.Fun
	// WHISPER_API int whisper_model_n_text_layer (struct whisper_context * ctx);
	modelNTextLayerFunc ffi.Fun
	// WHISPER_API int whisper_model_n_mels       (struct whisper_context * ctx);
	modelNMelsFunc ffi.Fun
	// WHISPER_API int whisper_model_ftype        (struct whisper_context * ctx);
	modelFtypeFunc ffi.Fun
	// WHISPER_API int whisper_model_type         (struct whisper_context * ctx);
	modelTypeFunc ffi.Fun
	// WHISPER_API const char * whisper_model_type_readable(struct whisper_context * ctx);
	modelTypeReadableFunc ffi.Fun
)

func loadModelFuncs(lib ffi.Lib) error {
	var err error

	if initFromFileWithParamsFunc, err = lib.Prep("whisper_init_from_file_with_params", &ffi.TypePointer, &ffi.TypePointer, &ffiTypeContextParams); err != nil {
		return loadError("whisper_init_from_file_with_params", err)
	}
	initSpecs := []struct {
		name string
		fn   *ffi.Fun
		args []*ffi.Type
	}{
		{"whisper_init_from_buffer_with_params", &initFromBufferWithParamsFunc, []*ffi.Type{&ffi.TypePointer, &ffi.TypeUint64, &ffiTypeContextParams}},
		{"whisper_init_with_params", &initWithParamsFunc, []*ffi.Type{&ffi.TypePointer, &ffiTypeContextParams}},
		{"whisper_init_from_file_with_params_no_state", &initFromFileWithParamsNoStateFunc, []*ffi.Type{&ffi.TypePointer, &ffiTypeContextParams}},
		{"whisper_init_from_buffer_with_params_no_state", &initFromBufferWithParamsNoStateFunc, []*ffi.Type{&ffi.TypePointer, &ffi.TypeUint64, &ffiTypeContextParams}},
		{"whisper_init_with_params_no_state", &initWithParamsNoStateFunc, []*ffi.Type{&ffi.TypePointer, &ffiTypeContextParams}},
		{"whisper_init_from_file", &initFromFileFunc, []*ffi.Type{&ffi.TypePointer}},
		{"whisper_init_from_buffer", &initFromBufferFunc, []*ffi.Type{&ffi.TypePointer, &ffi.TypeUint64}},
		{"whisper_init", &initFunc, []*ffi.Type{&ffi.TypePointer}},
		{"whisper_init_from_file_no_state", &initFromFileNoStateFunc, []*ffi.Type{&ffi.TypePointer}},
		{"whisper_init_from_buffer_no_state", &initFromBufferNoStateFunc, []*ffi.Type{&ffi.TypePointer, &ffi.TypeUint64}},
		{"whisper_init_no_state", &initNoStateFunc, []*ffi.Type{&ffi.TypePointer}},
	}
	for _, spec := range initSpecs {
		fn, prepErr := lib.Prep(spec.name, &ffi.TypePointer, spec.args...)
		if prepErr != nil {
			return loadError(spec.name, prepErr)
		}
		*spec.fn = fn
	}

	type sym struct {
		name string
		fn   *ffi.Fun
	}
	int32Accessors := []sym{
		{"whisper_model_n_vocab", &modelNVocabFunc},
		{"whisper_model_n_audio_ctx", &modelNAudioCtxFunc},
		{"whisper_model_n_audio_state", &modelNAudioStateFunc},
		{"whisper_model_n_audio_head", &modelNAudioHeadFunc},
		{"whisper_model_n_audio_layer", &modelNAudioLayerFunc},
		{"whisper_model_n_text_ctx", &modelNTextCtxFunc},
		{"whisper_model_n_text_state", &modelNTextStateFunc},
		{"whisper_model_n_text_head", &modelNTextHeadFunc},
		{"whisper_model_n_text_layer", &modelNTextLayerFunc},
		{"whisper_model_n_mels", &modelNMelsFunc},
		{"whisper_model_ftype", &modelFtypeFunc},
		{"whisper_model_type", &modelTypeFunc},
	}
	for _, s := range int32Accessors {
		fn, err := lib.Prep(s.name, &ffi.TypeSint32, &ffi.TypePointer)
		if err != nil {
			return loadError(s.name, err)
		}
		*s.fn = fn
	}

	if modelTypeReadableFunc, err = lib.Prep("whisper_model_type_readable", &ffi.TypePointer, &ffi.TypePointer); err != nil {
		return loadError("whisper_model_type_readable", err)
	}

	return nil
}

// InitFromFileWithParams loads a whisper model from a file. Returns a Context
// handle that must be released with Free when no longer needed.
func InitFromFileWithParams(pathModel string, params ContextParams) (Context, error) {
	var ctx Context
	if _, err := os.Stat(pathModel); errors.Is(err, os.ErrNotExist) {
		return ctx, err
	}

	cpath, err := utils.BytePtrFromString(pathModel)
	if err != nil {
		return ctx, err
	}

	initFromFileWithParamsFunc.Call(
		unsafe.Pointer(&ctx),
		unsafe.Pointer(&cpath),
		unsafe.Pointer(&params),
	)
	if ctx == 0 {
		return ctx, errors.New("whisper_init_from_file_with_params returned NULL")
	}
	return ctx, nil
}

func contextInitResult(ctx Context, name string) (Context, error) {
	if ctx == 0 {
		return 0, errors.New(name + " returned NULL")
	}
	return ctx, nil
}

func validateModelPath(pathModel string) error {
	if _, err := os.Stat(pathModel); errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// InitFromBufferWithParams loads a model from a memory buffer.
func InitFromBufferWithParams(buffer unsafe.Pointer, bufferSize uint64, params ContextParams) (Context, error) {
	var ctx Context
	initFromBufferWithParamsFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&buffer), unsafe.Pointer(&bufferSize), unsafe.Pointer(&params))
	return contextInitResult(ctx, "whisper_init_from_buffer_with_params")
}

// InitWithParams loads a model through loader callbacks.
func InitWithParams(loader *ModelLoader, params ContextParams) (Context, error) {
	var ctx Context
	initWithParamsFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&loader), unsafe.Pointer(&params))
	return contextInitResult(ctx, "whisper_init_with_params")
}

// InitFromFileWithParamsNoState loads a model without allocating its default state.
func InitFromFileWithParamsNoState(path string, params ContextParams) (Context, error) {
	if err := validateModelPath(path); err != nil {
		return 0, err
	}
	cpath, err := utils.BytePtrFromString(path)
	if err != nil {
		return 0, err
	}
	var ctx Context
	initFromFileWithParamsNoStateFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&cpath), unsafe.Pointer(&params))
	return contextInitResult(ctx, "whisper_init_from_file_with_params_no_state")
}

// InitFromBufferWithParamsNoState loads a buffer model without allocating its default state.
func InitFromBufferWithParamsNoState(buffer unsafe.Pointer, size uint64, params ContextParams) (Context, error) {
	var ctx Context
	initFromBufferWithParamsNoStateFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&buffer), unsafe.Pointer(&size), unsafe.Pointer(&params))
	return contextInitResult(ctx, "whisper_init_from_buffer_with_params_no_state")
}

// InitWithParamsNoState loads through callbacks without allocating a default state.
func InitWithParamsNoState(loader *ModelLoader, params ContextParams) (Context, error) {
	var ctx Context
	initWithParamsNoStateFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&loader), unsafe.Pointer(&params))
	return contextInitResult(ctx, "whisper_init_with_params_no_state")
}

// InitFromFile loads a model using default parameters.
//
// Deprecated: use InitFromFileWithParams instead.
func InitFromFile(path string) (Context, error) {
	if err := validateModelPath(path); err != nil {
		return 0, err
	}
	cpath, err := utils.BytePtrFromString(path)
	if err != nil {
		return 0, err
	}
	var ctx Context
	initFromFileFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&cpath))
	return contextInitResult(ctx, "whisper_init_from_file")
}

// InitFromBuffer loads a buffer model using default parameters.
//
// Deprecated: use InitFromBufferWithParams instead.
func InitFromBuffer(buffer unsafe.Pointer, size uint64) (Context, error) {
	var ctx Context
	initFromBufferFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&buffer), unsafe.Pointer(&size))
	return contextInitResult(ctx, "whisper_init_from_buffer")
}

// InitWithLoader loads a model through callbacks using default parameters.
//
// Deprecated: use InitWithParams instead.
//
// It is named InitWithLoader because Init is the package's existing backend
// initialization API.
func InitWithLoader(loader *ModelLoader) (Context, error) {
	var ctx Context
	initFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&loader))
	return contextInitResult(ctx, "whisper_init")
}

// InitFromFileNoState loads a model without a default state.
//
// Deprecated: use InitFromFileWithParamsNoState instead.
func InitFromFileNoState(path string) (Context, error) {
	if err := validateModelPath(path); err != nil {
		return 0, err
	}
	cpath, err := utils.BytePtrFromString(path)
	if err != nil {
		return 0, err
	}
	var ctx Context
	initFromFileNoStateFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&cpath))
	return contextInitResult(ctx, "whisper_init_from_file_no_state")
}

// InitFromBufferNoState loads a buffer model without a default state.
//
// Deprecated: use InitFromBufferWithParamsNoState instead.
func InitFromBufferNoState(buffer unsafe.Pointer, size uint64) (Context, error) {
	var ctx Context
	initFromBufferNoStateFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&buffer), unsafe.Pointer(&size))
	return contextInitResult(ctx, "whisper_init_from_buffer_no_state")
}

// InitNoState loads through callbacks without a default state.
//
// Deprecated: use InitWithParamsNoState instead.
func InitNoState(loader *ModelLoader) (Context, error) {
	var ctx Context
	initNoStateFunc.Call(unsafe.Pointer(&ctx), unsafe.Pointer(&loader))
	return contextInitResult(ctx, "whisper_init_no_state")
}

func callInt32Accessor(fn ffi.Fun, ctx Context) int32 {
	if ctx == 0 {
		return 0
	}
	var result ffi.Arg
	fn.Call(unsafe.Pointer(&result), unsafe.Pointer(&ctx))
	return int32(result)
}

// ModelNVocab returns the model vocabulary size.
func ModelNVocab(ctx Context) int32 { return callInt32Accessor(modelNVocabFunc, ctx) }

// ModelNAudioCtx returns the model audio context length.
func ModelNAudioCtx(ctx Context) int32 { return callInt32Accessor(modelNAudioCtxFunc, ctx) }

// ModelNAudioState returns the model audio state size.
func ModelNAudioState(ctx Context) int32 { return callInt32Accessor(modelNAudioStateFunc, ctx) }

// ModelNAudioHead returns the model audio head count.
func ModelNAudioHead(ctx Context) int32 { return callInt32Accessor(modelNAudioHeadFunc, ctx) }

// ModelNAudioLayer returns the model audio layer count.
func ModelNAudioLayer(ctx Context) int32 { return callInt32Accessor(modelNAudioLayerFunc, ctx) }

// ModelNTextCtx returns the model text context length.
func ModelNTextCtx(ctx Context) int32 { return callInt32Accessor(modelNTextCtxFunc, ctx) }

// ModelNTextState returns the model text state size.
func ModelNTextState(ctx Context) int32 { return callInt32Accessor(modelNTextStateFunc, ctx) }

// ModelNTextHead returns the model text head count.
func ModelNTextHead(ctx Context) int32 { return callInt32Accessor(modelNTextHeadFunc, ctx) }

// ModelNTextLayer returns the model text layer count.
func ModelNTextLayer(ctx Context) int32 { return callInt32Accessor(modelNTextLayerFunc, ctx) }

// ModelNMels returns the number of mel bands the model expects.
func ModelNMels(ctx Context) int32 { return callInt32Accessor(modelNMelsFunc, ctx) }

// ModelFtype returns the model file type (quantization indicator).
func ModelFtype(ctx Context) int32 { return callInt32Accessor(modelFtypeFunc, ctx) }

// ModelType returns the model size enum.
func ModelType(ctx Context) int32 { return callInt32Accessor(modelTypeFunc, ctx) }

// ModelTypeReadable returns a human-readable model type string (e.g. "tiny").
func ModelTypeReadable(ctx Context) string {
	if ctx == 0 {
		return ""
	}
	var ptr *byte
	modelTypeReadableFunc.Call(unsafe.Pointer(&ptr), unsafe.Pointer(&ctx))
	if ptr == nil {
		return ""
	}
	return utils.BytePtrToString(ptr)
}

package callbackfixture

import purego "github.com/ardanlabs/bucky/cmd/ffi-checker/testdata/purego"

type Params struct {
	GoodCallback uintptr
	BadCallback  uintptr
}

func WhisperGoodCallback() uintptr {
	return purego.NewCallback(func(value int32, data uintptr) {})
}

func WhisperBadCallback() uintptr {
	return purego.NewCallback(func(value int64) bool { return true })
}

func InstallCallbacks(params *Params) {
	params.GoodCallback = WhisperBadCallback()
	params.BadCallback = WhisperBadCallback()
}

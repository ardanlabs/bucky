package gocheckfixture

import (
	"unsafe"

	"github.com/ardanlabs/bucky/cmd/ffi-checker/testdata/utils"
)

type fakeFun struct{}

func (fakeFun) Call(...unsafe.Pointer) {}

var stringBadFunc fakeFun
var stringGoodFunc fakeFun
var deprecatedBadFunc fakeFun
var deprecatedGoodFunc fakeFun
var signedPointerFunc fakeFun

func StringBad(value string) {
	buffer := []byte(value)
	pointer := &buffer[0]
	stringBadFunc.Call(nil, unsafe.Pointer(&pointer))
}

func StringGood(value string) {
	pointer, _ := utils.BytePtrFromString(value)
	stringGoodFunc.Call(nil, unsafe.Pointer(&pointer))
}

func DeprecatedBad() {
	deprecatedBadFunc.Call(nil)
}

// DeprecatedGood is the documented control.
//
// Deprecated: use StringGood instead.
func DeprecatedGood() {
	deprecatedGoodFunc.Call(nil)
}

func SignedPointer(value uint32) {
	pointer := &value
	signedPointerFunc.Call(nil, unsafe.Pointer(&pointer))
}

package enumfixture

import "unsafe"

type Expected int32

const ExpectedA Expected = 7

type Other int32

const OtherA Other = 0

const Magic = 4

type fakeFun struct{}

func (fakeFun) Call(...unsafe.Pointer) {}

var semanticFunc fakeFun
var semanticOKFunc fakeFun

func callSemantic(value Other) {
	semanticFunc.Call(nil, unsafe.Pointer(&value))
}

func callSemanticOK(value Expected) {
	semanticOKFunc.Call(nil, unsafe.Pointer(&value))
}

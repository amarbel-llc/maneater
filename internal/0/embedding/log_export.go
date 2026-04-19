package embedding

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

//export maneaterLlamaLogCallback
func maneaterLlamaLogCallback(level C.int, text *C.char, _ unsafe.Pointer) {
	if text == nil {
		return
	}
	writeLlamaLog(C.GoString(text))
}

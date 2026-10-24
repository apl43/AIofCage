//go:build darwin && cgo

package desktopclipboard

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void *aiofcage_clipboard_image(int limit, int *length);
*/
import "C"

import "errors"

// Image 由用户的粘贴动作触发；没有图像时返回 nil。
func Image(limit int) ([]byte, error) {
	var length C.int
	data := C.aiofcage_clipboard_image(C.int(limit), &length)
	if data != nil {
		defer C.free(data)
	}
	if length < 0 {
		return nil, errors.New("剪贴板图片过大，请缩小至 10 MB 以内后重试")
	}
	if data == nil || length == 0 {
		return nil, nil
	}
	return C.GoBytes(data, length), nil
}

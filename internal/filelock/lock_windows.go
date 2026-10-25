package filelock

import (
	"context"
	"errors"
)

func Acquire(context.Context, string, bool) (func(), error) {
	return nil, errors.New("文件锁仅支持本产品的 macOS/Unix 运行环境")
}

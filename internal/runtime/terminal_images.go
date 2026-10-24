package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

type TerminalAttachment struct {
	Data      []byte `json:"data"`
	Extension string `json:"extension"`
}

var attachmentBatchPattern = regexp.MustCompile(`^[A-Za-z0-9]{20,40}$`)

// WriteTerminalImages 只将本次附件写入沙箱 home；图片经标准输入传递，不进入命令参数或日志。
func (m *Manager) WriteTerminalImages(ctx context.Context, id, batchID string, files []TerminalAttachment) ([]string, error) {
	if !attachmentBatchPattern.MatchString(batchID) || len(files) == 0 || len(files) > 4 {
		return nil, errors.New("图片附件参数无效")
	}
	paths := make([]string, len(files))
	for i, file := range files {
		switch file.Extension {
		case "png", "jpg", "gif", "webp":
		default:
			return nil, errors.New("图片附件格式无效")
		}
		paths[i] = fmt.Sprintf("/home/agent/.aiofcage/attachments/%s/%d.%s", batchID, i+1, file.Extension)
	}
	data, err := json.Marshal(files)
	if err != nil {
		return nil, err
	}
	script, err := assets.ReadFile("assets/agent/terminal-images.py")
	if err != nil {
		return nil, err
	}
	if err := m.Exec(ctx, id, false, bytes.NewReader(data), io.Discard, io.Discard, "python3", "-c", string(script), "write", batchID); err != nil {
		// 也处理上传中断后只写入一部分的情况。
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = m.RemoveTerminalImages(cleanup, id, batchID)
		return nil, errors.New("图片未能保存到沙箱，请确认沙箱正在运行后重试")
	}
	return paths, nil
}

func (m *Manager) RemoveTerminalImages(ctx context.Context, id, batchID string) error {
	if !attachmentBatchPattern.MatchString(batchID) {
		return errors.New("图片附件参数无效")
	}
	script, err := assets.ReadFile("assets/agent/terminal-images.py")
	if err != nil {
		return err
	}
	return m.Exec(ctx, id, false, nil, io.Discard, io.Discard, "python3", "-c", string(script), "remove", batchID)
}

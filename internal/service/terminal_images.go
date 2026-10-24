package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"
	"unicode"

	_ "golang.org/x/image/webp"

	"github.com/shaoxier/AIofCage/internal/desktopclipboard"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/session"
)

const (
	maxTerminalImages      = 4
	maxTerminalImageBytes  = 10 << 20
	maxTerminalTotalBytes  = 20 << 20
	maxTerminalImagePixels = 20_000_000
	maxTerminalTextBytes   = 64 << 10
)

type TerminalImage struct {
	Data string `json:"data"`
}

type ClipboardImage struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"`
}

// ReadClipboardImage 只在用户粘贴时读取图像，不读取剪贴板中的文字。
func (a *App) ReadClipboardImage() (*ClipboardImage, error) {
	data, err := desktopclipboard.Image(maxTerminalImageBytes)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	ext, err := validateTerminalImage(data)
	if err != nil {
		return nil, err
	}
	mime := "image/" + ext
	if ext == "jpg" {
		mime = "image/jpeg"
	}
	return &ClipboardImage{Name: "粘贴的图片." + ext, Type: mime, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func validateTerminalImage(data []byte) (string, error) {
	if len(data) == 0 || len(data) > maxTerminalImageBytes {
		return "", errors.New("每张图片需小于或等于 10 MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("图片无法读取，请使用完整的 PNG、JPEG、GIF 或 WebP 文件")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > maxTerminalImagePixels {
		return "", errors.New("图片过大，请缩小至 2000 万像素以内，且单边不超过 8192 像素")
	}
	ext := map[string]string{"png": "png", "jpeg": "jpg", "gif": "gif", "webp": "webp"}[format]
	if ext == "" {
		return "", errors.New("请选择 PNG、JPEG、GIF 或 WebP 图片")
	}
	// 解码整个首帧，避免仅凭文件名或有效文件头接受损坏的图片。
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", errors.New("图片已损坏，请重新复制或选择图片")
	}
	return ext, nil
}

func decodeTerminalImages(images []TerminalImage) ([]runtime.TerminalAttachment, error) {
	if len(images) > maxTerminalImages {
		return nil, errors.New("每次最多发送 4 张图片")
	}
	files := make([]runtime.TerminalAttachment, 0, len(images))
	total := 0
	for i, img := range images {
		if len(img.Data) > base64.StdEncoding.EncodedLen(maxTerminalImageBytes) {
			return nil, fmt.Errorf("第 %d 张图片超过 10 MB", i+1)
		}
		data, err := base64.StdEncoding.Strict().DecodeString(img.Data)
		if err != nil {
			return nil, fmt.Errorf("第 %d 张图片的数据无效，请重新添加", i+1)
		}
		total += len(data)
		if total > maxTerminalTotalBytes {
			return nil, errors.New("图片总大小不能超过 20 MB")
		}
		ext, err := validateTerminalImage(data)
		if err != nil {
			return nil, fmt.Errorf("第 %d 张图片：%w", i+1, err)
		}
		files = append(files, runtime.TerminalAttachment{Data: data, Extension: ext})
	}
	return files, nil
}

func terminalMessage(agent session.Agent, text string, paths []string) (string, error) {
	if len(text) > maxTerminalTextBytes {
		return "", errors.New("输入内容过长，请分次发送")
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", errors.New("输入中含有终端控制字符，请移除后重试")
		}
	}
	if strings.TrimSpace(text) == "" && len(paths) == 0 {
		return "", errors.New("请输入内容或添加图片")
	}
	if agent == "" {
		if len(paths) > 0 {
			return "", errors.New("请切换到智能体终端后发送图片")
		}
		return text + "\r", nil
	}
	paste := func(value string) string { return "\x1b[200~" + value + "\x1b[201~" }
	if agent == session.Codex {
		var input strings.Builder
		for _, path := range paths {
			// Codex 将独立粘贴的本地图片路径转换为原生 Image 附件。
			input.WriteString(paste(path))
		}
		if strings.TrimSpace(text) == "" {
			text = "请查看这些图片。"
		}
		input.WriteString(paste(text))
		input.WriteByte('\r')
		return input.String(), nil
	}
	if len(paths) > 0 {
		if strings.TrimSpace(text) == "" {
			text = "请查看这些图片。"
		}
		text += "\n\n请读取以下图片附件后回答"
		if agent == session.DSH {
			text += "（使用 read_image；如果当前模型不支持图片，请直接说明）"
		}
		text += "：\n" + strings.Join(paths, "\n")
	}
	return paste(text) + "\r", nil
}

// SendTerminalMessage 在同一个写锁下保存附件并提交任务，避免与按键交错。
func (a *App) SendTerminalMessage(id, text string, images []TerminalImage) error {
	t, err := a.lookupTerminal(id)
	if err != nil {
		return err
	}
	if len(images) > 0 && t.agent == "" {
		return errors.New("请切换到智能体终端后发送图片")
	}
	var pending []string
	if len(images) > 0 {
		pending = []string{"/pending.png"}
	}
	if _, err := terminalMessage(t.agent, text, pending); err != nil {
		return err
	}
	files, err := decodeTerminalImages(images)
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.closed.Load() {
		return errors.New("终端已关闭")
	}
	var paths []string
	batchID := rand.Text()
	if len(files) > 0 {
		if err := a.runtimeReady(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		paths, err = a.manager.WriteTerminalImages(ctx, t.sandboxID, batchID, files)
		if err != nil {
			return err
		}
	}
	input, err := terminalMessage(t.agent, text, paths)
	if err == nil && !t.closed.Load() {
		if t.agent == session.ClaudeCode {
			// Claude Code 异步处理粘贴和图片路径；同一数据块内的回车会被当作粘贴的一部分。
			_, err = t.pty.WriteString(strings.TrimSuffix(input, "\r"))
			if err == nil {
				time.Sleep(350 * time.Millisecond)
				if t.closed.Load() {
					err = errors.New("终端已关闭")
				} else {
					_, err = t.pty.WriteString("\r")
				}
			}
		} else {
			_, err = t.pty.WriteString(input)
		}
	} else if err == nil {
		err = errors.New("终端已关闭")
	}
	if err != nil && len(paths) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.manager.RemoveTerminalImages(ctx, t.sandboxID, batchID)
	}
	return err
}

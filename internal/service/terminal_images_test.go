package service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/session"
)

func terminalTestPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	img.Set(4, 6, color.NRGBA{R: 200, A: 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestTerminalImageValidation(t *testing.T) {
	valid := terminalTestPNG(t)
	encoded := func(data []byte) TerminalImage { return TerminalImage{Data: base64.StdEncoding.EncodeToString(data)} }
	files, err := decodeTerminalImages([]TerminalImage{encoded(valid)})
	if err != nil || len(files) != 1 || files[0].Extension != "png" || !bytes.Equal(files[0].Data, valid) {
		t.Fatalf("合法图片没有原样保留：%v", err)
	}
	cases := map[string][]TerminalImage{
		"损坏文件": {encoded(valid[:len(valid)/2])},
		"伪装图片": {encoded([]byte("<svg><script>alert(1)</script></svg>"))},
		"无效编码": {{Data: "not-base64"}},
		"空图片":  {{Data: ""}},
		"超出数量": {encoded(valid), encoded(valid), encoded(valid), encoded(valid), encoded(valid)},
		"超出体积": {{Data: strings.Repeat("A", base64.StdEncoding.EncodedLen(maxTerminalImageBytes)+1)}},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeTerminalImages(input); err == nil {
				t.Fatal("应拒绝无效图片")
			}
		})
	}
	padded := encoded(append(append([]byte{}, valid...), make([]byte, (7<<20)-len(valid))...))
	if _, err := decodeTerminalImages([]TerminalImage{padded, padded, padded}); err == nil || !strings.Contains(err.Error(), "20 MB") {
		t.Fatalf("后端没有限制图片总大小：%v", err)
	}
	largeHeader := bytes.Clone(valid)
	binary.BigEndian.PutUint32(largeHeader[16:20], 5000)
	binary.BigEndian.PutUint32(largeHeader[20:24], 5000)
	binary.BigEndian.PutUint32(largeHeader[29:33], crc32.ChecksumIEEE(largeHeader[12:29]))
	if _, err := validateTerminalImage(largeHeader); err == nil || !strings.Contains(err.Error(), "2000 万") {
		t.Fatalf("解码前没有限制总像素：%v", err)
	}
	var large bytes.Buffer
	if err := png.Encode(&large, image.NewGray(image.Rect(0, 0, 8193, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := validateTerminalImage(large.Bytes()); err == nil {
		t.Fatal("应拒绝尺寸过大的图片")
	}
	for _, format := range []string{"jpeg", "gif"} {
		var output bytes.Buffer
		img := image.NewGray(image.Rect(0, 0, 12, 12))
		if format == "jpeg" {
			err = jpeg.Encode(&output, img, nil)
		} else {
			err = gif.Encode(&output, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := validateTerminalImage(output.Bytes()); err != nil {
			t.Fatalf("%s 校验失败：%v", format, err)
		}
	}
}

func TestTerminalMessageImageProtocols(t *testing.T) {
	path := "/home/agent/.aiofcage/attachments/ABCDEFGH12345678ABCDEFGH12/1.png"
	message, err := terminalMessage(session.Codex, "第一行\r\n第二行", []string{path})
	if err != nil || !strings.HasPrefix(message, "\x1b[200~"+path+"\x1b[201~") || !strings.HasSuffix(message, "\x1b[200~第一行\n第二行\x1b[201~\r") {
		t.Fatalf("Codex 没有独立粘贴图片与正文：%q %v", message, err)
	}
	for _, agent := range []session.Agent{session.ClaudeCode, session.DSH} {
		message, err := terminalMessage(agent, "", []string{path})
		if err != nil || !strings.Contains(message, path) || strings.Count(message, "\r") != 1 {
			t.Fatalf("%s 附件不在同一条消息：%q %v", agent, message, err)
		}
		if agent == session.DSH && !strings.Contains(message, "read_image") {
			t.Fatal("DSH 缺少图片工具提示")
		}
	}
	for _, agent := range []session.Agent{"", session.Codex, session.ClaudeCode, session.DSH} {
		if _, err := terminalMessage(agent, "结束粘贴\x1b[201~\r破坏消息", nil); err == nil {
			t.Fatalf("%s 接受了控制序列", agent)
		}
	}
	if _, err := terminalMessage("", "查看图片", []string{path}); err == nil {
		t.Fatal("Shell 不得接收图片消息")
	}
}

func TestSendTerminalMessagePreservesTextAndRejectsClosed(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	term := &terminal{pty: writer, agent: session.DSH}
	app := &App{terms: terminals{items: map[string]*terminal{"t-test": term}}}
	if err := app.SendTerminalMessage("t-test", "第一行\n第二行", nil); err != nil {
		t.Fatal(err)
	}
	output := make([]byte, 512)
	n, err := reader.Read(output)
	if err != nil || string(output[:n]) != "\x1b[200~第一行\n第二行\x1b[201~\r" {
		t.Fatalf("多行消息被拆分：%q %v", output[:n], err)
	}
	term.closed.Store(true)
	if err := app.SendTerminalMessage("t-test", "不得送达", nil); err == nil {
		t.Fatal("已关闭的终端仍接受消息")
	}
}

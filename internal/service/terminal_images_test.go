package service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
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

func TestConversationAttachments(t *testing.T) {
	png := terminalTestPNG(t)
	pdf := []byte("%PDF-1.7\n机密内容\n")
	svg := []byte("<svg><script>alert(1)</script></svg>")
	encoded := func(data []byte, name string) TerminalImage {
		return TerminalImage{Data: base64.StdEncoding.EncodeToString(data), Name: name}
	}
	attachments, err := decodeConversationAttachments([]TerminalImage{
		encoded(png, "截图.png"),
		encoded(pdf, "../../Users/me/季度 报告.pdf"),
		// 伪装成图片的内容只作为普通文件复制，不交给图片输入。
		encoded(svg, "logo.png"),
		{Data: base64.StdEncoding.EncodeToString([]byte("旧版前端不带名称"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []runtime.TerminalAttachment{{Data: png, Extension: "png"}, {Data: pdf, Name: "季度 报告.pdf"},
		{Data: svg, Name: "logo.png"}, {Data: []byte("旧版前端不带名称"), Name: "附件"}}
	for i := range want {
		if attachments[i].Extension != want[i].Extension || attachments[i].Name != want[i].Name || !bytes.Equal(attachments[i].Data, want[i].Data) {
			t.Fatalf("第 %d 个附件：%q %q", i+1, attachments[i].Extension, attachments[i].Name)
		}
	}
	if names := attachmentNames(attachments); strings.Join(names, "|") != "季度 报告.pdf|logo.png|附件" {
		t.Fatalf("显示名：%q", names)
	}
	if names, _ := json.Marshal(attachmentNames(attachments[:1])); string(names) != "[]" {
		t.Fatalf("只有图片时文件列表应为空数组：%s", names)
	}
	seven := encoded(make([]byte, 7<<20), "a.bin")
	for name, tc := range map[string]struct {
		input []TerminalImage
		error string
	}{
		"超出数量": {slices.Repeat([]TerminalImage{encoded(pdf, "a.pdf")}, 5), "最多发送 4 个附件"},
		"空文件":  {[]TerminalImage{encoded(pdf, "a.pdf"), {Data: "", Name: "empty.txt"}}, "第 2 个附件是空文件"},
		"无效编码": {[]TerminalImage{{Data: "not-base64", Name: "a.txt"}}, "数据无效"},
		"单个过大": {[]TerminalImage{encoded(make([]byte, maxTerminalImageBytes+1), "big.bin")}, "超过 10 MB"},
		"编码过长": {[]TerminalImage{{Data: strings.Repeat("A", base64.StdEncoding.EncodedLen(maxTerminalImageBytes)+4)}}, "超过 10 MB"},
		"总量过大": {[]TerminalImage{seven, seven, seven}, "附件总大小不能超过 20 MB"},
	} {
		if _, err := decodeConversationAttachments(tc.input); err == nil || !strings.Contains(err.Error(), tc.error) {
			t.Errorf("%s：%v", name, err)
		}
	}
}

func TestConversationPrompt(t *testing.T) {
	image, file := runtime.TerminalAttachment{Extension: "png"}, runtime.TerminalAttachment{Name: "report.pdf"}
	dir := "/home/agent/.aiofcage/attachments/ABCDEFGHIJKLMNOPQRSTUVWXYZ/"
	list := "\n\n附件（已复制到沙箱，可直接读取）：\n- "
	for name, tc := range map[string]struct {
		text          string
		attachments   []runtime.TerminalAttachment
		paths         []string
		shown, prompt string
		images        []string
	}{
		"只有文字": {"你好", nil, nil, "你好", "你好", nil},
		"只有图片": {" ", []runtime.TerminalAttachment{image}, []string{dir + "1.png"}, "请查看这些图片。", "请查看这些图片。", []string{dir + "1.png"}},
		"只有文件": {"", []runtime.TerminalAttachment{file}, []string{dir + "1-report.pdf"}, "请查看这些文件。", "请查看这些文件。" + list + dir + "1-report.pdf", nil},
		"图片和文件": {"", []runtime.TerminalAttachment{file, image, file}, []string{dir + "1-report.pdf", dir + "2.png", dir + "3-report.pdf"},
			"请查看这些图片和文件。", "请查看这些图片和文件。" + list + dir + "1-report.pdf\n- " + dir + "3-report.pdf", []string{dir + "2.png"}},
		"文字后附文件列表": {"总结一下\n重点", []runtime.TerminalAttachment{file}, []string{dir + "1-report.pdf"}, "总结一下\n重点", "总结一下\n重点" + list + dir + "1-report.pdf", nil},
	} {
		shown, prompt, images := conversationPrompt(tc.text, tc.attachments, tc.paths)
		if shown != tc.shown || prompt != tc.prompt || !slices.Equal(images, tc.images) {
			t.Errorf("%s：%q %q %q", name, shown, prompt, images)
		}
	}
}

// 会话只对图片要求模型的图片能力；文件批次同样计入会话上限；上传失败时不发事件、不占用会话。
func TestSendConversationAttachmentChecks(t *testing.T) {
	app, _, st := newApp(t)
	app.manager = &runtime.Manager{} // 没有 Docker：写入沙箱一定失败
	var events []any
	app.emit = func(_ string, data any) { events = append(events, data) }
	p := provider.Provider{ID: "p1", Name: "测试供应商", Agent: session.DSH, Model: "deepseek-flash"}
	sbx := sandbox.Sandbox{ID: "sbx-files", Name: "files", Bindings: []sandbox.Binding{{Agent: session.DSH, ProviderID: p.ID}}}
	if err := st.SaveSandbox(sbx); err != nil {
		t.Fatal(err)
	}
	c := &conversation{id: "t-files", sandbox: sbx, agent: session.DSH, provider: p}
	app.conversations.items = map[string]*conversation{c.id: c}
	options := ConversationOptions{Model: p.Model}
	pdf := TerminalImage{Data: base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")), Name: "a.pdf"}
	png := TerminalImage{Data: base64.StdEncoding.EncodeToString(terminalTestPNG(t)), Name: "a.png"}
	if err := app.SendConversationMessage(c.id, "", nil, options); err == nil || !strings.Contains(err.Error(), "请输入内容或添加附件") {
		t.Fatalf("空消息：%v", err)
	}
	if err := app.SendConversationMessage(c.id, "看图", []TerminalImage{png}, options); err == nil || !strings.Contains(err.Error(), "图片能力") {
		t.Fatalf("图片未检查供应商能力：%v", err)
	}
	if err := app.SendConversationMessage(c.id, "", []TerminalImage{pdf}, options); err == nil || !strings.Contains(err.Error(), "附件未能保存到沙箱") {
		t.Fatalf("文件不需要图片能力，应走到写入沙箱：%v", err)
	}
	if c.busy || len(c.batches) != 0 || len(events) != 0 {
		t.Fatalf("失败的发送留下了状态：busy=%v batches=%d events=%d", c.busy, len(c.batches), len(events))
	}
	c.batches = make([]string, maxConversationBatches)
	if err := app.SendConversationMessage(c.id, "", []TerminalImage{pdf}, options); err == nil || !strings.Contains(err.Error(), "附件已达上限") {
		t.Fatalf("文件批次没有计入会话上限：%v", err)
	}
}

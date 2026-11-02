package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestAttachmentName(t *testing.T) {
	for raw, want := range map[string]string{
		"report.pdf":                  "report.pdf",
		"季度 报告（终稿）.docx":              "季度 报告（终稿）.docx",
		"../../etc/passwd":            "passwd",
		`C:\Users\me\Desktop\a.txt`:   "a.txt",
		"dir/":                        defaultAttachmentName,
		"/":                           defaultAttachmentName,
		"":                            defaultAttachmentName,
		"   ":                         defaultAttachmentName,
		".":                           defaultAttachmentName,
		"..":                          defaultAttachmentName,
		"notes/..":                    defaultAttachmentName,
		" . . ":                       defaultAttachmentName,
		".env":                        ".env",
		"-rf":                         "-rf",
		"a\x00b.txt":                  "a_b.txt",
		"line\nbreak\r.md":            "line_break_.md",
		"\tpadded.txt \u3000":         "_padded.txt",
		"nbsp\u00a0name.txt":          "nbsp name.txt",
		"$(rm -rf ~).sh":              "_(rm -rf ~).sh",
		"`id`.txt":                    "_id_.txt",
		"a;b|c&d.txt":                 "a_b_c_d.txt",
		"cafe\u0301 \u30cf\u3099.txt": "caf\u00e9 \u30d0.txt",
		`q"u'o.txt`:                   "q_u_o.txt",
		"x>y<z:w.txt":                 "x_y_z_w.txt",
		"w*h?y!.txt":                  "w_h_y_.txt",
		"invoice\u202efdp.exe":        "invoice_fdp.exe",
		"zero\u200bwidth.txt":         "zero_width.txt",
		"bad\xffutf8.txt":             "bad_utf8.txt",
	} {
		if got := AttachmentName(raw); got != want {
			t.Errorf("AttachmentName(%q) = %q, want %q", raw, got, want)
		}
	}
	long := AttachmentName(strings.Repeat("长", 100) + ".docx")
	if len(long) > maxAttachmentNameBytes || !utf8.ValidString(long) || !strings.HasPrefix(long, "长长") || !strings.HasSuffix(long, "长.docx") {
		t.Errorf("long name kept %d bytes: %q", len(long), long)
	}
	if got := AttachmentName(strings.Repeat("a", 200) + "." + strings.Repeat("b", 30)); got != strings.Repeat("a", maxAttachmentNameBytes) {
		t.Errorf("an overlong extension is not an extension: %q", got)
	}
}

// FuzzAttachmentName checks that every name is safe as one path component and as display text.
func FuzzAttachmentName(f *testing.F) {
	for _, seed := range []string{"", ".", "..", "a/../..", "x\x00y", "\u202e", strings.Repeat("報告 ", 80) + ".pdf", "$(id)`x`", "\xff\xfe"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		name := AttachmentName(raw)
		if name == "" || name == "." || name == ".." || len(name) > maxAttachmentNameBytes || !utf8.ValidString(name) ||
			strings.TrimSpace(name) != name || strings.ContainsAny(name, attachmentNameReplaced) || AttachmentName(name) != name {
			t.Fatalf("unsafe name %q from %q", name, raw)
		}
		for _, r := range name {
			if unicode.IsControl(r) || (!unicode.IsGraphic(r) && r != ' ') {
				t.Fatalf("name %q keeps %U", name, r)
			}
		}
	})
}

func TestAttachmentRecords(t *testing.T) {
	batch := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	records, paths, err := attachmentRecords(batch, []TerminalAttachment{{Data: []byte("png"), Extension: "png"}, {Data: []byte("pdf"), Name: "季度 报告.pdf"}, {Data: []byte("raw"), Name: defaultAttachmentName}})
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"1.png", "2-季度 报告.pdf", "3-" + defaultAttachmentName}
	for i, record := range records {
		if record.Name != wantNames[i] || paths[i] != "/home/agent/.aiofcage/attachments/"+batch+"/"+wantNames[i] {
			t.Fatalf("attachment %d: %q at %q", i+1, record.Name, paths[i])
		}
	}
	big := make([]byte, maxAttachmentBytes)
	for name, files := range map[string][]TerminalAttachment{
		"none":                 nil,
		"too many":             slices.Repeat([]TerminalAttachment{{Data: []byte("x"), Name: "a"}}, 5),
		"traversal name":       {{Data: []byte("x"), Name: "../x"}},
		"separator":            {{Data: []byte("x"), Name: "a/b"}},
		"control":              {{Data: []byte("x"), Name: "a\nb"}},
		"dot dot":              {{Data: []byte("x"), Name: ".."}},
		"unsanitized":          {{Data: []byte("x"), Name: " padded "}},
		"no name":              {{Data: []byte("x")}},
		"unknown image format": {{Data: []byte("x"), Extension: "svg"}},
		"image with name":      {{Data: []byte("x"), Extension: "png", Name: "a.png"}},
		"empty":                {{Name: "a"}},
		"too large":            {{Data: append(big, 'x'), Name: "a"}},
		"total too large":      {{Data: big, Name: "a"}, {Data: big, Name: "b"}, {Data: []byte("x"), Name: "c"}},
	} {
		if _, _, err := attachmentRecords(batch, files); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, _, err := attachmentRecords("../../tmp/batch-name-000", []TerminalAttachment{{Data: []byte("x"), Name: "a"}}); err == nil {
		t.Error("accepted an unsafe batch")
	}
}

// fakeAttachmentDocker records each call's arguments (NUL separated) and standard input under the
// operation name, the next-to-last argument. A write fails while the file "fail" exists.
func fakeAttachmentDocker(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nd='" + dir + "'\nop=''; last=''\nfor a in \"$@\"; do op=$last; last=$a; done\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\"; done > \"$d/$op-$$.args\"\ncat > \"$d/$op-$$.stdin\"\n" +
		"if [ \"$op\" = write ] && [ -f \"$d/fail\" ]; then exit 1; fi\nexit 0\n"
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, dir
}

func recordedCalls(t *testing.T, dir, operation string) (args [][]string, stdin [][]byte) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, operation+"-*.args"))
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		input, err := os.ReadFile(strings.TrimSuffix(match, ".args") + ".stdin")
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00"))
		stdin = append(stdin, input)
	}
	return args, stdin
}

// The writer runs as the agent user but outside the agent's shell setup, with the content on
// standard input only; what it receives is exactly what the real script accepts and writes.
func TestAttachmentWriterCommand(t *testing.T) {
	docker, dir := fakeAttachmentDocker(t)
	m := &Manager{docker: runner{path: docker}}
	ctx := context.Background()
	batch := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	files := []TerminalAttachment{{Data: []byte("\x89PNG image bytes"), Extension: "png"}, {Data: []byte("%PDF-1.7 机密内容"), Name: "季度 报告.pdf"}}
	paths, err := m.WriteTerminalImages(ctx, "sbx-attach01", batch, files)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "/home/agent/.aiofcage/attachments/" + batch + "/"
	if !slices.Equal(paths, []string{prefix + "1.png", prefix + "2-季度 报告.pdf"}) {
		t.Fatalf("paths %q", paths)
	}
	calls, inputs := recordedCalls(t, dir, "write")
	if len(calls) != 1 {
		t.Fatalf("%d write calls", len(calls))
	}
	script, err := assets.ReadFile("assets/agent/terminal-images.py")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--interactive", "--user", "1000:1000", "--workdir", "/", "aiofcage-sbx-attach01-agent",
		"/usr/bin/python3", "-I", "-c", string(script), "write", batch}
	if !slices.Equal(calls[0], want) {
		t.Fatalf("writer command %q", calls[0][:min(len(calls[0]), 10)])
	}
	var records []attachmentRecord
	if err := json.Unmarshal(inputs[0], &records); err != nil || len(records) != 2 || records[0].Name != "1.png" ||
		records[1].Name != "2-季度 报告.pdf" || !bytes.Equal(records[1].Data, files[1].Data) {
		t.Fatalf("writer input %+v %v", records, err)
	}
	for _, arg := range calls[0] {
		if strings.Contains(arg, "机密内容") || strings.Contains(arg, base64.StdEncoding.EncodeToString(files[1].Data)) || strings.Contains(arg, "季度") {
			t.Fatal("attachment content or name reached the command line")
		}
	}

	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("附件写入回归测试需要 python3")
	}
	home := t.TempDir()
	harness := "import importlib.util, sys\nspec = importlib.util.spec_from_file_location('attachments', sys.argv[1])\n" +
		"module = importlib.util.module_from_spec(spec)\nspec.loader.exec_module(module)\n" +
		"module.write(sys.argv[2], sys.argv[3], module.parse(sys.stdin.buffer.read()))\n"
	cmd := exec.Command(python, "-I", "-B", "-c", harness, "assets/agent/terminal-images.py", home, batch)
	cmd.Stdin = bytes.NewReader(inputs[0])
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the script refused what the app sends: %v\n%s", err, output)
	}
	for i, path := range paths {
		data, err := os.ReadFile(filepath.Join(home, strings.TrimPrefix(path, "/home/agent/")))
		if err != nil || !bytes.Equal(data, files[i].Data) {
			t.Fatalf("%s: %v", path, err)
		}
	}

	// A failed write removes whatever part of the batch reached the sandbox.
	if err := os.WriteFile(filepath.Join(dir, "fail"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WriteTerminalImages(ctx, "sbx-attach01", batch, files); err == nil || !strings.Contains(err.Error(), "附件未能保存") {
		t.Fatalf("failed write: %v", err)
	}
	if _, err := m.WriteTerminalImages(ctx, "sbx-attach01", batch, files[:1]); err == nil || !strings.Contains(err.Error(), "图片未能保存") {
		t.Fatalf("failed image write: %v", err)
	}
	removals, _ := recordedCalls(t, dir, "remove")
	if len(removals) != 2 {
		t.Fatalf("%d removals after 2 failed writes", len(removals))
	}
	for _, call := range removals {
		if !slices.Equal(call[:9], want[:9]) || call[len(call)-2] != "remove" || call[len(call)-1] != batch {
			t.Fatalf("remove command %q", call[:9])
		}
	}

	// Invalid input never reaches Docker.
	before, _ := filepath.Glob(filepath.Join(dir, "*.args"))
	if _, err := m.WriteTerminalImages(ctx, "sbx-attach01", batch, []TerminalAttachment{{Data: []byte("x"), Name: "../x"}}); err == nil {
		t.Fatal("accepted an unsafe name")
	}
	if err := m.RemoveTerminalImages(ctx, "sbx-attach01", "../../project"); err == nil {
		t.Fatal("accepted an unsafe batch")
	}
	if after, _ := filepath.Glob(filepath.Join(dir, "*.args")); len(after) != len(before) {
		t.Fatal("invalid input reached Docker")
	}
}

func TestAttachmentScript(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("附件安全回归测试需要 python3")
	}
	output, err := exec.Command(python, "-B", "testdata/test_terminal_images.py", "assets/agent/terminal-images.py").CombinedOutput()
	if err != nil {
		t.Fatalf("附件脚本安全回归失败：%v\n%s", err, output)
	}
	t.Log(string(output))
}

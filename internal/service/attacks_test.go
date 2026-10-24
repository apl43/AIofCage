package service

import (
	"encoding/json"
	"errors"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestIsolationEvidenceRequiresSuccessfulExecution(t *testing.T) {
	for _, tt := range []struct {
		out  string
		err  error
		want string
	}{
		{`{"status":"passed","evidence":"403 default_deny"}`, errors.New("docker exec failed"), checkInconclusive},
		{``, nil, checkInconclusive},
		{`{"status":"passed"}`, nil, checkInconclusive},
		{`{"status":"healthy","evidence":"timeout"}`, nil, checkInconclusive},
		{`{"status":"inconclusive","evidence":"TimeoutError"}`, nil, checkInconclusive},
		{`{"status":"passed","evidence":"403 default_deny"}`, nil, checkPassed},
		{`{"status":"problem","evidence":"connected"}`, nil, checkProblem},
	} {
		if got := classifyCheck(Attack{ID: "test"}, []byte(tt.out), tt.err); got.Status != tt.want {
			t.Fatalf("%+v -> %+v", tt, got)
		}
	}
}

func TestIsolationCurlIgnoresTunnelSuccessAndRequiresReason(t *testing.T) {
	for _, tt := range []struct {
		headers string
		code    int
		want    string
	}{
		{"HTTP/1.1 200 Connection established\r\n", 28, checkInconclusive},
		{"", 6, checkInconclusive},
		{"HTTP/1.1 403 Forbidden\r\nX-Aiofcage-Reason: default_deny\r\n", 56, checkPassed},
		{"HTTP/1.1 403 Forbidden\r\n", 0, checkInconclusive},
		{"HTTP/1.1 200 Connection established\r\n\r\nHTTP/2 200\r\n", 0, checkProblem},
	} {
		headers, _ := json.Marshal(tt.headers)
		script := checkPython + "\nclass Response:\n    stdout=" + string(headers) + "\n    returncode=" + jsonNumber(tt.code) + "\nsubprocess.run=lambda *args,**kwargs: Response()\ncurl_check('https://example.invalid',[],'default_deny')\n"
		out, err := exec.Command("python3", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("python: %v %s", err, out)
		}
		if got := classifyCheck(Attack{}, out, nil); got.Status != tt.want {
			t.Fatalf("headers %q: %+v", tt.headers, got)
		}
	}
}

func jsonNumber(value int) string { data, _ := json.Marshal(value); return string(data) }

func TestDedicatedIsolationChecks(t *testing.T) {
	id := os.Getenv("AIOFCAGE_ISOLATION_ID")
	if id == "" {
		t.Skip("设置 AIOFCAGE_ISOLATION_ID 对已有 uat- 沙箱执行诊断")
	}
	dir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sbx, err := st.Sandbox(id)
	if err != nil || !strings.HasPrefix(sbx.Name, "uat-") {
		t.Fatal("仅允许指定 uat- 测试沙箱")
	}
	keychain := vault.NewKeychain()
	manager, err := runtime.NewManager(st, keychain)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, keychain, manager, nil, nil)
	results, err := app.RunIsolationChecks(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		t.Logf("%s %s %s", r.ID, r.Status, r.Evidence)
		if r.Status == checkProblem {
			t.Errorf("发现隔离问题 %s", r.ID)
		}
	}
}

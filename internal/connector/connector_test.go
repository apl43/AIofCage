package connector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

const (
	testToken  = "connector-token-0123456789abcdef0123456789"
	testSecret = "db-password-for-tests"
)

// TestMain doubles as a fake stdio MCP server when re-executed by the tests.
func TestMain(m *testing.M) {
	if os.Getenv("AIOFCAGE_FAKE_MCP") == "1" {
		fakeServer()
		return
	}
	os.Exit(m.Run())
}

func fakeServer() {
	fmt.Fprintln(os.Stderr, "connecting with password "+os.Getenv("FAKE_PASSWORD"))
	scanner := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.Method == "" {
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		if msg.Method == "ask" {
			// A server→client request: the bridge must answer it without involving the client.
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": "srv-1", "method": "roots/list"})
			if !scanner.Scan() || !strings.Contains(scanner.Text(), `"srv-1"`) {
				os.Exit(3)
			}
		}
		if msg.Method == "tools/call" && os.Getenv("FAKE_FAIL") == "1" {
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID,
				"error": map[string]any{"code": -32603, "message": "password authentication failed (" + os.Getenv("FAKE_PASSWORD") + ")"}})
			continue
		}
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]string{"data": "noise"}})
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]string{
			"method": msg.Method, "password_len": fmt.Sprint(len(os.Getenv("FAKE_PASSWORD")))}})
	}
}

func newTestServer(t *testing.T) (*httptest.Server, *Server, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	var mu sync.Mutex
	server := NewServer(log.New(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return logs.Write(p)
	}), "", 0))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.SetLaunch(Launch{Token: testToken, Command: []string{executable},
		Env: map[string]string{"AIOFCAGE_FAKE_MCP": "1", "FAKE_PASSWORD": testSecret}, Redact: []string{testSecret}}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server)
	t.Cleanup(func() {
		ts.Close()
		server.Close()
	})
	return ts, server, logs
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func post(t *testing.T, url, session, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/mcp", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}

func TestSessionLifecycle(t *testing.T) {
	ts, _, logs := newTestServer(t)
	if resp := post(t, ts.URL, "", "wrong-token", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`); resp.StatusCode != 401 {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	if resp := post(t, ts.URL, "", testToken, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); resp.StatusCode != 400 {
		t.Fatalf("non-initialize without session: %d", resp.StatusCode)
	}
	if resp := post(t, ts.URL, "", testToken, `[{"jsonrpc":"2.0","id":1,"method":"initialize"}]`); resp.StatusCode != 400 {
		t.Fatalf("batch: %d", resp.StatusCode)
	}
	resp := post(t, ts.URL, "", testToken, "{\"jsonrpc\":\"2.0\",\n \"id\":1,\"method\":\"initialize\"}")
	session := resp.Header.Get("Mcp-Session-Id")
	if body := readBody(resp); resp.StatusCode != 200 || session == "" || !strings.Contains(body, `"method":"initialize"`) {
		t.Fatalf("initialize: %d %q %s", resp.StatusCode, session, body)
	}
	if resp := post(t, ts.URL, session, testToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); resp.StatusCode != 202 {
		t.Fatalf("notification: %d", resp.StatusCode)
	}
	resp = post(t, ts.URL, session, testToken, `{"jsonrpc":"2.0","id":"call-7","method":"tools/call"}`)
	if body := readBody(resp); !strings.Contains(body, `"id":"call-7"`) || !strings.Contains(body, fmt.Sprintf(`"password_len":"%d"`, len(testSecret))) {
		t.Fatalf("tools/call: %s", body)
	}
	if body := readBody(post(t, ts.URL, session, testToken, `{"jsonrpc":"2.0","id":8,"method":"ask"}`)); !strings.Contains(body, `"method":"ask"`) {
		t.Fatalf("server request was not answered by the bridge: %s", body)
	}
	if resp := post(t, ts.URL, "unknown", testToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); resp.StatusCode != 404 {
		t.Fatalf("unknown session: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Mcp-Session-Id", session)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 204 {
		t.Fatalf("delete: %v %v", resp, err)
	}
	if resp := post(t, ts.URL, session, testToken, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`); resp.StatusCode != 404 {
		t.Fatalf("closed session: %d", resp.StatusCode)
	}
	if strings.Contains(logs.String(), testSecret) || !strings.Contains(logs.String(), "[REDACTED]") {
		t.Fatalf("stderr must be redacted: %q", logs.String())
	}
}

func TestConcurrentCalls(t *testing.T) {
	ts, _, _ := newTestServer(t)
	resp := post(t, ts.URL, "", testToken, `{"jsonrpc":"2.0","id":0,"method":"initialize"}`)
	readBody(resp)
	session := resp.Header.Get("Mcp-Session-Id")
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := readBody(post(t, ts.URL, session, testToken, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"m%d"}`, i, i)))
			if !strings.Contains(body, fmt.Sprintf(`"id":%d,`, i)) || !strings.Contains(body, fmt.Sprintf(`"method":"m%d"`, i)) {
				t.Errorf("call %d got %s", i, body)
			}
		}()
	}
	wg.Wait()
}

func TestNotConfiguredAndGET(t *testing.T) {
	server := NewServer(log.New(io.Discard, "", 0))
	ts := httptest.NewServer(server)
	defer ts.Close()
	if resp := post(t, ts.URL, "", testToken, `{}`); resp.StatusCode != 503 {
		t.Fatalf("unconfigured: %d", resp.StatusCode)
	}
	if err := server.SetLaunch(Launch{Token: "short", Command: []string{"x"}}); err == nil {
		t.Fatal("short token must be rejected")
	}
	_ = server.SetLaunch(Launch{Token: testToken, Command: []string{"x"}})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 405 {
		t.Fatalf("GET: %d", resp.StatusCode)
	}
	if resp := post(t, ts.URL, "", testToken, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`); resp.StatusCode != 502 {
		t.Fatalf("missing binary: %d", resp.StatusCode)
	}
}

func TestCheck(t *testing.T) {
	executable, _ := os.Executable()
	launch := Launch{Token: testToken, Command: []string{executable}, Redact: []string{testSecret},
		Env: map[string]string{"AIOFCAGE_FAKE_MCP": "1", "FAKE_PASSWORD": testSecret}}
	ctx := context.Background()
	if _, err := Check(ctx, launch, "query", map[string]any{"sql": "SELECT 1"}, log.New(io.Discard, "", 0)); err != nil {
		t.Fatalf("check against a working server: %v", err)
	}
	launch.Env["FAKE_FAIL"] = "1"
	_, err := Check(ctx, launch, "query", map[string]any{"sql": "SELECT 1"}, log.New(io.Discard, "", 0))
	if err == nil || !strings.Contains(err.Error(), "password authentication failed") || strings.Contains(err.Error(), testSecret) {
		t.Fatalf("a failed login must be reported, redacted: %v", err)
	}
	launch.Command = []string{"/nonexistent/server"}
	if _, err := Check(ctx, launch, "query", nil, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("a missing server must fail")
	}
}

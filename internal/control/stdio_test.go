package control

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type testHandler struct {
	closed bool
	calls  int
}

func (*testHandler) Tools() []any { return []any{map[string]string{"name": "test"}} }
func (h *testHandler) Call(context.Context, string, json.RawMessage) (any, error) {
	h.calls++
	return map[string]bool{"ok": true}, nil
}
func (h *testHandler) Close() { h.closed = true }

func TestStdioRequiresInitializationAndClosesScope(t *testing.T) {
	h := &testHandler{}
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test"}}
{"jsonrpc":"2.0","id":2,"method":"initialize"}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":3,"method":"tools/list"}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"test","arguments":{}}}
`
	if err := Serve(context.Background(), strings.NewReader(input), &out, h); err != nil {
		t.Fatal(err)
	}
	if !h.closed || h.calls != 1 || !strings.Contains(out.String(), "-32002") || strings.Count(out.String(), "\n") != 4 {
		t.Fatal(out.String(), h)
	}
}

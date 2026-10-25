package democases

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBusinessMetricsAndReadOnlyBoundary(t *testing.T) {
	value, err := Call("sales", "summarize_data", json.RawMessage(`{"dataset":"orders","group_by":"channel"}`))
	raw, _ := json.Marshal(value)
	if err != nil || !strings.Contains(string(raw), `"paid_cny":3000`) || !strings.Contains(string(raw), `"refund_cny":2000`) || !strings.Contains(string(raw), `"rows":4`) {
		t.Fatalf("统计错误: %v %s", err, raw)
	}
	filtered, err := Call("sales", "read_data", json.RawMessage(`{"dataset":"orders","contains":"重复扣款"}`))
	data, _ := json.Marshal(filtered)
	if err != nil || !strings.Contains(string(data), `"matched":2`) {
		t.Fatalf("筛选错误: %v %s", err, data)
	}
	for _, test := range []struct{ id, tool, args string }{{"sales", "execute_sql", `{}`}, {"sales", "read_data", `{"dataset":"../../etc/passwd"}`}, {"sales", "read_data", `{"dataset":"orders","url":"http://localhost"}`}, {"sales", "read_data", `{"dataset":"orders","limit":1000}`}, {"sales", "summarize_data", `{"dataset":"orders","group_by":"paid_cny"}`}, {"missing", "case_overview", `{}`}} {
		if _, err := Call(test.id, test.tool, json.RawMessage(test.args)); err == nil {
			t.Fatalf("接受了越界请求: %+v", test)
		}
	}
	for _, c := range List() {
		if c.Datasets != nil {
			t.Fatal("列表泄漏完整数据")
		}
		if _, err := Call(c.ID, "case_overview", json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDemoStdioProtocol(t *testing.T) {
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"read_data\",\"arguments\":{\"dataset\":\"logs\",\"contains\":\"TAX_REGION\"}}}\n")
	var out bytes.Buffer
	if err := Serve("incident", input, &out); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(out.String(), "\n"); lines != 3 || !strings.Contains(out.String(), "L-01") || !strings.Contains(out.String(), "readOnlyHint") {
		t.Fatal(out.String())
	}
}

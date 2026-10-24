package gatekeeper

import "testing"

func TestUsageBreakdownAndZero(t *testing.T) {
	cases := []struct {
		body                       string
		input, output, read, write int64
	}{
		{`{"model":"m","usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":30}}}`, 100, 20, 30, 0},
		{`{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30}}}`, 100, 20, 30, 0},
		{`{"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":30,"cache_creation_input_tokens":40}}`, 80, 20, 30, 40},
		{`{"usage":{"input_tokens":0,"output_tokens":0}}`, 0, 0, 0, 0},
	}
	for _, c := range cases {
		s := newUsageSniffer("application/json")
		for _, b := range []byte(c.body) {
			s.Write([]byte{b})
		}
		i, o, found := s.Result()
		if !found || i != c.input || o != c.output || s.cacheRead != c.read || s.cacheWrite != c.write {
			t.Fatalf("%s: %+v", c.body, s)
		}
	}
	s := newUsageSniffer("text/event-stream")
	s.Write([]byte("data: {\"message\":{\"model\":\"m\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0,\"cache_read_input_tokens\":5}}}\n"))
	s.Write([]byte("data: {\"usage\":{\"output_tokens\":12}}\n"))
	i, o, found := s.Result()
	if !found || i != 15 || o != 12 || s.model != "m" {
		t.Fatalf("流式分项错误: %+v", s)
	}
}

func TestPartialUsageIsUnknown(t *testing.T) {
	for _, body := range []string{`{"usage":{"input_tokens":50}}`, `{"usage":{"output_tokens":7}}`, `{"usage":{"input_tokens":-1,"output_tokens":7}}`} {
		s := newUsageSniffer("application/json")
		s.Write([]byte(body))
		_, _, known := s.Result()
		if known {
			t.Fatalf("缺失计数被补成零：%s", body)
		}
	}
}

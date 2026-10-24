package gatekeeper

import (
	"bytes"
	"encoding/json"
	"strings"
)

const maxUsageBuffer = 16 << 20

// usageSniffer reads token usage from model API responses without changing the bytes forwarded
// to the agent. It understands the usage objects of OpenAI Responses / Chat Completions and
// Anthropic Messages, both as single JSON documents and as SSE streams.
type usageSniffer struct {
	sse      bool
	buffer   []byte
	overflow bool
	input    int64
	output   int64
	found    bool
}

func newUsageSniffer(contentType string) *usageSniffer {
	return &usageSniffer{sse: strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")}
}

func (s *usageSniffer) Write(p []byte) {
	if !s.sse {
		if len(s.buffer)+len(p) > maxUsageBuffer {
			s.overflow = true
			return
		}
		s.buffer = append(s.buffer, p...)
		return
	}
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.appendLine(p)
			return
		}
		s.appendLine(p[:i])
		s.line()
		p = p[i+1:]
	}
}

func (s *usageSniffer) appendLine(p []byte) {
	if s.overflow {
		return
	}
	if len(s.buffer)+len(p) > maxUsageBuffer {
		// A single oversized event is skipped; usage then stays unknown unless reported elsewhere.
		s.overflow = true
		s.buffer = s.buffer[:0]
		return
	}
	s.buffer = append(s.buffer, p...)
}

func (s *usageSniffer) line() {
	line := bytes.TrimRight(s.buffer, "\r")
	if !s.overflow && bytes.HasPrefix(line, []byte("data:")) {
		s.inspect(bytes.TrimSpace(line[len("data:"):]))
	}
	s.buffer = s.buffer[:0]
	s.overflow = false
}

// Result returns reported input/output tokens and whether any usage object was seen.
func (s *usageSniffer) Result() (int64, int64, bool) {
	if s.sse && len(s.buffer) > 0 {
		s.line()
	}
	if !s.sse && !s.overflow {
		s.inspect(s.buffer)
		s.buffer = nil
	}
	return s.input, s.output, s.found
}

func (s *usageSniffer) inspect(document []byte) {
	if !bytes.Contains(document, []byte(`"usage"`)) {
		return
	}
	var value any
	if json.Unmarshal(document, &value) != nil {
		return
	}
	s.walk(value, 0)
}

func (s *usageSniffer) walk(value any, depth int) {
	if depth > 8 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if usage, ok := typed["usage"].(map[string]any); ok {
			s.record(usage)
		}
		for _, child := range typed {
			s.walk(child, depth+1)
		}
	case []any:
		for _, child := range typed {
			s.walk(child, depth+1)
		}
	}
}

// record keeps the largest value seen per field: streams report cumulative counts (Anthropic
// sends input in message_start and cumulative output in message_delta).
func (s *usageSniffer) record(usage map[string]any) {
	input := number(usage, "input_tokens") + number(usage, "cache_creation_input_tokens") + number(usage, "cache_read_input_tokens")
	if input == 0 {
		input = number(usage, "prompt_tokens")
	}
	output := number(usage, "output_tokens")
	if output == 0 {
		output = number(usage, "completion_tokens")
	}
	if input == 0 && output == 0 {
		return
	}
	s.found = true
	s.input = max(s.input, input)
	s.output = max(s.output, output)
}

func number(values map[string]any, key string) int64 {
	if value, ok := values[key].(float64); ok && value > 0 && value < 1e12 {
		return int64(value)
	}
	return 0
}

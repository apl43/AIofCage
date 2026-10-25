package service

import (
	"strings"
	"testing"
)

func TestConversationValidation(t *testing.T) {
	for _, options := range []ConversationOptions{{Model: ""}, {Model: "test\x1b[200~"}, {Model: "test", Effort: "invalid"}} {
		if validateConversationOptions(options) == nil {
			t.Fatalf("接受了非法选项：%+v", options)
		}
	}
	if err := validateConversationOptions(ConversationOptions{Model: "vendor/model", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	buffer := &tailBuffer{limit: 8}
	_, _ = buffer.Write([]byte(strings.Repeat("a", 100)))
	_, _ = buffer.Write([]byte("END"))
	if string(buffer.data) != "aaaaaEND" {
		t.Fatalf("诊断未按上限保存尾部：%q", buffer.data)
	}
	for _, tc := range []struct{ detail, want string }{
		{"401 unauthorized", "供应商认证失败"},
		{"429 rate limit", "供应商限流"},
		{"dial timeout", "请求超时"},
		{"connection refused", "无法连接供应商"},
	} {
		if got := classifyConversationFailure(tc.detail); !strings.Contains(got, tc.want) {
			t.Fatalf("%q: %q", tc.detail, got)
		}
	}
}

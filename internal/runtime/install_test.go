package runtime

import (
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"testing"
)

func TestAddedBindingsPreservesExisting(t *testing.T) {
	original := []sandbox.Binding{{Agent: session.Codex, ProviderID: "p1"}}
	got, err := addedBindings(original, []session.Agent{session.DSH, session.Codex, session.ClaudeCode, session.DSH})
	if err != nil || len(got) != 3 || got[0] != original[0] || original[0].ProviderID != "p1" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := addedBindings(original, []session.Agent{"invalid"}); err == nil {
		t.Fatal("接受了无效智能体")
	}
	if _, err := addedBindings(nil, nil); err == nil {
		t.Fatal("接受了空选择")
	}
}

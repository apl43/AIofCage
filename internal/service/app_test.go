package service

import (
	"errors"
	"testing"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestDraftCannotStartSandbox(t *testing.T) {
	app := NewApp(session.NewMemoryStore(), runtime.NewUnavailable(), nil)
	item, err := app.CreateSession(CreateSessionRequest{Name: "测试项目", ProjectPath: "/example/project", Agent: session.Codex})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.StartSession(item.ID); !errors.Is(err, runtime.ErrNotReady) {
		t.Fatalf("start returned %v, want not ready", err)
	}
	items := app.ListSessions()
	if len(items) != 1 || items[0].State != session.Created {
		t.Fatalf("failed start changed draft state: %+v", items)
	}
	if err := app.StartSession("missing"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing session returned %v", err)
	}
	for _, request := range []CreateSessionRequest{
		{Name: "", ProjectPath: "/example/project", Agent: session.Codex},
		{Name: "test", ProjectPath: "relative/path", Agent: session.Codex},
		{Name: "test", ProjectPath: "/example/project", Agent: "sh -c command"},
	} {
		if _, err := app.CreateSession(request); err == nil {
			t.Fatalf("accepted invalid draft: %+v", request)
		}
	}
}

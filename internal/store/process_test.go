package store

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/shaoxier/AIofCage/internal/sandbox"
)

func TestStoreProcessWriter(t *testing.T) {
	dir := os.Getenv("AIOFCAGE_STORE_PROCESS_TEST")
	if dir == "" {
		t.Skip("仅供进程并发回归调用")
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := s.UpdateSandbox("shared", func(s *sandbox.Sandbox) error { s.Usage.Requests++; return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreDoesNotLoseConcurrentProcessWrites(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSandbox(sandbox.Sandbox{ID: "shared", Name: "shared"}); err != nil {
		t.Fatal(err)
	}
	commands := []*exec.Cmd{}
	for i := 0; i < 3; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoreProcessWriter$")
		cmd.Env = append(os.Environ(), "AIOFCAGE_STORE_PROCESS_TEST="+s.Dir())
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.Sandbox("shared")
	if err != nil || after.Usage.Requests != 60 {
		t.Fatal(fmt.Sprintf("跨进程计数丢失: %d err=%v", after.Usage.Requests, err))
	}
}

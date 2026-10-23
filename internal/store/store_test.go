package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/sandbox"
)

func TestRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if items, err := st.Providers(); err != nil || len(items) != 0 {
		t.Fatalf("empty store: %v %v", items, err)
	}
	p := provider.Provider{ID: "a", Name: "A", CreatedAt: time.Now()}
	if err := st.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	p.Name = "A2"
	if err := st.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Provider("a"); err != nil || got.Name != "A2" {
		t.Fatalf("replace: %+v %v", got, err)
	}
	s1 := sandbox.Sandbox{ID: "sbx-1", Name: "one"}
	if err := st.SaveSandbox(s1); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSandbox(sandbox.Sandbox{ID: "sbx-2", Name: "one"}); err == nil {
		t.Fatal("duplicate sandbox name accepted")
	}
	if got, err := st.Sandbox("one"); err != nil || got.ID != "sbx-1" {
		t.Fatalf("lookup by name: %+v %v", got, err)
	}
	if err := st.DeleteSandbox("sbx-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sandbox("sbx-1"); err == nil {
		t.Fatal("deleted sandbox still present")
	}
	for _, name := range []string{"providers.json", "sandboxes.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions: %v %v", name, info.Mode(), err)
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contextFixture(t *testing.T) (*Environment, string) {
	t.Helper()
	home := t.TempDir()
	// macOS may expose its temporary directory through /var -> /private/var.
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("AIOFCAGE_CONTEXT_TEST_DIR", home)
	workspace := filepath.Join(home, "CageWorkspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		path := filepath.Join(home, name)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	vm := write("colima", `#!/bin/sh
case "$1" in
 list) printf '%s\n' '{"name":"aiofcage","status":"Running","arch":"aarch64"}' ;;
 ssh) printf 'none %s/CageWorkspace virtiofs rw 0 0\n' "$HOME" ;;
 *) exit 1 ;;
esac
`)
	config, _ := json.Marshal(map[string]any{"config": map[string]any{"vmType": "vz", "mountType": "virtiofs", "mounts": []map[string]any{{"location": workspace, "writable": true}}}})
	write("lima.json", string(config))
	lima := write("limactl", "#!/bin/sh\ncat \"$HOME/lima.json\"\n")
	endpoint := "unix://" + filepath.Join(home, ".colima", ColimaProfile, "docker.sock")
	metadata, _ := json.Marshal([]map[string]any{{"Endpoints": map[string]any{"docker": map[string]any{"Host": endpoint}}}})
	write("expected.json", string(metadata))
	docker := write("docker", `#!/bin/sh
dir="$AIOFCAGE_CONTEXT_TEST_DIR"
printf '%s\n' "$*" >> "$dir/commands"
# A missing selected context may appear in ls, so metadata commands must use default.
[ "$1" = --context ] && [ "$2" = default ] || exit 90
shift 2
[ "$1" = context ] || exit 91
case "$2" in
 inspect)
  [ "$3" = colima-aiofcage ] || exit 92
  if [ -f "$dir/unreadable" ]; then echo 'permission denied' >&2; exit 1; fi
  if [ ! -f "$dir/context.json" ]; then echo 'context not found' >&2; exit 1; fi
  cat "$dir/context.json"
  ;;
 ls)
  if [ -f "$dir/list-fails" ]; then echo 'cannot list contexts' >&2; exit 1; fi
  echo default
  if [ -f "$dir/context.json" ]; then echo colima-aiofcage; fi
  ;;
 create)
  [ "$3" = colima-aiofcage ] && [ "$4" = --docker ] && [ "$5" = "host=unix://$HOME/.colima/aiofcage/docker.sock" ] || exit 93
  if [ -f "$dir/create-fails" ]; then echo 'cannot write context' >&2; exit 1; fi
  if [ -f "$dir/context.json" ]; then echo 'already exists' >&2; exit 1; fi
  cp "$dir/expected.json" "$dir/context.json"
  if [ -f "$dir/concurrent-create" ]; then echo 'already exists' >&2; exit 1; fi
  ;;
 *) exit 94 ;;
esac
`)
	return &Environment{colima: runner{path: vm}, limactl: lima, docker: runner{path: docker, args: []string{"--context", DockerContext}}}, home
}

func TestContextIsCreatedOnStartupButNeverByInspection(t *testing.T) {
	e, home := contextFixture(t)
	ctx := context.Background()
	if err := e.Verify(ctx); !errors.Is(err, errDockerContextMissing) {
		t.Fatalf("missing connection not identified: %v", err)
	}
	report := e.Inspect(ctx)
	found := false
	for _, check := range report.Checks {
		if check.ID == "docker_context" && check.Status == "missing" {
			found = true
		}
	}
	if report.Ready || !found {
		t.Fatalf("missing context reported as ready: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(home, "context.json")); !os.IsNotExist(err) {
		t.Fatal("read-only inspection created a connection")
	}
	for range 2 {
		if err := e.verify(ctx, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Verify(ctx); err != nil {
		t.Fatal("created context was not usable", err)
	}
	commands, _ := os.ReadFile(filepath.Join(home, "commands"))
	if strings.Count(string(commands), "context create") != 1 || strings.Contains(string(commands), "context use") {
		t.Fatalf("connection recreated or current context changed: %s", commands)
	}
}

func TestContextFailuresDoNotOverwriteOrHideExistingConnections(t *testing.T) {
	for _, mode := range []string{"wrong-endpoint", "unreadable", "malformed", "list-fails", "create-fails"} {
		t.Run(mode, func(t *testing.T) {
			e, home := contextFixture(t)
			metadata := filepath.Join(home, "context.json")
			switch mode {
			case "wrong-endpoint":
				os.WriteFile(metadata, []byte(`[{"Endpoints":{"docker":{"Host":"unix:///some-other-docker.sock"}}}]`), 0o600)
			case "malformed":
				os.WriteFile(metadata, []byte(`broken json`), 0o600)
			case "unreadable":
				os.WriteFile(metadata, []byte(`existing`), 0o600)
				os.WriteFile(filepath.Join(home, mode), nil, 0o600)
			default:
				os.WriteFile(filepath.Join(home, mode), nil, 0o600)
			}
			before, _ := os.ReadFile(metadata)
			err := e.verify(context.Background(), true)
			if err == nil || errors.Is(err, errDockerContextMissing) {
				t.Fatalf("failure hidden: %v", err)
			}
			want := map[string]string{"wrong-endpoint": "没有指向专用虚拟机", "malformed": "没有指向专用虚拟机", "unreadable": "permission denied", "list-fails": "无法读取 Docker 连接列表", "create-fails": "创建 Docker context"}[mode]
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("wrong failure: want %s, got %v", want, err)
			}
			after, _ := os.ReadFile(metadata)
			if string(before) != string(after) {
				t.Fatal("existing connection was overwritten")
			}
			commands, _ := os.ReadFile(filepath.Join(home, "commands"))
			if mode != "create-fails" && strings.Contains(string(commands), "context create") {
				t.Fatalf("created connection after a read/validation failure: %s", commands)
			}
		})
	}
}

func TestContextCreatedConcurrentlyIsValidated(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "dedicated", false: "wrong-endpoint"}[valid], func(t *testing.T) {
			e, home := contextFixture(t)
			os.WriteFile(filepath.Join(home, "concurrent-create"), nil, 0o600)
			if !valid {
				os.WriteFile(filepath.Join(home, "expected.json"), []byte(`[{"Endpoints":{"docker":{"Host":"tcp://other-host:2375"}}}]`), 0o600)
			}
			err := e.verify(context.Background(), true)
			if (err == nil) != valid {
				t.Fatalf("concurrent context validation: valid=%v err=%v", valid, err)
			}
		})
	}
}

func TestContextIsNotCreatedBeforeVerifyingVMIsolation(t *testing.T) {
	e, home := contextFixture(t)
	os.WriteFile(filepath.Join(home, "lima.json"), []byte(`{"config":{"vmType":"vz","mountType":"virtiofs","mounts":[{"location":"/","writable":true}]}}`), 0o600)
	if err := e.verify(context.Background(), true); err == nil || !strings.Contains(err.Error(), "共享目录") {
		t.Fatalf("unsafe VM allowed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "commands")); !os.IsNotExist(err) {
		t.Fatal("Docker configuration accessed before VM boundary validation")
	}
}

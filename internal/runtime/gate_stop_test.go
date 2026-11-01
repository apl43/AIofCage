package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 应用停门卫之前先得到通知（这时 Agent 已经停过），事件流结束才不会被当成采集中断；门卫没在运行就不通知。
func TestAppIsToldBeforeGateStops(t *testing.T) {
	for _, tc := range []struct {
		name, gate string
		agentFails bool
		want       int
	}{
		{"门卫运行中", "running", false, 1},
		{"门卫已停", "stopped", false, 0},
		{"Agent 没停下来", "running", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docker, log := fakeDocker(t, tc.gate)
			if tc.agentFails {
				dir := t.TempDir()
				log = filepath.Join(dir, "commands")
				script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\ncase \"$*\" in\n" +
					"*'container inspect'*) echo 'true img spec';;\n*stop*-agent) exit 1;;\nesac\nexit 0\n"
				docker = filepath.Join(dir, "docker")
				if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			m := &Manager{docker: runner{path: docker}}
			calls := 0
			m.BeforeGateStop = func(id string) {
				calls++
				data, _ := os.ReadFile(log)
				if id != "sbx-a" || !strings.Contains(string(data), "stop --time 5 aiofcage-sbx-a-agent") || strings.Contains(string(data), "stop --time 5 aiofcage-sbx-a-gate") {
					t.Errorf("通知时机不对：%q\n%s", id, data)
				}
			}
			err := m.stopContainers(context.Background(), names("sbx-a"))
			if (err != nil) != tc.agentFails || calls != tc.want {
				t.Fatalf("err=%v，通知 %d 次，应为 %d 次", err, calls, tc.want)
			}
			data, _ := os.ReadFile(log)
			if tc.gate == "running" && !strings.Contains(string(data), "stop --time 5 aiofcage-sbx-a-gate") {
				t.Fatalf("门卫没有停：\n%s", data)
			}
		})
	}
}

// 门卫没停下来（或没删掉）时告诉应用它还在运行，照常跟随；删除时，门卫在运行就先通知再删。
func TestAppIsToldWhenGateKeepsRunning(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\ncase \"$*\" in\n" +
		"*'container inspect'*) echo 'true img spec';;\n*stop*-gate) exit 1;;\n*rm*--force*-gate) exit 1;;\nesac\nexit 0\n"
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{docker: runner{path: docker}, gates: map[string]*gateClient{}, connectorTokens: map[string]string{}}
	var calls []string
	m.BeforeGateStop = func(id string) { calls = append(calls, "stop "+id) }
	m.GateStarted = func(id string) { calls = append(calls, "started "+id) }
	if err := m.stopContainers(context.Background(), names("sbx-a")); err == nil {
		t.Fatal("门卫停止失败没有报错")
	}
	if err := m.removeRuntimeObjects(context.Background(), "sbx-a", true); err == nil {
		t.Fatal("门卫删除失败没有报错")
	}
	if strings.Join(calls, ",") != "stop sbx-a,started sbx-a,stop sbx-a,started sbx-a" {
		t.Fatalf("通知不对：%v", calls)
	}
}

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

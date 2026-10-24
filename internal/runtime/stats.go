package runtime

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

// ContainerStats 汇总沙箱所有实际组件的资源使用，Valid=false 表示没有完整样本。
type ContainerStats struct {
	Valid       bool      `json:"valid"`
	Error       string    `json:"error,omitempty"`
	CollectedAt time.Time `json:"collected_at"`
	NetworkIn   uint64    `json:"network_in"`
	NetworkOut  uint64    `json:"network_out"`
	BlockRead   uint64    `json:"block_read"`
	BlockWrite  uint64    `json:"block_write"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemoryBytes uint64    `json:"memory_bytes"`
	MemoryLimit uint64    `json:"memory_limit"`
	PIDs        int       `json:"pids"`
}

// Stats samples resource use for the given running sandboxes with a single docker call.
func (m *Manager) Stats(ctx context.Context, ids []string) map[string]ContainerStats {
	result := map[string]ContainerStats{}
	for id, health := range m.SampleHealth(ctx, ids) {
		result[id] = health.Stats
	}
	return result
}

// parseSize understands docker's human-readable sizes such as "12.5MiB" or "1.2GB".
func parseSize(value string) uint64 {
	size, _ := parseSizeChecked(value)
	return size
}

func parseSizeChecked(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	units := []struct {
		suffix string
		factor float64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40}, {"kB", 1e3}, {"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"B", 1}}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			number, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, unit.suffix)), 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number*unit.factor >= math.Exp2(64) {
				return 0, false
			}
			return uint64(number * unit.factor), true
		}
	}
	return 0, false
}

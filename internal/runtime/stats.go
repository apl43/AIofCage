package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ContainerStats is the combined resource use of a sandbox's agent and gatekeeper containers.
type ContainerStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
	MemoryLimit uint64  `json:"memory_limit"`
	PIDs        int     `json:"pids"`
}

// Stats samples resource use for the given running sandboxes with a single docker call.
func (m *Manager) Stats(ctx context.Context, ids []string) map[string]ContainerStats {
	result := map[string]ContainerStats{}
	if len(ids) == 0 {
		return result
	}
	args := []string{"stats", "--no-stream", "--format", "{{json .}}"}
	owner := map[string]string{}
	for _, id := range ids {
		n := names(id)
		args = append(args, n.agent, n.gate)
		owner[n.agent], owner[n.gate] = id, id
	}
	out, err := m.docker.run(ctx, 20*time.Second, nil, args...)
	if err != nil && len(out) == 0 {
		return result
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var row struct {
			Name     string `json:"Name"`
			CPUPerc  string `json:"CPUPerc"`
			MemUsage string `json:"MemUsage"`
			PIDs     string `json:"PIDs"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		id, ok := owner[row.Name]
		if !ok {
			continue
		}
		stats := result[id]
		cpu, _ := strconv.ParseFloat(strings.TrimSuffix(row.CPUPerc, "%"), 64)
		stats.CPUPercent += cpu
		used, limit, _ := strings.Cut(row.MemUsage, "/")
		stats.MemoryBytes += parseSize(used)
		if strings.HasSuffix(row.Name, "-agent") {
			stats.MemoryLimit = parseSize(limit)
		}
		pids, _ := strconv.Atoi(row.PIDs)
		stats.PIDs += pids
		result[id] = stats
	}
	return result
}

// parseSize understands docker's human-readable sizes such as "12.5MiB" or "1.2GB".
func parseSize(value string) uint64 {
	value = strings.TrimSpace(value)
	units := []struct {
		suffix string
		factor float64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40}, {"kB", 1e3}, {"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"B", 1}}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			number, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, unit.suffix)), 64)
			if err != nil {
				return 0
			}
			return uint64(number * unit.factor)
		}
	}
	return 0
}

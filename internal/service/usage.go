package service

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
)

type UsageQuery struct {
	SandboxID  string `json:"sandbox_id"`
	ProviderID string `json:"provider_id"`
	Period     string `json:"period"`
}
type UsageRecord struct {
	audit.Event
	SandboxName string `json:"sandbox_name"`
}
type UsageGroup struct {
	Costs            map[string]float64 `json:"costs"`
	Unpriced         uint64             `json:"unpriced"`
	ProviderID       string             `json:"provider_id"`
	ProviderName     string             `json:"provider_name"`
	Model            string             `json:"model"`
	Agent            string             `json:"agent"`
	Requests         uint64             `json:"requests"`
	Unknown          uint64             `json:"unknown"`
	InputTokens      int64              `json:"input_tokens"`
	OutputTokens     int64              `json:"output_tokens"`
	CacheReadTokens  int64              `json:"cache_read_tokens"`
	CacheWriteTokens int64              `json:"cache_write_tokens"`
}
type UsageReport struct {
	Costs    map[string]float64 `json:"costs"`
	Unpriced uint64             `json:"unpriced"`
	Rows     []UsageRecord      `json:"rows"`
	Groups   []UsageGroup       `json:"groups"`
	Requests uint64             `json:"requests"`
	Unknown  uint64             `json:"unknown"`
	Gaps     uint64             `json:"gaps"`
	Legacy   bool               `json:"legacy"`
	Since    time.Time          `json:"since"`
}

// QueryUsage 以持久化审计为唯一来源流式汇总，事件 ID 去重；最近明细最多 100 条。
func (a *App) QueryUsage(query UsageQuery) (UsageReport, error) {
	report := UsageReport{Costs: map[string]float64{}, Rows: []UsageRecord{}, Groups: []UsageGroup{}}
	now := a.now()
	switch query.Period {
	case "today":
		report.Since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		report.Since = now.AddDate(0, 0, -7)
	case "all", "":
	default:
		return report, errors.New("不支持的时间范围")
	}
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return report, err
	}
	names := map[string]string{}
	for _, s := range sandboxes {
		names[s.ID] = s.Name
	}
	paths, err := filepath.Glob(filepath.Join(a.store.Dir(), "audit", "*.jsonl"))
	if err != nil {
		return report, err
	}
	files := map[string]bool{}
	for _, path := range paths {
		files[strings.TrimSuffix(filepath.Base(path), ".jsonl")] = true
	}
	for _, s := range sandboxes {
		if (query.SandboxID == "" || query.SandboxID == s.ID) && s.Usage.Requests > 0 && !files[s.ID] {
			report.Legacy = true
		}
	}
	seen := map[string]bool{}
	groups := map[string]*UsageGroup{}
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if query.SandboxID != "" && query.SandboxID != id {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return report, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			var event audit.Event
			if json.Unmarshal(scanner.Bytes(), &event) != nil {
				report.Gaps++
				continue
			}
			if !report.Since.IsZero() && event.Time.Before(report.Since) {
				continue
			}
			key := id + ":" + event.ID
			if event.ID != "" {
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			if event.Action == audit.ActionGap {
				report.Gaps++
				continue
			}
			if event.Action == audit.ActionRequest && !event.ModelRequest && event.Agent == "" && event.SecretRef != "" {
				report.Legacy = true
			}
			if event.Action != audit.ActionRequest || !event.ModelRequest {
				continue
			}
			if query.ProviderID != "" && query.ProviderID != event.SecretRef {
				continue
			}
			report.Requests++
			if event.UsageKind != "reported" {
				report.Unknown++
			}
			key = event.SecretRef + "\x00" + event.Model
			group := groups[key]
			if group == nil {
				group = &UsageGroup{Costs: map[string]float64{}, ProviderID: event.SecretRef, ProviderName: event.ProviderName, Agent: event.Agent, Model: event.Model}
				groups[key] = group
			}
			if event.EstimatedCost != nil && event.Pricing != nil {
				group.Costs[event.Pricing.Currency] += *event.EstimatedCost
				report.Costs[event.Pricing.Currency] += *event.EstimatedCost
			} else {
				group.Unpriced++
				report.Unpriced++
			}

			group.Requests++
			if event.UsageKind != "reported" {
				group.Unknown++
			}
			group.InputTokens += event.InputTokens
			group.OutputTokens += event.OutputTokens
			group.CacheReadTokens += event.CacheReadTokens
			group.CacheWriteTokens += event.CacheWriteTokens
			name := names[id]
			if name == "" {
				name = id + "（已删除）"
			}
			report.Rows = append(report.Rows, UsageRecord{Event: event, SandboxName: name})
			if len(report.Rows) > 200 {
				sort.Slice(report.Rows, func(i, j int) bool { return report.Rows[i].Time.After(report.Rows[j].Time) })
				report.Rows = report.Rows[:100]
			}
		}
		scanErr := scanner.Err()
		file.Close()
		if scanErr != nil {
			return report, scanErr
		}
	}
	for _, group := range groups {
		report.Groups = append(report.Groups, *group)
	}
	sort.Slice(report.Groups, func(i, j int) bool {
		if report.Groups[i].ProviderName == report.Groups[j].ProviderName {
			return report.Groups[i].Model < report.Groups[j].Model
		}
		return report.Groups[i].ProviderName < report.Groups[j].ProviderName
	})
	sort.Slice(report.Rows, func(i, j int) bool { return report.Rows[i].Time.After(report.Rows[j].Time) })
	if len(report.Rows) > 100 {
		report.Rows = report.Rows[:100]
	}
	return report, nil
}

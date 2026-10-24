package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
)

const (
	monitorInterval = 5 * time.Second
	ringCapacity    = 500
	recentLimit     = 300
)

// monitor follows every running sandbox: it subscribes to the gatekeeper event stream, appends
// events to a per-sandbox JSONL audit log, and polls usage and container stats for the UI.
type monitor struct {
	mu     sync.Mutex
	subs   map[string]context.CancelFunc
	rings  map[string][]audit.Event
	lastID map[string]string
	live   map[string]runtime.GateUsage
	stats  map[string]runtime.ContainerStats
	// oauthAlerted remembers services whose expired login was already reported.
	oauthAlerted map[string]bool
}

func newMonitor() *monitor {
	return &monitor{subs: map[string]context.CancelFunc{}, rings: map[string][]audit.Event{},
		lastID: map[string]string{}, live: map[string]runtime.GateUsage{}, stats: map[string]runtime.ContainerStats{},
		oauthAlerted: map[string]bool{}}
}

// StartMonitor runs until ctx ends. The desktop app calls it once the window exists.
func (a *App) StartMonitor(ctx context.Context) {
	if a.manager == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(monitorInterval)
		defer ticker.Stop()
		for {
			a.tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(oauthCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.refreshOAuth(ctx)
			}
		}
	}()
}

func (a *App) tick(ctx context.Context) {
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return
	}
	var running []string
	for _, s := range sandboxes {
		if a.manager.Status(ctx, s.ID).State == "running" {
			running = append(running, s.ID)
			a.subscribe(ctx, s)
			if usage, err := a.manager.Usage(ctx, s.ID); err == nil {
				_ = a.manager.RecordUsage(s.ID, usage)
				a.monitor.mu.Lock()
				a.monitor.live[s.ID] = usage
				a.monitor.mu.Unlock()
				a.emit("sandbox:usage", map[string]any{"id": s.ID, "usage": usage})
			}
		} else {
			a.unsubscribe(s.ID)
		}
	}
	stats := a.manager.Stats(ctx, running)
	a.monitor.mu.Lock()
	a.monitor.stats = stats
	for id := range a.monitor.live {
		if _, ok := stats[id]; !ok {
			delete(a.monitor.live, id)
		}
	}
	a.monitor.mu.Unlock()
	a.emit("sandbox:stats", stats)
}

func (a *App) subscribe(ctx context.Context, s sandbox.Sandbox) {
	a.monitor.mu.Lock()
	defer a.monitor.mu.Unlock()
	if _, ok := a.monitor.subs[s.ID]; ok {
		return
	}
	subCtx, cancel := context.WithCancel(ctx)
	a.monitor.subs[s.ID] = cancel
	go a.follow(subCtx, s)
}

func (a *App) unsubscribe(id string) {
	a.monitor.mu.Lock()
	defer a.monitor.mu.Unlock()
	if cancel, ok := a.monitor.subs[id]; ok {
		cancel()
		delete(a.monitor.subs, id)
	}
}

// follow resumes from the last recorded event of the same gatekeeper run. A new run (sandbox
// restarted) starts from its beginning; the gatekeeper itself reports gaps it cannot fill.
func (a *App) follow(ctx context.Context, s sandbox.Sandbox) {
	for ctx.Err() == nil {
		run, err := a.manager.RunID(ctx, s.ID)
		if err == nil {
			cursor := a.cursor(s.ID)
			if !strings.HasPrefix(cursor, run+":") {
				cursor = ""
			}
			_ = a.manager.Events(ctx, s.ID, cursor, func(event audit.Event) { a.record(s, event) })
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (a *App) cursor(id string) string {
	a.monitor.mu.Lock()
	cursor, known := a.monitor.lastID[id]
	a.monitor.mu.Unlock()
	if known {
		return cursor
	}
	for _, event := range a.loadTail(id, 50) {
		if event.Action != audit.ActionGap {
			cursor = event.ID
		}
	}
	a.monitor.mu.Lock()
	a.monitor.lastID[id] = cursor
	a.monitor.mu.Unlock()
	return cursor
}

func (a *App) record(s sandbox.Sandbox, event audit.Event) {
	a.monitor.mu.Lock()
	if event.Action != audit.ActionGap {
		a.monitor.lastID[s.ID] = event.ID
	}
	ring := append(a.monitor.rings[s.ID], event)
	if len(ring) > ringCapacity {
		ring = append([]audit.Event(nil), ring[len(ring)-ringCapacity:]...)
	}
	a.monitor.rings[s.ID] = ring
	a.monitor.mu.Unlock()
	if err := a.appendAudit(s.ID, event); err != nil {
		a.emit("sandbox:alert", map[string]string{"id": s.ID, "message": "审计日志写入失败：" + err.Error()})
	}
	a.emit("sandbox:event", EventView{Event: event, SandboxName: s.Name})
}

func (a *App) auditPath(id string) string {
	return filepath.Join(a.store.Dir(), "audit", id+".jsonl")
}

func (a *App) appendAudit(id string, event audit.Event) error {
	if err := os.MkdirAll(filepath.Dir(a.auditPath(id)), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(a.auditPath(id), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	return err
}

// loadTail reads the last n events of a sandbox's audit log.
func (a *App) loadTail(id string, n int) []audit.Event {
	file, err := os.Open(a.auditPath(id))
	if err != nil {
		return nil
	}
	defer file.Close()
	const window = 512 << 10
	if info, err := file.Stat(); err == nil && info.Size() > window {
		_, _ = file.Seek(info.Size()-window, io.SeekStart)
	}
	var events []audit.Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var event audit.Event
		if json.Unmarshal(bytes.TrimSpace(scanner.Bytes()), &event) == nil && event.Action != "" {
			events = append(events, event)
		}
	}
	if len(events) > n {
		events = events[len(events)-n:]
	}
	return events
}

// EventView is a gatekeeper event with the sandbox's display name.
type EventView struct {
	audit.Event
	SandboxName string `json:"sandbox_name"`
}

// RecentEvents returns recent events for one sandbox, or for all sandboxes when id is empty.
func (a *App) RecentEvents(id string) ([]EventView, error) {
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return nil, err
	}
	var result []EventView
	for _, s := range sandboxes {
		if id != "" && s.ID != id {
			continue
		}
		a.monitor.mu.Lock()
		ring := append([]audit.Event(nil), a.monitor.rings[s.ID]...)
		a.monitor.mu.Unlock()
		if len(ring) == 0 {
			ring = a.loadTail(s.ID, ringCapacity)
			a.monitor.mu.Lock()
			if len(a.monitor.rings[s.ID]) == 0 {
				a.monitor.rings[s.ID] = ring
			}
			a.monitor.mu.Unlock()
		}
		for _, event := range ring {
			result = append(result, EventView{Event: event, SandboxName: s.Name})
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Time.Before(result[j].Time) })
	if len(result) > recentLimit {
		result = result[len(result)-recentLimit:]
	}
	return result, nil
}

type SandboxUsage struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Requests    uint64                  `json:"requests"`
	Tokens      uint64                  `json:"tokens"`
	Denied      uint64                  `json:"denied"`
	MaxRequests uint64                  `json:"max_requests"`
	MaxTokens   uint64                  `json:"max_tokens"`
	Live        bool                    `json:"live"`
	Stats       *runtime.ContainerStats `json:"stats,omitempty"`
}

type ProviderUsageView struct {
	ID           string   `json:"id"`
	Requests     uint64   `json:"requests"`
	InputTokens  uint64   `json:"input_tokens"`
	OutputTokens uint64   `json:"output_tokens"`
	Sandboxes    []string `json:"sandboxes"`
}

type UsageSummary struct {
	Sandboxes []SandboxUsage               `json:"sandboxes"`
	Providers map[string]ProviderUsageView `json:"providers"`
}

// UsageSummary combines stored cumulative usage with the latest live readings.
func (a *App) UsageSummary() (UsageSummary, error) {
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return UsageSummary{}, err
	}
	summary := UsageSummary{Sandboxes: []SandboxUsage{}, Providers: map[string]ProviderUsageView{}}
	a.monitor.mu.Lock()
	defer a.monitor.mu.Unlock()
	for _, s := range sandboxes {
		row := SandboxUsage{ID: s.ID, Name: s.Name, Requests: s.Usage.Requests, Tokens: s.Usage.Tokens,
			MaxRequests: s.Limits.MaxRequests, MaxTokens: s.Limits.MaxTokens}
		if live, ok := a.monitor.live[s.ID]; ok {
			row.Live, row.Denied = true, live.Denied
			row.Requests, row.Tokens = max(row.Requests, live.QuotaRequests), max(row.Tokens, live.QuotaTokens)
		}
		if stats, ok := a.monitor.stats[s.ID]; ok {
			row.Stats = &stats
		}
		summary.Sandboxes = append(summary.Sandboxes, row)
		for id, usage := range s.ProviderUsage {
			view := summary.Providers[id]
			view.ID = id
			view.Requests += usage.Requests
			view.InputTokens += usage.InputTokens
			view.OutputTokens += usage.OutputTokens
			view.Sandboxes = append(view.Sandboxes, s.Name)
			summary.Providers[id] = view
		}
	}
	return summary, nil
}

// RevealAuditLog shows the sandbox's JSONL audit log in Finder.
func (a *App) RevealAuditLog(id string) error {
	path := a.auditPath(id)
	if _, err := os.Stat(path); err != nil {
		return errors.New("这个沙箱还没有审计日志")
	}
	return exec.Command("/usr/bin/open", "-R", path).Run()
}

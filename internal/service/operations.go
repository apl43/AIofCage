package service

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/shaoxier/AIofCage/internal/sandbox"
)

// operations remembers lifecycle operations in progress and their latest progress message, so
// every page (and a reloaded window) shows the same state, and a second click cannot start the
// same sandbox twice while its first start is still building images.
type operations struct {
	mu    sync.Mutex
	items map[string]*operation
}

type operation struct{ label, progress string }

// begin marks an operation as running; the returned func ends it.
func (o *operations) begin(id, name, label string) (func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.items == nil {
		o.items = map[string]*operation{}
	}
	if current := o.items[id]; current != nil {
		return nil, fmt.Errorf("「%s」正在%s，请等它完成", name, current.label)
	}
	o.items[id] = &operation{label: label}
	return func() {
		o.mu.Lock()
		delete(o.items, id)
		o.mu.Unlock()
	}, nil
}

func (o *operations) setProgress(id, message string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if current := o.items[id]; current != nil {
		current.progress = message
	}
}

func (o *operations) get(id string) (string, string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if current := o.items[id]; current != nil {
		return current.label, current.progress
	}
	return "", ""
}

// retiredUsageKey keeps the provider usage of deleted sandboxes, so a provider's cumulative usage
// does not drop when a sandbox is removed.
const retiredUsageKey = "retired_provider_usage"

func (a *App) retiredUsage() map[string]sandbox.ProviderUsage {
	usage := map[string]sandbox.ProviderUsage{}
	if raw := a.store.Setting(retiredUsageKey); raw != "" {
		_ = json.Unmarshal([]byte(raw), &usage)
	}
	return usage
}

func (a *App) retireUsage(sbx sandbox.Sandbox) error {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	usage := a.retiredUsage()
	for id, u := range sbx.ProviderUsage {
		total := usage[id]
		total.Requests += u.Requests
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		usage[id] = total
	}
	data, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	return a.store.SetSetting(retiredUsageKey, string(data))
}

func (a *App) forgetRetiredUsage(providerID string) {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	usage := a.retiredUsage()
	if _, ok := usage[providerID]; !ok {
		return
	}
	delete(usage, providerID)
	if data, err := json.Marshal(usage); err == nil {
		_ = a.store.SetSetting(retiredUsageKey, string(data))
	}
}

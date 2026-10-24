package gatekeeper

import (
	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/provider"
	"testing"
	"time"
)

func TestRequestKeepsPriceSnapshot(t *testing.T) {
	g, err := NewGate()
	if err != nil {
		t.Fatal(err)
	}
	p1 := &provider.Pricing{Currency: "CNY", Input: 1, Version: "v1"}
	state := &requestState{started: time.Now(), modelRequest: true, model: "m", pricing: p1}
	e := audit.Event{Action: audit.ActionRequest, UsageKind: "reported", InputTokens: 1000000}
	g.finish(state, e)
	state.pricing = &provider.Pricing{Currency: "USD", Input: 10, Version: "v2"}
	g.finish(state, e)
	events, _, _ := g.events.since(0)
	if len(events) != 2 || *events[0].EstimatedCost != 1 || events[0].Pricing.Version != "v1" || *events[1].EstimatedCost != 10 || events[1].Pricing.Currency != "USD" {
		t.Fatalf("%+v", events)
	}
	e.UsageKind = "unknown"
	g.finish(state, e)
	events, _, _ = g.events.since(2)
	if events[0].EstimatedCost != nil || events[0].CostStatus != "usage-unknown" {
		t.Fatal("未知用量被作为免费请求")
	}
}

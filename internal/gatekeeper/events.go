package gatekeeper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
)

const (
	eventCapacity  = 1000
	heartbeatEvery = 15 * time.Second
	sseWriteWindow = 30 * time.Second
)

// eventLog keeps the most recent redacted events for SSE replay. Sequence numbers are per run and
// strictly increasing; a reader that falls behind the buffer receives an explicit audit.gap.
type eventLog struct {
	mu      sync.Mutex
	runID   string
	events  []audit.Event
	seq     uint64
	changed chan struct{}
	now     func() time.Time
}

func newEventLog(runID string, now func() time.Time) *eventLog {
	return &eventLog{runID: runID, changed: make(chan struct{}), now: now}
}

func (l *eventLog) append(event audit.Event) audit.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	event.Sequence = l.seq
	event.RunID = l.runID
	event.ID = fmt.Sprintf("%s:%d", l.runID, l.seq)
	event.Source = audit.SourceGatekeeper
	if event.Time.IsZero() {
		event.Time = l.now().UTC()
	}
	l.events = append(l.events, event)
	if len(l.events) > eventCapacity {
		l.events = append([]audit.Event(nil), l.events[len(l.events)-eventCapacity:]...)
	}
	close(l.changed)
	l.changed = make(chan struct{})
	return event
}

// since returns events after cursor, whether part of the requested range was lost, and a channel
// that is closed when new events arrive.
func (l *eventLog) since(cursor uint64) ([]audit.Event, bool, chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gap := false
	if len(l.events) > 0 && cursor+1 < l.events[0].Sequence {
		gap = true
	}
	var result []audit.Event
	for _, event := range l.events {
		if event.Sequence > cursor {
			result = append(result, event)
		}
	}
	return result, gap, l.changed
}

func (l *eventLog) last() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}

// parseCursor reads "Last-Event-ID: <run_id>:<seq>". A cursor from another run means everything
// after it in that run may be lost, so the caller restarts from the beginning with a gap marker.
func parseCursor(value, runID string) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	run, seq, ok := strings.Cut(value, ":")
	if !ok || run != runID {
		return 0, true
	}
	n, err := strconv.ParseUint(seq, 10, 64)
	if err != nil {
		return 0, true
	}
	return n, false
}

func (g *Gate) serveEvents(w http.ResponseWriter, r *http.Request) {
	cursor, mismatch := parseCursor(r.Header.Get("Last-Event-ID"), g.runID)
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	write := func(payload string) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(sseWriteWindow))
		if _, err := fmt.Fprint(w, payload); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	gapEvent := func(reason string) audit.Event {
		return audit.Event{ID: fmt.Sprintf("%s:%d", g.runID, cursor), Time: g.now().UTC(), Source: audit.SourceGatekeeper,
			RunID: g.runID, Sequence: cursor, SessionID: g.sessionID(), Action: audit.ActionGap,
			Decision: audit.DecisionNotApplicable, ReasonCode: reason}
	}
	if mismatch {
		if !write(formatSSE(gapEvent("cursor_from_other_run"))) {
			return
		}
	}
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	for {
		events, gap, changed := g.events.since(cursor)
		if gap {
			if !write(formatSSE(gapEvent("buffer_overflow"))) {
				return
			}
		}
		for _, event := range events {
			if !write(formatSSE(event)) {
				return
			}
			cursor = event.Sequence
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-ticker.C:
			if !write(": heartbeat\n\n") {
				return
			}
		}
	}
}

func formatSSE(event audit.Event) string {
	data, _ := json.Marshal(event)
	return fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Action, data)
}

// waitForEvents is used by tests to avoid sleeping.
func (l *eventLog) waitFor(ctx context.Context, seq uint64) error {
	for {
		l.mu.Lock()
		current, changed := l.seq, l.changed
		l.mu.Unlock()
		if current >= seq {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

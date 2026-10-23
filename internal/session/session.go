package session

import "time"

type Agent string

const (
	Codex      Agent = "codex"
	ClaudeCode Agent = "claude-code"
	DSH        Agent = "dsh"
)

func (a Agent) Valid() bool {
	return a == Codex || a == ClaudeCode || a == DSH
}

type State string

const (
	Created  State = "created"
	Starting State = "starting"
	Running  State = "running"
	Stopped  State = "stopped"
	Failed   State = "failed"
)

// Session contains metadata only. Recording ProjectPath does not grant access.
type Session struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ProjectPath string    `json:"project_path"`
	Agent       Agent     `json:"agent"`
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
}

// Package session names the agents AIofCage can install in a sandbox.
package session

type Agent string

const (
	Codex      Agent = "codex"
	ClaudeCode Agent = "claude-code"
	DSH        Agent = "dsh"
)

func (a Agent) Valid() bool {
	return a == Codex || a == ClaudeCode || a == DSH
}

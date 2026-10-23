// Package service exposes desktop use cases without exposing credentials or process execution.
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/buildinfo"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/session"
)

type DirectoryPicker func() (string, error)

type App struct {
	sessions session.Store
	runtime  runtime.Backend
	picker   DirectoryPicker
}

func NewApp(store session.Store, backend runtime.Backend, picker DirectoryPicker) *App {
	return &App{sessions: store, runtime: backend, picker: picker}
}

type Agent struct {
	ID     session.Agent `json:"id"`
	Name   string        `json:"name"`
	Status string        `json:"status"`
}

type Overview struct {
	Version     string         `json:"version"`
	Stage       string         `json:"stage"`
	Environment runtime.Report `json:"environment"`
	Agents      []Agent        `json:"agents"`
}

func (a *App) GetOverview() Overview {
	return Overview{
		Version: buildinfo.Version, Stage: buildinfo.Stage,
		Environment: a.runtime.Inspect(context.Background()),
		Agents: []Agent{
			{ID: session.Codex, Name: "Codex", Status: "未验证"},
			{ID: session.ClaudeCode, Name: "Claude Code", Status: "未验证"},
			{ID: session.DSH, Name: "dsh", Status: "未验证"},
		},
	}
}

func (a *App) SelectProject() (string, error) {
	if a.picker == nil {
		return "", errors.New("项目选择器不可用，请在桌面应用中操作")
	}
	return a.picker()
}

func (a *App) ListSessions() []session.Session { return a.sessions.List() }

type CreateSessionRequest struct {
	Name        string        `json:"name"`
	ProjectPath string        `json:"project_path"`
	Agent       session.Agent `json:"agent"`
}

// CreateSession only records an in-memory draft; import and mounting are not implemented.
func (a *App) CreateSession(request CreateSessionRequest) (session.Session, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" || len([]rune(name)) > 80 {
		return session.Session{}, errors.New("会话名称须为 1 至 80 个字符")
	}
	if !filepath.IsAbs(request.ProjectPath) || strings.ContainsRune(request.ProjectPath, '\x00') {
		return session.Session{}, errors.New("请选择有效的项目绝对路径")
	}
	if !request.Agent.Valid() {
		return session.Session{}, errors.New("请选择列表中的 Agent")
	}
	item := session.Session{
		ID: rand.Text(), Name: name, ProjectPath: filepath.Clean(request.ProjectPath),
		Agent: request.Agent, State: session.Created, CreatedAt: time.Now().UTC(),
	}
	if err := a.sessions.Create(item); err != nil {
		return session.Session{}, err
	}
	return item, nil
}

func (a *App) StartSession(id string) error {
	if _, err := a.sessions.Get(id); err != nil {
		return err
	}
	// The lifecycle coordinator will replace this stub once reconciliation and audit exist.
	return runtime.ErrNotReady
}

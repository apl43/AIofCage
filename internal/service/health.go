package service

import (
	"errors"
	"github.com/shaoxier/AIofCage/internal/runtime"
)

type HealthView struct {
	Current runtime.SandboxHealth    `json:"current"`
	History []runtime.ContainerStats `json:"history"`
}

func (a *App) SandboxHealth(id string) (HealthView, error) {
	if _, err := a.store.Sandbox(id); err != nil {
		return HealthView{}, err
	}
	a.monitor.mu.Lock()
	defer a.monitor.mu.Unlock()
	current, ok := a.monitor.health[id]
	if !ok {
		return HealthView{}, errors.New("正在采集运行状态，请稍后刷新")
	}
	return HealthView{Current: current, History: append([]runtime.ContainerStats{}, a.monitor.history[id]...)}, nil
}

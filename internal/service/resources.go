package service

import (
	"context"
	"time"

	"github.com/shaoxier/AIofCage/internal/runtime"
)

func (a *App) EnvironmentResources() (runtime.ResourceSettings, error) {
	if err := a.runtimeReady(); err != nil {
		return runtime.ResourceSettings{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.manager.Environment().ResourceSettings(ctx), nil
}

func (a *App) SaveEnvironmentResources(config runtime.ResourceConfig) error {
	if err := a.runtimeReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.manager.Environment().SaveResources(ctx, config)
}

func (a *App) SuspendEnvironment() error {
	if err := a.runtimeReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return a.manager.SuspendEnvironment(ctx)
}

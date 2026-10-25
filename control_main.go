package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/shaoxier/AIofCage/internal/service"
)

func runControl() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return service.RunAgentControl(ctx, os.Stdin, os.Stdout)
}

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/shaoxier/AIofCage/internal/gatekeeper"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gatekeeper:", err)
		os.Exit(1)
	}
}

func run() error {
	var addresses gatekeeper.Addresses
	flag.StringVar(&addresses.Management, "management-listen", "127.0.0.1:8899", "管理接口 IP:端口")
	flag.StringVar(&addresses.Proxy, "proxy-listen", "127.0.0.1:8888", "代理拒绝入口 IP:端口")
	flag.StringVar(&addresses.MCP, "mcp-listen", "127.0.0.1:8890", "MCP 拒绝入口 IP:端口")
	flag.Parse()
	handlers, err := gatekeeper.NewHandlers(os.Getenv("CAGE_ADMIN_TOKEN"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// No request logging, credential loading, configuration activation, or forwarding yet.
	return gatekeeper.Serve(ctx, addresses, handlers)
}

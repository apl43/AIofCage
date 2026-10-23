package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
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
	var tokenFromStdin bool
	flag.StringVar(&addresses.Management, "management-listen", "127.0.0.1:8899", "管理接口 IP:端口")
	flag.StringVar(&addresses.Proxy, "proxy-listen", "127.0.0.1:8888", "代理入口 IP:端口")
	flag.StringVar(&addresses.MCP, "mcp-listen", "127.0.0.1:8890", "MCP 入口 IP:端口（尚未实现，始终拒绝）")
	flag.BoolVar(&tokenFromStdin, "token-stdin", false, "从标准输入第一行读取管理 token（容器内使用，避免出现在 docker inspect 中）")
	flag.Parse()
	token := os.Getenv("CAGE_ADMIN_TOKEN")
	if tokenFromStdin {
		line, err := bufio.NewReader(io.LimitReader(os.Stdin, 1024)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return errors.New("无法从标准输入读取管理 token")
		}
		token = strings.TrimSpace(line)
	}
	handlers, err := gatekeeper.NewHandlers(token)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The proxy denies everything until the desktop app delivers a configuration via PUT /config.
	// Nothing is logged here: events are only available through the authenticated SSE stream.
	return gatekeeper.Serve(ctx, addresses, handlers)
}

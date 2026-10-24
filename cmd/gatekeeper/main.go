package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
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
	var tokenOut string
	flag.StringVar(&addresses.Management, "management-listen", "127.0.0.1:8899", "管理接口 IP:端口")
	flag.StringVar(&addresses.Proxy, "proxy-listen", "127.0.0.1:8888", "代理入口 IP:端口")
	flag.StringVar(&addresses.MCP, "mcp-listen", "127.0.0.1:8890", "MCP 入口 IP:端口（尚未实现，始终拒绝）")
	flag.BoolVar(&tokenFromStdin, "token-stdin", false, "从标准输入第一行读取管理 token（避免出现在 docker inspect 中）")
	flag.StringVar(&tokenOut, "token-out", "", "自行生成管理 token 并写入该文件（应位于只有门卫可读的 tmpfs；宿主用 docker exec 读取）")
	flag.Parse()
	token := os.Getenv("CAGE_ADMIN_TOKEN")
	if tokenOut != "" {
		generated, err := writeToken(tokenOut)
		if err != nil {
			return err
		}
		token = generated
	} else if tokenFromStdin {
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

// writeToken creates a fresh random token in a new file readable only by the gatekeeper user.
func writeToken(path string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return "", errors.New("无法写入管理 token 文件（文件已存在或目录不可写）")
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		file.Close()
		return "", err
	}
	return token, file.Close()
}

// Command aiofcage-connector runs in a trusted connector container next to a sandbox. It holds
// the secret of one MCP service (e.g. a database password) and serves that service's stdio MCP
// server over streamable HTTP to the sandbox's gatekeeper only.
//
//	aiofcage-connector serve --listen 0.0.0.0:8080 --launch /run/aiofcage/launch.json
//	aiofcage-connector deliver /run/aiofcage/launch.json   # launch JSON on stdin (docker exec -i)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shaoxier/AIofCage/internal/connector"
)

func main() {
	var err error
	switch {
	case len(os.Args) > 1 && os.Args[1] == "serve":
		err = serve(os.Args[2:])
	case len(os.Args) == 3 && os.Args[1] == "deliver":
		err = deliver(os.Args[2], os.Stdin)
	default:
		err = errors.New("用法：aiofcage-connector serve --listen 地址 --launch 文件 | deliver 文件")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "connector:", err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := flags.String("listen", "0.0.0.0:8080", "监听地址（容器只接入沙箱的出网网络，只有门卫能访问）")
	launchPath := flags.String("launch", "/run/aiofcage/launch.json", "宿主投递启动配置的文件（tmpfs，读取后删除）")
	if err := flags.Parse(args); err != nil {
		return err
	}
	logger := log.New(os.Stderr, "connector: ", log.LstdFlags)
	server := connector.NewServer(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go server.Reap(ctx)
	go watchLaunch(ctx, *launchPath, server, logger)
	httpServer := &http.Server{Addr: *listen, Handler: server, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 32 << 10}
	errs := make(chan error, 1)
	go func() { errs <- httpServer.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-errs:
		server.Close()
		return err
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := httpServer.Shutdown(shutdown)
	server.Close()
	return err
}

// watchLaunch picks up launch files delivered by the desktop app. Each file is removed as soon as
// it is read, so the secret stays only in this process and its MCP server processes.
func watchLaunch(ctx context.Context, path string, server *connector.Server, logger *log.Logger) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		_ = os.Remove(path)
		var launch connector.Launch
		if err := json.Unmarshal(data, &launch); err != nil {
			logger.Print("启动配置格式错误")
			continue
		}
		if err := server.SetLaunch(launch); err != nil {
			logger.Print("启动配置无效：", err)
			continue
		}
		logger.Print("已加载启动配置")
	}
}

// deliver writes the launch JSON from stdin atomically, readable only by the connector user, and
// waits until the serving process has taken it, so the sandbox is never started before its
// connector can answer.
func deliver(path string, stdin io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return err
	}
	var launch connector.Launch
	if err := json.Unmarshal(data, &launch); err != nil {
		return errors.New("启动配置不是有效的 JSON")
	}
	if err := launch.Validate(); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".launch-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	_ = os.Remove(path)
	return errors.New("连接器没有加载启动配置（服务进程未运行？）")
}

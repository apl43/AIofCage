package gatekeeper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

type Addresses struct {
	Management string
	Proxy      string
	MCP        string
}

// Serve binds all three listeners before serving; failure leaves no partially running service.
// Callers must select concrete addresses on the appropriate network interfaces.
func Serve(ctx context.Context, addresses Addresses, handlers Handlers) error {
	entries := []struct {
		name    string
		address string
		handler http.Handler
	}{
		{"management", addresses.Management, handlers.Management},
		{"proxy", addresses.Proxy, handlers.Proxy},
		{"mcp", addresses.MCP, handlers.MCP},
	}
	for _, entry := range entries {
		if err := validateAddress(entry.address); err != nil {
			return fmt.Errorf("%s: %w", entry.name, err)
		}
		if entry.handler == nil {
			return fmt.Errorf("%s handler 未配置", entry.name)
		}
	}
	listeners := make([]net.Listener, 0, len(entries))
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for _, entry := range entries {
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", entry.address)
		if err != nil {
			return fmt.Errorf("%s 监听失败: %w", entry.name, err)
		}
		listeners = append(listeners, listener)
	}
	servers := make([]*http.Server, 0, len(entries))
	errorsCh := make(chan error, len(entries))
	for i, entry := range entries {
		server := &http.Server{
			Handler: entry.handler, ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
			IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
		servers = append(servers, server)
		go func() { errorsCh <- server.Serve(listeners[i]) }()
	}
	var result error
	select {
	case <-ctx.Done():
	case result = <-errorsCh:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, server := range servers {
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			result = errors.Join(result, err)
		}
	}
	return result
}

func validateAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("监听地址须为具体 IP:端口")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" {
		return errors.New("禁止通配或域名监听，须显式选择接口 IP")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return errors.New("监听端口须为 1 至 65535")
	}
	return nil
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	proxyIP := flag.String("proxy-ip", "", "Agent 内网接口的 IPv4 地址")
	adminIP := flag.String("admin-ip", "", "出网接口的 IPv4 地址")
	flag.Parse()
	if net.ParseIP(*proxyIP) == nil || net.ParseIP(*adminIP) == nil ||
		*proxyIP == *adminIP || net.ParseIP(*proxyIP).IsUnspecified() || net.ParseIP(*adminIP).IsUnspecified() {
		fmt.Fprintln(os.Stderr, "必须指定两个不同的具体接口地址")
		os.Exit(1)
	}
	var config Bootstrap
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 65536))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		fmt.Fprintln(os.Stderr, "bootstrap 无效")
		os.Exit(1)
	}
	g, err := NewGate(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap 校验失败")
		os.Exit(1)
	}
	proxyListener, err := net.Listen("tcp4", net.JoinHostPort(*proxyIP, "8888"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "代理监听失败")
		os.Exit(1)
	}
	adminListener, err := net.Listen("tcp4", net.JoinHostPort(*adminIP, "8899"))
	if err != nil {
		proxyListener.Close()
		fmt.Fprintln(os.Stderr, "管理监听失败")
		os.Exit(1)
	}
	admin := &http.Server{Handler: g.management(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16384}
	proxy := &http.Server{Handler: g.proxy, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32768}
	errors := make(chan error, 2)
	go func() { errors <- admin.Serve(adminListener) }()
	go func() { errors <- proxy.Serve(proxyListener) }()
	<-errors
	admin.Close()
	proxy.Close()
	fmt.Fprintln(os.Stderr, "监听停止，实验退出")
	os.Exit(1)
}

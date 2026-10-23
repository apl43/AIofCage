package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
)

// Bootstrap 只从 stdin 接收；不能序列化回管理接口或日志。
type Bootstrap struct {
	AdminToken  string   `json:"admin_token"`
	Secret      string   `json:"secret"`
	Placeholder string   `json:"placeholder"`
	Target      string   `json:"target"`
	Paths       []string `json:"paths"`
	MaxRequests int      `json:"max_requests"`
}

type Event struct {
	Seq      uint64 `json:"seq"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Path     string `json:"path,omitempty"`
	Status   int    `json:"status,omitempty"`
}

type Gate struct {
	config Bootstrap
	proxy  *goproxy.ProxyHttpServer
	caPEM  []byte
	mu     sync.Mutex
	events []Event
	seq    uint64
	used   int
}

func ownCA() (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	template := &x509.Certificate{SerialNumber: serial,
		Subject:   pkix.Name{CommonName: "AIofCage disposable validation CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf},
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), err
}

var forbiddenPrefixes = func() []netip.Prefix {
	var result []netip.Prefix
	for _, value := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32",
		"2002::/16", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.Zone() != "" {
		return false
	}
	for _, prefix := range forbiddenPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// 每次建连接重新解析、检查所有答案，并直接拨已检查的 IP，避免检查后再次解析。
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, errors.New("destination-denied")
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("dns-failed")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("destination-denied")
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("upstream-unavailable")
}

func NewGate(config Bootstrap) (*Gate, error) {
	if len(config.AdminToken) < 43 || config.Secret == "" ||
		strings.ContainsAny(config.Secret, "\r\n") || !strings.HasPrefix(config.Placeholder, "cage-") ||
		len(config.Placeholder) < 20 || strings.ContainsAny(config.Placeholder, "\r\n ") ||
		config.Secret == config.Placeholder || config.MaxRequests < 1 || config.MaxRequests > 100 {
		return nil, errors.New("invalid-bootstrap")
	}
	if config.Target != strings.ToLower(config.Target) || strings.ContainsAny(config.Target, "/:@%?# ") ||
		!strings.Contains(config.Target, ".") || strings.HasSuffix(config.Target, ".") {
		return nil, errors.New("invalid-target")
	}
	if _, err := netip.ParseAddr(config.Target); err == nil {
		return nil, errors.New("ip-target-denied")
	}
	if len(config.Paths) == 0 || len(config.Paths) > 8 {
		return nil, errors.New("invalid-paths")
	}
	for _, p := range config.Paths {
		if !strings.HasPrefix(p, "/") || path.Clean(p) != p || strings.ContainsAny(p, "%?#\\") {
			return nil, errors.New("invalid-path")
		}
	}
	ca, cert, err := ownCA()
	if err != nil {
		return nil, err
	}
	g := &Gate{config: config, caPEM: cert}
	p := goproxy.NewProxyHttpServer()
	p.Logger = log.New(io.Discard, "", 0) // goproxy 原始错误可能包含请求 URL。
	p.Tr = &http.Transport{Proxy: nil, DialContext: publicDial,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 45 * time.Second,
		IdleConnTimeout: 30 * time.Second, MaxIdleConnsPerHost: 2}
	p.ConnectDial = nil
	p.ConnectDialWithReq = nil
	mitm := &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&ca)}
	p.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if host != config.Target+":443" {
			ctx.Resp = g.reject(ctx.Req, "connect-denied", http.StatusForbidden)
			return goproxy.RejectConnect, host
		}
		ctx.UserData = host
		ctx.RoundTripper = goproxy.RoundTripperFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Response, error) {
			resp, err := p.Tr.RoundTrip(req)
			if err != nil {
				return g.reject(req, "upstream-failed", http.StatusBadGateway), nil
			}
			if resp.StatusCode >= 400 {
				resp.Body.Close()
				return g.reject(req, "upstream-status", resp.StatusCode), nil
			}
			return resp, nil
		})
		return mitm, host
	})
	p.OnRequest().DoFunc(g.request)
	p.OnResponse().DoFunc(g.response)
	g.proxy = p
	return g, nil
}

func (g *Gate) knownPath(req *http.Request) string {
	if req != nil && req.URL != nil {
		for _, p := range g.config.Paths {
			if req.URL.Path == p {
				return p
			}
		}
	}
	return ""
}

func (g *Gate) event(decision, reason, p string, status int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	g.events = append(g.events, Event{g.seq, decision, reason, p, status})
	if len(g.events) > 256 {
		g.events = g.events[len(g.events)-256:]
	}
}

func (g *Gate) reject(req *http.Request, reason string, status int) *http.Response {
	g.event("deny", reason, g.knownPath(req), status)
	return goproxy.NewResponse(req, "text/plain", status, "AIofCage validation: "+reason+"\n")
}

func (g *Gate) request(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	u := req.URL
	// 不提供明文代理、任意目标、管理转发、查询串或模糊路径。
	if ctx.UserData != g.config.Target+":443" || u == nil || u.Scheme != "https" ||
		u.Hostname() != g.config.Target || (u.Port() != "" && u.Port() != "443") ||
		(req.Host != g.config.Target && req.Host != g.config.Target+":443") ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.RawPath != "" || strings.ContainsAny(u.Path, "%\\") || path.Clean(u.Path) != u.Path {
		return req, g.reject(req, "target-denied", http.StatusForbidden)
	}
	if req.Method != http.MethodPost || g.knownPath(req) == "" {
		return req, g.reject(req, "operation-denied", http.StatusForbidden)
	}
	if req.ContentLength < 0 || req.ContentLength > 2<<20 {
		return req, g.reject(req, "body-size-denied", http.StatusRequestEntityTooLarge)
	}
	values := req.Header.Values("Authorization")
	if len(values) != 1 || values[0] != "Bearer "+g.config.Placeholder ||
		len(req.Header.Values("X-Api-Key")) != 0 || len(req.Header.Values("Cookie")) != 0 {
		return req, g.reject(req, "credential-denied", http.StatusForbidden)
	}
	// 不允许通过 Connection 提名授权字段，影响代理清理顺序。
	for _, v := range req.Header.Values("Connection") {
		if !strings.EqualFold(v, "close") && !strings.EqualFold(v, "keep-alive") {
			return req, g.reject(req, "connection-denied", http.StatusForbidden)
		}
	}
	g.mu.Lock()
	if g.used >= g.config.MaxRequests {
		g.mu.Unlock()
		return req, g.reject(req, "request-cap", http.StatusTooManyRequests)
	}
	g.used++
	g.mu.Unlock()
	for key := range req.Header {
		lower := strings.ToLower(key)
		if lower == "proxy-authorization" || lower == "forwarded" || strings.HasPrefix(lower, "x-forwarded-") {
			req.Header.Del(key)
		}
	}
	req.Header.Set("Authorization", "Bearer "+g.config.Secret)
	g.event("inject", "authorized", g.knownPath(req), 0)
	return req, nil
}

func (g *Gate) response(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp == nil {
		return g.reject(ctx.Req, "upstream-failed", http.StatusBadGateway)
	}
	// 实验保守拒绝全部重定向，不把 Location 或上游错误正文返回 Agent。
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return g.reject(ctx.Req, "redirect-denied", http.StatusForbidden)
	}
	// 成功响应仍按上游协议流式转发；上游属于可信边界，不能承诺恶意上游不反射秘密。
	return resp
}

func (g *Gate) management() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte("Bearer "+g.config.AdminToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Origin") != "" {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ready":true,"experiment":true}`)
		case "/ca":
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Write(g.caPEM)
		case "/events":
			g.mu.Lock()
			defer g.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Events  []Event `json:"events"`
				LastSeq uint64  `json:"last_seq"`
				Used    int     `json:"used"`
			}{g.events, g.seq, g.used})
		default:
			http.NotFound(w, r)
		}
	})
}

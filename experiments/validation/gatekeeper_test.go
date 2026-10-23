package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig() Bootstrap {
	return Bootstrap{AdminToken: strings.Repeat("a", 48), Secret: "synthetic-upstream-secret",
		Placeholder: "cage-synthetic-placeholder", Target: "model.example",
		Paths: []string{"/v1/responses"}, MaxRequests: 50}
}

type fixture struct {
	gate   *Gate
	client *http.Client
	proxy  *httptest.Server
	roots  *x509.CertPool
	calls  atomic.Int32
}

func setup(t *testing.T, trust bool, handler http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{}
	ca, _, err := ownCA()
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"model.example"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca.Leaf, &key.PublicKey, ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		handler(w, r)
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	upstream.StartTLS()
	t.Cleanup(upstream.Close)
	f.gate, err = NewGate(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	// 唯一测试注入点：把已经校验的 model.example:443 映射到本地 TLS fixture。
	// 可执行程序不暴露此功能，也不提供关闭 TLS 验证的开关。
	f.gate.proxy.Tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "model.example:443" {
			return nil, fmt.Errorf("unexpected destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	if trust {
		roots := x509.NewCertPool()
		roots.AddCert(ca.Leaf)
		f.gate.proxy.Tr.TLSClientConfig.RootCAs = roots
	}
	f.proxy = httptest.NewServer(f.gate.proxy)
	t.Cleanup(f.proxy.Close)
	u, _ := url.Parse(f.proxy.URL)
	f.roots = x509.NewCertPool()
	f.roots.AppendCertsFromPEM(f.gate.caPEM)
	transport := &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{RootCAs: f.roots}}
	f.client = &http.Client{Transport: transport, Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(transport.CloseIdleConnections)
	t.Cleanup(f.gate.proxy.Tr.CloseIdleConnections)
	return f
}

func (f *fixture) request(t *testing.T, method, address string, auth []string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, address, strings.NewReader(`{"input":"cage-synthetic-placeholder"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range auth {
		req.Header.Add("Authorization", value)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func TestInjectionPreservesBodyAndStripsProxyHeaders(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	f := setup(t, true, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-upstream-secret" {
			t.Error("missing injection")
		}
		for _, key := range []string{"Proxy-Authorization", "X-Forwarded-For", "Forwarded"} {
			if r.Header.Get(key) != "" {
				t.Errorf("unstripped %s", key)
			}
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"input":"cage-synthetic-placeholder"}` {
			t.Error("body was changed")
		}
		io.WriteString(w, "ok")
	})
	req, _ := http.NewRequest("POST", "https://model.example/v1/responses", strings.NewReader(`{"input":"cage-synthetic-placeholder"}`))
	req.Header.Set("Authorization", "Bearer cage-synthetic-placeholder")
	req.Header.Set("Proxy-Authorization", "must-not-forward")
	req.Header.Set("X-Forwarded-For", "must-not-forward")
	req.Header.Set("Forwarded", "must-not-forward")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || f.calls.Load() != 1 {
		t.Fatal("authorized request failed")
	}
	if f.gate.proxy.Tr.Proxy != nil || f.gate.proxy.ConnectDial != nil {
		t.Fatal("environment proxy inherited")
	}
}

func TestDeniedRequestsNeverReachUpstream(t *testing.T) {
	f := setup(t, true, func(w http.ResponseWriter, r *http.Request) { t.Error("denied request forwarded") })
	valid := []string{"Bearer cage-synthetic-placeholder"}
	for _, tc := range []struct {
		name, method, address string
		auth                  []string
	}{
		{"method", "GET", "https://model.example/v1/responses", valid},
		{"path", "POST", "https://model.example/v1/files", valid},
		{"missing", "POST", "https://model.example/v1/responses", nil},
		{"foreign", "POST", "https://model.example/v1/responses", []string{"Bearer foreign-account"}},
		{"duplicate", "POST", "https://model.example/v1/responses", append(valid, valid...)},
		{"query", "POST", "https://model.example/v1/responses?token=hidden", valid},
		{"empty-query", "POST", "https://model.example/v1/responses?", valid},
		{"encoded", "POST", "https://model.example/v1/%72esponses", valid},
		{"dot-path", "POST", "https://model.example/v1/x/../responses", valid},
		{"http", "POST", "http://model.example/v1/responses", valid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := f.request(t, tc.method, tc.address, tc.auth)
			if status != 403 {
				t.Fatalf("status = %d", status)
			}
			if strings.Contains(body, "hidden") || strings.Contains(body, "foreign-account") {
				t.Fatal("error leaked input")
			}
		})
	}
	if f.calls.Load() != 0 {
		t.Fatal("denied request reached upstream")
	}
}

func TestConnectDestinationAndTunnelHostCannotBeChanged(t *testing.T) {
	f := setup(t, true, func(w http.ResponseWriter, r *http.Request) { t.Error("forwarded") })
	for _, target := range []string{"127.0.0.1:8899", "169.254.169.254:443", "model.example:8899", "evil.example:443"} {
		conn, err := net.Dial("tcp", f.proxy.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 403 {
			t.Errorf("CONNECT %s status %d", target, resp.StatusCode)
		}
		conn.Close()
	}
	conn, err := net.Dial("tcp", f.proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprint(conn, "CONNECT model.example:443 HTTP/1.1\r\nHost: model.example:443\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("CONNECT failed", err)
	}
	tunnel := tls.Client(conn, &tls.Config{ServerName: "model.example", RootCAs: f.roots})
	defer tunnel.Close()
	fmt.Fprint(tunnel, "POST /v1/responses HTTP/1.1\r\nHost: evil.example\r\nAuthorization: Bearer cage-synthetic-placeholder\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	resp, err = http.ReadResponse(bufio.NewReader(tunnel), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 || f.calls.Load() != 0 {
		t.Fatal("tunnel authority bypass")
	}
}

func TestUntrustedUpstreamCertificateFailsClosed(t *testing.T) {
	f := setup(t, false, func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted upstream received request") })
	status, body := f.request(t, "POST", "https://model.example/v1/responses", []string{"Bearer cage-synthetic-placeholder"})
	if status != 502 || strings.Contains(body, "synthetic-upstream-secret") {
		t.Fatalf("status %d; unsafe error", status)
	}
	if f.calls.Load() != 0 {
		t.Fatal("TLS validation was skipped")
	}
}

func TestRedirectIsRejectedAndNotFollowed(t *testing.T) {
	f := setup(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://evil.example/?token=synthetic-upstream-secret")
		w.WriteHeader(302)
	})
	status, body := f.request(t, "POST", "https://model.example/v1/responses", []string{"Bearer cage-synthetic-placeholder"})
	if status != 403 || strings.Contains(body, "synthetic-upstream-secret") || f.calls.Load() != 1 {
		t.Fatal("unsafe redirect")
	}
}

func TestSSEStreamsBeforeUpstreamCompletes(t *testing.T) {
	release := make(chan struct{})
	f := setup(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "data: last\n\n")
	})
	defer close(release)
	req, _ := http.NewRequest("POST", "https://model.example/v1/responses", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer cage-synthetic-placeholder")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatal("stream buffered", err)
	}
}

func TestConcurrentRequestCap(t *testing.T) {
	f := setup(t, true, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	f.gate.config.MaxRequests = 5
	var wg sync.WaitGroup
	var ok, capped atomic.Int32
	for range 20 {
		wg.Go(func() {
			status, _ := f.request(t, "POST", "https://model.example/v1/responses", []string{"Bearer cage-synthetic-placeholder"})
			switch status {
			case 200:
				ok.Add(1)
			case 429:
				capped.Add(1)
			default:
				t.Errorf("status %d", status)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 5 || capped.Load() != 15 || f.calls.Load() != 5 {
		t.Fatal("concurrent cap bypass")
	}
}

func TestManagementAuthenticationAndEventRedaction(t *testing.T) {
	f := setup(t, true, func(w http.ResponseWriter, r *http.Request) {})
	f.request(t, "POST", "https://model.example/secret-in-path?query=secret-in-query", []string{"Bearer credential-must-not-log"})
	for _, auth := range []string{"", "Bearer cage-synthetic-placeholder", "Bearer synthetic-upstream-secret"} {
		r := httptest.NewRequest("GET", "/events", nil)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		f.gate.management().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("management auth bypass")
		}
	}
	r := httptest.NewRequest("GET", "/events", nil)
	r.Header.Set("Authorization", "Bearer "+testConfig().AdminToken)
	w := httptest.NewRecorder()
	f.gate.management().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("management unavailable")
	}
	for _, secret := range []string{"secret-in-path", "secret-in-query", "credential-must-not-log", testConfig().Secret, testConfig().AdminToken, testConfig().Placeholder} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("event leaked input")
		}
	}
}

func TestSpecialUseAddressesDenied(t *testing.T) {
	for _, value := range []string{"0.0.0.0", "10.1.1.1", "127.0.0.1", "100.64.0.1", "169.254.169.254", "172.17.0.1", "192.168.1.1", "198.18.0.1", "192.0.2.1", "224.0.0.1", "255.255.255.255", "::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "fc00::1", "fe80::1", "2002:7f00:1::", "2001:db8::1"} {
		if publicIP(netip.MustParseAddr(value)) {
			t.Errorf("allowed %s", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(value)) {
			t.Errorf("denied %s", value)
		}
	}
}

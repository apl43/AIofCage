// Package oauth runs MCP OAuth on the trusted host: discovery, dynamic client registration,
// browser authorization with PKCE, and serialized refresh. Credentials live in the vault; the
// gatekeeper only ever receives a short-lived access token, and the sandbox only a placeholder.
//
// It follows the MCP authorization spec: protected resource metadata (RFC 9728), authorization
// server metadata (RFC 8414), dynamic client registration (RFC 7591), PKCE S256 (RFC 7636) and
// resource indicators (RFC 8707), with a loopback redirect (RFC 8252).
package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Credentials are stored as one JSON vault item per service.
type Credentials struct {
	ClientID      string    `json:"client_id"`
	ClientSecret  string    `json:"client_secret,omitempty"`
	TokenEndpoint string    `json:"token_endpoint"`
	Resource      string    `json:"resource,omitempty"`
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	Scope         string    `json:"scope,omitempty"`
}

// Fresh reports whether the access token stays valid for at least margin.
func (c Credentials) Fresh(now time.Time, margin time.Duration) bool {
	return c.AccessToken != "" && (c.ExpiresAt.IsZero() || now.Add(margin).Before(c.ExpiresAt))
}

// Usable reports whether a request can be authorized now or after a refresh.
func (c Credentials) Usable(now time.Time) bool {
	return c.Fresh(now, 0) || c.RefreshToken != ""
}

func Parse(data []byte) (Credentials, error) {
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil || c.ClientID == "" || c.TokenEndpoint == "" {
		return Credentials{}, errors.New("授权信息损坏，请重新登录")
	}
	return c, nil
}

// Metadata is what discovery found for one MCP server.
type Metadata struct {
	Resource              string   `json:"resource"`
	Scopes                []string `json:"scopes"`
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
}

type Client struct {
	HTTP        *http.Client
	Now         func() time.Time
	OpenBrowser func(string) error
	// LoginTimeout bounds how long Login waits for the user in the browser.
	LoginTimeout time.Duration
	// allowHTTP permits http:// endpoints; tests only.
	allowHTTP bool
}

func NewClient(httpClient *http.Client, openBrowser func(string) error) *Client {
	return &Client{HTTP: httpClient, Now: time.Now, OpenBrowser: openBrowser, LoginTimeout: 5 * time.Minute}
}

const maxResponse = 1 << 20

// Discover finds the authorization server of an MCP server: first through its protected resource
// metadata (path-specific, then root), falling back to the MCP server's own origin.
func (c *Client) Discover(ctx context.Context, mcpURL string) (Metadata, error) {
	u, err := c.parse(mcpURL)
	if err != nil {
		return Metadata{}, err
	}
	origin := u.Scheme + "://" + u.Host
	meta := Metadata{Resource: mcpURL}
	issuer := origin
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	candidates := []string{origin + "/.well-known/oauth-protected-resource"}
	if path != "" {
		candidates = append([]string{origin + "/.well-known/oauth-protected-resource" + path}, candidates...)
	}
	for _, candidate := range candidates {
		var resource struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			Scopes               []string `json:"scopes_supported"`
		}
		if c.getJSON(ctx, candidate, &resource) != nil || len(resource.AuthorizationServers) == 0 {
			continue
		}
		if resource.Resource != "" {
			meta.Resource = resource.Resource
		}
		meta.Scopes, issuer = resource.Scopes, resource.AuthorizationServers[0]
		break
	}
	if _, err := c.parse(issuer); err != nil {
		return Metadata{}, errors.New("授权服务器地址无效")
	}
	issuerURL, _ := url.Parse(issuer)
	issuerPath := strings.TrimSuffix(issuerURL.EscapedPath(), "/")
	issuerOrigin := issuerURL.Scheme + "://" + issuerURL.Host
	for _, candidate := range []string{
		issuerOrigin + "/.well-known/oauth-authorization-server" + issuerPath,
		issuerOrigin + "/.well-known/openid-configuration" + issuerPath,
		strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration",
	} {
		var server Metadata
		if c.getJSON(ctx, candidate, &server) != nil || server.AuthorizationEndpoint == "" || server.TokenEndpoint == "" {
			continue
		}
		for _, endpoint := range []string{server.AuthorizationEndpoint, server.TokenEndpoint} {
			if _, err := c.parse(endpoint); err != nil {
				return Metadata{}, errors.New("授权服务器返回了无效的地址")
			}
		}
		if server.RegistrationEndpoint != "" {
			if _, err := c.parse(server.RegistrationEndpoint); err != nil {
				return Metadata{}, errors.New("授权服务器返回了无效的地址")
			}
		}
		meta.Issuer, meta.AuthorizationEndpoint = issuer, server.AuthorizationEndpoint
		meta.TokenEndpoint, meta.RegistrationEndpoint = server.TokenEndpoint, server.RegistrationEndpoint
		return meta, nil
	}
	return Metadata{}, errors.New("这个 MCP 服务没有公开 OAuth 授权信息，可能需要改用令牌方式")
}

// Login registers a client, opens the browser for the user to approve, receives the code on a
// loopback address and exchanges it. It blocks until done, cancelled or timed out.
func (c *Client) Login(ctx context.Context, mcpURL string) (Credentials, error) {
	meta, err := c.Discover(ctx, mcpURL)
	if err != nil {
		return Credentials{}, err
	}
	if meta.RegistrationEndpoint == "" {
		return Credentials{}, errors.New("这个 MCP 服务不支持自动注册客户端，暂时无法用 OAuth 登录")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Credentials{}, err
	}
	defer listener.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	clientID, clientSecret, err := c.register(ctx, meta, redirect)
	if err != nil {
		return Credentials{}, err
	}
	verifier, state := randomString(32), randomString(24)
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
		"state": {state}, "resource": {meta.Resource}}
	if len(meta.Scopes) > 0 {
		query.Set("scope", strings.Join(meta.Scopes, " "))
	}
	authorize := meta.AuthorizationEndpoint
	if strings.Contains(authorize, "?") {
		authorize += "&" + query.Encode()
	} else {
		authorize += "?" + query.Encode()
	}

	type callback struct{ code, err string }
	results := make(chan callback, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		result := callback{code: values.Get("code")}
		switch {
		case values.Get("state") != state:
			result = callback{err: "授权回调的 state 不匹配，已忽略"}
		case values.Get("error") != "":
			result = callback{err: "授权被拒绝或失败：" + values.Get("error")}
		case result.code == "":
			result = callback{err: "授权回调缺少 code"}
		}
		message := "授权完成，可以关闭这个页面，回到 AIofCage。"
		if result.err != "" {
			message = result.err
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>AIofCage</title><p style="font:16px -apple-system;margin:3em">%s</p>`, html.EscapeString(message))
		once.Do(func() { results <- result })
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	if err := c.OpenBrowser(authorize); err != nil {
		return Credentials{}, fmt.Errorf("无法打开浏览器：%w", err)
	}
	timer := time.NewTimer(c.LoginTimeout)
	defer timer.Stop()
	var result callback
	select {
	case result = <-results:
	case <-timer.C:
		return Credentials{}, errors.New("等待浏览器授权超时，请重试")
	case <-ctx.Done():
		return Credentials{}, ctx.Err()
	}
	if result.err != "" {
		return Credentials{}, errors.New(result.err)
	}
	creds := Credentials{ClientID: clientID, ClientSecret: clientSecret, TokenEndpoint: meta.TokenEndpoint, Resource: meta.Resource}
	return c.token(ctx, creds, url.Values{"grant_type": {"authorization_code"}, "code": {result.code},
		"redirect_uri": {redirect}, "code_verifier": {verifier}})
}

func (c *Client) register(ctx context.Context, meta Metadata, redirect string) (string, string, error) {
	body, _ := json.Marshal(map[string]any{"client_name": "AIofCage", "redirect_uris": []string{redirect},
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var registered struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := c.do(req, &registered); err != nil || registered.ClientID == "" {
		return "", "", fmt.Errorf("注册客户端失败%s", detail(err))
	}
	return registered.ClientID, registered.ClientSecret, nil
}

// Refresh exchanges the refresh token. Servers that rotate refresh tokens return a new one; the
// old one is kept otherwise.
func (c *Client) Refresh(ctx context.Context, creds Credentials) (Credentials, error) {
	if creds.RefreshToken == "" {
		return Credentials{}, errors.New("授权已过期，请重新登录")
	}
	return c.token(ctx, creds, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {creds.RefreshToken}})
}

func (c *Client) token(ctx context.Context, creds Credentials, form url.Values) (Credentials, error) {
	if _, err := c.parse(creds.TokenEndpoint); err != nil {
		return Credentials{}, err
	}
	form.Set("client_id", creds.ClientID)
	if creds.ClientSecret != "" {
		form.Set("client_secret", creds.ClientSecret)
	}
	if creds.Resource != "" {
		form.Set("resource", creds.Resource)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, creds.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var response struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := c.do(req, &response); err != nil {
		if form.Get("grant_type") == "refresh_token" {
			return Credentials{}, fmt.Errorf("续期失败，请重新登录%s", detail(err))
		}
		return Credentials{}, fmt.Errorf("换取访问令牌失败%s", detail(err))
	}
	if response.AccessToken == "" || (response.TokenType != "" && !strings.EqualFold(response.TokenType, "bearer")) ||
		strings.ContainsAny(response.AccessToken, " \t\r\n") {
		return Credentials{}, errors.New("授权服务器返回的令牌无效")
	}
	creds.AccessToken, creds.ExpiresAt = response.AccessToken, time.Time{}
	if response.ExpiresIn > 0 {
		creds.ExpiresAt = c.now().Add(time.Duration(response.ExpiresIn) * time.Second).UTC()
	}
	if response.RefreshToken != "" {
		creds.RefreshToken = response.RefreshToken
	}
	if response.Scope != "" {
		creds.Scope = response.Scope
	}
	return creds, nil
}

// Vault is the subset of vault.Store needed to keep credentials.
type Vault interface {
	Read(ctx context.Context, reference string) ([]byte, error)
	Put(ctx context.Context, reference string, value []byte) error
}

var refreshLocks sync.Map // reference → *sync.Mutex

// AccessToken returns a token valid for at least margin, refreshing and saving it first when
// needed. Refreshes of one item are serialized: servers may rotate refresh tokens.
func (c *Client) AccessToken(ctx context.Context, store Vault, reference string, margin time.Duration) (Credentials, bool, error) {
	lock, _ := refreshLocks.LoadOrStore(reference, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	data, err := store.Read(ctx, reference)
	if err != nil {
		return Credentials{}, false, errors.New("还没有登录授权")
	}
	creds, err := Parse(data)
	if err != nil {
		return Credentials{}, false, err
	}
	if creds.Fresh(c.now(), margin) {
		return creds, false, nil
	}
	refreshed, err := c.Refresh(ctx, creds)
	if err != nil {
		return Credentials{}, false, err
	}
	encoded, _ := json.Marshal(refreshed)
	if err := store.Put(ctx, reference, encoded); err != nil {
		return Credentials{}, false, err
	}
	return refreshed, true, nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) parse(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(c.allowHTTP && u.Scheme == "http")) {
		return nil, errors.New("OAuth 地址必须是 https:// 网址")
	}
	return u, nil
}

func (c *Client) getJSON(ctx context.Context, target string, value any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, value)
}

type statusError struct {
	status int
	code   string
}

func (e statusError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("HTTP %d，%s", e.status, e.code)
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

// do sends a request without following redirects and decodes a JSON answer. Error bodies are
// reduced to the OAuth error code: they never reach the UI verbatim.
func (c *Client) do(req *http.Request, value any) error {
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("网络请求失败")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &problem)
		code := ""
		if len(problem.Error) <= 64 && !strings.ContainsAny(problem.Error, "<>\"") {
			code = problem.Error
		}
		return statusError{resp.StatusCode, code}
	}
	return json.Unmarshal(data, value)
}

func detail(err error) string {
	if err == nil {
		return ""
	}
	return "（" + err.Error() + "）"
}

func randomString(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is an MCP server whose authorization server lives under /as.
type fakeServer struct {
	*httptest.Server
	mu        sync.Mutex
	challenge string
	redirect  string
	refresh   string
	noPRM     bool
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{refresh: "rt1"}
	mux := http.NewServeMux()
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	asBase := f.URL + "/as"
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, _ *http.Request) {
		if f.noPRM {
			http.NotFound(w, nil)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"resource": f.URL + "/mcp", "authorization_servers": []string{asBase}, "scopes_supported": []string{"read"}})
	})
	metadata := func(w http.ResponseWriter, base string) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize",
			"token_endpoint": base + "/token", "registration_endpoint": base + "/register", "code_challenge_methods_supported": []string{"S256"}})
	}
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/as", func(w http.ResponseWriter, _ *http.Request) { metadata(w, asBase) })
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) { metadata(w, f.URL+"/as") })
	mux.HandleFunc("POST /as/register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RedirectURIs []string `json:"redirect_uris"`
			AuthMethod   string   `json:"token_endpoint_auth_method"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.RedirectURIs) != 1 || !strings.HasPrefix(body.RedirectURIs[0], "http://127.0.0.1:") || body.AuthMethod != "none" {
			http.Error(w, `{"error":"invalid_client_metadata"}`, 400)
			return
		}
		f.mu.Lock()
		f.redirect = body.RedirectURIs[0]
		f.mu.Unlock()
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"client_id": "client-1"})
	})
	mux.HandleFunc("GET /as/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		defer f.mu.Unlock()
		if q.Get("client_id") != "client-1" || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
			q.Get("redirect_uri") != f.redirect || q.Get("resource") != f.URL+"/mcp" || (!f.noPRM && q.Get("scope") != "read") {
			http.Error(w, "bad authorize request", 400)
			return
		}
		f.challenge = q.Get("code_challenge")
		http.Redirect(w, r, f.redirect+"?code=code-1&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("POST /as/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		fail := func() { w.WriteHeader(400); w.Write([]byte(`{"error":"invalid_grant"}`)) }
		if r.Form.Get("client_id") != "client-1" || r.Form.Get("resource") != f.URL+"/mcp" {
			fail()
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "code-1" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge || r.Form.Get("redirect_uri") != f.redirect {
				fail()
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at1", "refresh_token": "rt1", "expires_in": 3600, "token_type": "Bearer"})
		case "refresh_token":
			if r.Form.Get("refresh_token") != f.refresh { // rotated tokens are single-use
				fail()
				return
			}
			f.refresh = "rt2"
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at2", "refresh_token": "rt2", "expires_in": 3600, "token_type": "bearer"})
		default:
			fail()
		}
	})
	return f
}

func testClient() *Client {
	c := NewClient(&http.Client{Timeout: 5 * time.Second}, func(target string) error {
		// The "browser": follow the authorization redirect to the loopback callback.
		resp, err := http.Get(target)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New(resp.Status)
		}
		return nil
	})
	c.allowHTTP, c.LoginTimeout = true, 5*time.Second
	return c
}

func TestLoginAndRefresh(t *testing.T) {
	f := newFakeServer(t)
	c := testClient()
	creds, err := c.Login(context.Background(), f.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "at1" || creds.RefreshToken != "rt1" || creds.ClientID != "client-1" ||
		creds.TokenEndpoint != f.URL+"/as/token" || !creds.Fresh(time.Now(), 50*time.Minute) {
		t.Fatalf("%+v", creds)
	}
	store := &memoryVault{items: map[string][]byte{}}
	creds.ExpiresAt = time.Now().Add(-time.Minute)
	data, _ := json.Marshal(creds)
	store.items["ref"] = data
	fresh, refreshed, err := c.AccessToken(context.Background(), store, "ref", time.Minute)
	if err != nil || !refreshed || fresh.AccessToken != "at2" || fresh.RefreshToken != "rt2" {
		t.Fatalf("refresh: %+v %v %v", fresh, refreshed, err)
	}
	saved, _ := Parse(store.items["ref"])
	if saved.AccessToken != "at2" || saved.RefreshToken != "rt2" {
		t.Fatalf("refreshed credentials must be saved: %+v", saved)
	}
	if again, refreshed, err := c.AccessToken(context.Background(), store, "ref", time.Minute); err != nil || refreshed || again.AccessToken != "at2" {
		t.Fatalf("fresh token must not be refreshed: %+v %v %v", again, refreshed, err)
	}
	stale := creds // still holds the rotated-out refresh token
	if _, err := c.Refresh(context.Background(), stale); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("reused refresh token must fail with the OAuth error code: %v", err)
	}
}

func TestDiscoveryFallsBackToOrigin(t *testing.T) {
	f := newFakeServer(t)
	f.noPRM = true
	meta, err := testClient().Discover(context.Background(), f.URL+"/mcp")
	if err != nil || meta.TokenEndpoint != f.URL+"/as/token" || meta.Resource != f.URL+"/mcp" || len(meta.Scopes) != 0 {
		t.Fatalf("%+v %v", meta, err)
	}
}

func TestStateMismatchAndHTTPS(t *testing.T) {
	f := newFakeServer(t)
	c := testClient()
	c.OpenBrowser = func(target string) error {
		u, _ := url.Parse(target)
		resp, err := http.Get(u.Query().Get("redirect_uri") + "?code=code-1&state=forged")
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	if _, err := c.Login(context.Background(), f.URL+"/mcp"); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("forged state must be rejected: %v", err)
	}
	strict := NewClient(http.DefaultClient, nil)
	if _, err := strict.Discover(context.Background(), f.URL+"/mcp"); err == nil {
		t.Fatal("http:// must be refused outside tests")
	}
	if (Credentials{AccessToken: "x"}).Usable(time.Now()) != true || (Credentials{AccessToken: "x", ExpiresAt: time.Now().Add(-time.Second)}).Usable(time.Now()) {
		t.Fatal("usable")
	}
}

type memoryVault struct {
	mu    sync.Mutex
	items map[string][]byte
}

func (m *memoryVault) Read(_ context.Context, ref string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.items[ref]
	if !ok {
		return nil, errors.New("missing")
	}
	return value, nil
}

func (m *memoryVault) Put(_ context.Context, ref string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[ref] = value
	return nil
}

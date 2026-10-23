// Package service exposes desktop use cases to the Wails frontend. No method returns a secret:
// API keys only flow inward (to the Keychain) and from the Keychain to the gatekeeper.
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/buildinfo"
	"github.com/shaoxier/AIofCage/internal/oauth"
	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
)

type DirectoryPicker func() (string, error)

type App struct {
	store      *store.Store
	vault      vault.Store
	manager    *runtime.Manager
	managerErr error
	picker     DirectoryPicker
	client     *http.Client
	oauth      *oauth.Client
	now        func() time.Time
	emit       Emitter
	terminal   TerminalOpener
	monitor    *monitor
	explain    explainCache
}

// NewApp wires the use cases. manager may be nil when Docker or Colima is missing; managerErr then
// explains why, and environment checks surface it to the user.
func NewApp(st *store.Store, secrets vault.Store, manager *runtime.Manager, managerErr error, picker DirectoryPicker) *App {
	// Connection tests and OAuth go straight to the service; shell proxy settings are ignored on purpose.
	client := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: nil, TLSHandshakeTimeout: 8 * time.Second}}
	return &App{store: st, vault: secrets, manager: manager, managerErr: managerErr, picker: picker, now: time.Now,
		emit: func(string, any) {}, terminal: openInTerminal, monitor: newMonitor(), explain: explainCache{items: map[string]Explanation{}},
		client: client, oauth: oauth.NewClient(client, openURL)}
}

type AgentInfo struct {
	ID      session.Agent `json:"id"`
	Name    string        `json:"name"`
	Version string        `json:"version"`
	Command string        `json:"command"`
}

type Overview struct {
	Version     string         `json:"version"`
	Stage       string         `json:"stage"`
	Environment runtime.Report `json:"environment"`
	Agents      []AgentInfo    `json:"agents"`
	DataDir     string         `json:"data_dir"`
	Workspace   string         `json:"workspace"`
}

func (a *App) GetOverview() Overview {
	overview := Overview{Version: buildinfo.Version, Stage: buildinfo.Stage, DataDir: a.store.Dir(), Workspace: sandbox.Workspace()}
	for _, spec := range runtime.Catalog() {
		overview.Agents = append(overview.Agents, AgentInfo{spec.Agent, spec.Name, spec.Version, spec.Command})
	}
	if a.manager == nil {
		detail := "缺少运行环境"
		if a.managerErr != nil {
			detail = a.managerErr.Error()
		}
		overview.Environment = runtime.Report{Checks: []runtime.Check{{ID: "tools", Name: "Docker / Colima", Status: "missing", Detail: detail}}}
		return overview
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	overview.Environment = a.manager.Environment().Inspect(ctx)
	return overview
}

func (a *App) SelectProject() (string, error) {
	if a.picker == nil {
		return "", errors.New("项目选择器不可用，请在桌面应用中操作")
	}
	return a.picker()
}

// ProviderView never contains the key itself, only whether one is saved.
type ProviderView struct {
	provider.Provider
	KeySaved   bool     `json:"key_saved"`
	KeyManaged bool     `json:"key_managed"`
	UsedBy     []string `json:"used_by"`
}

func (a *App) ListPresets() []provider.Preset { return provider.Presets() }

func (a *App) ListProviders() ([]ProviderView, error) {
	providers, err := a.store.Providers()
	if err != nil {
		return nil, err
	}
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return nil, err
	}
	views := make([]ProviderView, 0, len(providers))
	for _, p := range providers {
		views = append(views, a.view(p, sandboxes))
	}
	return views, nil
}

func (a *App) view(p provider.Provider, sandboxes []sandbox.Sandbox) ProviderView {
	view := ProviderView{Provider: p, KeyManaged: p.KeyRef == provider.KeyRefFor(p.ID), UsedBy: []string{}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.vault.Read(ctx, p.KeyRef); err == nil {
		view.KeySaved = true
	}
	for _, s := range sandboxes {
		for _, binding := range s.Bindings {
			if binding.ProviderID == p.ID {
				view.UsedBy = append(view.UsedBy, s.Name)
			}
		}
	}
	return view
}

type SaveProviderRequest struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Agent    session.Agent     `json:"agent"`
	Protocol provider.Protocol `json:"protocol"`
	BaseURL  string            `json:"base_url"`
	Model    string            `json:"model"`
	Auth     provider.Auth     `json:"auth"`
	Preset   string            `json:"preset"`
	// APIKey is write-only: it goes straight to the Keychain and is never returned.
	APIKey string `json:"api_key"`
	// ExistingKeyRef reuses a Keychain item. Only AIofCage's own items and items named by a
	// preset are allowed, so the UI cannot be used to point a provider at arbitrary secrets.
	ExistingKeyRef string `json:"existing_key_ref"`
}

func (a *App) SaveProvider(request SaveProviderRequest) (ProviderView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var p provider.Provider
	existing := request.ID != ""
	if existing {
		current, err := a.store.Provider(request.ID)
		if err != nil {
			return ProviderView{}, err
		}
		p = current
	} else {
		p = provider.Provider{ID: newProviderID(), CreatedAt: a.now().UTC()}
	}
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return ProviderView{}, err
	}
	used := a.view(p, sandboxes).UsedBy
	if existing && request.Agent != p.Agent && len(used) > 0 {
		return ProviderView{}, fmt.Errorf("还有沙箱在用这个供应商（%s），不能改成别的 Agent", strings.Join(used, "、"))
	}
	p.Name, p.Agent, p.Protocol, p.BaseURL, p.Model, p.Auth, p.Preset =
		request.Name, request.Agent, request.Protocol, request.BaseURL, request.Model, request.Auth, request.Preset
	key := strings.TrimSpace(request.APIKey)
	switch {
	case request.ExistingKeyRef != "":
		if !allowedExistingKeyRef(request.ExistingKeyRef, p.ID) {
			return ProviderView{}, errors.New("只能使用 AIofCage 保存的密钥，或预设中列出的钥匙串条目")
		}
		p.KeyRef = request.ExistingKeyRef
	case key != "" || !existing:
		p.KeyRef = provider.KeyRefFor(p.ID)
	}
	p.Normalize()
	if err := p.Validate(); err != nil {
		return ProviderView{}, err
	}
	switch {
	case key != "":
		if err := a.vault.Put(ctx, p.KeyRef, []byte(key)); err != nil {
			return ProviderView{}, err
		}
	case request.ExistingKeyRef != "":
		if _, err := a.vault.Read(ctx, p.KeyRef); err != nil {
			return ProviderView{}, fmt.Errorf("钥匙串中找不到条目 %s", p.KeyRef)
		}
	case !existing:
		return ProviderView{}, errors.New("请填写 API Key")
	}
	if err := a.store.SaveProvider(p); err != nil {
		return ProviderView{}, err
	}
	// Running sandboxes using this provider get the new key/endpoint right away.
	if existing && a.manager != nil {
		for _, s := range sandboxes {
			if s.ProviderFor(p.Agent) == p.ID {
				if err := a.manager.Refresh(ctx, s.ID); err != nil {
					return a.view(p, sandboxes), fmt.Errorf("已保存，但更新运行中的沙箱「%s」失败：%w", s.Name, err)
				}
			}
		}
	}
	return a.view(p, sandboxes), nil
}

func allowedExistingKeyRef(ref, providerID string) bool {
	if ref == provider.KeyRefFor(providerID) {
		return true
	}
	for _, preset := range provider.Presets() {
		if preset.ExistingKeyRef != "" && preset.ExistingKeyRef == ref {
			return true
		}
	}
	return false
}

func newProviderID() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	var b [6]byte
	_, _ = rand.Read(b[:])
	id := []byte("p-")
	for _, value := range b {
		id = append(id, alphabet[int(value)%len(alphabet)])
	}
	return string(id)
}

func (a *App) DeleteProvider(id string) error {
	p, err := a.store.Provider(id)
	if err != nil {
		return err
	}
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return err
	}
	if used := a.view(p, sandboxes).UsedBy; len(used) > 0 {
		return fmt.Errorf("还有沙箱在用这个供应商（%s），请先给它们换一个供应商", strings.Join(used, "、"))
	}
	if err := a.store.DeleteProvider(id); err != nil {
		return err
	}
	if p.KeyRef == provider.KeyRefFor(p.ID) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.vault.Delete(ctx, p.KeyRef)
	}
	return nil
}

type TestResult struct {
	OK        bool   `json:"ok"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Message   string `json:"message"`
}

// TestProvider checks that the endpoint is reachable and the key is accepted by listing models.
// The request is made by the trusted desktop process, never from inside a sandbox.
func (a *App) TestProvider(id string) (TestResult, error) {
	p, err := a.store.Provider(id)
	if err != nil {
		return TestResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key, err := a.vault.Read(ctx, p.KeyRef)
	if err != nil {
		return TestResult{Message: "钥匙串中没有这个供应商的密钥"}, nil
	}
	host, base, err := p.Endpoint()
	if err != nil {
		return TestResult{}, err
	}
	path := base + "/models"
	if p.Protocol == provider.Anthropic {
		path = base + "/v1/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+path, nil)
	if err != nil {
		return TestResult{}, err
	}
	header, prefix := p.Injection()
	req.Header.Set(header, prefix+string(key))
	if p.Protocol == provider.Anthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	started := a.now()
	resp, err := a.client.Do(req)
	result := TestResult{LatencyMS: a.now().Sub(started).Milliseconds()}
	if err != nil {
		result.Message = "无法连接到 " + host + "（" + networkReason(err) + "）"
		return result, nil
	}
	resp.Body.Close()
	result.Status = resp.StatusCode
	switch {
	case resp.StatusCode == 200:
		result.OK, result.Message = true, "连接正常，密钥有效"
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		result.Message = "服务可达，但密钥无效或没有权限"
	case resp.StatusCode == 404 || resp.StatusCode == 405:
		result.OK, result.Message = true, "服务可达（该服务不提供模型列表接口，无法在这里校验密钥）"
	case resp.StatusCode == 429:
		result.OK, result.Message = true, "服务可达，但当前被限流"
	default:
		result.Message = fmt.Sprintf("服务返回了 %d", resp.StatusCode)
	}
	return result, nil
}

func networkReason(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, "Client.Timeout") || strings.Contains(text, "deadline exceeded") || strings.Contains(text, "timeout"):
		return "超时"
	case strings.Contains(text, "no such host"):
		return "域名无法解析"
	case strings.Contains(text, "certificate"):
		return "证书校验失败"
	case strings.Contains(text, "connection refused"):
		return "连接被拒绝"
	}
	return "网络错误"
}

// SandboxView is what the sandbox list shows.
type SandboxView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ProjectPath string         `json:"project_path"`
	State       string         `json:"state"`
	Bindings    []BindingView  `json:"bindings"`
	Limits      sandbox.Limits `json:"limits"`
	Usage       sandbox.Usage  `json:"usage"`
	MCP         []string       `json:"mcp"`
	Filesystem  bool           `json:"filesystem_mcp"`
	CreatedAt   time.Time      `json:"created_at"`
}

type BindingView struct {
	Agent        session.Agent `json:"agent"`
	ProviderID   string        `json:"provider_id"`
	ProviderName string        `json:"provider_name"`
}

func (a *App) ListSandboxes() ([]SandboxView, error) {
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return nil, err
	}
	providers, err := a.store.Providers()
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, p := range providers {
		names[p.ID] = p.Name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	views := make([]SandboxView, 0, len(sandboxes))
	for _, s := range sandboxes {
		view := SandboxView{ID: s.ID, Name: s.Name, ProjectPath: s.ProjectPath, State: "unknown",
			Limits: s.Limits, Usage: s.Usage, CreatedAt: s.CreatedAt, Bindings: []BindingView{},
			MCP: append([]string{}, s.MCP...), Filesystem: s.FilesystemMCP}
		if a.manager != nil {
			view.State = a.manager.Status(ctx, s.ID).State
		}
		for _, b := range s.Bindings {
			view.Bindings = append(view.Bindings, BindingView{Agent: b.Agent, ProviderID: b.ProviderID, ProviderName: names[b.ProviderID]})
		}
		views = append(views, view)
	}
	return views, nil
}

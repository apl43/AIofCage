// Package connector runs a stdio MCP server that needs a secret (a database password, an API
// token) inside a trusted container, and serves it over streamable HTTP to the gatekeeper only.
//
// The sandbox never sees the secret and cannot reach this container: the agent talks to the
// gatekeeper's MCP entrypoint with a placeholder, the gatekeeper checks the service and the tool,
// then forwards here with a connector token. Each MCP session gets its own server process.
package connector

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Launch is delivered by the desktop app through `docker exec` into a tmpfs file, read once and
// removed. Secret values appear only in Command or Env and are listed in Redact.
type Launch struct {
	Token   string            `json:"token"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	Redact  []string          `json:"redact,omitempty"`
}

func (l Launch) Validate() error {
	if len(l.Token) < 32 {
		return errors.New("连接器 token 过短")
	}
	if len(l.Command) == 0 || l.Command[0] == "" {
		return errors.New("缺少启动命令")
	}
	return nil
}

const (
	maxBody     = 4 << 20
	maxLine     = 16 << 20
	maxSessions = 8
	idleTimeout = 30 * time.Minute
	callTimeout = 10 * time.Minute
	// Only what the MCP server needs; the container environment is not inherited.
	childPath = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

type Server struct {
	logger *log.Logger
	now    func() time.Time

	mu       sync.Mutex
	launch   *Launch
	expected [32]byte
	sessions map[string]*session
}

func NewServer(logger *log.Logger) *Server {
	return &Server{logger: logger, now: time.Now, sessions: map[string]*session{}}
}

// SetLaunch replaces the launch configuration. Running sessions keep their process; new sessions
// use the new command, e.g. after the password was changed in the desktop app.
func (s *Server) SetLaunch(l Launch) error {
	if err := l.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.launch = &l
	s.expected = sha256.Sum256([]byte("Bearer " + l.Token))
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" && r.Method == http.MethodGet {
		s.mu.Lock()
		ready := s.launch != nil
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]bool{"ready": ready})
		return
	}
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	launch, expected := s.launch, s.expected
	s.mu.Unlock()
	if launch == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "not_configured"})
		return
	}
	values := r.Header.Values("Authorization")
	actual := sha256.Sum256([]byte(strings.Join(values, "")))
	if len(values) != 1 || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "unauthorized"})
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.post(w, r, *launch)
	case http.MethodDelete:
		if sess := s.lookup(r.Header.Get("Mcp-Session-Id")); sess != nil {
			s.closeSession(sess)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	default:
		// No server-initiated stream: the stdio servers we run never push requests to clients.
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

func (m message) hasID() bool { return len(m.ID) > 0 && string(m.ID) != "null" }

func (s *Server) post(w http.ResponseWriter, r *http.Request, launch Launch) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "body_too_large"})
		return
	}
	var line bytes.Buffer
	if err := json.Compact(&line, body); err != nil || !bytes.HasPrefix(line.Bytes(), []byte("{")) {
		// JSON-RPC batches were removed from MCP; one message per request.
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_message"})
		return
	}
	var msg message
	if err := json.Unmarshal(line.Bytes(), &msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_message"})
		return
	}
	var sess *session
	if id := r.Header.Get("Mcp-Session-Id"); id != "" {
		if sess = s.lookup(id); sess == nil {
			http.NotFound(w, r) // the client starts a new session on 404
			return
		}
	} else {
		if msg.Method != "initialize" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "missing_session"})
			return
		}
		if sess, err = s.newSession(launch); err != nil {
			s.logger.Printf("启动 MCP 服务失败：%s", redact(err.Error(), launch.Redact))
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "server_start_failed"})
			return
		}
		w.Header().Set("Mcp-Session-Id", sess.id)
	}
	line.WriteByte('\n')
	if msg.Method == "" || !msg.hasID() {
		// Notifications and responses get no reply.
		if err := sess.write(line.Bytes()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "server_exited"})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	key := string(msg.ID)
	reply := sess.register(key)
	defer sess.unregister(key)
	if err := sess.write(line.Bytes()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"code": "server_exited"})
		return
	}
	timer := time.NewTimer(callTimeout)
	defer timer.Stop()
	select {
	case response := <-reply:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(response)
	case <-sess.done:
		writeRPCError(w, msg.ID, "MCP 服务已退出")
	case <-timer.C:
		writeRPCError(w, msg.ID, "MCP 服务响应超时")
	case <-r.Context().Done():
	}
}

func (s *Server) lookup(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess != nil {
		sess.lastUsed = s.now()
	}
	return sess
}

func (s *Server) newSession(launch Launch) (*session, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	sess := &session{id: hex.EncodeToString(raw[:]), pending: map[string]chan []byte{}, done: make(chan struct{})}
	s.mu.Lock()
	var oldest *session
	if len(s.sessions) >= maxSessions {
		for _, candidate := range s.sessions {
			if oldest == nil || candidate.lastUsed.Before(oldest.lastUsed) {
				oldest = candidate
			}
		}
	}
	s.mu.Unlock()
	if oldest != nil {
		s.closeSession(oldest)
	}
	if err := sess.start(launch, s.logger); err != nil {
		return nil, err
	}
	s.mu.Lock()
	sess.lastUsed = s.now()
	s.sessions[sess.id] = sess
	s.mu.Unlock()
	go func() {
		<-sess.done
		s.mu.Lock()
		if s.sessions[sess.id] == sess {
			delete(s.sessions, sess.id)
		}
		s.mu.Unlock()
	}()
	return sess, nil
}

func (s *Server) closeSession(sess *session) {
	s.mu.Lock()
	delete(s.sessions, sess.id)
	s.mu.Unlock()
	sess.kill()
}

// Reap closes sessions idle for longer than idleTimeout until ctx ends.
func (s *Server) Reap(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var idle []*session
		s.mu.Lock()
		for _, sess := range s.sessions {
			if s.now().Sub(sess.lastUsed) > idleTimeout {
				idle = append(idle, sess)
			}
		}
		s.mu.Unlock()
		for _, sess := range idle {
			s.closeSession(sess)
		}
	}
}

// Close stops every session.
func (s *Server) Close() {
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	for _, sess := range sessions {
		s.closeSession(sess)
	}
}

// session is one MCP server process speaking newline-delimited JSON-RPC on stdio.
type session struct {
	id       string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	writeMu  sync.Mutex
	lastUsed time.Time // guarded by Server.mu
	done     chan struct{}

	mu      sync.Mutex
	pending map[string]chan []byte
}

func (sess *session) start(launch Launch, logger *log.Logger) error {
	cmd := exec.Command(launch.Command[0], launch.Command[1:]...)
	cmd.Env = []string{childPath, "HOME=/tmp", "NODE_ENV=production"}
	for key, value := range launch.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sess.cmd, sess.stdin = cmd, stdin
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		sess.readReplies(stdout)
	}()
	go func() {
		defer readers.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64<<10), maxLine)
		for scanner.Scan() {
			logger.Printf("[%s] %s", sess.id[:8], truncate(redact(scanner.Text(), launch.Redact), 500))
		}
	}()
	go func() {
		readers.Wait()
		_ = cmd.Wait()
		close(sess.done)
	}()
	return nil
}

func (sess *session) readReplies(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	for scanner.Scan() {
		line := scanner.Bytes()
		var msg message
		if json.Unmarshal(line, &msg) != nil || !msg.hasID() {
			continue // server notifications (logs, progress) have no HTTP request to go to
		}
		if msg.Method != "" {
			// A request from the server (sampling, roots): this bridge offers none.
			reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID,
				"error": map[string]any{"code": -32601, "message": "not supported"}})
			_ = sess.write(append(reply, '\n'))
			continue
		}
		var key bytes.Buffer
		if json.Compact(&key, msg.ID) != nil {
			continue
		}
		sess.mu.Lock()
		reply := sess.pending[key.String()]
		delete(sess.pending, key.String())
		sess.mu.Unlock()
		if reply != nil {
			reply <- append([]byte(nil), line...)
		}
	}
}

func (sess *session) register(key string) chan []byte {
	reply := make(chan []byte, 1)
	sess.mu.Lock()
	sess.pending[key] = reply
	sess.mu.Unlock()
	return reply
}

func (sess *session) unregister(key string) {
	sess.mu.Lock()
	delete(sess.pending, key)
	sess.mu.Unlock()
}

func (sess *session) write(line []byte) error {
	sess.writeMu.Lock()
	defer sess.writeMu.Unlock()
	select {
	case <-sess.done:
		return errors.New("MCP 服务已退出")
	default:
	}
	_, err := sess.stdin.Write(line)
	return err
}

func (sess *session) kill() {
	_ = sess.stdin.Close()
	select {
	case <-sess.done:
		return
	case <-time.After(2 * time.Second):
	}
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
	}
}

func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	return text
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, text string) {
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": -32603, "message": text}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

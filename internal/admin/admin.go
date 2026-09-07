package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcp-hub/internal/config"
)

const (
	cookieName       = "__Host-mcphub_admin"
	localCookieName  = "mcphub_admin"
	secretSentinel   = "__MCP_HUB_SECRET_SET__"
	maxAdminSessions = 16
	maxLoginWindows  = 4096
	maxAdminBodySize = 1024 * 1024
)

//go:embed web/*
var webFiles embed.FS

type StatusProvider func() any
type ReloadFunc func(context.Context) error
type PreflightFunc func(context.Context, *config.ResolvedConfig) error

type Options struct {
	ConfigPath     string
	AdminToken     string
	PublicURL      string
	PublicMode     bool
	TrustedProxies []string
	SessionTimeout time.Duration
	Status         StatusProvider
	Reload         ReloadFunc
	Preflight      PreflightFunc
	// ConfigTransaction must use the same serialization domain as file polling.
	// Its callback receives a reload function that does not reacquire that lock.
	ConfigTransaction func(func(func(context.Context) error))
}

type session struct {
	csrf       string
	createdAt  time.Time
	lastSeen   time.Time
	apiStarted time.Time
	apiCount   int
}

type loginWindow struct {
	started time.Time
	count   int
}

type Handler struct {
	opts       Options
	origin     string
	trusted    []*net.IPNet
	writeMu    sync.Mutex // standalone fallback; never hold the session mutex during reload
	mu         sync.Mutex
	sessions   map[string]session
	loginLimit map[string]loginWindow
	static     http.Handler
}

func New(opts Options) (*Handler, error) {
	if strings.TrimSpace(opts.ConfigPath) == "" || strings.TrimSpace(opts.AdminToken) == "" {
		return nil, errors.New("admin config path and token are required")
	}
	if opts.SessionTimeout <= 0 {
		opts.SessionTimeout = 30 * time.Minute
	}
	origin := strings.TrimRight(opts.PublicURL, "/")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, errors.New("invalid admin public URL")
		}
		origin = u.Scheme + "://" + u.Host
	}
	var trusted []*net.IPNet
	for _, raw := range opts.TrustedProxies {
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, err
		}
		trusted = append(trusted, network)
	}
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	return &Handler{
		opts:       opts,
		origin:     origin,
		trusted:    trusted,
		sessions:   make(map[string]session),
		loginLimit: make(map[string]loginWindow),
		static:     http.FileServer(http.FS(sub)),
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.securityHeaders(w)
	if h.opts.PublicMode && !h.isHTTPS(r) {
		http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/admin/v1/") {
		h.serveAPI(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/admin" {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/admin/") {
		http.NotFound(w, r)
		return
	}
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/admin")
	if r.URL.Path == "/" || path.Ext(r.URL.Path) == "" {
		r.URL.Path = "/"
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	// Go's platform MIME database is not consistent for JavaScript modules
	// (notably, Windows may serve .mjs as text/plain). With nosniff enabled,
	// browsers then refuse to load the admin console.
	if ext := strings.ToLower(path.Ext(r.URL.Path)); ext == ".js" || ext == ".mjs" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	h.static.ServeHTTP(w, r)
}

func (h *Handler) serveAPI(w http.ResponseWriter, r *http.Request) {
	if !h.validOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/api/admin/v1/auth/login" {
		h.login(w, r)
		return
	}
	sid, sess, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.allowAPI(sid) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	if isMutation(r.Method) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(sess.csrf)) != 1 {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
	}

	switch {
	case r.URL.Path == "/api/admin/v1/auth/me" && r.Method == http.MethodGet:
		h.writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrfToken": sess.csrf})
	case r.URL.Path == "/api/admin/v1/auth/logout" && r.Method == http.MethodPost:
		h.mu.Lock()
		delete(h.sessions, sid)
		h.mu.Unlock()
		h.clearCookie(w, r)
		h.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.URL.Path == "/api/admin/v1/status" && r.Method == http.MethodGet:
		if h.opts.Status == nil {
			h.writeJSON(w, http.StatusOK, map[string]any{})
		} else {
			h.writeJSON(w, http.StatusOK, h.opts.Status())
		}
	case r.URL.Path == "/api/admin/v1/config" && r.Method == http.MethodGet:
		h.getConfig(w)
	case strings.HasPrefix(r.URL.Path, "/api/admin/v1/servers/"):
		id, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/admin/v1/servers/"))
		if err != nil || id == "" || strings.Contains(id, "/") {
			http.Error(w, "invalid server id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			h.putServer(w, r, id)
		case http.MethodDelete:
			h.deleteServer(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := h.clientIP(r)
	if !h.allowLogin(ip) {
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Token), []byte(h.opts.AdminToken)) != 1 {
		time.Sleep(150 * time.Millisecond)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	sid, err := randomToken()
	if err != nil {
		http.Error(w, "session initialization failed", http.StatusInternalServerError)
		return
	}
	csrf, err := randomToken()
	if err != nil {
		http.Error(w, "session initialization failed", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	h.mu.Lock()
	h.pruneLocked(now)
	if len(h.sessions) >= maxAdminSessions {
		h.mu.Unlock()
		http.Error(w, "session capacity reached", http.StatusServiceUnavailable)
		return
	}
	h.sessions[sid] = session{csrf: csrf, createdAt: now, lastSeen: now}
	delete(h.loginLimit, ip)
	h.mu.Unlock()
	h.setCookie(w, r, sid)
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "csrfToken": csrf})
}

func (h *Handler) authenticate(r *http.Request) (string, session, bool) {
	name := localCookieName
	if h.isHTTPS(r) {
		name = cookieName
	}
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", session{}, false
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked(now)
	sess, ok := h.sessions[cookie.Value]
	if !ok || now.Sub(sess.lastSeen) > h.opts.SessionTimeout {
		delete(h.sessions, cookie.Value)
		return "", session{}, false
	}
	sess.lastSeen = now
	h.sessions[cookie.Value] = sess
	return cookie.Value, sess, true
}

func (h *Handler) getConfig(w http.ResponseWriter) {
	cfg, digest, err := h.readRawConfig()
	if err != nil {
		http.Error(w, "configuration unavailable", http.StatusInternalServerError)
		return
	}
	redactConfig(cfg)
	w.Header().Set("ETag", `"`+digest+`"`)
	h.writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) putServer(w http.ResponseWriter, r *http.Request, id string) {
	h.withConfigTransaction(func(reload func(context.Context) error) {
		h.putServerLocked(w, r, id, reload)
	})
}

func (h *Handler) putServerLocked(w http.ResponseWriter, r *http.Request, id string, reload func(context.Context) error) {
	var incoming config.ServerConfig
	if err := decodeJSONBody(r, &incoming); err != nil {
		http.Error(w, "invalid server configuration", http.StatusBadRequest)
		return
	}
	cfg, digest, err := h.readRawConfig()
	if err != nil {
		http.Error(w, "configuration unavailable", http.StatusInternalServerError)
		return
	}
	if !matchETag(r.Header.Get("If-Match"), digest) {
		http.Error(w, "configuration changed", http.StatusConflict)
		return
	}
	if existing, ok := cfg.MCPServers[id]; ok {
		incoming.Env = mergeSecrets(existing.Env, incoming.Env)
		incoming.Headers = mergeSecrets(existing.Headers, incoming.Headers)
	} else if hasSecretSentinel(incoming.Env) || hasSecretSentinel(incoming.Headers) {
		http.Error(w, "secret sentinel cannot be used for a new server", http.StatusBadRequest)
		return
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]config.ServerConfig)
	}
	cfg.MCPServers[id] = incoming
	h.writeConfigLocked(w, r, cfg, digest, reload)
}

func (h *Handler) deleteServer(w http.ResponseWriter, r *http.Request, id string) {
	h.withConfigTransaction(func(reload func(context.Context) error) {
		h.deleteServerLocked(w, r, id, reload)
	})
}

func (h *Handler) deleteServerLocked(w http.ResponseWriter, r *http.Request, id string, reload func(context.Context) error) {
	cfg, digest, err := h.readRawConfig()
	if err != nil {
		http.Error(w, "configuration unavailable", http.StatusInternalServerError)
		return
	}
	if !matchETag(r.Header.Get("If-Match"), digest) {
		http.Error(w, "configuration changed", http.StatusConflict)
		return
	}
	if _, ok := cfg.MCPServers[id]; !ok {
		http.NotFound(w, r)
		return
	}
	delete(cfg.MCPServers, id)
	h.writeConfigLocked(w, r, cfg, digest, reload)
}

func (h *Handler) withConfigTransaction(fn func(func(context.Context) error)) {
	if h.opts.ConfigTransaction != nil {
		h.opts.ConfigTransaction(fn)
		return
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	fn(h.opts.Reload)
}

func (h *Handler) writeConfig(w http.ResponseWriter, r *http.Request, cfg *config.Config, digest string) {
	h.withConfigTransaction(func(reload func(context.Context) error) {
		h.writeConfigLocked(w, r, cfg, digest, reload)
	})
}

func (h *Handler) writeConfigLocked(w http.ResponseWriter, r *http.Request, cfg *config.Config, digest string, reload func(context.Context) error) {
	if err := config.Validate(cfg); err != nil {
		http.Error(w, "configuration validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	resolved, err := config.Resolve(cfg, filepath.Dir(h.opts.ConfigPath), os.LookupEnv)
	if err != nil {
		http.Error(w, "configuration resolution failed", http.StatusBadRequest)
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		http.Error(w, "configuration encoding failed", http.StatusInternalServerError)
		return
	}
	data = append(data, '\n')
	previous, err := config.ReadFileLimited(h.opts.ConfigPath)
	if err != nil {
		http.Error(w, "configuration unavailable", http.StatusInternalServerError)
		return
	}
	if config.ComputeDigest(previous) != digest {
		http.Error(w, "configuration changed", http.StatusConflict)
		return
	}
	if h.opts.Preflight != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		preflightErr := h.opts.Preflight(ctx, resolved)
		cancel()
		if preflightErr != nil {
			http.Error(w, "configuration preflight failed: "+preflightErr.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := config.WriteConfigFileAtomic(h.opts.ConfigPath, data, digest); err != nil {
		var mismatch config.ErrDigestMismatch
		if errors.As(err, &mismatch) {
			http.Error(w, "configuration changed", http.StatusConflict)
			return
		}
		http.Error(w, "configuration write failed", http.StatusInternalServerError)
		return
	}
	newDigest := config.ComputeDigest(data)
	if reload != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		err = reload(ctx)
		cancel()
		if err != nil {
			if rollbackErr := config.WriteConfigFileAtomic(h.opts.ConfigPath, previous, newDigest); rollbackErr != nil {
				http.Error(w, "configuration reload failed and rollback could not be completed", http.StatusInternalServerError)
				return
			}
			restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 15*time.Second)
			restoreErr := reload(restoreCtx)
			restoreCancel()
			if restoreErr != nil {
				http.Error(w, "configuration change was rolled back but runtime restoration failed", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "configuration reload failed; change was rolled back", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("ETag", `"`+newDigest+`"`)
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "digest": newDigest})
}

func (h *Handler) readRawConfig() (*config.Config, string, error) {
	data, err := config.ReadFileLimited(h.opts.ConfigPath)
	if err != nil {
		return nil, "", err
	}
	var cfg config.Config
	if err := config.DecodeStrict(data, &cfg); err != nil {
		return nil, "", err
	}
	return &cfg, config.ComputeDigest(data), nil
}

func redactConfig(cfg *config.Config) {
	if cfg.Hub.Auth.BearerToken != "" {
		cfg.Hub.Auth.BearerToken = secretSentinel
	}
	if cfg.Hub.Admin.Token != "" {
		cfg.Hub.Admin.Token = secretSentinel
	}
	for id, srv := range cfg.MCPServers {
		for key := range srv.Env {
			srv.Env[key] = secretSentinel
		}
		for key := range srv.Headers {
			srv.Headers[key] = secretSentinel
		}
		cfg.MCPServers[id] = srv
	}
}

func mergeSecrets(existing, incoming map[string]string) map[string]string {
	if incoming == nil {
		return nil
	}
	out := make(map[string]string, len(incoming))
	for key, value := range incoming {
		if value == secretSentinel {
			if old, ok := existing[key]; ok {
				out[key] = old
			}
		} else {
			out[key] = value
		}
	}
	return out
}

func hasSecretSentinel(values map[string]string) bool {
	for _, value := range values {
		if value == secretSentinel {
			return true
		}
	}
	return false
}

func (h *Handler) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	expected := h.origin
	if expected == "" {
		scheme := "http"
		if h.isHTTPS(r) {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimRight(origin, "/")), []byte(expected)) == 1
}

func (h *Handler) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := remoteIP(r.RemoteAddr)
	if h.isTrusted(peer) {
		return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
	}
	return false
}

func (h *Handler) clientIP(r *http.Request) string {
	peer := remoteIP(r.RemoteAddr)
	if !h.isTrusted(peer) {
		return peer.String()
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip != nil && !h.isTrusted(ip) {
			return ip.String()
		}
	}
	return peer.String()
}

func remoteIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

func (h *Handler) isTrusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, network := range h.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (h *Handler) allowAPI(sid string) bool {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	sess, ok := h.sessions[sid]
	if !ok {
		return false
	}
	if sess.apiStarted.IsZero() || now.Sub(sess.apiStarted) >= time.Minute {
		sess.apiStarted = now
		sess.apiCount = 0
	}
	sess.apiCount++
	h.sessions[sid] = sess
	return sess.apiCount <= 120
}

func (h *Handler) allowLogin(ip string) bool {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for candidate, window := range h.loginLimit {
		if now.Sub(window.started) > 15*time.Minute {
			delete(h.loginLimit, candidate)
		}
	}
	window := h.loginLimit[ip]
	if window.started.IsZero() && len(h.loginLimit) >= maxLoginWindows {
		return false
	}
	if window.started.IsZero() || now.Sub(window.started) > 15*time.Minute {
		window = loginWindow{started: now}
	}
	window.count++
	h.loginLimit[ip] = window
	return window.count <= 5
}

func (h *Handler) pruneLocked(now time.Time) {
	for id, sess := range h.sessions {
		if now.Sub(sess.lastSeen) > h.opts.SessionTimeout || now.Sub(sess.createdAt) > 24*time.Hour {
			delete(h.sessions, id)
		}
	}
	for ip, window := range h.loginLimit {
		if now.Sub(window.started) > 15*time.Minute {
			delete(h.loginLimit, ip)
		}
	}
}

func (h *Handler) setCookie(w http.ResponseWriter, r *http.Request, value string) {
	secure := h.isHTTPS(r)
	name := localCookieName
	if secure {
		name = cookieName
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: int(h.opts.SessionTimeout.Seconds())})
}

func (h *Handler) clearCookie(w http.ResponseWriter, r *http.Request) {
	secure := h.isHTTPS(r)
	name := localCookieName
	if secure {
		name = cookieName
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func (h *Handler) securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Cache-Control", "no-store")
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func decodeJSONBody(r *http.Request, target any) error {
	if r.Body == nil {
		return io.EOF
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxAdminBodySize+1))
	if err != nil {
		return err
	}
	if len(data) > maxAdminBodySize {
		return errors.New("request body exceeds 1 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func matchETag(raw, digest string) bool {
	return strings.Trim(strings.TrimSpace(raw), `"`) == digest
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

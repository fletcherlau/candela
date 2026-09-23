// Package server is the authenticated browser-facing boundary. It never connects to MySQL.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type Config struct {
	Issuer, Audience, Origin, SyncerURL, APIKey string
	Static, Research                            fs.FS
	HTTPClient                                  *http.Client
}

type application struct {
	cfg      Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func New(ctx context.Context, cfg Config) (http.Handler, error) {
	if cfg.Issuer == "" || cfg.Audience == "" || cfg.APIKey == "" || cfg.Static == nil {
		return nil, fmt.Errorf("issuer, audience, internal API key and static files are required")
	}
	for _, value := range []string{cfg.Issuer, cfg.Origin, cfg.SyncerURL} {
		u, err := url.Parse(value)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("invalid service origin")
		}
	}
	cfg.Issuer = strings.TrimRight(cfg.Issuer, "/")
	cfg.SyncerURL = strings.TrimRight(cfg.SyncerURL, "/")
	cfg.Origin = strings.TrimRight(cfg.Origin, "/")
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	keyCtx := oidc.ClientContext(ctx, client)
	verifier := oidc.NewVerifier(cfg.Issuer, oidc.NewRemoteKeySet(keyCtx, cfg.Issuer+"/cdn-cgi/access/certs"), &oidc.Config{ClientID: cfg.Audience, SupportedSigningAlgs: []string{"RS256"}})
	return &application{cfg: cfg, verifier: verifier, client: client}, nil
}

func (a *application) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	token := r.Header.Get("Cf-Access-Jwt-Assertion")
	if len(token) == 0 || len(token) > 16384 {
		http.Error(w, "请通过 Cloudflare Access 登录后访问。", http.StatusUnauthorized)
		return
	}
	verified, err := a.verifier.Verify(r.Context(), token)
	if err != nil {
		http.Error(w, "登录凭证无效或已过期，请重新登录。", http.StatusUnauthorized)
		return
	}
	var claims struct {
		NotBefore int64 `json:"nbf"`
	}
	if verified.Claims(&claims) != nil || claims.NotBefore > time.Now().Unix() {
		http.Error(w, "登录凭证尚未生效。", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		cookie, err := r.Cookie("__Host-candela-csrf")
		supplied := r.Header.Get("X-CSRF-Token")
		if r.Header.Get("Origin") != a.cfg.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" || err != nil || len(supplied) < 32 || subtle.ConstantTimeCompare([]byte(supplied), []byte(cookie.Value)) != 1 {
			http.Error(w, "跨站或缺少验证的写入请求已拒绝。", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/sync-runs" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "不支持此操作。", http.StatusMethodNotAllowed)
			return
		}
	}
	if r.URL.Path == "/api/sync-runs" || strings.HasPrefix(r.URL.Path, "/api/sync-runs/") {
		a.syncRuns(w, r)
		return
	}
	switch r.URL.Path {
	case "/etf-rotation/dca-dashboard/":
		http.Redirect(w, r, "/etf-rotation/dca-dashboard/index.html", http.StatusFound)
	case "/api/rotation/daily":
		a.rotationDaily(w, r)
	case "/api/rotation/backtest", "/api/rotation/backtest/range":
		a.rotationBacktest(w, r)
	case "/api/catalog":
		a.catalog(w, r)
	case "/api/session":
		value := make([]byte, 32)
		if _, err := rand.Read(value); err != nil {
			http.Error(w, "无法创建会话验证。", 500)
			return
		}
		csrf := base64.RawURLEncoding.EncodeToString(value)
		http.SetCookie(w, &http.Cookie{Name: "__Host-candela-csrf", Value: csrf, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 3600})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"csrfToken": csrf})
	case "/data":
		http.Redirect(w, r, "/admin/data", http.StatusFound)
	case "/research":
		http.Redirect(w, r, "/", http.StatusFound)
	case "/", "/market", "/admin", "/admin/data", "/strategies/four-etf-rotation":
		a.frontend(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/assets/") && fs.ValidPath(strings.TrimPrefix(r.URL.Path, "/")) {
			a.file(w, r, a.cfg.Static, strings.TrimPrefix(r.URL.Path, "/"))
			return
		}
		legacy := strings.TrimPrefix(r.URL.Path, "/research/")
		if strings.HasPrefix(r.URL.Path, "/etf-rotation/dca-dashboard/") {
			legacy = strings.TrimPrefix(r.URL.Path, "/etf-rotation/dca-dashboard/")
		}
		if legacy == "index.html" || legacy == "no-valve.html" || legacy == "data.js" || legacy == "data_valveon.js" {
			if a.cfg.Research == nil {
				http.NotFound(w, r)
				return
			}
			// Existing research is an explicitly allowlisted, trusted static bundle.
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
			a.file(w, r, a.cfg.Research, legacy)
			return
		}
		http.NotFound(w, r)
	}
}

// Radix's modal scroll lock creates a style element. Authorize that style with
// a fresh response nonce while retaining the strict script policy and Access guard.
func (a *application) frontend(w http.ResponseWriter, r *http.Request) {
	body, err := fs.ReadFile(a.cfg.Static, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		http.Error(w, "无法打开页面。", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(value)
	policy := w.Header().Get("Content-Security-Policy")
	w.Header().Set("Content-Security-Policy", strings.Replace(policy, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	meta := `<meta name="candela-style-nonce" content="` + nonce + `">`
	io.WriteString(w, strings.Replace(string(body), "</head>", meta+"</head>", 1))
}

func (a *application) file(w http.ResponseWriter, r *http.Request, files fs.FS, name string) {
	f, err := files.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	switch path.Ext(name) {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, f)
}

func (a *application) catalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.SyncerURL+"/api/v1/data/catalog", nil)
	if err != nil {
		http.Error(w, "数据服务暂时不可用。", 502)
		return
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	res, err := a.client.Do(req)
	if err != nil {
		http.Error(w, "数据服务暂时不可用，请稍后重试。", 502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		http.Error(w, "数据服务读取失败，请稍后重试。", 502)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	var decoded struct {
		Groups []json.RawMessage `json:"groups"`
	}
	if err != nil || json.Unmarshal(payload, &decoded) != nil || decoded.Groups == nil {
		http.Error(w, "数据服务返回异常。", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}

func (a *application) rotationBacktest(w http.ResponseWriter, r *http.Request) {
	target := a.cfg.SyncerURL + "/api/v1/rotation/backtest"
	isRange := r.URL.Path == "/api/rotation/backtest/range"
	if isRange {
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "查询参数无效", 400)
			return
		}
		for key, values := range query {
			if (key != "start" && key != "end") || len(values) != 1 {
				http.Error(w, "查询参数无效", 400)
				return
			}
			if _, err := time.Parse("20060102", values[0]); err != nil {
				http.Error(w, "日期无效", 400)
				return
			}
		}
		if (query.Get("start") == "") != (query.Get("end") == "") {
			http.Error(w, "请同时提供起止日期", 400)
			return
		}
		target += "/range?" + query.Encode()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "回测服务暂不可用", 502)
		return
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	res, err := a.client.Do(req)
	if err != nil {
		http.Error(w, "回测服务暂不可用", 502)
		return
	}
	defer res.Body.Close()
	if isRange && res.StatusCode == 400 {
		http.Error(w, "日期无效或观察区间超过十年", 400)
		return
	}
	if res.StatusCode != 200 {
		http.Error(w, "回测服务暂不可用", 502)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	var decoded struct {
		Status string `json:"status"`
	}
	if err != nil || json.Unmarshal(payload, &decoded) != nil || decoded.Status == "" {
		http.Error(w, "回测结果暂不可用", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodHead {
		w.Write(payload)
	}
}

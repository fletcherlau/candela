package server_test

import (
	"candela/web/internal/server"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func TestAccessProtectsPagesAndCatalog(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
	}))
	defer jwks.Close()
	var syncCalls atomic.Int32
	var syncFailure atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/rotation/reference-captures") {
			syncCalls.Add(1)
			if r.Method != "GET" || r.Header.Get("X-Api-Key") != "server-only-secret" || r.Header.Get("Cf-Access-Jwt-Assertion") != "" {
				t.Error("capture credential boundary")
			}
			if syncFailure.Load() {
				http.Error(w, "server-only-secret", 500)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/20250101") {
				http.Error(w, "server-only-secret", 404)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/20250102") {
				io.WriteString(w, `{"run":{"tradeDate":"20250102","state":"captured"}}`)
				return
			}
			io.WriteString(w, `{"runs":[]}`)
			return
		}
		if r.URL.Path == "/api/v1/rotation/daily" || r.URL.Path == "/api/v1/rotation/daily/dates" {
			if r.Header.Get("X-Api-Key") != "server-only-secret" || r.Header.Get("Cf-Access-Jwt-Assertion") != "" {
				t.Error("incorrect daily credential boundary")
			}
			if syncFailure.Load() {
				w.WriteHeader(500)
				io.WriteString(w, "server-only-secret database details")
				return
			}
			if r.URL.Path == "/api/v1/rotation/daily/dates" {
				if r.URL.RawQuery != "" && (r.URL.Query().Get("before") != "20250103" || r.URL.Query().Get("limit") != "2") {
					t.Error("archive cursor not forwarded")
				}
				io.WriteString(w, `{"status":"ready","dates":[],"nextBefore":""}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "ready", "tradeDate": r.URL.Query().Get("tradeDate"), "close": nil})
			return
		}
		if r.URL.Path == "/api/v1/rotation/backtest/range" {
			if r.Header.Get("X-Api-Key") != "server-only-secret" || r.URL.Query().Get("start") != "20240102" || r.URL.Query().Get("end") != "20250102" {
				t.Error("range query or credentials lost")
			}
			io.WriteString(w, `{"status":"ready","result":null,"range":null}`)
			return
		}
		if r.URL.Path == "/api/v1/rotation/backtest" {
			if r.Header.Get("X-Api-Key") != "server-only-secret" || r.Header.Get("Cf-Access-Jwt-Assertion") != "" {
				t.Error("incorrect backtest credential boundary")
			}
			if syncFailure.Load() {
				w.WriteHeader(500)
				io.WriteString(w, "server-only-secret database details")
				return
			}
			io.WriteString(w, `{"status":"ready","result":null}`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/data/etf-syncs") {
			syncCalls.Add(1)
			if r.Header.Get("X-Api-Key") != "server-only-secret" || r.Header.Get("Cf-Access-Jwt-Assertion") != "" || r.Header.Get("X-CSRF-Token") != "" {
				t.Error("ETF credentials crossed boundary")
			}
			if syncFailure.Load() {
				http.Error(w, "server-only-secret", 500)
				return
			}
			if r.Method == "POST" {
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path == "/api/v1/data/etf-syncs" && string(body) != `{"codes":["510880.SH"]}` {
					t.Errorf("ETF scope changed: %s", body)
				}
				if r.URL.Path != "/api/v1/data/etf-syncs" && len(body) != 0 {
					t.Error("recovery scope not empty")
				}
				w.WriteHeader(202)
				io.WriteString(w, `{"batch":{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"queued"}}`)
			} else if r.URL.Path == "/api/v1/data/etf-syncs" {
				io.WriteString(w, `{"batches":[]}`)
			} else {
				io.WriteString(w, `{"batch":{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"partial"}}`)
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/data/sync-runs") {
			syncCalls.Add(1)
			if r.Header.Get("X-Api-Key") != "server-only-secret" || r.Header.Get("Cf-Access-Jwt-Assertion") != "" || r.Header.Get("X-CSRF-Token") != "" {
				t.Error("incorrect internal credentials")
			}
			if syncFailure.Load() {
				w.WriteHeader(500)
				io.WriteString(w, "server-only-secret database details")
				return
			}
			if r.URL.Path == "/api/v1/data/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel" {
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || len(body) != 0 {
					t.Error("invalid cancellation forwarded")
				}
				io.WriteString(w, `{"run":{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"cancelled"}}`)
				return
			}
			if r.Method == http.MethodPost {
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"mode":"backfill"}` {
					t.Errorf("unexpected body %s", body)
				}
				w.WriteHeader(202)
				io.WriteString(w, `{"run":{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"queued"},"deduplicated":false}`)
			} else if strings.HasSuffix(r.URL.Path, "/sync-runs") {
				io.WriteString(w, `{"runs":[]}`)
			} else {
				io.WriteString(w, `{"run":{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"queued"}}`)
			}
			return
		}
		if r.URL.Path != "/api/v1/data/catalog" || r.Header.Get("X-Api-Key") != "server-only-secret" {
			t.Error("incorrect internal catalog request")
		}
		if r.Header.Get("Cf-Access-Jwt-Assertion") != "" {
			t.Error("Access credential forwarded upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"groups":[{"id":"etf","items":[]}]}`)
	}))
	defer upstream.Close()
	app, err := server.New(context.Background(), server.Config{Issuer: jwks.URL, Audience: "demo-app", Origin: "https://demo.candlea.cn", SyncerURL: upstream.URL, APIKey: "server-only-secret", Static: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><head></head><body>Candela market overview</body></html>")}}})
	if err != nil {
		t.Fatal(err)
	}
	sign := func(issuer, audience string, expiry int64, otherKey *rsa.PrivateKey) string {
		enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
		msg := enc(map[string]any{"alg": "RS256", "kid": "test"}) + "." + enc(map[string]any{"iss": issuer, "aud": []string{audience}, "sub": "user", "exp": expiry, "iat": time.Now().Unix()})
		sum := sha256.Sum256([]byte(msg))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, otherKey, crypto.SHA256, sum[:])
		return msg + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	wrong, _ := rsa.GenerateKey(rand.Reader, 2048)
	valid := sign(jwks.URL, "demo-app", time.Now().Add(time.Hour).Unix(), key)
	for _, token := range []string{"", "not-a-jwt", sign(jwks.URL, "other-app", time.Now().Add(time.Hour).Unix(), key), sign(jwks.URL, "demo-app", time.Now().Add(-time.Hour).Unix(), key), sign("https://evil.invalid", "demo-app", time.Now().Add(time.Hour).Unix(), key), sign(jwks.URL, "demo-app", time.Now().Add(time.Hour).Unix(), wrong)} {
		for _, path := range []string{"/", "/admin", "/admin/data", "/data", "/api/catalog", "/api/rotation/backtest", "/api/rotation/backtest/range", "/api/rotation/daily", "/api/rotation/daily/dates", "/api/rotation/reference-captures", "/api/rotation/reference-captures/20250102", "/strategies/four-etf-rotation", "/api/sync-runs", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/api/etf-syncs", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/assets/app.js", "/research/index.html"} {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", token)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			if rec.Code != 401 {
				t.Fatalf("unauthorized %s: %d", path, rec.Code)
			}
		}
	}
	for _, path := range []string{"/", "/market", "/admin", "/admin/data", "/api/catalog", "/api/rotation/backtest", "/api/rotation/daily", "/api/rotation/daily/dates", "/api/rotation/reference-captures", "/api/rotation/reference-captures/20250102", "/strategies/four-etf-rotation"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), "server-only-secret") {
			t.Fatalf("authorized %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	t.Run("capture reads are bounded authenticated and sanitized", func(t *testing.T) {
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{"GET", "/api/rotation/reference-captures", 200},
			{"HEAD", "/api/rotation/reference-captures/20250102", 200},
			{"GET", "/api/rotation/reference-captures/20250101", 404},
			{"GET", "/api/rotation/reference-captures/20250230", 400},
			{"GET", "/api/rotation/reference-captures/20250102/retry", 404},
			{"GET", "/api/rotation/reference-captures?source=now", 400},
			{"POST", "/api/rotation/reference-captures", 405},
		} {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", valid)
			req.Header.Set("Origin", "https://demo.candlea.cn")
			req.Header.Set("X-CSRF-Token", strings.Repeat("a", 32))
			req.AddCookie(&http.Cookie{Name: "__Host-candela-csrf", Value: strings.Repeat("a", 32)})
			before := syncCalls.Load()
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			if rec.Code != tc.status || strings.Contains(rec.Body.String(), "server-only-secret") {
				t.Fatalf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			if tc.status == 400 || tc.status == 405 || strings.HasSuffix(tc.path, "/retry") {
				if syncCalls.Load() != before {
					t.Fatal("invalid capture request forwarded")
				}
			}
			if tc.method == "HEAD" && rec.Body.Len() != 0 {
				t.Fatal("HEAD returned body")
			}
		}
		syncFailure.Store(true)
		defer syncFailure.Store(false)
		req := httptest.NewRequest("GET", "/api/rotation/reference-captures", nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 502 || strings.Contains(rec.Body.String(), "server-only-secret") {
			t.Fatal("upstream capture failure leaked", rec.Code)
		}
	})
	t.Run("range proxy forwards bounded query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/rotation/backtest/range?start=20240102&end=20250102", nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"range":null`) {
			t.Fatalf("range proxy: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("daily archive uses exact read-only proxy and validates pagination", func(t *testing.T) {
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{"GET", "/api/rotation/daily/dates?before=20250103&limit=2", 200},
			{"HEAD", "/api/rotation/daily/dates?before=20250103&limit=2", 200},
			{"GET", "/api/rotation/daily/dates?before=20250230", 400},
			{"GET", "/api/rotation/daily/dates?limit=101", 400},
			{"GET", "/api/rotation/daily/dates?limit=1&limit=2", 400},
			{"GET", "/api/rotation/daily/dates?tradeDate=20250102", 400},
			{"GET", "/api/rotation/daily/dates/extra", 404},
			{"POST", "/api/rotation/daily/dates", 405},
		} {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", valid)
			req.Header.Set("Origin", "https://demo.candlea.cn")
			req.Header.Set("X-CSRF-Token", strings.Repeat("a", 32))
			req.AddCookie(&http.Cookie{Name: "__Host-candela-csrf", Value: strings.Repeat("a", 32)})
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("%s %s: %d want %d", tc.method, tc.path, rec.Code, tc.status)
			}
			if tc.method == "HEAD" && rec.Body.Len() != 0 {
				t.Fatal("HEAD returned body")
			}
		}
	})
	t.Run("daily date query is forwarded and upstream errors are sanitized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/rotation/daily?tradeDate=20250102", nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "20250102") {
			t.Fatalf("daily proxy: %d %s", rec.Code, rec.Body.String())
		}
		syncFailure.Store(true)
		defer syncFailure.Store(false)
		rec = httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 502 || strings.Contains(rec.Body.String(), "server-only-secret") {
			t.Fatal("unsafe upstream error", rec.Code, rec.Body.String())
		}
	})
	t.Run("frontend style nonce is unique and does not relax scripts", func(t *testing.T) {
		previous := ""
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodGet, "/strategies/four-etf-rotation", nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", valid)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			match := regexp.MustCompile(`name="candela-style-nonce" content="([A-Za-z0-9+/]+)"`).FindStringSubmatch(rec.Body.String())
			if len(match) != 2 || match[1] == previous {
				t.Fatal("missing or reused style nonce")
			}
			policy := rec.Header().Get("Content-Security-Policy")
			if !strings.Contains(policy, "style-src 'self' 'nonce-"+match[1]+"'") || !strings.Contains(policy, "script-src 'self';") || strings.Contains(policy, "unsafe-inline") {
				t.Fatal("unexpected frontend CSP", policy)
			}
			previous = match[1]
		}
	})
	t.Run("backtest upstream failure is sanitized", func(t *testing.T) {
		syncFailure.Store(true)
		defer syncFailure.Store(false)
		req := httptest.NewRequest("GET", "/api/rotation/backtest", nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 502 || strings.Contains(rec.Body.String(), "server-only-secret") {
			t.Fatal("internal error escaped")
		}
	})
	for from, to := range map[string]string{"/data": "/admin/data", "/research": "/"} {
		req := httptest.NewRequest("GET", from, nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != to {
			t.Fatalf("old route %s did not redirect to %s", from, to)
		}
	}

	req := httptest.NewRequest("GET", "/api/v1/sync/status", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", valid)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatal("arbitrary internal endpoint exposed")
	}
	t.Run("cross-site and unverified writes never reach syncer", func(t *testing.T) {
		for _, origin := range []string{"https://evil.invalid", "https://demo.candlea.cn", ""} {
			req := httptest.NewRequest("POST", "/api/catalog", nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", valid)
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			if rec.Code != 403 {
				t.Fatalf("unsafe write: %d", rec.Code)
			}
		}
		req := httptest.NewRequest("GET", "/api/session", nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		var session struct {
			CSRFToken string `json:"csrfToken"`
		}
		json.Unmarshal(rec.Body.Bytes(), &session)
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatal("CSRF cookie is not protected")
		}
		req = httptest.NewRequest("POST", "/api/catalog", nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		req.Header.Set("Origin", "https://demo.candlea.cn")
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		req.AddCookie(cookies[0])
		rec = httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 405 {
			t.Fatal("read-only site accepted a write")
		}
	})
	t.Run("unknown and traversal paths remain closed", func(t *testing.T) {
		for _, path := range []string{"/.env", "/research/../.env", "/research/", "/api/catalog/extra"} {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", valid)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			if rec.Code != 404 {
				t.Fatalf("unexpected exposure %s %d", path, rec.Code)
			}
		}
	})

	t.Run("sync writes require Access CSRF and an allowlisted body", func(t *testing.T) {
		csrf := strings.Repeat("x", 43)
		send := func(method, path, body, origin, token string, cookie bool) *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Cf-Access-Jwt-Assertion", valid)
			req.Header.Set("Origin", origin)
			req.Header.Set("X-CSRF-Token", token)
			if cookie {
				req.AddCookie(&http.Cookie{Name: "__Host-candela-csrf", Value: csrf})
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			return rec
		}
		for _, tc := range []struct {
			method, path, body, origin, token string
			cookie                            bool
			status                            int
		}{
			{"POST", "/api/etf-syncs", `{"codes":["510880.SH"]}`, "https://evil.invalid", csrf, true, 403},
			{"POST", "/api/etf-syncs", `{}`, "https://demo.candlea.cn", csrf, false, 403},
			{"POST", "/api/etf-syncs", `{"codes":["510880.SH"],"endDate":"20300101"}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/etf-syncs", `{"codes":["../sync"]}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/retry", `{}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/retry", "", "https://evil.invalid", csrf, true, 403},
			{"POST", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", `{}`, "https://demo.candlea.cn", csrf, true, 405},
			{"GET", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/retry", "", "", "", false, 405},
			{"GET", "/api/etf-syncs?endDate=20300101", "", "", "", false, 400},
			{"POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://evil.invalid", csrf, true, 403},
			{"POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://demo.candlea.cn", csrf, false, 403},
			{"POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://demo.candlea.cn", "mismatch", true, 403},
			{"POST", "/api/sync-runs", `{"mode":"cancel"}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs", `{"mode":"backfill","code":"510300.SH"}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs", `{"mode":"backfill"}{}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs", strings.Repeat("x", 2048), "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", `{}`, "https://demo.candlea.cn", csrf, true, 405},
			{"DELETE", "/api/sync-runs", `{}`, "https://demo.candlea.cn", csrf, true, 405},
			{"POST", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel", "", "https://evil.invalid", csrf, true, 403},
			{"POST", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel", "", "https://demo.candlea.cn", csrf, false, 403},
			{"POST", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel", "{}", "https://demo.candlea.cn", csrf, true, 400},
			{"GET", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel", "", "", "", false, 405},
			{"POST", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel/extra", "", "https://demo.candlea.cn", csrf, true, 405},
			{"GET", "/api/sync-runs/../../sync/status", "", "", "", false, 404},
			{"GET", "/api/sync-runs?target=evil", "", "", "", false, 400},
		} {
			before := syncCalls.Load()
			rec := send(tc.method, tc.path, tc.body, tc.origin, tc.token, tc.cookie)
			if rec.Code != tc.status || syncCalls.Load() != before {
				t.Fatalf("unsafe %s %s: %d", tc.method, tc.path, rec.Code)
			}
		}
		rec := send("POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://demo.candlea.cn", csrf, true)
		if rec.Code != 202 || !strings.Contains(rec.Body.String(), "queued") {
			t.Fatalf("accepted %d %s", rec.Code, rec.Body.String())
		}
		cancelPath := "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel"
		if rec := send("POST", cancelPath, "", "https://demo.candlea.cn", csrf, true); rec.Code != 200 {
			t.Fatalf("cancel proxy: %d %s", rec.Code, rec.Body.String())
		}
		for _, path := range []string{"/api/sync-runs", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
			if rec := send("GET", path, "", "", "", false); rec.Code != 200 {
				t.Fatal("query failed", rec.Code)
			}
		}
		for _, path := range []string{"/api/etf-syncs", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/retry", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/cancel"} {
			body := ""
			if path == "/api/etf-syncs" {
				body = `{"codes":["510880.SH"]}`
			}
			if res := send("POST", path, body, "https://demo.candlea.cn", csrf, true); res.Code != 202 {
				t.Fatalf("ETF write %s: %d %s", path, res.Code, res.Body.String())
			}
		}
		for _, path := range []string{"/api/etf-syncs", "/api/etf-syncs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
			if res := send("GET", path, "", "", "", false); res.Code != 200 {
				t.Fatalf("ETF read %s: %d", path, res.Code)
			}
		}
		syncFailure.Store(true)
		defer syncFailure.Store(false)
		if res := send("POST", "/api/etf-syncs", `{"codes":["510880.SH"]}`, "https://demo.candlea.cn", csrf, true); res.Code != 502 || strings.Contains(res.Body.String(), "server-only-secret") {
			t.Fatal("ETF upstream details exposed", res.Code)
		}
		rec = send("POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://demo.candlea.cn", csrf, true)
		if rec.Code != 502 || strings.Contains(rec.Body.String(), "server-only-secret") {
			t.Fatal("upstream details leaked")
		}
	})

}

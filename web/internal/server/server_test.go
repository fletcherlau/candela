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
	app, err := server.New(context.Background(), server.Config{Issuer: jwks.URL, Audience: "demo-app", Origin: "https://demo.candlea.cn", SyncerURL: upstream.URL, APIKey: "server-only-secret", Static: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("Candela market overview")}}})
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
		for _, path := range []string{"/", "/admin", "/admin/data", "/data", "/api/catalog", "/api/sync-runs", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/assets/app.js", "/research/index.html"} {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", token)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			if rec.Code != 401 {
				t.Fatalf("unauthorized %s: %d", path, rec.Code)
			}
		}
	}
	for _, path := range []string{"/", "/market", "/admin", "/admin/data", "/api/catalog"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", valid)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), "server-only-secret") {
			t.Fatalf("authorized %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
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
			{"POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://evil.invalid", csrf, true, 403},
			{"POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://demo.candlea.cn", csrf, false, 403},
			{"POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://demo.candlea.cn", "mismatch", true, 403},
			{"POST", "/api/sync-runs", `{"mode":"cancel"}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs", `{"mode":"backfill","code":"510300.SH"}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs", `{"mode":"backfill"}{}`, "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs", strings.Repeat("x", 2048), "https://demo.candlea.cn", csrf, true, 400},
			{"POST", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", `{}`, "https://demo.candlea.cn", csrf, true, 405},
			{"DELETE", "/api/sync-runs", `{}`, "https://demo.candlea.cn", csrf, true, 405},
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
		for _, path := range []string{"/api/sync-runs", "/api/sync-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
			if rec := send("GET", path, "", "", "", false); rec.Code != 200 {
				t.Fatal("query failed", rec.Code)
			}
		}
		syncFailure.Store(true)
		defer syncFailure.Store(false)
		rec = send("POST", "/api/sync-runs", `{"mode":"backfill"}`, "https://demo.candlea.cn", csrf, true)
		if rec.Code != 502 || strings.Contains(rec.Body.String(), "server-only-secret") {
			t.Fatal("upstream details leaked")
		}
	})

}

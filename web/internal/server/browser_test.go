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
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type browserFixtureFiles struct{ fs.FS }

func (f browserFixtureFiles) ReadFile(name string) ([]byte, error) {
	body, err := fs.ReadFile(f.FS, name)
	if err == nil && name == "index.html" {
		body = []byte(strings.Replace(string(body), "<body>", `<body><aside role="note">隔离测试行情 · 仅用于功能和布局验收，不是真实市场数据</aside>`, 1))
	}
	return body, err
}

// TestBrowserHarness is opt-in and only serves isolated fixtures on loopback.
// It does not add any authentication bypass to the shipped website binary.
func TestBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
	}
	previewFile := os.Getenv("CANDELA_PREVIEW_RESULT")
	address := "127.0.0.1:18081"
	tokenFile := "/tmp/candela-browser-test-token"
	lifetime := time.Hour
	var published []byte
	if previewFile != "" {
		address = "127.0.0.1:18083"
		tokenFile = "/tmp/candela-rotation-preview-token"
		lifetime = 24 * time.Hour
		var err error
		published, err = os.ReadFile(previewFile)
		if err != nil || !json.Valid(published) {
			t.Fatal("preview requires an existing valid published-result JSON snapshot")
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "alg": "RS256", "kid": "browser", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
	}))
	defer jwks.Close()
	fixture := `{"groups":[{"id":"csi","name":"中证全指","status":"not_connected","items":[{"id":"csi:000985.CSI","code":"000985.CSI","name":"中证全指","kind":"指数日线","status":"not_connected","rows":null,"startDate":"","endDate":""}]},{"id":"etf","name":"ETF","status":"available","items":[{"id":"etf-daily:510300.SH","code":"510300.SH","name":"沪深300ETF","kind":"日线行情","status":"available","rows":200,"syncEnabled":true,"startDate":"20240102","endDate":"20240906"},{"id":"etf-factor:159999.SZ","code":"159999.SZ","name":"测试空数据ETF","kind":"复权因子","status":"no_data","rows":0,"syncEnabled":false,"startDate":"","endDate":""}]},{"id":"sw","name":"申万行业","status":"error","message":"申万数据读取失败，请稍后重试。","items":[]}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/rotation/reference-captures") {
			if os.Getenv("CANDELA_CAPTURE_BROWSER_TEST") == "1" || os.Getenv("CANDELA_REFERENCE_BROWSER_TEST") == "1" {
				address := "http://127.0.0.1:18086"
				if os.Getenv("CANDELA_REFERENCE_BROWSER_TEST") == "1" {
					address = "http://127.0.0.1:18087"
				}
				target, _ := url.Parse(address)
				httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
			} else {
				w.Write([]byte(`{"runs":[]}`))
			}
			return
		}
		if r.URL.Path == "/api/v1/rotation/daily" || r.URL.Path == "/api/v1/rotation/daily/dates" {
			if os.Getenv("CANDELA_DAILY_BROWSER_TEST") == "1" || os.Getenv("CANDELA_REFERENCE_BROWSER_TEST") == "1" {
				address := "http://127.0.0.1:18084"
				if os.Getenv("CANDELA_REFERENCE_BROWSER_TEST") == "1" {
					address = "http://127.0.0.1:18087"
				}
				target, _ := url.Parse(address)
				httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
			} else {
				if r.URL.Path == "/api/v1/rotation/daily/dates" {
					w.Write([]byte(`{"status":"ready","currentDate":"20250103","earliestDate":"","latestDate":"","dates":[],"nextBefore":""}`))
					return
				}
				stage := map[string]any{"status": "unavailable", "available": 0, "message": "浏览器测试未接入每日数据", "updatedAt": "", "missing": []any{}}
				slips := []map[string]any{}
				for _, code := range []string{"510880.SH", "518880.SH", "159915.SZ", "513100.SH"} {
					slips = append(slips, map[string]any{"code": code, "bps": nil, "reason": "未接入每日数据"})
				}
				json.NewEncoder(w).Encode(map[string]any{"requestedDate": "", "currentDate": "", "currentTradingDate": "", "tradeDate": "", "selectionMode": "default", "fallback": false, "fallbackReason": "", "calendarStatus": "unavailable", "status": "unavailable", "message": "浏览器测试未接入每日数据", "close": nil, "reference": nil, "referenceStatus": "missing", "referenceState": stage, "closeState": stage, "priceSlippage": slips})
			}
			return
		}
		if r.URL.Path == "/api/v1/rotation/backtest/range" {
			target, _ := url.Parse("http://127.0.0.1:18085")
			httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/v1/rotation/backtest" {
			if previewFile != "" {
				w.Write(published)
				return
			}
			rows := []map[string]any{}
			codes := []string{"510880.SH", "518880.SH", "159915.SZ", "513100.SH"}
			for i := 0; i < 700; i++ {
				dt := time.Date(2024, 10, 1+i, 0, 0, 0, 0, time.UTC).Format("20060102")
				rows = append(rows, map[string]any{"date": dt, "nav": 1 + float64(i)*.0005 + math.Sin(float64(i)/25)*.025, "holding": codes[(i/80)%4], "weight": .7, "cashWeight": .3, "cost": 0, "turnover": 0, "benchmarks": []float64{1 + float64(i)*.0002, 1 + float64(i)*.0006, 1 + math.Sin(float64(i)/40)*.08, 1 + float64(i)*.0004}})
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "ready", "message": "", "updatedAt": "2026-09-01T00:00:00Z", "result": map[string]any{"version": "fixture", "start": rows[0]["date"], "end": rows[len(rows)-1]["date"], "costBps": 10, "codes": codes, "names": []string{"红利 ETF", "黄金 ETF", "创业板 ETF", "纳指 ETF"}, "days": rows}})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/data/etf-syncs") {
			if os.Getenv("CANDELA_ETF_BROWSER_TEST") == "1" {
				target, _ := url.Parse("http://127.0.0.1:18089")
				httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
			} else {
				w.Write([]byte(`{"batches":[]}`))
			}
			return
		}
		if r.URL.Path == "/api/v1/data/sync-runs" {
			w.Write([]byte(`{"runs":[]}`))
			return
		}
		w.Write([]byte(fixture))
	}))
	defer upstream.Close()
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	msg := enc(map[string]any{"alg": "RS256", "kid": "browser"}) + "." + enc(map[string]any{"iss": jwks.URL, "aud": []string{"browser-test"}, "sub": "fixture", "exp": time.Now().Add(lifetime).Unix()})
	hash := sha256.Sum256([]byte(msg))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	token := msg + "." + base64.RawURLEncoding.EncodeToString(sig)
	if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tokenFile)
	upstreamURL := upstream.URL
	if os.Getenv("CANDELA_SYNC_BROWSER_TEST") == "1" {
		upstreamURL = "http://127.0.0.1:18082"
	}
	var static fs.FS = os.DirFS("../../frontend/dist")
	if previewFile == "" {
		static = browserFixtureFiles{static}
	}
	app, err := server.New(context.Background(), server.Config{Issuer: jwks.URL, Audience: "browser-test", Origin: "http://" + address, APIKey: "fixture-only", SyncerURL: upstreamURL, Static: static, Research: os.DirFS("../../../etf-rotation/dca-dashboard")})
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler = app
	if previewFile != "" {
		// Local preview only: exchange a signed test JWT for an HttpOnly cookie.
		// Every request still passes the real application's signature/audience verifier.
		// No production key, issuer or write endpoint is used by this snapshot preview.
		loginURL := "http://" + address + "/preview-session?token=" + token
		if err := os.WriteFile("/tmp/candela-rotation-preview-url", []byte(loginURL), 0600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove("/tmp/candela-rotation-preview-url")
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/preview-session" {
				candidate := r.URL.Query().Get("token")
				check := httptest.NewRequest(http.MethodGet, "/api/rotation/backtest", nil)
				check.Header.Set("Cf-Access-Jwt-Assertion", candidate)
				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, check)
				if rec.Code != http.StatusOK {
					http.Error(w, "预览凭证无效", http.StatusUnauthorized)
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "candela-local-preview", Value: candidate, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(lifetime.Seconds())})
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
				http.Redirect(w, r, "/strategies/four-etf-rotation", http.StatusSeeOther)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.Error(w, "只读预览", http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("Cf-Access-Jwt-Assertion") == "" {
				if cookie, err := r.Cookie("candela-local-preview"); err == nil && !strings.ContainsAny(cookie.Value, "\r\n") {
					r.Header.Set("Cf-Access-Jwt-Assertion", cookie.Value)
				}
			}
			app.ServeHTTP(w, r)
		})
	}
	t.Fatal(http.ListenAndServe(address, handler))
}

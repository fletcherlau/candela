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
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestBrowserHarness is opt-in and only serves isolated fixtures on loopback.
// It does not add any authentication bypass to the shipped website binary.
func TestBrowserHarness(t *testing.T) {
	if os.Getenv("CANDELA_BROWSER_TEST") != "1" {
		t.Skip("browser harness is opt-in")
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
		if r.URL.Path == "/api/v1/rotation/backtest" {
			rows := []map[string]any{}
			codes := []string{"510880.SH", "518880.SH", "159915.SZ", "513100.SH"}
			for i := 0; i < 700; i++ {
				dt := time.Date(2024, 10, 1+i, 0, 0, 0, 0, time.UTC).Format("20060102")
				rows = append(rows, map[string]any{"date": dt, "nav": 1 + float64(i)*.0005 + math.Sin(float64(i)/25)*.025, "holding": codes[(i/80)%4], "weight": .7, "cashWeight": .3, "cost": 0, "turnover": 0, "benchmarks": []float64{1 + float64(i)*.0002, 1 + float64(i)*.0006, 1 + math.Sin(float64(i)/40)*.08, 1 + float64(i)*.0004}})
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "ready", "message": "", "updatedAt": "2026-09-01T00:00:00Z", "result": map[string]any{"version": "fixture", "start": rows[0]["date"], "end": rows[len(rows)-1]["date"], "costBps": 10, "codes": codes, "names": []string{"红利 ETF", "黄金 ETF", "创业板 ETF", "纳指 ETF"}, "days": rows}})
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
	msg := enc(map[string]any{"alg": "RS256", "kid": "browser"}) + "." + enc(map[string]any{"iss": jwks.URL, "aud": []string{"browser-test"}, "sub": "fixture", "exp": time.Now().Add(time.Hour).Unix()})
	hash := sha256.Sum256([]byte(msg))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	token := msg + "." + base64.RawURLEncoding.EncodeToString(sig)
	if err := os.WriteFile("/tmp/candela-browser-test-token", []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove("/tmp/candela-browser-test-token")
	upstreamURL := upstream.URL
	if os.Getenv("CANDELA_SYNC_BROWSER_TEST") == "1" {
		upstreamURL = "http://127.0.0.1:18082"
	}
	app, err := server.New(context.Background(), server.Config{Issuer: jwks.URL, Audience: "browser-test", Origin: "http://127.0.0.1:18081", APIKey: "fixture-only", SyncerURL: upstreamURL, Static: os.DirFS("../../frontend/dist"), Research: os.DirFS("../../../etf-rotation/dca-dashboard")})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal(http.ListenAndServe("127.0.0.1:18081", app))
}

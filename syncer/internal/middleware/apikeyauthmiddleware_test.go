package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRejectsMissingAndWrongKey(t *testing.T) {
	reached := false
	handler := NewApiKeyAuthMiddleware("secret").Handle(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})

	for _, key := range []string{"", "wrong", "secret-longer", "secre"} {
		reached = false
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/etf-daily", nil)
		if key != "" {
			req.Header.Set("X-Api-Key", key)
		}
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("key %q: expect 401, got %d", key, rec.Code)
		}
		if reached {
			t.Fatalf("key %q: downstream handler must not be reached on 401", key)
		}
	}
}

func TestPassesWithCorrectKey(t *testing.T) {
	reached := false
	handler := NewApiKeyAuthMiddleware("secret").Handle(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
	req.Header.Set("X-Api-Key", "secret")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !reached || rec.Code != http.StatusOK {
		t.Fatalf("expect pass-through with correct key, reached=%v code=%d", reached, rec.Code)
	}
}

func TestPassThroughWhenKeyUnconfigured(t *testing.T) {
	reached := false
	handler := NewApiKeyAuthMiddleware("").Handle(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/etf-daily", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !reached {
		t.Fatal("expect pass-through when api key unconfigured")
	}
}

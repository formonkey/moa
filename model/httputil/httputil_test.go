package httputil

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDoWithRetry_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer ts.Close()

	client := &http.Client{}
	resp, err := DoWithRetry(client, func() (*http.Request, error) {
		return http.NewRequest("GET", ts.URL, nil)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDoWithRetry_RetryOn429(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer ts.Close()

	client := &http.Client{}
	resp, err := DoWithRetry(client, func() (*http.Request, error) {
		return http.NewRequest("GET", ts.URL, nil)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoWithRetry_ExhaustsRetries(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write([]byte("service unavailable"))
	}))
	defer ts.Close()

	client := &http.Client{}
	_, err := DoWithRetry(client, func() (*http.Request, error) {
		return http.NewRequest("GET", ts.URL, nil)
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestDoWithRetry_NonRetryableError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("bad request"))
	}))
	defer ts.Close()

	client := &http.Client{}
	resp, err := DoWithRetry(client, func() (*http.Request, error) {
		return http.NewRequest("GET", ts.URL, nil)
	})
	if err != nil {
		t.Fatalf("unexpected error for non-retryable: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDoWithRetry_RetryAfterHeader(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	client := &http.Client{}
	resp, err := DoWithRetry(client, func() (*http.Request, error) {
		return http.NewRequest("GET", ts.URL, nil)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   int
	}{
		{"", 0},
		{"5", 5},
		{"invalid", 0},
		{"0", 0},
	}

	for _, tt := range tests {
		resp := &http.Response{Header: http.Header{}}
		if tt.header != "" {
			resp.Header.Set("Retry-After", tt.header)
		}
		got := parseRetryAfter(resp)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %d, want %d", tt.header, got, tt.want)
		}
	}
}

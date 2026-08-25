package app

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHandlePricingCachesUpstreamResponse(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ipricing" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected authorization: %q", r.Header.Get("Authorization"))
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"video":[{"model_name":"MiniMax-H3","duration":"4-15","price_info":[{"768p":0.2}]}]}}`))
	}))
	defer upstream.Close()

	empty := ""
	apiKey := "test-token"
	verbose := false
	s := &webUIServer{clientOpts: clientOptions{
		baseURL: &upstream.URL, ossURL: &empty, apiKey: &apiKey,
		username: &empty, password: &empty, verbose: &verbose, logFile: &empty,
	}}

	for range 2 {
		recorder := httptest.NewRecorder()
		s.handlePricing(recorder, httptest.NewRequest(http.MethodGet, "/api/ipricing", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Cache-Control") != "private, max-age=86400" {
			t.Fatalf("unexpected cache control: %q", recorder.Header().Get("Cache-Control"))
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", requests.Load())
	}
}

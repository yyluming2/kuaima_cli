package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRechargeAPI(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token1" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/user/dogetinfo":
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"amount_options":[10,100],"discount":{"100":0.95},"plans":[{"id":2,"title":"Pro套餐","price_amount":48,"currency":"USD","duration_unit":"month","duration_value":1,"enabled":true,"total_amount":25000000}]}}`))
		case "/api/user/dopay":
			var req doPayRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Amount != 100 || req.PaymentMethod != defaultPaymentMethod {
				t.Fatalf("unexpected pay request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"success","data":{"scancode_url":"https://pay.example/1","trade_no":"T1"}}`))
		case "/api/user/dosubscription":
			var req doSubscriptionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.PlanID != 2 || req.PaymentMethod != defaultPaymentMethod {
				t.Fatalf("unexpected subscription request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"success","data":{"scancode_url":"https://pay.example/2","trade_no":"T2"}}`))
		case "/api/usage/token/exchange":
			var req exchangeTokenRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Key != "code-1" {
				t.Fatalf("unexpected exchange request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":""}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	c := &client{baseURL: server.URL, apiKey: "token1", httpClient: server.Client()}
	info, err := c.getRechargeInfo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(info.AmountOptions) != 2 || info.Discount["100"] != 0.95 || len(info.Plans) != 1 || info.Plans[0].ID != 2 {
		t.Fatalf("unexpected info: %#v", info)
	}
	pay, err := c.createRechargePayment(t.Context(), 100, defaultPaymentMethod)
	if err != nil {
		t.Fatal(err)
	}
	if pay.TradeNo != "T1" || pay.ScanCodeURL != "https://pay.example/1" {
		t.Fatalf("unexpected pay response: %#v", pay)
	}
	sub, err := c.createSubscriptionPayment(t.Context(), 2, defaultPaymentMethod)
	if err != nil {
		t.Fatal(err)
	}
	if sub.TradeNo != "T2" || sub.ScanCodeURL != "https://pay.example/2" {
		t.Fatalf("unexpected subscription response: %#v", sub)
	}
	if err := c.exchangeToken(t.Context(), " code-1 "); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 {
		t.Fatalf("unexpected requests: %#v", seen)
	}
}

func TestPrintQRCode(t *testing.T) {
	var buf bytes.Buffer
	if err := printQRCode(&buf, "https://pay.example/1"); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected QR output")
	}
}

func TestWriteQRCodePNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pay.png")
	if err := writeQRCodePNG(path, "https://pay.example/1", 4); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("unexpected PNG header: %x", data[:min(8, len(data))])
	}
}

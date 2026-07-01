package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultPaymentMethod = "custom1_wxpay"

type doPayRequest struct {
	Amount        float64 `json:"amount"`
	PaymentMethod string  `json:"payment_method"`
}

type doSubscriptionRequest struct {
	PlanID        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

type exchangeTokenRequest struct {
	Key string `json:"key"`
}

func (c *client) getRechargeInfo(ctx context.Context) (*rechargeInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/user/dogetinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	//c.logRequest(req, nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	//c.logResponse(resp, data)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("recharge info request failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var payload rechargeInfoResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode recharge info response: %w", err)
	}
	if !payload.Success {
		if strings.TrimSpace(payload.Message) == "" {
			payload.Message = "recharge info request failed"
		}
		return nil, errors.New(payload.Message)
	}
	return &payload.Data, nil
}

func (c *client) createRechargePayment(ctx context.Context, amount float64, paymentMethod string) (*paymentData, error) {
	return c.createPayment(ctx, "/api/user/dopay", doPayRequest{
		Amount:        amount,
		PaymentMethod: paymentMethod,
	})
}

func (c *client) createSubscriptionPayment(ctx context.Context, planID int, paymentMethod string) (*paymentData, error) {
	return c.createPayment(ctx, "/api/user/dosubscription", doSubscriptionRequest{
		PlanID:        planID,
		PaymentMethod: paymentMethod,
	})
}

func (c *client) exchangeToken(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("exchange key is required")
	}
	body, err := json.Marshal(exchangeTokenRequest{Key: key})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/usage/token/exchange", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.logRequest(req, body)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	c.logResponse(resp, data)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("exchange request failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var payload simpleAPIResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode exchange response: %w", err)
	}
	if !payload.Success {
		if strings.TrimSpace(payload.Message) == "" {
			payload.Message = "exchange request failed"
		}
		return errors.New(payload.Message)
	}
	return nil
}

func (c *client) createPayment(ctx context.Context, path string, request any) (*paymentData, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.logRequest(req, body)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.logResponse(resp, data)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("payment request failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var payload paymentResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode payment response: %w", err)
	}
	if !payload.Success {
		if strings.TrimSpace(payload.Message) == "" {
			payload.Message = "payment request failed"
		}
		return nil, errors.New(payload.Message)
	}
	if strings.TrimSpace(payload.Data.ScanCodeURL) == "" {
		return nil, errors.New("payment response did not include scancode_url")
	}
	return &payload.Data, nil
}

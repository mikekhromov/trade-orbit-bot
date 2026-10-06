// Package core implements the private HTTP contract with Trade Orbit core-api.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type Settings struct {
	Username   string `json:"username,omitempty"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
}

type Strategy struct {
	Symbol, ConditionType, TargetPrice, RuntimeState string
}

type OutboxItem struct {
	Kind              string `json:"kind,omitempty"`
	ID                string `json:"id"`
	ActionExecutionID string `json:"actionExecutionId"`
	TriggerID         string `json:"triggerId"`
	StrategyName      string `json:"strategyName"`
	Symbol            string `json:"symbol"`
	MarketPrice       string `json:"marketPrice"`
	TargetPrice       string `json:"targetPrice"`
	ConditionType     string `json:"conditionType"`
	Channel           string `json:"channel"`
	RecipientID       string `json:"recipientId"`
	Message           string `json:"message,omitempty"`
	Attempts          int    `json:"attempts"`
}

type Candle struct {
	Time, Open, High, Low, Close, Volume string
	Complete                             bool
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Bind(ctx context.Context, chatID, username string) (bool, error) {
	var response struct {
		Accepted bool `json:"accepted"`
	}
	err := c.internal(ctx, http.MethodPost, "/internal/v1/telegram/bind", map[string]string{"chatID": chatID, "username": username}, &response)
	return response.Accepted, err
}

func (c *Client) Disable(ctx context.Context, chatID string) error {
	return c.internal(ctx, http.MethodPost, "/internal/v1/telegram/disable", map[string]string{"chatID": chatID}, nil)
}

func (c *Client) Settings(ctx context.Context) (Settings, error) {
	var response struct {
		Telegram Settings `json:"telegram"`
	}
	err := c.public(ctx, "/api/v1/notification-settings", &response)
	return response.Telegram, err
}

func (c *Client) Offset(ctx context.Context) (int64, error) {
	var response struct {
		Offset int64 `json:"offset"`
	}
	err := c.internal(ctx, http.MethodGet, "/internal/v1/telegram/offset", nil, &response)
	return response.Offset, err
}

func (c *Client) SaveOffset(ctx context.Context, value int64) error {
	return c.internal(ctx, http.MethodPut, "/internal/v1/telegram/offset", map[string]int64{"offset": value}, nil)
}

func (c *Client) Strategies(ctx context.Context) ([]Strategy, error) {
	var response struct {
		Strategies []Strategy `json:"strategies"`
	}
	err := c.public(ctx, "/api/v1/strategies", &response)
	return response.Strategies, err
}

func (c *Client) Candles(ctx context.Context, symbol string) ([]Candle, error) {
	var response struct {
		Candles []Candle `json:"candles"`
	}
	err := c.public(ctx, "/api/v1/market/candles?symbol="+url.QueryEscape(symbol)+"&range=1D", &response)
	return response.Candles, err
}

func (c *Client) Claim(ctx context.Context) (*OutboxItem, error) {
	var result OutboxItem
	status, err := c.request(ctx, http.MethodPost, "/internal/v1/notifications/claim", nil, &result, true)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	return &result, nil
}

func (c *Client) Complete(ctx context.Context, item OutboxItem, message string) error {
	return c.internal(ctx, http.MethodPost, "/internal/v1/notifications/complete", map[string]any{"item": item, "message": message}, nil)
}

func (c *Client) Retry(ctx context.Context, item OutboxItem, code string, permanent bool) error {
	return c.internal(ctx, http.MethodPost, "/internal/v1/notifications/retry", map[string]any{"item": item, "errorCode": code, "permanent": permanent}, nil)
}

func (c *Client) public(ctx context.Context, path string, target any) error {
	_, err := c.request(ctx, http.MethodGet, path, nil, target, false)
	return err
}

func (c *Client) internal(ctx context.Context, method, path string, body, target any) error {
	_, err := c.request(ctx, method, path, body, target, true)
	return err
}

func (c *Client) request(ctx context.Context, method, path string, body, target any, authorized bool) (int, error) {
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			return 0, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, &payload)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	if authorized {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("core-api %s %s returned %s", method, path, response.Status)
	}
	if target != nil && response.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(response.Body).Decode(target); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

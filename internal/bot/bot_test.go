package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mikekhromov/trade-orbit-bot/internal/core"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportErrorsDoNotExposeToken(t *testing.T) {
	const token = "12345:FAKE_TOKEN_FOR_TESTS"
	for _, method := range []string{"getUpdates", "sendPhoto"} {
		t.Run(method, func(t *testing.T) {
			b := New("https://api.telegram.org", token, false, nil)
			b.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.Contains(r.URL.Path, token) {
					t.Fatal("request must still carry the actual token")
				}
				return nil, context.DeadlineExceeded
			})
			var err error
			if method == "sendPhoto" {
				err = b.sendPhoto(context.Background(), "1", "test", []byte("picture"))
			} else {
				_, err = b.getUpdates(context.Background(), 0)
			}
			if err == nil || strings.Contains(fmt.Sprintf("%v", err), token) {
				t.Fatal("token leaked or transport error lost")
			}
			if !strings.Contains(err.Error(), method) || !strings.Contains(err.Error(), "[REDACTED]") {
				t.Fatalf("missing diagnostic context: %v", err)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("timeout classification lost")
			}
		})
	}
}

func TestMorningPairNotificationIncludesCombinedChart(t *testing.T) {
	coreServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pair-strategies":
			_, _ = io.WriteString(w, `{"strategies":[{"id":"pair-1","name":"Сбер / Яндекс","morningReport":true},{"id":"pair-2","name":"Не включена","morningReport":false}]}`)
		case "/api/v1/pair-strategies/pair-1/chart":
			_, _ = io.WriteString(w, `{"points":[{"time":"2026-10-07T08:00:00Z","zScore":0.5},{"time":"2026-10-07T09:00:00Z","zScore":1.2}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer coreServer.Close()

	var photoSent bool
	bot := &Bot{baseURL: "https://telegram.test", token: "test-token", core: core.New(coreServer.URL, ""), http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(request.URL.Path, "/sendPhoto") {
			t.Fatalf("unexpected Telegram endpoint: %s", request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "Сбер / Яндекс") || !strings.Contains(string(body), "PAPER-стратегий") {
			t.Fatal("morning chart caption does not identify the strategy")
		}
		photoSent = true
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
	})}}

	message, err := bot.sendNotification(context.Background(), core.OutboxItem{Kind: "PAIR", EventType: "MORNING_REPORT", RecipientID: "42", Message: "Утренний отчёт PAPER-стратегий"})
	if err != nil {
		t.Fatal(err)
	}
	if !photoSent || !strings.Contains(message, "Сбер / Яндекс") {
		t.Fatalf("morning report chart was not sent: %q", message)
	}
}

func TestInvalidRequestURLDoesNotExposeToken(t *testing.T) {
	b := New("https://invalid\n.example", "12345:FAKE_TOKEN_FOR_TESTS", false, nil)
	_, err := b.getUpdates(context.Background(), 0)
	if err == nil || strings.Contains(err.Error(), b.token) {
		t.Fatal("invalid URL error leaked token or was lost")
	}
}

func TestProviderErrorsDoNotExposeToken(t *testing.T) {
	for _, method := range []string{"getUpdates", "sendMessage", "sendPhoto", "setMyCommands"} {
		t.Run(method, func(t *testing.T) {
			b := New("https://api.telegram.org", "12345:FAKE_TOKEN_FOR_TESTS", false, nil)
			b.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				body, _ := json.Marshal(map[string]any{
					"ok": false, "error_code": 403,
					"description": "invalid " + b.token + " or " + url.QueryEscape(b.token),
				})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})
			var err error
			switch method {
			case "getUpdates":
				_, err = b.getUpdates(context.Background(), 0)
			case "sendMessage":
				err = b.sendMessage(context.Background(), "1", "test")
			case "sendPhoto":
				err = b.sendPhoto(context.Background(), "1", "test", []byte("picture"))
			case "setMyCommands":
				err = b.setCommands(context.Background())
			}
			if err == nil || strings.Contains(err.Error(), b.token) || strings.Contains(err.Error(), url.QueryEscape(b.token)) {
				t.Fatal("provider error leaked token or was lost")
			}
			if method == "sendMessage" || method == "sendPhoto" {
				var classified interface{ Permanent() bool }
				if !errors.As(err, &classified) || !classified.Permanent() {
					t.Fatal("provider error classification lost")
				}
			}
		})
	}
}

func TestPairNotificationUsesPersistedMessageWithoutChartLookup(t *testing.T) {
	var sent string
	bot := &Bot{baseURL: "https://telegram.test", token: "test-token", http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(request.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected Telegram endpoint: %s", request.URL.Path)
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		sent = payload.Text
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
	})}}
	message, err := bot.sendNotification(context.Background(), core.OutboxItem{Kind: "PAIR", RecipientID: "42", Message: "Trade Orbit · PAPER\nAAA / BBB"})
	if err != nil {
		t.Fatal(err)
	}
	if message != sent || !strings.Contains(sent, "AAA / BBB") {
		t.Fatalf("unexpected pair notification: %q", sent)
	}
}

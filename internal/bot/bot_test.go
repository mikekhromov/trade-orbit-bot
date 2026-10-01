package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
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

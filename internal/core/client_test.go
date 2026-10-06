package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type clientRoundTripFunc func(*http.Request) (*http.Response, error)

func (f clientRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPairStrategiesUsesPaperPairEndpoint(t *testing.T) {
	client := New("https://core-api.test", "")
	client.http.Transport = clientRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/pair-strategies" {
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"strategies":[{"name":"Сбер / Яндекс","symbolA":"SBER","symbolB":"YDEX","mode":"PAPER","status":"ACTIVE","dataState":"DEGRADED","dataReason":"котировки устарели","executionState":"FLAT"}]}`)),
		}, nil
	})

	strategies, err := client.PairStrategies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(strategies) != 1 || strategies[0].SymbolA != "SBER" || strategies[0].DataState != "DEGRADED" || strategies[0].DataReason == "" {
		t.Fatalf("unexpected PAPER strategies response: %+v", strategies)
	}
}

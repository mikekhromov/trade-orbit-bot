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
			Body:       io.NopCloser(strings.NewReader(`{"strategies":[{"id":"pair-1","name":"Сбер / Яндекс","symbolA":"SBER","symbolB":"YDEX","mode":"PAPER","status":"ACTIVE","dataState":"DEGRADED","dataReason":"котировки устарели","executionState":"FLAT","lastZScore":2.25,"morningReport":true}]}`)),
		}, nil
	})

	strategies, err := client.PairStrategies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(strategies) != 1 || strategies[0].ID != "pair-1" || strategies[0].SymbolA != "SBER" || strategies[0].DataState != "DEGRADED" || strategies[0].DataReason == "" || strategies[0].LastZScore == nil || *strategies[0].LastZScore != 2.25 || !strategies[0].MorningReport {
		t.Fatalf("unexpected PAPER strategies response: %+v", strategies)
	}
}

func TestPairChartLoadsStrategyZScoreSeries(t *testing.T) {
	client := New("https://core-api.test", "")
	client.http.Transport = clientRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/pair-strategies/pair-1/chart" {
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"points":[{"time":"2026-10-07T08:00:00Z","zScore":1.25}]}`)),
		}, nil
	})

	data, err := client.PairChart(context.Background(), "pair-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Points) != 1 || data.Points[0].ZScore != 1.25 || data.Points[0].Time.IsZero() {
		t.Fatalf("unexpected strategy chart response: %+v", data)
	}
}

func TestPairHistoryLoadsPaperPnl(t *testing.T) {
	client := New("https://core-api.test", "")
	client.http.Transport = clientRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/pair-strategies/pair-1/history" {
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"executions":[],"stats":{"completedTrades":2,"winRate":0.5,"netPnl":"12.30","openNetPnl":"-1.25"}}`)),
		}, nil
	})

	history, err := client.PairHistory(context.Background(), "pair-1")
	if err != nil {
		t.Fatal(err)
	}
	if history.Stats.CompletedTrades != 2 || history.Stats.WinRate != .5 || history.Stats.NetPnL != "12.30" || history.Stats.OpenNetPnL != "-1.25" {
		t.Fatalf("unexpected PAPER history: %+v", history)
	}
}

package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClaimUsesPrivateContractAndBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/internal/v1/notifications/claim" { t.Errorf("path=%s", request.URL.Path) }
		if request.Header.Get("Authorization") != "Bearer internal-secret" { t.Error("missing internal bearer token") }
		_ = json.NewEncoder(writer).Encode(map[string]any{"id":"outbox-1","channel":"TELEGRAM","recipientId":"42"})
	}))
	defer server.Close()

	item, err := New(server.URL, "internal-secret").Claim(t.Context())
	if err != nil { t.Fatal(err) }
	if item == nil || item.ID != "outbox-1" || item.RecipientID != "42" { t.Fatalf("item=%+v", item) }
}

func TestClaimReturnsNilForEmptyQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	item, err := New(server.URL, "internal-secret").Claim(t.Context())
	if err != nil { t.Fatal(err) }
	if item != nil { t.Fatalf("item=%+v", item) }
}

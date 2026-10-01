package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mikekhromov/trade-orbit-bot/internal/bot"
	"github.com/mikekhromov/trade-orbit-bot/internal/core"
)

func main() {
	token := os.Getenv("TOKEN_TG_BOT")
	if token == "" {
		log.Fatal("TOKEN_TG_BOT is required")
	}
	coreClient := core.New(value("CORE_API_URL", "http://core-api:8080"), value("INTERNAL_SERVICE_TOKEN", "local-development-only"))
	telegram := bot.New(value("TELEGRAM_API_BASE_URL", "https://api.telegram.org"), token, boolean("TELEGRAM_INSECURE_SKIP_VERIFY"), coreClient)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go telegram.RunPolling(ctx)
	go telegram.RunDispatcher(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "telegram-bot"})
	})
	server := &http.Server{Addr: value("HTTP_ADDR", ":8081"), Handler: mux}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("telegram-bot listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func value(key, fallback string) string {
	if current := os.Getenv(key); current != "" {
		return current
	}
	return fallback
}
func boolean(key string) bool {
	result, err := strconv.ParseBool(os.Getenv(key))
	return err == nil && result
}

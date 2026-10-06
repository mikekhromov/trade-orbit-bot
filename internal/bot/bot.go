// Package bot owns Telegram interaction and durable notification delivery.
package bot

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mikekhromov/trade-orbit-bot/internal/chart"
	"github.com/mikekhromov/trade-orbit-bot/internal/core"
)

type Bot struct {
	baseURL, token string
	http           *http.Client
	core           *core.Client
}

type providerError struct {
	method, description string
	code                int
}

func (e providerError) Error() string   { return fmt.Sprintf("telegram %s: %s", e.method, e.description) }
func (e providerError) Permanent() bool { return e.code >= 400 && e.code < 500 && e.code != 429 }

type redactedError struct {
	message string
	cause   error
}

func (e redactedError) Error() string { return e.message }
func (e redactedError) Unwrap() error { return e.cause }

func (b *Bot) redact(text string) string {
	if b.token == "" {
		return text
	}
	for _, secret := range []string{b.token, url.PathEscape(b.token), url.QueryEscape(b.token)} {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func (b *Bot) safeError(err error) error {
	if err == nil {
		return nil
	}
	message := b.redact(err.Error())
	if message == err.Error() {
		return err
	}
	return redactedError{message: message, cause: err}
}

func New(baseURL, token string, insecure bool, coreClient *core.Client) *Bot {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} // #nosec G402 local-only escape hatch.
	return &Bot{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 35 * time.Second, Transport: transport}, core: coreClient}
}

func (b *Bot) RunPolling(ctx context.Context) {
	if err := b.setCommands(ctx); err != nil {
		log.Printf("telegram set commands: %v", err)
	}
	offset, err := b.core.Offset(ctx)
	if err != nil {
		log.Printf("telegram offset: %v", err)
	}
	for ctx.Err() == nil {
		updates, requestErr := b.getUpdates(ctx, offset+1)
		if requestErr != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("telegram polling: %v", requestErr)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		for _, value := range updates {
			if value.UpdateID > offset {
				offset = value.UpdateID
				if err := b.core.SaveOffset(ctx, offset); err != nil {
					log.Printf("save telegram offset: %v", err)
				}
			}
			b.handle(ctx, value)
		}
	}
}

func (b *Bot) RunDispatcher(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				item, err := b.core.Claim(ctx)
				if err != nil {
					log.Printf("claim notification: %v", err)
					break
				}
				if item == nil {
					break
				}
				message, sendErr := b.sendNotification(ctx, *item)
				if sendErr == nil {
					if err := b.core.Complete(ctx, *item, message); err != nil {
						log.Printf("complete notification: %v", err)
					}
					continue
				}
				permanent := false
				var classified interface{ Permanent() bool }
				if errors.As(sendErr, &classified) {
					permanent = classified.Permanent()
				}
				if err := b.core.Retry(ctx, *item, "TELEGRAM_PROVIDER_ERROR", permanent); err != nil {
					log.Printf("retry notification: %v", err)
				}
			}
		}
	}
}

func (b *Bot) sendNotification(ctx context.Context, item core.OutboxItem) (string, error) {
	if item.Kind == "PAIR" && item.Message != "" {
		return item.Message, b.sendMessage(ctx, item.RecipientID, item.Message)
	}
	text := fmt.Sprintf("🛰 Trade Orbit\n\nЦель достигнута\n%s · %s\nТекущая цена: %s\nЦелевой уровень: %s\nСтратегия: %s", item.Symbol, conditionLabel(item.ConditionType), item.MarketPrice, item.TargetPrice, item.StrategyName)
	candles, err := b.core.Candles(ctx, item.Symbol)
	if err == nil {
		picture, renderErr := chart.Render(recentCandles(candles, 4*time.Hour), item.TargetPrice, item.MarketPrice)
		if renderErr == nil {
			if photoErr := b.sendPhoto(ctx, item.RecipientID, text, picture); photoErr == nil {
				return text, nil
			}
			log.Printf("telegram chart upload fallback for %s", item.Symbol)
		} else {
			log.Printf("telegram chart render fallback for %s", item.Symbol)
		}
	} else {
		log.Printf("telegram chart data fallback for %s", item.Symbol)
	}
	return text, b.sendMessage(ctx, item.RecipientID, text)
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"chat"`
	} `json:"message"`
}

func (b *Bot) getUpdates(ctx context.Context, offset int64) ([]update, error) {
	var response struct {
		OK          bool     `json:"ok"`
		Result      []update `json:"result"`
		Description string   `json:"description"`
	}
	if err := b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message"}}, &response); err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, fmt.Errorf("getUpdates: %s", b.redact(response.Description))
	}
	return response.Result, nil
}

func (b *Bot) handle(ctx context.Context, value update) {
	if value.Message == nil || value.Message.Text == "" {
		return
	}
	chatID := strconv.FormatInt(value.Message.Chat.ID, 10)
	command := strings.ToLower(strings.Fields(value.Message.Text)[0])
	if at := strings.IndexByte(command, '@'); at >= 0 {
		command = command[:at]
	}
	switch command {
	case "/start":
		ok, err := b.core.Bind(ctx, chatID, value.Message.Chat.Username)
		if err != nil {
			log.Printf("telegram bind: %v", err)
			_ = b.sendMessage(ctx, chatID, "Не удалось подключить Trade Orbit. Попробуйте позже.")
			return
		}
		if !ok {
			_ = b.sendMessage(ctx, chatID, "Trade Orbit уже привязан к другому чату. Сначала отключите его командой /stop в привязанном чате.")
			return
		}
		_ = b.sendMessage(ctx, chatID, "Trade Orbit подключён. Уведомления стратегий будут приходить сюда.\n\n/status — состояние\n/strategies — стратегии\n/test — тест уведомления\n/stop — отключить")
	case "/stop":
		if err := b.core.Disable(ctx, chatID); err != nil {
			log.Printf("telegram disable: %v", err)
		}
		_ = b.sendMessage(ctx, chatID, "Уведомления Trade Orbit отключены. Для повторного подключения используйте /start.")
	case "/status":
		settings, _ := b.core.Settings(ctx)
		items, _ := b.core.Strategies(ctx)
		state := "отключён"
		if settings.Enabled {
			state = "подключён"
		}
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Trade Orbit работает.\nTelegram: %s\nСтратегий: %d", state, len(items)))
	case "/strategies":
		items, _ := b.core.Strategies(ctx)
		lines := []string{"Стратегии Trade Orbit:"}
		for _, item := range items {
			sign := "≥"
			if item.ConditionType == "PRICE_BELOW" {
				sign = "≤"
			}
			lines = append(lines, fmt.Sprintf("• %s: %s %s · %s", item.Symbol, sign, item.TargetPrice, item.RuntimeState))
		}
		if len(items) == 0 {
			lines = append(lines, "Активных стратегий пока нет.")
		}
		_ = b.sendMessage(ctx, chatID, strings.Join(lines, "\n"))
	case "/test":
		_ = b.sendMessage(ctx, chatID, "✅ Тестовое уведомление Trade Orbit доставлено.")
	default:
		_ = b.sendMessage(ctx, chatID, "Команды: /start, /status, /strategies, /test, /stop, /help")
	}
}

func (b *Bot) sendMessage(ctx context.Context, chatID, text string) error {
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		ErrorCode   int    `json:"error_code"`
	}
	if err := b.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, &response); err != nil {
		return err
	}
	if !response.OK {
		return providerError{"sendMessage", b.redact(response.Description), response.ErrorCode}
	}
	return nil
}
func (b *Bot) sendPhoto(ctx context.Context, chatID, caption string, picture []byte) (err error) {
	defer func() { err = b.safeError(err) }()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", chatID)
	_ = writer.WriteField("caption", caption)
	part, err := writer.CreateFormFile("photo", "trade-orbit.png")
	if err != nil {
		return err
	}
	if _, err := part.Write(picture); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/bot"+b.token+"/sendPhoto", &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := b.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var payload struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		ErrorCode   int    `json:"error_code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return err
	}
	if !payload.OK {
		return providerError{"sendPhoto", b.redact(payload.Description), payload.ErrorCode}
	}
	return nil
}
func (b *Bot) setCommands(ctx context.Context) error {
	commands := []map[string]string{{"command": "start", "description": "Подключить уведомления"}, {"command": "status", "description": "Статус Trade Orbit"}, {"command": "strategies", "description": "Список стратегий"}, {"command": "test", "description": "Проверить уведомления"}, {"command": "stop", "description": "Отключить уведомления"}, {"command": "help", "description": "Показать команды"}}
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := b.call(ctx, "setMyCommands", map[string]any{"commands": commands}, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("setMyCommands: %s", b.redact(response.Description))
	}
	return nil
}
func (b *Bot) call(ctx context.Context, method string, requestBody, responseBody any) (err error) {
	defer func() { err = b.safeError(err) }()
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/bot"+b.token+"/"+method, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := b.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram %s status %s", method, response.Status)
	}
	return json.NewDecoder(response.Body).Decode(responseBody)
}
func conditionLabel(value string) string {
	if value == "PRICE_BELOW" {
		return "цена ниже уровня"
	}
	return "цена выше уровня"
}
func recentCandles(candles []core.Candle, window time.Duration) []core.Candle {
	if len(candles) == 0 {
		return candles
	}
	last, err := time.Parse(time.RFC3339, candles[len(candles)-1].Time)
	if err != nil {
		return candles
	}
	threshold := last.Add(-window)
	for index, item := range candles {
		stamp, parseErr := time.Parse(time.RFC3339, item.Time)
		if parseErr == nil && !stamp.Before(threshold) {
			return candles[index:]
		}
	}
	return candles
}

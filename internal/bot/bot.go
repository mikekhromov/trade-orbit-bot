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
	"math/big"
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
		if item.EventType == "MORNING_REPORT" {
			return b.sendMorningReport(ctx, item)
		}
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

func (b *Bot) sendMorningReport(ctx context.Context, item core.OutboxItem) (string, error) {
	strategies, err := b.core.PairStrategies(ctx)
	if err != nil {
		log.Printf("morning report strategies: %v", err)
		return item.Message, b.sendMessage(ctx, item.RecipientID, item.Message)
	}
	series := make([]chart.Series, 0, len(strategies))
	legend := make([]string, 0, len(strategies))
	colors := []string{"🔵", "🟣", "🟢", "🟠", "🔴", "🩵"}
	for _, strategy := range strategies {
		if !strategy.MorningReport {
			continue
		}
		data, chartErr := b.core.PairChart(ctx, strategy.ID)
		if chartErr != nil {
			log.Printf("morning report chart for %s: %v", strategy.ID, chartErr)
			continue
		}
		values := make([]float64, 0, len(data.Points))
		for _, point := range data.Points {
			values = append(values, point.ZScore)
		}
		if len(values) == 0 {
			continue
		}
		series = append(series, chart.Series{Name: strategy.Name, Values: values})
		legend = append(legend, fmt.Sprintf("%s %s", colors[(len(series)-1)%len(colors)], strategy.Name))
	}
	if len(series) == 0 {
		return item.Message, b.sendMessage(ctx, item.RecipientID, item.Message)
	}
	picture, err := chart.RenderZScoreSeries(series)
	if err != nil {
		log.Printf("morning report chart render: %v", err)
		return item.Message, b.sendMessage(ctx, item.RecipientID, item.Message)
	}
	caption := item.Message + "\n\nДинамика Z-score PAPER-стратегий:\n" + strings.Join(legend, " · ")
	if err := b.sendPhoto(ctx, item.RecipientID, caption, picture); err != nil {
		log.Printf("morning report chart upload: %v", err)
		return item.Message, b.sendMessage(ctx, item.RecipientID, item.Message)
	}
	return caption, nil
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
		_ = b.sendMessage(ctx, chatID, "Trade Orbit подключён. PAPER-уведомления будут приходить сюда.\n\n"+helpText())
	case "/stop":
		if err := b.core.Disable(ctx, chatID); err != nil {
			log.Printf("telegram disable: %v", err)
		}
		_ = b.sendMessage(ctx, chatID, "Уведомления Trade Orbit отключены. Для повторного подключения используйте /start.")
	case "/status":
		settings, err := b.core.Settings(ctx)
		if err != nil {
			log.Printf("telegram status settings: %v", err)
			_ = b.sendMessage(ctx, chatID, "Не удалось получить состояние Trade Orbit. Попробуйте позже.")
			return
		}
		items, err := b.core.PairStrategies(ctx)
		if err != nil {
			log.Printf("telegram status strategies: %v", err)
			_ = b.sendMessage(ctx, chatID, "Не удалось получить состояние PAPER-стратегий. Попробуйте позже.")
			return
		}
		state := "отключён"
		if settings.Enabled {
			state = "подключён"
		}
		ready, warming, degraded, open, active, paused := 0, 0, 0, 0, 0, 0
		for _, item := range items {
			switch item.DataState {
			case "READY":
				ready++
			case "WARMING_UP":
				warming++
			case "DEGRADED":
				degraded++
			}
			if item.ExecutionState == "OPEN" {
				open++
			}
			if item.Status == "ACTIVE" {
				active++
			} else if item.Status == "PAUSED" {
				paused++
			}
		}
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Trade Orbit · PAPER\nTelegram: %s\nСтратегий: %d · активных: %d · пауза: %d\nДанные: готовы %d · прогрев %d · проблемы %d\nОткрытых пар: %d", state, len(items), active, paused, ready, warming, degraded, open))
	case "/strategies":
		items, err := b.core.PairStrategies(ctx)
		if err != nil {
			log.Printf("telegram strategies: %v", err)
			_ = b.sendMessage(ctx, chatID, "Не удалось получить PAPER-стратегии. Попробуйте позже.")
			return
		}
		lines := []string{"PAPER-стратегии Trade Orbit:"}
		for _, item := range items {
			line := fmt.Sprintf("%s %s — %s / %s\n  %s · %s · Z %s", dataStateIcon(item.DataState), item.Name, item.SymbolA, item.SymbolB, strategyStatusLabel(item.Status), executionStateLabel(item.ExecutionState), formatZScore(item.LastZScore))
			if item.DataReason != "" {
				line += "\n  Причина: " + item.DataReason
			}
			lines = append(lines, line)
		}
		if len(items) == 0 {
			lines = append(lines, "Активных стратегий пока нет.")
		}
		_ = b.sendMessage(ctx, chatID, strings.Join(lines, "\n"))
	case "/pnl":
		items, err := b.core.PairStrategies(ctx)
		if err != nil {
			log.Printf("telegram pnl strategies: %v", err)
			_ = b.sendMessage(ctx, chatID, "Не удалось загрузить PAPER-стратегии. Попробуйте позже.")
			return
		}
		if len(items) == 0 {
			_ = b.sendMessage(ctx, chatID, "PAPER-стратегий пока нет.")
			return
		}
		closedTotal, openTotal := new(big.Rat), new(big.Rat)
		lines := []string{"Результаты PAPER:"}
		for _, item := range items {
			history, err := b.core.PairHistory(ctx, item.ID)
			if err != nil {
				log.Printf("telegram pnl history for %s: %v", item.ID, err)
				_ = b.sendMessage(ctx, chatID, "Не удалось загрузить PnL всех стратегий. Попробуйте позже.")
				return
			}
			closed := parseAmount(history.Stats.NetPnL)
			openPnL := parseAmount(history.Stats.OpenNetPnL)
			closedTotal.Add(closedTotal, closed)
			openTotal.Add(openTotal, openPnL)
			lines = append(lines, fmt.Sprintf("• %s (%s / %s)\n  Сделок: %d · win rate: %.0f%%\n  PnL закрытых: %s ₽ · открытых: %s ₽", item.Name, item.SymbolA, item.SymbolB, history.Stats.CompletedTrades, history.Stats.WinRate*100, formatSignedMoney(closed), formatSignedMoney(openPnL)))
		}
		total := new(big.Rat).Add(new(big.Rat).Set(closedTotal), openTotal)
		lines = append(lines, fmt.Sprintf("Итого: закрытые %s ₽ · открытые %s ₽ · всего %s ₽", formatSignedMoney(closedTotal), formatSignedMoney(openTotal), formatSignedMoney(total)))
		_ = b.sendMessage(ctx, chatID, strings.Join(lines, "\n"))
	case "/help":
		_ = b.sendMessage(ctx, chatID, helpText())
	case "/test":
		if err := b.sendTestChart(ctx, chatID); err != nil {
			log.Printf("telegram test chart: %v", err)
		}
	default:
		_ = b.sendMessage(ctx, chatID, "Не знаю такую команду.\n\n"+helpText())
	}
}

func (b *Bot) sendTestChart(ctx context.Context, chatID string) error {
	picture, err := chart.RenderZScoreSeries([]chart.Series{
		{Name: "Демонстрационная стратегия", Values: []float64{0.3, 0.8, 1.4, 0.9, 0.2, -0.5, -1.1, -0.6, 0.1}},
	})
	if err != nil {
		return b.sendMessage(ctx, chatID, "✅ Тестовое уведомление Trade Orbit доставлено, но график не удалось построить.")
	}
	caption := "✅ Тестовый график Trade Orbit · PAPER\nДемонстрационные данные, не реальные котировки."
	if err := b.sendPhoto(ctx, chatID, caption, picture); err != nil {
		return err
	}
	return nil
}

func helpText() string {
	return "/status — сводка Telegram и PAPER-стратегий\n/strategies — состояние пар, Z-score и причины проблем\n/pnl — PnL закрытых и открытых PAPER-позиций\n/test — тестовое сообщение\n/stop — отключить уведомления"
}

func dataStateIcon(state string) string {
	switch state {
	case "READY":
		return "🟢"
	case "DEGRADED":
		return "🔴"
	default:
		return "🟡"
	}
}

func strategyStatusLabel(status string) string {
	if status == "PAUSED" {
		return "входы на паузе"
	}
	return "активна"
}

func executionStateLabel(state string) string {
	if state == "OPEN" {
		return "пара открыта"
	}
	return "без открытой пары"
}

func formatZScore(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%+.2f", *value)
}

func parseAmount(value string) *big.Rat {
	amount, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return new(big.Rat)
	}
	return amount
}

func formatMoney(value *big.Rat) string {
	return strings.ReplaceAll(value.FloatString(2), ".", ",")
}

func formatSignedMoney(value *big.Rat) string {
	formatted := formatMoney(value)
	if value.Sign() > 0 {
		return "+" + formatted
	}
	return formatted
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

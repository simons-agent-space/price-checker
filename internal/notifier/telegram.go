package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Telegram sends Deals to a Telegram chat via the Bot API. The send
// timeout is 10s; callers (the checker) bound their own ctx as well.
type Telegram struct {
	botToken string
	chatID   string
	apiURL   string
	client   *http.Client
}

// NewTelegram returns a Telegram notifier. Both botToken and chatID
// must be non-empty; the caller is expected to gate this on env-var
// presence so a Noop is used when either is missing.
func NewTelegram(botToken, chatID string) *Telegram {
	return &Telegram{
		botToken: botToken,
		chatID:   chatID,
		apiURL:   "https://api.telegram.org/bot%s/sendMessage",
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify posts a HTML-formatted message to the configured chat. Returns
// an error on network failure, non-2xx status, or body read failure.
func (t *Telegram) Notify(ctx context.Context, deal Deal) error {
	body, err := json.Marshal(map[string]string{
		"chat_id":    t.chatID,
		"text":       FormatMessage(deal),
		"parse_mode": "HTML",
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf(t.apiURL, t.botToken), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		// http.Client.Do wraps transport errors in *url.Error with the
		// full request URL — and our URL embeds the bot token. Redact
		// it so the token never reaches logs or the checker.
		var ue *url.Error
		if errors.As(err, &ue) {
			ue.URL = "<redacted>"
		}
		return fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain the body so the connection can be reused, but don't
		// surface Telegram's error string — it can include the message
		// we just sent, which is private.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("telegram: status %d", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// FormatMessage renders a Deal as a Telegram-friendly HTML message.
// The function is exported so tests can pin the format and the web UI
// can reuse it for a "preview" without going through the API.
func FormatMessage(d Deal) string {
	current := formatPrice(d.PriceCents, d.Currency)
	// Without a baseline median we cannot compute a meaningful discount;
	// emit just the current price so the message is still useful but
	// does not show a misleading "0% below median" line.
	if d.MedianCents <= 0 {
		return fmt.Sprintf(
			"🛍 <b>Price drop</b>\n\nCurrent: %s\n\n%s",
			current, d.ProductURL,
		)
	}
	normal := formatPrice(d.MedianCents, d.Currency)
	discount := (1 - float64(d.PriceCents)/float64(d.MedianCents)) * 100
	return fmt.Sprintf(
		"🛍 <b>Price drop</b>\n\nCurrent: %s\nNormal: %s\n(%.0f%% below median)\n\n%s",
		current, normal, discount, d.ProductURL,
	)
}

// formatPrice renders an integer cents amount as a localised string.
// Two decimal places, no thousand separator.
func formatPrice(cents int64, currency string) string {
	major := cents / 100
	minor := cents % 100
	return fmt.Sprintf("%s%d.%02d", currency, major, minor)
}

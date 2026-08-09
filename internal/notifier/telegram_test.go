package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoop_NotifyReturnsNil(t *testing.T) {
	if err := (Noop{}).Notify(context.Background(), Deal{}); err != nil {
		t.Errorf("Noop.Notify err = %v, want nil", err)
	}
}

func TestTelegram_SendsExpectedPayload(t *testing.T) {
	var got struct {
		ChatID    string `json:"chat_id"`
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		// Verify the URL path embeds the bot token.
		if !strings.Contains(r.URL.Path, "test-token") {
			t.Errorf("path = %q, want to contain bot token", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewTelegram("test-token", "test-chat")
	n.apiURL = srv.URL + "/bot%s/sendMessage"

	deal := Deal{
		ProductID:   42,
		ProductURL:  "https://example.com/p/42",
		PriceCents:  1999,
		MedianCents: 4999,
		Currency:    "EUR",
	}
	if err := n.Notify(context.Background(), deal); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if got.ChatID != "test-chat" {
		t.Errorf("chat_id = %q, want test-chat", got.ChatID)
	}
	if got.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", got.ParseMode)
	}
	if !strings.Contains(got.Text, "EUR19.99") {
		t.Errorf("text = %q, want to contain EUR19.99", got.Text)
	}
	if !strings.Contains(got.Text, "EUR49.99") {
		t.Errorf("text = %q, want to contain EUR49.99", got.Text)
	}
	if !strings.Contains(got.Text, "60% below median") {
		t.Errorf("text = %q, want to contain '60%% below median'", got.Text)
	}
	if !strings.Contains(got.Text, "https://example.com/p/42") {
		t.Errorf("text = %q, want to contain the URL", got.Text)
	}
}

func TestTelegram_NonOKReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	n := NewTelegram("t", "c")
	n.apiURL = srv.URL + "/bot%s/sendMessage"

	err := n.Notify(context.Background(), Deal{
		ProductURL:  "https://example.com",
		PriceCents:  100,
		MedianCents: 200,
		Currency:    "EUR",
	})
	if err == nil {
		t.Fatal("Notify returned nil, want error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, want mention of 403", err)
	}
}

func TestFormatMessage_ZeroMedianSkipsDiscount(t *testing.T) {
	// A zero median (no baseline) should not divide by zero; the
	// message should render without a discount percentage.
	got := FormatMessage(Deal{
		ProductURL:  "https://example.com",
		PriceCents:  100,
		MedianCents: 0,
		Currency:    "EUR",
	})
	if !strings.Contains(got, "EUR1.00") {
		t.Errorf("text = %q, want to contain EUR1.00", got)
	}
	if strings.Contains(got, "below median") {
		t.Errorf("text = %q, want no discount line when median is 0", got)
	}
}

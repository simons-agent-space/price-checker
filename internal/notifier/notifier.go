// Package notifier turns deal detections into user-visible messages.
// The Notifier interface is satisfied by Telegram and a Noop so the
// checker can be wired with either at compile time.
package notifier

import "context"

// Deal is the payload sent to a notifier. The checker fills it with
// the current price, the rolling median (the "normal" price), the
// product URL, and the currency. The notifier formats the message.
type Deal struct {
	ProductID   int64
	ProductURL  string
	PriceCents  int64
	MedianCents int64
	Currency    string
}

// Notifier delivers a Deal to the user. Implementations must honour
// ctx: the checker cancels the call when the scheduler is shutting down.
type Notifier interface {
	Notify(ctx context.Context, deal Deal) error
}

// Noop is a Notifier that does nothing. Used when no Telegram (or
// other) credentials are configured, so the checker can stay wired
// to a valid Notifier in every deployment.
type Noop struct{}

func (Noop) Notify(context.Context, Deal) error { return nil }

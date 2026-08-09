package checker

import (
	"errors"
	"regexp"
	"strconv"
)

// ErrPriceNotFound is returned by ParsePrice when no price appears in the body.
var ErrPriceNotFound = errors.New("price not found")

// priceRegex matches a European-style price: either "€12.99" / "EUR 12.99"
// (currency before) or "12.99 €" / "12.99 EUR" (currency after). The
// integer and decimal parts are captured separately so we don't have to
// guess the separator. The negative lookahead (?![0-9]) prevents matching
// a fragment of a thousand-separator form like "1.234,56" as "1.23".
// Doesn't match: "$12.99" (no euro), "1.299,99" (thousand separator).
var priceRegex = regexp.MustCompile(`(?i)(?:€|EUR)\s*([0-9]+)[.,]([0-9]{2})(?:[^0-9]|$)|([0-9]+)[.,]([0-9]{2})(?:[^0-9]|$)\s*(?:€|EUR)`)

// ParsePrice extracts the first price and its currency from a page body.
// Returns ErrPriceNotFound if no price matches. The currency is normalised
// to its ISO 4217 code (€ → EUR).
func ParsePrice(body []byte) (priceCents int64, currency string, err error) {
	matches := priceRegex.FindSubmatch(body)
	if len(matches) < 5 {
		return 0, "", ErrPriceNotFound
	}
	intStr, fracStr := "", ""
	switch {
	case len(matches[1]) > 0:
		intStr, fracStr = string(matches[1]), string(matches[2])
	case len(matches[3]) > 0:
		intStr, fracStr = string(matches[3]), string(matches[4])
	default:
		return 0, "", ErrPriceNotFound
	}
	integer, err := strconv.ParseInt(intStr, 10, 64)
	if err != nil {
		return 0, "", err
	}
	frac, err := strconv.ParseInt(fracStr, 10, 64)
	if err != nil {
		return 0, "", err
	}
	return integer*100 + frac, "EUR", nil
}

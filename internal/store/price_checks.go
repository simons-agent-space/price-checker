package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PriceCheck is one observation of a product's price (successful or failed).
// It is recorded exactly once per checker.Check call. Append-only; nothing
// in the v1 service reads or writes existing rows except the deal detector,
// which reads the last N successful rows per product.
type PriceCheck struct {
	ID         int64
	ProductID  int64
	PriceCents int64
	Currency   string
	Success    bool
	Error      string
	CheckedAt  time.Time
}

// RecordPriceCheck inserts a row and returns the new id. The Error field is
// ignored when Success is true; it is required when Success is false (the
// checker fills it from the underlying fetch/parse failure).
func (s *Store) RecordPriceCheck(ctx context.Context, check *PriceCheck) (int64, error) {
	if check == nil {
		return 0, errors.New("nil price check")
	}
	var errStr sql.NullString
	if !check.Success && check.Error != "" {
		errStr = sql.NullString{String: check.Error, Valid: true}
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO price_checks (product_id, price_cents, currency, success, error)
		VALUES (?, ?, ?, ?, ?)
	`, check.ProductID, check.PriceCents, check.Currency, int64(b2i(check.Success)), errStr)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CountPriceChecks returns the total number of price_checks for a product,
// including failed ones. Used by the dashboard's "checks per product" stat
// and by tests that need to verify a failed check was recorded.
func (s *Store) CountPriceChecks(ctx context.Context, productID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_checks WHERE product_id = ?`, productID).Scan(&n)
	return n, err
}

// ListRecentPriceChecks returns the most recent `limit` successful checks for
// a product, newest first. Used by the deal detector to compute a baseline.
// Failed checks are excluded because they have no price to compare against.
func (s *Store) ListRecentPriceChecks(ctx context.Context, productID int64, limit int) ([]*PriceCheck, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, product_id, price_cents, currency, success, error, checked_at
		FROM price_checks
		WHERE product_id = ? AND success = 1
		ORDER BY checked_at DESC
		LIMIT ?
	`, productID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []*PriceCheck
	for rows.Next() {
		check, err := scanPriceCheck(rows)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func scanPriceCheck(s scanner) (*PriceCheck, error) {
	var check PriceCheck
	var errStr sql.NullString
	var success int64
	var checkedAt int64
	err := s.Scan(&check.ID, &check.ProductID, &check.PriceCents, &check.Currency, &success, &errStr, &checkedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	check.Success = success != 0
	if errStr.Valid {
		check.Error = errStr.String
	}
	check.CheckedAt = time.Unix(checkedAt, 0)
	return &check, nil
}

// b2i converts a bool to SQLite's INTEGER 0/1 representation. SQLite has
// no native bool; the driver stores false as 0 and true as 1.
func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

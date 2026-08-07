package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) AddProduct(ctx context.Context, product *Product) (int64, error) {
	var lastChecked any
	if product.LastCheckedAt != nil {
		lastChecked = product.LastCheckedAt.Unix()
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO products (search_id, url, last_checked_at)
		VALUES (?, ?, ?)
	`, product.SearchID, product.URL, lastChecked)
	if err != nil {
		return 0, mapError(err)
	}
	return result.LastInsertId()
}

func (s *Store) GetProduct(ctx context.Context, id int64) (*Product, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, search_id, url, last_checked_at, created_at
		FROM products WHERE id = ?
	`, id)
	return scanProduct(row)
}

func (s *Store) ListProductsBySearch(ctx context.Context, searchID int64) ([]*Product, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, search_id, url, last_checked_at, created_at
		FROM products WHERE search_id = ? ORDER BY id
	`, searchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []*Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (s *Store) DeleteProduct(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM products WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanProduct(s scanner) (*Product, error) {
	var product Product
	var lastChecked sql.NullInt64
	var createdAt int64
	err := s.Scan(&product.ID, &product.SearchID, &product.URL, &lastChecked, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if lastChecked.Valid {
		t := time.Unix(lastChecked.Int64, 0)
		product.LastCheckedAt = &t
	}
	product.CreatedAt = time.Unix(createdAt, 0)
	return &product, nil
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) CreateSearch(ctx context.Context, search *Search) (*Search, error) {
	result, err := scanSearch(s.db.QueryRowContext(ctx, `
		INSERT INTO searches (name, query, check_interval_s, next_check_at)
		VALUES (?, ?, ?, ?)
		RETURNING id, name, query, check_interval_s, next_check_at, created_at
	`, search.Name, search.Query, int64(search.CheckInterval.Seconds()), search.NextCheckAt.Unix()))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return result, nil
}

func (s *Store) GetSearch(ctx context.Context, id int64) (*Search, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, query, check_interval_s, next_check_at, created_at
		FROM searches WHERE id = ?
	`, id)
	return scanSearch(row)
}

func (s *Store) ListSearches(ctx context.Context) ([]*Search, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, query, check_interval_s, next_check_at, created_at
		FROM searches ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var searches []*Search
	for rows.Next() {
		search, err := scanSearch(rows)
		if err != nil {
			return nil, err
		}
		searches = append(searches, search)
	}
	return searches, rows.Err()
}

func (s *Store) DeleteSearch(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM searches WHERE id = ?`, id)
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

func (s *Store) UpdateNextCheck(ctx context.Context, id int64, next time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE searches SET next_check_at = ? WHERE id = ?`, next.Unix(), id)
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

func (s *Store) ListDueSearches(ctx context.Context, now time.Time) ([]*Search, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, query, check_interval_s, next_check_at, created_at
		FROM searches WHERE next_check_at <= ? ORDER BY next_check_at
	`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var searches []*Search
	for rows.Next() {
		search, err := scanSearch(rows)
		if err != nil {
			return nil, err
		}
		searches = append(searches, search)
	}
	return searches, rows.Err()
}

func scanSearch(s scanner) (*Search, error) {
	var search Search
	var intervalSec, nextCheck, createdAt int64
	err := s.Scan(&search.ID, &search.Name, &search.Query, &intervalSec, &nextCheck, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	search.CheckInterval = time.Duration(intervalSec) * time.Second
	search.NextCheckAt = time.Unix(nextCheck, 0)
	search.CreatedAt = time.Unix(createdAt, 0)
	return &search, nil
}

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateAndGetSearch(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	search := &Search{
		Name:          "test",
		Query:         "sony wh-1000xm5",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour).Truncate(time.Second),
	}

	created, err := s.CreateSearch(ctx, search)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero id")
	}

	got, err := s.GetSearch(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
	if got.Query != "sony wh-1000xm5" {
		t.Errorf("Query = %q, want %q", got.Query, "sony wh-1000xm5")
	}
	if got.CheckInterval != time.Hour {
		t.Errorf("CheckInterval = %v, want %v", got.CheckInterval, time.Hour)
	}
	if !got.NextCheckAt.Equal(search.NextCheckAt) {
		t.Errorf("NextCheckAt = %v, want %v", got.NextCheckAt, search.NextCheckAt)
	}
}

func TestGetSearchNotFound(t *testing.T) {
	s := NewTestStore(t)
	_, err := s.GetSearch(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListSearches(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"first", "second", "third"} {
		_, err := s.CreateSearch(ctx, &Search{
			Name:          name,
			Query:         "test",
			CheckInterval: time.Hour,
			NextCheckAt:   time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	searches, err := s.ListSearches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(searches) != 3 {
		t.Errorf("len = %d, want 3", len(searches))
	}
}

func TestDeleteSearch(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	created, err := s.CreateSearch(ctx, &Search{
		Name:          "test",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteSearch(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	_, err = s.GetSearch(ctx, created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteSearchCascadesProducts(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	created, _ := s.CreateSearch(ctx, &Search{
		Name:          "test",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	product, _ := s.AddProduct(ctx, &Product{
		SearchID: created.ID,
		URL:      "https://example.com",
	})

	if err := s.DeleteSearch(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetProduct(ctx, product.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("product should be deleted via cascade, err = %v", err)
	}
}

func TestUpdateNextCheck(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	created, _ := s.CreateSearch(ctx, &Search{
		Name:          "test",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})

	newNext := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	if err := s.UpdateNextCheck(ctx, created.ID, newNext); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetSearch(ctx, created.ID)
	if !got.NextCheckAt.Equal(newNext) {
		t.Errorf("NextCheckAt = %v, want %v", got.NextCheckAt, newNext)
	}
}

func TestUpdateNextCheckNotFound(t *testing.T) {
	s := NewTestStore(t)
	err := s.UpdateNextCheck(context.Background(), 999, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListDueSearches(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	now := time.Now()

	_, _ = s.CreateSearch(ctx, &Search{
		Name:          "due",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   now.Add(-time.Minute),
	})
	_, _ = s.CreateSearch(ctx, &Search{
		Name:          "not due",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   now.Add(time.Hour),
	})

	due, err := s.ListDueSearches(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Errorf("len = %d, want 1", len(due))
	}
	if len(due) > 0 && due[0].Name != "due" {
		t.Errorf("Name = %q, want %q", due[0].Name, "due")
	}
}

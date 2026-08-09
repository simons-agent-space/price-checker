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
	productID, _ := s.AddProduct(ctx, &Product{
		SearchID: created.ID,
		URL:      "https://example.com",
	})

	if err := s.DeleteSearch(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetProduct(ctx, productID)
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

// TestCreateSearchWithProduct: the new method creates both a search
// and a product in one transaction. The product's URL is the search
// query, its search_id is the new search's ID.
func TestCreateSearchWithProduct(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	created, product, err := s.CreateSearchWithProduct(ctx, &Search{
		Name:          "watch",
		Query:         "https://example.com/product",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Error("search ID = 0, want non-zero")
	}
	if product.ID == 0 {
		t.Error("product ID = 0, want non-zero")
	}
	if product.SearchID != created.ID {
		t.Errorf("product.SearchID = %d, want %d", product.SearchID, created.ID)
	}
	if product.URL != "https://example.com/product" {
		t.Errorf("product.URL = %q, want %q", product.URL, "https://example.com/product")
	}

	// Rows are queryable through the normal store API.
	got, err := s.GetSearch(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "watch" {
		t.Errorf("Name = %q, want %q", got.Name, "watch")
	}
	products, err := s.ListProductsBySearch(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Errorf("len(products) = %d, want 1", len(products))
	}
	if len(products) > 0 && products[0].URL != "https://example.com/product" {
		t.Errorf("products[0].URL = %q, want %q", products[0].URL, "https://example.com/product")
	}
}

// TestCreateSearchWithProductConflict: a duplicate (name, query) returns
// ErrConflict and does not create a stray product or leave a half-written
// search. This is the only atomicity path we can exercise at the store
// layer: the product insert cannot fail on a fresh search_id because the
// (search_id, url) unique constraint is satisfied for every new call.
func TestCreateSearchWithProductConflict(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	req := &Search{
		Name:          "watch",
		Query:         "https://example.com/product",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	}

	first, firstProduct, err := s.CreateSearchWithProduct(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// Second create with the same (name, query) — must conflict.
	_, _, err = s.CreateSearchWithProduct(ctx, req)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}

	// The first search and product are intact; no extra product was
	// created from the conflicted call.
	products, err := s.ListProductsBySearch(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Errorf("len(products) = %d, want 1 (no extra from conflict)", len(products))
	}
	if products[0].ID != firstProduct.ID {
		t.Errorf("products[0].ID = %d, want %d", products[0].ID, firstProduct.ID)
	}

	all, err := s.ListSearches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("len(searches) = %d, want 1 (no orphan from conflict)", len(all))
	}
}

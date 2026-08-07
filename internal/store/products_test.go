package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAddAndListProducts(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	searchID, _ := s.CreateSearch(ctx, &Search{
		Name:          "test",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})

	_, _ = s.AddProduct(ctx, &Product{SearchID: searchID, URL: "https://a.com"})
	_, _ = s.AddProduct(ctx, &Product{SearchID: searchID, URL: "https://b.com"})

	products, err := s.ListProductsBySearch(ctx, searchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 2 {
		t.Errorf("len = %d, want 2", len(products))
	}
}

func TestAddProductDuplicate(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	searchID, _ := s.CreateSearch(ctx, &Search{
		Name:          "test",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	_, _ = s.AddProduct(ctx, &Product{SearchID: searchID, URL: "https://example.com"})

	_, err := s.AddProduct(ctx, &Product{
		SearchID: searchID,
		URL:      "https://example.com",
	})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

func TestAddProductFKError(t *testing.T) {
	s := NewTestStore(t)
	_, err := s.AddProduct(context.Background(), &Product{
		SearchID: 999,
		URL:      "https://example.com",
	})
	if err == nil {
		t.Fatal("expected error for non-existent search")
	}
}

func TestGetProduct(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	searchID, _ := s.CreateSearch(ctx, &Search{
		Name:          "test",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	productID, _ := s.AddProduct(ctx, &Product{
		SearchID: searchID,
		URL:      "https://example.com",
	})

	got, err := s.GetProduct(ctx, productID)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", got.URL, "https://example.com")
	}
}

func TestGetProductNotFound(t *testing.T) {
	s := NewTestStore(t)
	_, err := s.GetProduct(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProduct(t *testing.T) {
	s := NewTestStore(t)
	ctx := context.Background()

	searchID, _ := s.CreateSearch(ctx, &Search{
		Name:          "test",
		Query:         "test",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	productID, _ := s.AddProduct(ctx, &Product{
		SearchID: searchID,
		URL:      "https://example.com",
	})

	if err := s.DeleteProduct(ctx, productID); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetProduct(ctx, productID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPingAfterClose(t *testing.T) {
	s := NewTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(context.Background()); err == nil {
		t.Fatal("expected error after close")
	}
}

package main

import (
	"context"

	"github.com/shubhesh07/gojpa/jpa"
)

//go:generate go run github.com/shubhesh07/gojpa/cmd/jpagen -type ProductRepository -mock

// ProductRepository: method names are parsed at `go generate` time; a typo in
// a property name is a generation error, like a Spring startup failure.
type ProductRepository interface {
	jpa.CRUD[Product, int64] // FindByID, FindAll, FindPage, Save, SaveAll, Update, DeleteByID, Count, ExistsByID ...

	FindByCode(ctx context.Context, code string) (*Product, error)
	FindByNameContainingOrderByPriceDesc(ctx context.Context, fragment string) ([]Product, error)
	FindByActiveTrue(ctx context.Context, p jpa.Pageable) (jpa.Page[Product], error)
	CountByPriceLessThan(ctx context.Context, max float64) (int64, error)
	DeleteByCode(ctx context.Context, code string) (int64, error)

	// jpa:query UPDATE product SET price = price * :factor WHERE code IN (:codes)
	// jpa:modifying
	RaisePriceByCodes(ctx context.Context, factor float64, codes []string) (int64, error)

	// jpa:query SELECT COUNT(*) AS n, MIN(price) AS min_price, MAX(price) AS max_price FROM product WHERE active = TRUE
	PriceStats(ctx context.Context) (*Stats, error)
}

// Stats is a DTO projection: any struct with db tags.
type Stats struct {
	N        int64   `db:"n"`
	MinPrice float64 `db:"min_price"`
	MaxPrice float64 `db:"max_price"`
}

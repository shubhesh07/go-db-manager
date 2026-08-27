// Quickstart: a complete gojpa repository running against in-memory SQLite.
// No database to install:
//
//	git clone https://github.com/shubhesh07/gojpa && cd gojpa && make quickstart
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/shubhesh07/gojpa/jpa"
	"github.com/shubhesh07/gojpa/sqlgen"
	_ "modernc.org/sqlite"
)

// 1. Entity: a struct with db tags (+ optional TableName / SoftDelete).
type Product struct {
	ID        int64     `db:"id" orm:"pk,auto"`
	Code      string    `db:"code"`
	Name      string    `db:"name"`
	Price     float64   `db:"price"`
	Active    bool      `db:"active"`
	UpdatedOn time.Time `db:"updated_on" orm:"updated"`
}

func (*Product) TableName() string            { return "product" }
func (*Product) SoftDelete() (string, string) { return "active = TRUE", "active = FALSE" }

// 2. Repository interface, Spring Data style. `go generate` writes
//    product_repository_gen.go from it (see repository.go in this package).
//    Here the generated file is checked in, so the program runs as is.

func main() {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE product (
		id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT, name TEXT, price REAL,
		active INTEGER DEFAULT 1, updated_on TEXT DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		log.Fatal(err)
	}

	// 3. Build the repository on any *sql.DB (or *sql.Tx) with the dialect.
	repo := NewProductRepository(db, sqlgen.SQLite,
		jpa.WithDefaultTimeout(2*time.Second),
		jpa.WithHook(jpa.Metrics(func(name string, d time.Duration, rows int64, err error) {
			fmt.Printf("  %-45s %6dµs rows=%d err=%v\n", name, d.Microseconds(), rows, err)
		})),
	)
	ctx := context.Background()

	// 4. Use it.
	must(repo.SaveAll(ctx, []*Product{
		{Code: "P1", Name: "Paracetamol 500", Price: 20, Active: true},
		{Code: "P2", Name: "Paracetamol 650", Price: 30, Active: true},
		{Code: "A1", Name: "Aspirin", Price: 15, Active: true},
	}))

	rows, err := repo.FindByNameContainingOrderByPriceDesc(ctx, "para")
	must(err)
	fmt.Println("containing 'para':", codes(rows))

	page, err := repo.FindByActiveTrue(ctx, jpa.PageRequest(0, 2, jpa.Asc("Code")))
	must(err)
	fmt.Printf("page 0: %v total=%d pages=%d\n", codes(page.Content), page.Total, page.TotalPages)

	cheap, err := repo.CountByPriceLessThan(ctx, 25)
	must(err)
	fmt.Println("count price < 25:", cheap)

	n, err := repo.RaisePriceByCodes(ctx, 1.1, []string{"P1", "P2"}) // jpa:query + jpa:modifying
	must(err)
	fmt.Println("raised:", n)

	stats, err := repo.PriceStats(ctx) // jpa:query into a DTO
	must(err)
	fmt.Printf("stats: %+v\n", *stats)

	_, err = repo.DeleteByCode(ctx, "A1") // soft delete: UPDATE product SET active = FALSE ...
	must(err)
	all, _ := repo.FindAll(ctx)
	everything, _ := repo.IncludingDeleted().FindAll(ctx)
	fmt.Printf("visible=%d including deleted=%d\n", len(all), len(everything))

	if _, err := repo.FindByCode(ctx, "nope"); err == jpa.ErrNotFound {
		fmt.Println("missing row ->", err)
	}
}

func codes(ps []Product) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Code
	}
	return out
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// Package integration runs the example/warehouse repository against real
// databases: SQLite in-process (always), MySQL 8 and Postgres 16 via
// testcontainers (skipped when Docker is unavailable or GOJPA_SKIP_DOCKER=1).
// It exists because sqlmock cannot catch driver/type behaviour (BIT(1),
// DECIMAL, DATETIME, RETURNING, LAST_INSERT_ID) — both staging bugs found in
// the catalog-service pilot were of that kind.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/shubhesh07/gojpa/example/warehouse"
	"github.com/shubhesh07/gojpa/jpa"
	"github.com/shubhesh07/gojpa/sqlgen"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	_ "modernc.org/sqlite"
)

const mysqlSchema = `
CREATE TABLE medicine_warehouse_master (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  product_code VARCHAR(64) NOT NULL,
  warehouse_id BIGINT NOT NULL,
  availability BIT(1) NOT NULL DEFAULT b'1',
  is_searchable BIT(1) NOT NULL DEFAULT b'1',
  supplied_by_tm BIT(1) NOT NULL DEFAULT b'0',
  mrp DECIMAL(10,2) NULL,
  updated_by BIGINT NOT NULL DEFAULT 0,
  updated_on DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE product_sold_count (product_code VARCHAR(64), subs_taken_count BIGINT);
INSERT INTO product_sold_count VALUES ('A', 3), ('A', 7), ('B', 1);`

const postgresSchema = `
CREATE TABLE medicine_warehouse_master (
  id BIGSERIAL PRIMARY KEY,
  product_code VARCHAR(64) NOT NULL,
  warehouse_id BIGINT NOT NULL,
  availability BOOLEAN NOT NULL DEFAULT TRUE,
  is_searchable BOOLEAN NOT NULL DEFAULT TRUE,
  supplied_by_tm BOOLEAN NOT NULL DEFAULT FALSE,
  mrp NUMERIC(10,2) NULL,
  updated_by BIGINT NOT NULL DEFAULT 0,
  updated_on TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE TABLE product_sold_count (product_code VARCHAR(64), subs_taken_count BIGINT);
INSERT INTO product_sold_count VALUES ('A', 3), ('A', 7), ('B', 1);`

const sqliteSchema = `
CREATE TABLE medicine_warehouse_master (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  product_code TEXT NOT NULL,
  warehouse_id INTEGER NOT NULL,
  availability INTEGER NOT NULL DEFAULT 1,
  is_searchable INTEGER NOT NULL DEFAULT 1,
  supplied_by_tm INTEGER NOT NULL DEFAULT 0,
  mrp REAL NULL,
  updated_by INTEGER NOT NULL DEFAULT 0,
  updated_on TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE product_sold_count (product_code TEXT, subs_taken_count INTEGER);
INSERT INTO product_sold_count VALUES ('A', 3), ('A', 7), ('B', 1);`

type target struct {
	name    string
	dialect sqlgen.Dialect
	open    func(t *testing.T) *sql.DB
	schema  string
}

func execAll(t *testing.T, db *sql.DB, schema string) {
	t.Helper()
	for _, stmt := range splitStatements(schema) {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v\n%s", err, stmt)
		}
	}
}

func splitStatements(s string) []string {
	var out []string
	cur := ""
	for _, line := range splitLines(s) {
		cur += line + "\n"
		if len(line) > 0 && line[len(line)-1] == ';' {
			out = append(out, cur)
			cur = ""
		}
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func docker(t *testing.T) {
	if os.Getenv("GOJPA_SKIP_DOCKER") != "" {
		t.Skip("GOJPA_SKIP_DOCKER set")
	}
}

func targets() []target {
	return []target{
		{"sqlite", sqlgen.SQLite, func(t *testing.T) *sql.DB {
			db, err := sql.Open("sqlite", "file::memory:?cache=shared")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			return db
		}, sqliteSchema},
		{"mysql", sqlgen.MySQL, func(t *testing.T) *sql.DB {
			docker(t)
			ctx := context.Background()
			c, err := tcmysql.Run(ctx, "mysql:8.0", tcmysql.WithDatabase("t"), tcmysql.WithUsername("u"), tcmysql.WithPassword("p"))
			if err != nil {
				t.Skipf("mysql container: %v", err)
			}
			t.Cleanup(func() { _ = c.Terminate(ctx) })
			dsn, err := c.ConnectionString(ctx, "parseTime=false") // deliberately off: exercises DATETIME text coercion
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("mysql", dsn)
			if err != nil {
				t.Fatal(err)
			}
			return db
		}, mysqlSchema},
		{"postgres", sqlgen.Postgres, func(t *testing.T) *sql.DB {
			docker(t)
			ctx := context.Background()
			c, err := tcpostgres.Run(ctx, "postgres:16-alpine", tcpostgres.WithDatabase("t"), tcpostgres.WithUsername("u"), tcpostgres.WithPassword("p"), tcpostgres.BasicWaitStrategies())
			if err != nil {
				t.Skipf("postgres container: %v", err)
			}
			t.Cleanup(func() { _ = c.Terminate(ctx) })
			dsn, err := c.ConnectionString(ctx, "sslmode=disable")
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("postgres", dsn)
			if err != nil {
				t.Fatal(err)
			}
			return db
		}, postgresSchema},
	}
}

func TestRealDatabases(t *testing.T) {
	for _, tg := range targets() {
		t.Run(tg.name, func(t *testing.T) {
			db := tg.open(t)
			defer db.Close()
			execAll(t, db, tg.schema)
			run(t, db, tg.dialect)
		})
	}
}

func run(t *testing.T, db *sql.DB, d sqlgen.Dialect) {
	ctx := jpa.WithActor(context.Background(), int64(9))
	var slow int
	repo := warehouse.NewRepository(db, d,
		jpa.WithDefaultTimeout(5*time.Second),
		jpa.WithRetry(jpa.RetryPolicy{Attempts: 3, Backoff: 10 * time.Millisecond, Retryable: jpa.IsTransient}),
		jpa.WithHook(jpa.SlowQuery(0, func(jpa.Query, time.Duration) { slow++ })),
	)
	tr := true
	mrp := 12.5

	// Save / SaveAll: generated keys written back on every dialect.
	rows := []*warehouse.MedicineWarehouse{
		{ProductCode: "A", WarehouseID: 1, Availability: true, IsSearchable: true, MRP: sql.NullFloat64{Float64: 10, Valid: true}},
		{ProductCode: "B", WarehouseID: 1, Availability: false, IsSearchable: true},
		{ProductCode: "C", WarehouseID: 2, Availability: true, IsSearchable: true},
	}
	if err := repo.SaveAll(ctx, rows); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	for i, r := range rows {
		if r.ID == 0 {
			t.Fatalf("row %d: id not written back", i)
		}
	}

	// Derived find with IN + scalar; BIT/DECIMAL/DATETIME scanning.
	got, err := repo.FindByProductCodeInAndWarehouseId(ctx, []string{"A", "B"}, 1)
	if err != nil || len(got) != 2 {
		t.Fatalf("FindByProductCodeInAndWarehouseId: %v %d", err, len(got))
	}
	byCode := map[string]warehouse.MedicineWarehouse{}
	for _, g := range got {
		byCode[g.ProductCode] = g
	}
	if !byCode["A"].Availability || byCode["B"].Availability || !byCode["A"].MRP.Valid || byCode["A"].MRP.Float64 != 10 || byCode["A"].UpdatedOn.IsZero() {
		t.Errorf("scan: %+v", byCode)
	}

	// FindOne + ErrNotFound; count/exists; lock.
	if _, err := repo.FindByProductCodeAndWarehouseId(ctx, "ZZ", 1); !errors.Is(err, jpa.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	if n, err := repo.CountByWarehouseIdAndAvailabilityTrue(ctx, 1); err != nil || n != 1 {
		t.Errorf("count: %v %d", err, n)
	}
	if ok, err := repo.ExistsByProductCodeAndWarehouseId(ctx, "C", 2); err != nil || !ok {
		t.Errorf("exists: %v %v", err, ok)
	}
	if err := jpa.Transactional(ctx, db, func(tx *sql.Tx) error {
		_, err := repo.WithTx(tx).FindLockedByProductCodeAndWarehouseId(ctx, "A", 1, jpa.ForUpdate)
		return err
	}); err != nil {
		t.Errorf("FOR UPDATE: %v", err)
	}

	// Page + keyset window.
	page, err := repo.FindByWarehouseIdOrderByProductCodeAsc(ctx, 1, jpa.PageRequest(0, 1))
	if err != nil || page.Total != 2 || len(page.Content) != 1 || !page.HasNext() {
		t.Errorf("page: %v %+v", err, page)
	}
	w, err := repo.FindByWarehouseIdOrderByProductCodeAscIdAsc(ctx, 1, jpa.Keyset(1))
	if err != nil || len(w.Content) != 1 || !w.HasNext || w.Content[0].ProductCode != "A" {
		t.Fatalf("window1: %v %+v", err, w)
	}
	w, err = repo.FindByWarehouseIdOrderByProductCodeAscIdAsc(ctx, 1, w.Next)
	if err != nil || len(w.Content) != 1 || w.HasNext || w.Content[0].ProductCode != "B" {
		t.Errorf("window2: %v %+v", err, w)
	}

	// Declared query with DTO projection + paging.
	sc, err := repo.GetSoldCounts(ctx, []string{"A", "B"})
	if err != nil || len(sc) != 2 {
		t.Fatalf("GetSoldCounts: %v %+v", err, sc)
	}
	pg, err := repo.PageSoldCounts(ctx, []string{"A", "B"}, jpa.PageRequest(0, 10, jpa.Asc("ProductCode")))
	if err != nil || pg.Total != 2 || pg.Content[0].ProductCode != "A" || pg.Content[0].Count != 7 {
		t.Errorf("PageSoldCounts: %v %+v", err, pg)
	}

	// jpa:modifying, hand-written CASE-WHEN fragment, BatchUpdate helper.
	if d.Name() != "sqlite" { // the declared query hardcodes NOW(); SQLite has no such function
		if n, err := repo.UpdateAvailabilityByProductCodesAndWarehouseID(ctx, false, 9, []string{"A"}, 1); err != nil || n != 1 {
			t.Errorf("modifying: %v %d", err, n)
		}
	}
	if n, err := repo.UpdateBatch(ctx, 9, []warehouse.Update{{Key: warehouse.Key{ProductCode: "A", WarehouseID: 1}, Availability: &tr}, {Key: warehouse.Key{ProductCode: "B", WarehouseID: 1}, MRP: &mrp}}); err != nil || n != 2 {
		t.Errorf("UpdateBatch: %v %d", err, n)
	}
	if n, err := repo.BatchUpdate(ctx, []jpa.BatchRow{{Key: map[string]any{"ProductCode": "C", "WarehouseId": 2}, Set: map[string]any{"SuppliedByTM": true}}}); err != nil || n != 1 {
		t.Errorf("BatchUpdate: %v %d", err, n)
	}
	c, err := repo.FindByProductCodeAndWarehouseId(ctx, "C", 2)
	if err != nil || !c.SuppliedByTM || c.UpdatedBy != 9 {
		t.Errorf("after batch: %v %+v", err, c)
	}

	// Whole-entity Update, Cond queries, soft delete + IncludingDeleted.
	c.MRP = sql.NullFloat64{Float64: 1.25, Valid: true}
	if err := repo.Update(ctx, c); err != nil {
		t.Errorf("Update: %v", err)
	}
	if n, err := repo.CountWhere(ctx, jpa.And(jpa.Gt("Mrp", 1.0), jpa.In("WarehouseId", []int64{1, 2}))); err != nil || n != 3 {
		t.Errorf("CountWhere: %v %d", err, n)
	}
	if n, err := repo.DeleteByProductCodeIn(ctx, []string{"B"}); err != nil || n != 1 {
		t.Errorf("soft delete: %v %d", err, n)
	}
	if n, _ := repo.Count(ctx); n != 2 {
		t.Errorf("count after soft delete = %d", n)
	}
	if n, _ := repo.IncludingDeleted().Count(ctx); n != 3 {
		t.Errorf("IncludingDeleted count = %d", n)
	}
	// Transaction rollback.
	_ = jpa.Transactional(ctx, db, func(tx *sql.Tx) error {
		if _, err := repo.WithTx(tx).IncludingDeleted().DeleteByProductCodeIn(ctx, []string{"A", "C"}); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if n, _ := repo.IncludingDeleted().Count(ctx); n != 3 {
		t.Errorf("rollback failed, count = %d", n)
	}
	if slow == 0 {
		t.Error("SlowQuery hook never fired")
	}
}

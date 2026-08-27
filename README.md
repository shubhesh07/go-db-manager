# gojpa

Spring Data JPA for Go. Write the repository **interface**; `jpagen` writes the implementation.

```go
//go:generate go run github.com/shubhesh07/gojpa/cmd/jpagen -type Repository
type Repository interface {
    jpa.CRUD[MedicineWarehouse, int64]                       // FindByID, FindAll, FindPage, Save, SaveAll, Update, DeleteByID, Count, ExistsByID, Exec, Rows

    FindByProductCodeInAndWarehouseId(ctx context.Context, codes []string, wh int64) ([]MedicineWarehouse, error)
    FindByProductCodeAndWarehouseId(ctx context.Context, code string, wh int64) (*MedicineWarehouse, error) // ErrNotFound
    FindByWarehouseIdOrderByProductCodeAsc(ctx context.Context, wh int64, p jpa.Pageable) (jpa.Page[MedicineWarehouse], error)
    CountByWarehouseIdAndAvailabilityTrue(ctx context.Context, wh int64) (int64, error)
    ExistsByProductCodeAndWarehouseId(ctx context.Context, code string, wh int64) (bool, error)
    DeleteByProductCodeIn(ctx context.Context, codes []string) (int64, error)               // soft delete if entity declares SoftDelete()

    // jpa:query UPDATE medicine_warehouse_master SET availability = :availability, updated_on = NOW()
    //   WHERE product_code IN (:codes) AND warehouse_id = :wh
    // jpa:modifying
    UpdateAvailability(ctx context.Context, availability bool, codes []string, wh int64) (int64, error)

    // jpa:query SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count
    //   WHERE product_code IN (:codes) GROUP BY product_code
    GetSoldCounts(ctx context.Context, codes []string) ([]SoldCount, error)                 // any struct with db tags

    UpdateBatch(ctx context.Context, userID int64, updates []Update) (int64, error)          // hand-written on *RepositoryImpl
}
```

```go
repo := warehouse.NewRepository(db, sqlgen.MySQL, jpa.WithHook(jpa.Timeout(3*time.Second)), jpa.WithHook(zapHook))
rows, err := repo.FindByProductCodeInAndWarehouseId(ctx, codes, 12)
err = jpa.Transactional(ctx, db, func(tx *sql.Tx) error { return repo.WithTx(tx).Save(ctx, &row) })
```

Full example: [`example/warehouse`](example/warehouse). Design and Spring mapping: [`DESIGN.md`](DESIGN.md).

## Getting started (new project)

Try it first with no database to install — SQLite in memory:

```
git clone https://github.com/shubhesh07/gojpa && cd gojpa && make quickstart
```

That runs [`integration/cmd/quickstart`](integration/cmd/quickstart): one entity, one interface, generated implementation, derived + declared queries, paging, soft delete, hooks. Then in your own module:

**1. Install**
```
go get github.com/shubhesh07/gojpa
```

**2. Define the entity** — a plain struct with `db` tags (sqlx convention) and optional `TableName()` / `SoftDelete()`:
```go
type Product struct {
    ID        int64     `db:"id" orm:"pk,auto"`
    Code      string    `db:"code"`
    Name      string    `db:"name"`
    Price     float64   `db:"price"`
    Active    bool      `db:"active"`
    UpdatedOn time.Time `db:"updated_on" orm:"updated"`   // set to NOW() on insert/update
}
func (*Product) TableName() string { return "product" }
```

**3. Declare the repository interface** and put a `go:generate` line above it:
```go
//go:generate go run github.com/shubhesh07/gojpa/cmd/jpagen -type ProductRepository -mock
type ProductRepository interface {
    jpa.CRUD[Product, int64]
    FindByCode(ctx context.Context, code string) (*Product, error)
    FindByNameContainingOrderByPriceDesc(ctx context.Context, fragment string) ([]Product, error)
    FindByActiveTrue(ctx context.Context, p jpa.Pageable) (jpa.Page[Product], error)
    // jpa:query UPDATE product SET price = price * :factor WHERE code IN (:codes)
    // jpa:modifying
    RaisePriceByCodes(ctx context.Context, factor float64, codes []string) (int64, error)
}
```

**4. Generate** — produces `product_repository_gen.go` (`ProductRepositoryImpl`, `NewProductRepository`) and `product_repository_mock.go`. Commit both.
```
go generate ./...
```
A wrong property name or argument count fails here, not at runtime.

**5. Connect** — any `*sql.DB` (or `*sql.Tx`) plus a dialect. Hooks give you timeouts, logging, metrics, retry:
```go
repo := NewProductRepository(db, sqlgen.MySQL,          // or sqlgen.Postgres / sqlgen.SQLite
    jpa.WithDefaultTimeout(3*time.Second),
    jpa.WithRetry(jpa.RetryPolicy{Attempts: 3, Backoff: 20*time.Millisecond, Retryable: jpa.IsDeadlock}),
    jpa.WithHook(jpa.Metrics(func(name string, d time.Duration, rows int64, err error) { /* prometheus/otel/zap */ })),
)
```

**6. Use it**
```go
p, err := repo.FindByCode(ctx, "P1")                    // errors.Is(err, jpa.ErrNotFound) when missing
page, _ := repo.FindByActiveTrue(ctx, jpa.PageRequest(0, 20, jpa.Asc("Code")))
err = jpa.Transactional(ctx, db, func(tx *sql.Tx) error { return repo.WithTx(tx).Save(ctx, &Product{...}) })
```

**7. Test** — unit-test callers with the generated mock; unit-test the repository with [go-sqlmock](https://github.com/DATA-DOG/go-sqlmock) (exact SQL is deterministic); see [`example/warehouse/repository_test.go`](example/warehouse/repository_test.go).
```go
m := &ProductRepositoryMock{FindByCodeFunc: func(ctx context.Context, c string) (*Product, error) { return &Product{Code: c}, nil }}
```

**8. CI** — fail the build when generated files are stale:
```
go run github.com/shubhesh07/gojpa/cmd/jpagen -type ProductRepository -mock -check
```

Adding gojpa to an existing sqlx/GORM service without touching callers: [`docs/MIGRATION.md`](docs/MIGRATION.md).

## Entities

```go
type MedicineWarehouse struct {
    ID           int64           `db:"id" orm:"pk,auto"`
    ProductCode  string          `db:"product_code"`
    MRP          sql.NullFloat64 `db:"mrp"`
    UpdatedBy    int64           `db:"updated_by" orm:"updated_by"`   // from jpa.WithActor(ctx, userID)
    UpdatedOn    time.Time       `db:"updated_on" orm:"updated"`      // NOW() on insert/update
}
func (*MedicineWarehouse) TableName() string            { return "medicine_warehouse_master" }
func (*MedicineWarehouse) SoftDelete() (string, string) { return "is_searchable = 1", "is_searchable = 0" } // optional, opt-in
```

`db:"col"` follows sqlx, so existing models port unchanged. Embedded structs are flattened; pointers to structs, slices and maps are ignored (not columns). Nullable columns use `sql.Null*` or pointers. Plain `bool` scans MySQL `BIT(1)`/`TINYINT`, and `time.Time` scans text `DATETIME`, without driver flags. Only listed columns are selected — never `SELECT *`.

## Method-name grammar

`{find|read|get|query|search|stream|count|exists|delete|remove}[Distinct][First|Top[N]]…By<Prop>[Op][And|Or<Prop>[Op]…][AllIgnoreCase][OrderBy<Prop>[Asc|Desc]…]`

Operators: `Is/Equals`, `Not`, `In`, `NotIn`, `IsNull`, `IsNotNull`, `True`, `False`, `Like`, `NotLike`, `StartingWith`, `EndingWith`, `Containing`, `NotContaining`, `Between`, `LessThan[Equal]`, `GreaterThan[Equal]`, `Before`, `After`, `IgnoreCase`.
Properties resolve against Go field names or column names (`WarehouseId`, `WarehouseID`, `warehouse_id` all work). A trailing `jpa.Pageable`, `jpa.Sort` or `jpa.Limit` parameter is applied dynamically. Unknown properties or wrong argument counts fail at `go generate`, like Spring fails at startup.

Return types: `[]T`, `*T` (ErrNotFound), `jpa.Page[T]`, `jpa.Slice[T]`, `jpa.Window[T]` (with a `jpa.ScrollPosition` param — keyset scrolling), `int64` (count/delete), `bool` (exists), `error` (delete). For `jpa:query`: `[]R`, `*R`, scalar, `jpa.Page[R]` (add `// jpa:count <sql>`), `jpa.Slice[R]`, or `(int64, error)`/`error` with `jpa:modifying`. `:name` parameters bind by Go parameter name; slices expand.

## Dynamic filters (Specification) and keyset scrolling

```go
c := jpa.Or(
    jpa.And(jpa.Eq("ProductCode", "A"), jpa.Eq("WarehouseId", 1)),
    jpa.And(jpa.Eq("ProductCode", "B"), jpa.Eq("WarehouseId", 2)),
)
rows, _ := repo.FindWhere(ctx, c, jpa.Sort{jpa.Desc("Id")}, jpa.Limit(50))
page, _ := repo.PageWhere(ctx, jpa.In("ProductCode", codes), jpa.PageRequest(0, 20))
n, _    := repo.DeleteWhere(ctx, jpa.Lt("UpdatedOn", cutoff))

w, _ := repo.FindByWarehouseIdOrderByProductCodeAscIdAsc(ctx, 2, jpa.Keyset(100))
for w.HasNext { w, _ = repo.FindByWarehouseIdOrderByProductCodeAscIdAsc(ctx, 2, w.Next) }
```

Properties are validated against the entity; `jpa.RawCond("price IS NULL OR price > :p", params)` is the unvalidated escape hatch.

## Also in the box

- **Retry**: `jpa.WithRetry(jpa.RetryPolicy{Attempts: 3, Backoff: 20*time.Millisecond, Retryable: jpa.IsDeadlock})` — reads always, writes only with `Writes: true` (must be idempotent).
- **Hooks**: `jpa.WithDefaultTimeout(d)`, `jpa.Metrics(func(name, dur, rows, err))`, `jpa.SlowQuery(threshold, report)`, or your own `jpa.Hook`.
- **Locks**: add a trailing `jpa.LockMode` parameter (`jpa.ForUpdate`, `jpa.ForShare`) to a derived method: `FindLockedByProductCode(ctx, code, jpa.ForUpdate)`.
- **Optimistic locking**: tag a counter `orm:"version"`; `Update` checks and increments it, returning `jpa.ErrOptimisticLock` on conflict.
- **Batch update**: `repo.BatchUpdate(ctx, []jpa.BatchRow{{Key: {"ProductCode": "A"}, Set: {"Mrp": 9.5}}, …})` renders one CASE-WHEN statement per 500 rows.
- **Streaming**: `repo.EachBy(ctx, "FindByWarehouseId", func(r T) error { … }, id)`.
- **Mocks**: `jpagen -type Repository -mock` emits `RepositoryMock` with a func field per method.
- **CI**: `jpagen … -check` exits 1 when generated files are stale.
- **Safety**: reserved identifiers are quoted per dialect, `Containing/StartingWith` escape `%`/`_`, and statements over the driver's bind limit fail early with `sqlgen.ErrTooManyParams` (use `jpa.Chunk`).
- **Dialects**: `sqlgen.MySQL`, `sqlgen.Postgres`, `sqlgen.SQLite`.
- **Real-database tests**: `make integration` runs the example repository against SQLite, MySQL 8 and Postgres 16 (Docker).

Migrating an existing sqlx/GORM codebase: [`docs/MIGRATION.md`](docs/MIGRATION.md).

## Runtime

- `jpa.New[T,ID](exec, dialect, opts...)` — `exec` is `*sql.DB` or `*sql.Tx`; dialects `sqlgen.MySQL`, `sqlgen.Postgres`.
- `jpa.Hook` sees every statement (`Name`, `SQL`, `Args`) → timeouts, zap logging, metrics, deadlock retry. `jpa.Timeout(d)` is built in.
- `Repository.IncludingDeleted()` bypasses the soft-delete filter and hard-deletes.
- `SaveAll` writes generated keys back (Postgres: one multi-row INSERT with `RETURNING`; MySQL: one INSERT per row). `SaveAllNoKeys` does chunked multi-row INSERTs when ids aren't needed.
- No relationships, no auto-migration, no global connection singleton — by design (see DESIGN.md).

## Develop

```
make test        # parser, sqlgen, entity, jpa (sqlmock), example/warehouse (generated code)
make integration # SQLite + MySQL + Postgres (needs Docker; GOJPA_SKIP_DOCKER=1 for SQLite only)
make generate    # rerun jpagen
make lint        # golangci-lint (0 issues expected)
```

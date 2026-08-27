# Migrating an existing service to gojpa

This is the pattern used to move catalog-service (MySQL + sqlx, 33 DAL packages) package by package without touching callers or their mocks.

## 1. Keep your public interface, generate from a private one

Your service already has something like:

```go
type Repository interface {
    GetByProductCodes(ctx context.Context, codes []string) (map[string]*ProductModel, error)
}
func NewRepository(db *sqlx.DB) Repository
```

Leave both **byte-identical** (tests mock `Repository`; wiring calls `NewRepository`). Add an unexported interface for the generator:

```go
//go:generate go run github.com/shubhesh07/gojpa/cmd/jpagen -type queries -entity ProductModel -id int64 -impl RepositoryImpl -ctor newRepositoryImpl
type queries interface {
    FindByProductCodeIn(ctx context.Context, codes []string) ([]ProductModel, error)

    // jpa:query SELECT DISTINCT mother_brand FROM medicine_master WHERE mother_brand IS NOT NULL
    GetDistinctMotherBrand(ctx context.Context) ([]string, error)
}
```

`jpagen` emits `queries_gen.go` with `RepositoryImpl` (embeds `*jpa.Repository`) and `newRepositoryImpl(exec, dialect, opts...)`. Then, in `impl.go`:

```go
func NewRepository(sdb *sqlx.DB) Repository {          // unchanged signature
    return newRepositoryImpl(sdb.DB, sqlgen.MySQL, dbinfra.JPAOptions()...)
}

func (r *RepositoryImpl) GetByProductCodes(ctx context.Context, codes []string) (map[string]*ProductModel, error) {
    if len(codes) == 0 { return map[string]*ProductModel{}, nil }
    rows, err := r.FindByProductCodeIn(ctx, codes)
    ...
}
```

`*sqlx.DB` embeds `*sql.DB`, so the same OTel-instrumented pool is reused — no second connection.

## 2. Models port unchanged

`db:"col"` tags are the sqlx convention. Add `TableName()`; keep `sql.Null*` and any `sql.Scanner` types (e.g. a `BitBool`). Plain `bool`/integer/`time.Time` fields also scan MySQL `BIT(1)`, `TINYINT` and text `DATETIME`.

Only tag `orm:"pk"` after confirming the column is the real primary key on the **live** schema; composite-key tables get no tag (their `*ByID` methods return `ErrNoPrimaryKey`).

## 3. Derived vs declared

| Use | When |
|---|---|
| derived name (`FindByStatusTrueAndIdGreaterThanOrderByIdAsc`) | every WHERE column is on the model and selecting all model columns is fine |
| `// jpa:query <sql>` | functions, `DISTINCT` on a subset, `GROUP BY`, literal constants, joins |
| `// jpa:query` + `// jpa:modifying` | bulk `UPDATE … WHERE pk IN (:ids)` |
| hand-written method on `*RepositoryImpl` | anything else (`r.Exec`, `r.ExecArgs` for `?`-style SQL with `sqlx.In` semantics, `jpa.Select[R]`) |
| `jpa.BatchUpdate` | per-row CASE WHEN updates |

## 4. Cross-cutting behaviour goes in one hook

```go
func JPAOptions() []jpa.Option {
    return []jpa.Option{
        jpa.WithDefaultTimeout(cfg.DB.QueryTimeout),
        jpa.WithHook(zapHook{}),                      // query name, duration_ms, rows, error
        jpa.WithRetry(jpa.RetryPolicy{Attempts: 3, Backoff: 20 * time.Millisecond, Retryable: jpa.IsDeadlock}),
    }
}
```

## 5. Tests

- Existing `sqlmock` tests keep working; change `newTestRepo` to call `NewRepository(sqlx.NewDb(db, "mysql"))` and update expected SQL text where the column list changed.
- `jpagen -type Repository -mock` emits `RepositoryMock` (func fields) — drop hand-written mocks.
- Add `jpagen ... -check` (or `make generate-check`) to CI so stale generated files fail the build.
- Verify column types against the live schema, not your test DDL: `BIT(1)` vs `TINYINT(1)` differences are invisible in sqlmock.

## 6. Mixed packages (reads migrated, writes untouched)

```go
type repositoryImpl struct {
    *RepositoryImpl      // generated reads
    db *sqlx.DB          // legacy writes, verbatim
}
func NewRepository(sdb *sqlx.DB) Repository {
    return &repositoryImpl{RepositoryImpl: newRepositoryImpl(sdb.DB, sqlgen.MySQL, JPAOptions()...), db: sdb}
}
```

## From GORM

Replace `gorm.Model` with explicit `orm:"pk,auto"`, `orm:"created"`, `orm:"updated"` tags; `DeletedAt` soft delete becomes `SoftDelete() (string, string)` on the entity (`"deleted_at IS NULL"`, `"deleted_at = NOW()"`); `db.Where(...).Find(&rows)` becomes a derived method or `FindWhere(ctx, jpa.Cond)`; `Preload` has no equivalent — fetch children with one `FindByParentIdIn` and join in Go.

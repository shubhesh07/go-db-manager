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

## Runtime

- `jpa.New[T,ID](exec, dialect, opts...)` — `exec` is `*sql.DB` or `*sql.Tx`; dialects `sqlgen.MySQL`, `sqlgen.Postgres`.
- `jpa.Hook` sees every statement (`Name`, `SQL`, `Args`) → timeouts, zap logging, metrics, deadlock retry. `jpa.Timeout(d)` is built in.
- `Repository.IncludingDeleted()` bypasses the soft-delete filter and hard-deletes.
- `SaveAll` writes generated keys back (Postgres: one multi-row INSERT with `RETURNING`; MySQL: one INSERT per row). `SaveAllNoKeys` does chunked multi-row INSERTs when ids aren't needed.
- No relationships, no auto-migration, no global connection singleton — by design (see DESIGN.md).

## Develop

```
make test        # parser, sqlgen, entity, jpa (sqlmock), example/warehouse (generated code)
make generate    # rerun jpagen
```

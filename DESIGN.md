# Spring Data JPA → Go: how it works and how we mirror it

Source: Spring Data JPA reference (query-methods, query-methods-details), read 2026-08-27, plus Spring Data Commons internals.
Target: power every catalog-service DAL method (see CLAUDE.md coverage matrix) with the same developer model.

## 1. How Spring Data JPA actually works

### 1.1 Runtime pipeline

```
interface OrderRepo extends JpaRepository<Order, Long> { List<Order> findByCustomerIdAndStatusIn(Long c, Collection<Integer> s, Pageable p); }
        │ startup (@EnableJpaRepositories)
        ▼
RepositoryFactorySupport.getRepository()
  ├─ target = SimpleJpaRepository<Order,Long>(EntityManager)      ← CRUD: save/findById/findAll/delete/count/exists
  ├─ fragments = *Impl classes found by name (OrderRepoImpl)        ← custom hand-written methods
  └─ JDK dynamic Proxy over the interface
        QueryExecutorMethodInterceptor → for every abstract method, QueryLookupStrategy resolves ONE RepositoryQuery:
             CREATE_IF_NOT_FOUND (default):
               1. @Query / @NativeQuery on the method            → StringQuery (JPQL or SQL)
               2. @NamedQuery "Order.findByX" on the entity        → NamedQuery
               3. else derive from name                            → PartTreeJpaQuery
        │ call time
        ▼
RepositoryQuery.execute(args)
  ├─ ParametersParameterAccessor: splits args into bindable params vs special params (Pageable, Sort, Limit, ScrollPosition)
  ├─ JpaQueryExecution chosen by return type: SingleEntity / Collection / Paged (runs COUNT too) / Sliced (fetch size+1) / Stream / Modifying / Delete / Exists
  └─ result converted (Optional, List, Page, Slice, Window, projection interface/DTO, primitive)
```

Key point: **Spring has a proxy at runtime; Go does not.** Everything else (parse, build, bind, execute, map) is portable.

### 1.2 Method-name derivation (`PartTree`)

1. Validate prefix with regex `^(find|read|get|query|search|stream|count|exists|delete|remove)(\p{Lu}.*?)??By`.
2. **Subject** = text before first `By`: may contain `Distinct`, `First<N>`/`Top<N>` (N default 1), and free descriptive words (`findActiveUsersBy…` – `ActiveUsers` is ignored).
3. **Predicate** = text after `By`, split on `OrderBy` → (criteria, ordering).
4. Criteria split by `Or` into `OrPart`s, each split by `And` into `Part`s. Splitting is **token-based on CamelCase** (`And`/`Or` must start upper-case and be followed by upper-case or end) — this is why Spring never mis-splits `Order`, `Vendor`, `Category`.
5. Each `Part` = `PropertyPath` + `Type` (keyword). `Type` is detected **longest first** in a fixed order (`IsNotNull`, `NotNull`, `IsNull`, `Null`, `NotIn`, `In`, `IsNotLike`, `NotLike`, `Like`, `StartingWith`, `EndingWith`, `NotContaining`, `Containing`, `Between`, `Before`, `After`, `LessThanEqual`, `LessThan`, `GreaterThanEqual`, `GreaterThan`, `IsTrue`, `True`, `IsFalse`, `False`, `IsNot`, `Not`, `IsEmpty`, `Empty`, `IsNotEmpty`, `NotEmpty`, `Regex`, `Exists`, `Is`, `Equals`, then `SIMPLE_PROPERTY`). Each `Type` knows how many args it consumes (0, 1 or 2).
6. `IgnoreCase` suffix per part; `AllIgnoreCase` on the whole tree.
7. `PropertyPath.from("AddressZipCode", Order.class)` resolves against the **entity metamodel**: try the whole camel-cased head; if not a property, move the split left (`Address` + `ZipCode`), recurse. `_` forces a split (`Address_ZipCode`). Unknown property → **startup failure**, not runtime.
8. `OrderByLastnameDescFirstnameAsc` → list of (`property`, `Asc|Desc`); direction token is stripped from the tail of each property.
9. Reserved names: `findById/existsById/deleteById` always mean the `@Id` property whatever it is called.

### 1.3 Query building (`JpaQueryCreator`)

`PartTree` → JPA Criteria: one `Predicate` per `Part`, `and`-ed inside an `OrPart`, `or`-ed across. The ORM dialect then handles placeholders, `LIMIT/OFFSET` vs `FETCH FIRST`, `UPPER()` for IgnoreCase, and `%` wrapping on the **bound value** for StartingWith/EndingWith/Containing (with `_`/`%` escaped when configured).

### 1.4 Declared queries

- `@Query("jpql")` — JPQL, positional `?1` or named `:name` (`@Param`). A `Sort` parameter appends `order by`.
- `@NativeQuery("sql")` — raw SQL; `Page` needs `countQuery`. Sort on native queries is unsafe unless whitelisted.
- `@Modifying` marks UPDATE/DELETE; returns `int` rows affected; `clearAutomatically/flushAutomatically` manage the persistence-context cache (irrelevant in Go — no first-level cache).
- Derived `deleteBy…` loads entities then deletes one by one (lifecycle callbacks); `@Modifying @Query("delete …")` is one bulk statement. **There is no derived `updateBy…`** in Spring — bulk updates always go through `@Modifying @Query`.
- SpEL: `#{#entityName}`, `?#{[0]}`, `escape([0])`.

### 1.5 Special parameters and return types

| Param | Effect |
|---|---|
| `Pageable` (page, size, Sort) | `LIMIT size OFFSET page*size` + `ORDER BY`; `Page<T>` return triggers a derived `COUNT`, `Slice<T>` fetches size+1 and skips the count |
| `Sort` | dynamic ORDER BY, property names validated against the entity |
| `Limit` | dynamic `LIMIT`; cannot combine with `Pageable` |
| `ScrollPosition` | offset or keyset → `Window<T>`; keyset builds `WHERE (a,b) > (?,?)` from the sort keys |

Return types: `T`, `Optional<T>`, `List`, `Stream` (cursor, must close), `Page`, `Slice`, `Window`, `long/int` (count/modifying), `boolean` (exists), interface projection (only listed columns selected), DTO projection (`select new …`), `Map<String,Object>` for native.

### 1.6 Beyond method names

- **Specifications** — `Specification<T> = (root, query, cb) -> Predicate`, composable `and/or/not`; `findAll(spec, pageable)`. The dynamic-filter story.
- **Query by Example** — probe entity + `ExampleMatcher`; nulls ignored.
- **Querydsl** — generated `QOrder` types → type-safe builder (closest analogue to what Go must do anyway).
- **Entity graphs / relationships** — `@OneToMany` etc., join fetch via `@EntityGraph`.
- **Auditing** — `@CreatedDate/@LastModifiedDate/@CreatedBy` via `AuditingEntityListener`.
- **Soft delete** — not built in; Hibernate `@SQLDelete` + `@SQLRestriction("active = 1")` per entity — **opt-in per entity with a custom expression**.
- **Transactions** — `@Transactional`; repository methods transactional by default via a thread-bound `EntityManager`.
- **Custom fragments** — `interface OrderRepoCustom`, impl `OrderRepoCustomImpl`, extend both; merged into one proxy.

## 2. Mapping to Go

### 2.1 The one thing we can't copy: the proxy

| Option | Dev experience | Cost | Verdict |
|---|---|---|---|
| A. `repo.CallMethod(ctx, "FindByXAndY", args...) (any, error)` | string names, `any` results, no compile-time checks | none | keep as the engine, **not** the API |
| B. **`go:generate` codegen**: user writes the interface, generator parses each method name at build time and emits `order_repo_gen.go` with typed bodies calling the engine. Unknown property or bad grammar = **generation error** (= Spring's startup error). | identical to Spring: write the interface, get the impl | one tool (`cmd/jpagen`) using `go/ast` + the same PartTree parser | **do this** |
| C. Querydsl-style generated `Q` types | verbose | codegen anyway | later, for dynamic filters |

Codegen also gives `@Query`/`@Modifying` for free as **doc-comment directives**:

```go
//go:generate jpagen -type OrderRepository
type OrderRepository interface {
    jpa.Repository[Order, int64]                                   // CRUD fragment (SimpleJpaRepository)

    FindByCustomerIdAndStatusIdIn(ctx context.Context, c int64, s []int) ([]Order, error)
    FindTop5ByCustomerIdOrderByCreatedOnDesc(ctx context.Context, c int64) ([]Order, error)
    FindByOrderId(ctx context.Context, id int64) (*Order, error)   // *T → single; nil + ErrNotFound
    CountByStatusId(ctx context.Context, s int) (int64, error)
    ExistsByOrderId(ctx context.Context, id int64) (bool, error)
    FindByCustomerId(ctx context.Context, c int64, p jpa.Pageable) (jpa.Page[Order], error)

    // jpa:query UPDATE medicine_master SET active = :active, updated_on = NOW() WHERE product_code IN (:codes)
    // jpa:modifying
    UpdateActiveByProductCodes(ctx context.Context, active bool, codes []string) (int64, error)

    // jpa:query SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (:codes) GROUP BY product_code
    GetSoldCounts(ctx context.Context, codes []string) ([]SoldCount, error)   // DTO projection = any struct with db tags
}
```

Generated file: one body per method → `engine.Find[Order](ctx, r.exec, parsedQuery, args...)`. Any method the user implements on `OrderRepositoryImpl` (Spring's `*Impl` rule) is skipped by the generator.

### 2.2 Component map

| Spring | Go package | Notes |
|---|---|---|
| Entity metamodel (`@Entity/@Id/@Column`) | `entity` | `db:"col"` tags (sqlx convention so catalog models port unchanged), `orm:"pk"`, `orm:"auto"`, embedded structs flattened, `orm:"-"`. Explicit column list — **never `SELECT *`**. |
| `PartTree/Part/Type` | `parser` (rewrite of `method_parser.go`) | CamelCase tokeniser; `And/Or` as tokens; keyword table longest-first; property resolution against entity fields with `_` split; `Distinct`, `First/Top N`, `IgnoreCase`, `After/Before`, `Not`, multi-`OrderBy`. Pure, no DB. |
| `JpaQueryCreator` + dialect | `sqlgen` | `Dialect{Placeholder(i), Limit(l,o), Returning bool, Upper, Now}`; MySQL + Postgres. `IN` expands slices; empty slice → `1=0`. |
| `SimpleJpaRepository` | `jpa.Repository[T, ID]` | `FindByID, FindAllByID, FindAll(Sort), FindAll(Pageable), Save (insert or update by PK zero-ness), SaveAll (multi-row INSERT, chunk 500), Update, DeleteByID, DeleteAll, Count, ExistsByID`. `ErrNotFound` sentinel. |
| `@Query/@NativeQuery/@Modifying/@Param` | `// jpa:query`, `// jpa:modifying`, `// jpa:count` directives | named `:name` params bound from Go parameter names; slices expand like `sqlx.In`; `Pageable` param appends `LIMIT/OFFSET`. |
| `Pageable/Page/Slice/Sort/Limit` | `jpa.Pageable`, `Page[T]`, `Slice[T]`, `Sort`, `Limit` | Sort fields validated against entity columns (injection guard). Keyset `Window` later. |
| `Specification` | `jpa.Spec` + `FindAll(ctx, spec, pageable)` | small typed builder over entity columns; replaces `query.QueryBuilder`. |
| `EntityManager/@Transactional` | `jpa.Executor` (`*sql.DB` and `*sql.Tx` both satisfy it) + `repo.WithTx(tx)` + `jpa.Transactional(ctx, db, fn)` | explicit, no thread-local. Catalog-service keeps its no-tx convention by not calling it. |
| `AuditingEntityListener` | `orm:"created"`, `orm:"updated"`, `orm:"updated_by"` tags; actor from `jpa.WithActor(ctx, userID)` | use `Dialect.Now()` (server `NOW()`), not a Go time, to keep DB clock semantics like catalog-service. |
| `@SQLRestriction("active = 1")` | `orm:"soft_delete:active=1"` on the struct, **opt-in**, plus generated `…IncludingDeleted` variant | `deleted_at IS NULL` is just one preset. |
| Query hints/`@Meta`/logging | `jpa.Hook{Before, After}` middleware; default adds per-query timeout + zap log `{query: "pkg.Method", duration_ms, rows}` | matches catalog-service conventions. |
| Relationships / entity graphs | **out of scope** — catalog-service does N queries + Go maps; delete `relationship/` | revisit with a real consumer. |
| `AutoMigrate` | out of scope (schema owned elsewhere); delete or park `migration/` | |

### 2.3 Coverage of catalog-service's 63 methods

| Bucket | Mechanism |
|---|---|
| 34 simple lookups (`=`, `IN`, flags, `IS NOT NULL`, `>`, `ORDER BY`, `DISTINCT`) | derived names, generated |
| 9 literal-predicate lookups (`name='gst' AND active=1`) | derived `FindByNameAndActiveTrue(ctx, "gst")` |
| 6 bulk flag updates | `// jpa:query … // jpa:modifying` (Spring has no derived update either) |
| 3 CASE-WHEN per-row batch updates | hand-written fragment on `*Impl` via `repo.Exec`; later optional `jpa.BatchUpdate[T](ctx, rows, keyCol, cols...)` emitting CASE-WHEN + chunking (the one place Go can beat Spring) |
| 3 OR-tuple composite keys | `Spec` builder or `jpa:query` |
| 2 multi-VALUES inserts | `SaveAll` |
| `MAX() GROUP BY`, `CONCAT_WS`, `ORDER BY x IS NULL, x` | `jpa:query` with DTO projection |
| Null-safe update guard | `jpa:query` |
| 4 DynamoDB methods | untouched |
| timeouts, logging, `IsDeadlock` | default `Hook` |
| `sql.Null*`, `BitBool`, DECIMAL/DATETIME `[]byte` | `Scanner` fields honoured + `[]byte`→number/time coercion in `entity.Scan` |

Everything catalog-service does today is expressible; ~70% becomes a one-line interface method.

## 3. Build order (supersedes CLAUDE.md priority list where they differ)

1. `parser`: PartTree port, table-driven tests for every keyword × MySQL/Postgres. No DB.
2. `sqlgen` + `Dialect` (MySQL first — that's the consumer).
3. `entity` metadata cache (columns, PK, auto, audit, soft-delete tags) + row scanner. `sqlmock` tests.
4. `jpa.Repository[T,ID]` on an injected `Executor`; `Hook`; `ErrNotFound`.
5. `cmd/jpagen`: derived methods, directives, `Pageable/Sort/Limit` params, `*T/[]T/Page/Slice/int64/bool` returns, skip `*Impl` methods.
6. Port `medicinewarehousemaster` end-to-end (IN+IN, CASE-WHEN fragment, bulk update) as acceptance test; then the rest mechanically.
7. Delete `relationship/`, `migration/`, `query/`, `utils/`, `pagination.Paginate`; rewrite README to what exists.

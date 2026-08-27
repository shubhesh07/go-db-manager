// Package jpa is the runtime behind generated repositories: CRUD
// (SimpleJpaRepository), derived-query execution, paging, hooks, raw queries
// and transactions.
package jpa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/shubhesh07/gojpa/entity"
	"github.com/shubhesh07/gojpa/parser"
	"github.com/shubhesh07/gojpa/sqlgen"
)

// ErrNotFound is returned by single-result lookups that match no row.
var ErrNotFound = errors.New("jpa: not found")

// Executor is satisfied by *sql.DB and *sql.Tx.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ---- special parameters -------------------------------------------------

type Order = parser.Order

// Sort is a dynamic ORDER BY; properties are validated against the entity.
type Sort []Order

func Asc(prop string) Order  { return Order{Property: prop} }
func Desc(prop string) Order { return Order{Property: prop, Desc: true} }

// Limit is a dynamic LIMIT.
type Limit int

// LockMode is a trailing special parameter adding FOR UPDATE / FOR SHARE.
type LockMode = sqlgen.Lock

const (
	ForUpdate = sqlgen.ForUpdate
	ForShare  = sqlgen.ForShare
)

// Pageable is a zero-based page request.
type Pageable struct {
	Page, Size int
	Sort       Sort
}

func PageRequest(page, size int, sort ...Order) Pageable {
	return Pageable{Page: page, Size: size, Sort: sort}
}

type Page[T any] struct {
	Content    []T   `json:"content"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func (p Page[T]) HasNext() bool { return p.Page+1 < p.TotalPages }

type Slice[T any] struct {
	Content []T  `json:"content"`
	Page    int  `json:"page"`
	Size    int  `json:"size"`
	HasNext bool `json:"has_next"`
}

// ---- hooks & context ------------------------------------------------------

// Query describes a statement about to run. Name is "Type.Method".
type Query struct {
	Name string
	SQL  string
	Args []any
}

// Hook observes every statement. done is called with rows affected/returned
// and the error. Use it for timeouts, logging, metrics, deadlock retry.
type Hook interface {
	Before(ctx context.Context, q Query) (context.Context, func(rows int64, err error))
}

type HookFunc func(ctx context.Context, q Query) (context.Context, func(rows int64, err error))

func (f HookFunc) Before(ctx context.Context, q Query) (context.Context, func(int64, error)) {
	return f(ctx, q)
}

// Timeout is a Hook applying a per-statement deadline.
func Timeout(d time.Duration) Hook {
	return HookFunc(func(ctx context.Context, _ Query) (context.Context, func(int64, error)) {
		ctx, cancel := context.WithTimeout(ctx, d)
		return ctx, func(int64, error) { cancel() }
	})
}

type actorKey struct{}

// WithActor records who is writing; used for created_by/updated_by columns.
func WithActor(ctx context.Context, actor any) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func Actor(ctx context.Context) any { return ctx.Value(actorKey{}) }

// ---- repository -----------------------------------------------------------

const batchSize = 500

type Option func(*config)

type config struct {
	hooks    []Hook
	name     string
	retry    *RetryPolicy
	comments bool
	replica  Executor
	cache    *cacheCfg
}

// WithComments prefixes every statement with "/* Type.Method */" so slow-query
// logs and SHOW PROCESSLIST show which repository method is running.
func WithComments() Option { return func(c *config) { c.comments = true } }

// WithReplica sends reads (no row lock, not inside WithTx) to replica.
func WithReplica(replica Executor) Option { return func(c *config) { c.replica = replica } }

// ---- read cache ----------------------------------------------------------

// CacheStore is a minimal cache; MemoryCache is the built-in one.
type CacheStore interface {
	Get(key string) (any, bool)
	Set(key string, v any, ttl time.Duration)
	Clear()
}

type cacheCfg struct {
	store CacheStore
	ttl   time.Duration
}

// WithCache caches derived/Cond SELECT results for ttl, keyed by SQL+args.
// Any write through the same repository clears the store; call
// InvalidateCache after writes made elsewhere. Meant for read-mostly master
// data, not for rows that change under the request.
func WithCache(store CacheStore, ttl time.Duration) Option {
	return func(c *config) { c.cache = &cacheCfg{store: store, ttl: ttl} }
}

type memEntry struct {
	v   any
	exp time.Time
}

type memoryCache struct{ m sync.Map }

// MemoryCache is an in-process CacheStore with per-entry expiry.
func MemoryCache() CacheStore { return &memoryCache{} }

func (c *memoryCache) Get(key string) (any, bool) {
	e, ok := c.m.Load(key)
	if !ok {
		return nil, false
	}
	me := e.(memEntry)
	if time.Now().After(me.exp) {
		c.m.Delete(key)
		return nil, false
	}
	return me.v, true
}
func (c *memoryCache) Set(key string, v any, ttl time.Duration) {
	c.m.Store(key, memEntry{v: v, exp: time.Now().Add(ttl)})
}
func (c *memoryCache) Clear() { c.m.Range(func(k, _ any) bool { c.m.Delete(k); return true }) }

// ---- request-scoped filter (Hibernate @Filter / multi-tenancy) ------------

type filterKey struct{}

// WithFilter ANDs cond into every derived/Cond query (find, count, exists,
// window, delete) executed with ctx. Save/Update/raw queries are unaffected.
func WithFilter(ctx context.Context, cond Cond) context.Context {
	if prev, ok := ctx.Value(filterKey{}).(Cond); ok {
		cond = And(prev, cond)
	}
	return context.WithValue(ctx, filterKey{}, cond)
}

func ctxFilter(ctx context.Context) Cond {
	c, _ := ctx.Value(filterKey{}).(Cond)
	return c
}

func WithHook(h Hook) Option   { return func(c *config) { c.hooks = append(c.hooks, h) } }
func WithName(n string) Option { return func(c *config) { c.name = n } }

// WithDefaultTimeout applies a per-statement deadline to every query
// (same as WithHook(Timeout(d)), named for discoverability).
func WithDefaultTimeout(d time.Duration) Option { return WithHook(Timeout(d)) }

// ---- retry ----------------------------------------------------------------

// RetryPolicy re-runs a statement when Retryable(err) is true. Reads are
// always eligible; writes only when Writes is true (they must be idempotent —
// the CASE-WHEN batch updates are, plain INSERTs are not).
type RetryPolicy struct {
	Attempts  int           // total attempts, >= 1
	Backoff   time.Duration // base; doubles per attempt
	Retryable func(error) bool
	Writes    bool
}

func WithRetry(p RetryPolicy) Option { return func(c *config) { c.retry = &p } }

// IsDeadlock reports MySQL 1213/1205 and Postgres 40001/40P01 (serialization
// failure / deadlock) without importing a driver.
func IsDeadlock(err error) bool {
	if err == nil {
		return false
	}
	var st interface{ SQLState() string }
	if errors.As(err, &st) {
		switch st.SQLState() {
		case "40001", "40P01":
			return true
		}
	}
	var num interface{ Number() uint16 }
	if errors.As(err, &num) {
		switch num.Number() {
		case 1213, 1205:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "error 1213") || strings.Contains(msg, "error 1205") ||
		strings.Contains(msg, "deadlock") || strings.Contains(msg, "lock wait timeout")
}

// IsTransient is IsDeadlock plus connection resets ("bad connection",
// "connection reset", "broken pipe").
func IsTransient(err error) bool {
	if IsDeadlock(err) {
		return true
	}
	if errors.Is(err, sql.ErrConnDone) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bad connection") || strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe")
}

// ---- ready-made hooks ------------------------------------------------------

// Metrics calls observe after every statement; plug Prometheus/OTel in there.
func Metrics(observe func(name string, d time.Duration, rows int64, err error)) Hook {
	return HookFunc(func(ctx context.Context, q Query) (context.Context, func(int64, error)) {
		start := time.Now()
		return ctx, func(rows int64, err error) { observe(q.Name, time.Since(start), rows, err) }
	})
}

// SlowQuery calls report for statements slower than threshold.
func SlowQuery(threshold time.Duration, report func(q Query, d time.Duration)) Hook {
	return HookFunc(func(ctx context.Context, q Query) (context.Context, func(int64, error)) {
		start := time.Now()
		return ctx, func(int64, error) {
			if d := time.Since(start); d >= threshold {
				report(q, d)
			}
		}
	})
}

// Repository is the CRUD + derived-query engine for entity T with key ID.
// It is safe for concurrent use.
type Repository[T any, ID any] struct {
	exec           Executor
	d              sqlgen.Dialect
	meta           *entity.Meta
	cfg            config
	includeDeleted bool
	trees          *sync.Map // method name -> *parser.Tree
}

// New builds a repository. It panics on an invalid entity, mirroring Spring's
// startup failure.
func New[T any, ID any](exec Executor, d sqlgen.Dialect, opts ...Option) *Repository[T, ID] {
	m, err := entity.Of[T]()
	if err != nil {
		panic(err)
	}
	r := &Repository[T, ID]{exec: exec, d: d, meta: m, trees: &sync.Map{}}
	r.cfg.name = m.Type.Name()
	for _, o := range opts {
		o(&r.cfg)
	}
	return r
}

// WithTx returns a copy bound to tx (or any Executor).
func (r *Repository[T, ID]) WithTx(exec Executor) *Repository[T, ID] {
	c := *r
	c.exec = exec
	c.cfg.replica = nil // everything inside a transaction goes to that connection
	return &c
}

// InvalidateCache clears the WithCache store (e.g. after writes made
// outside this repository).
func (r *Repository[T, ID]) InvalidateCache() {
	if r.cfg.cache != nil {
		r.cfg.cache.store.Clear()
	}
}

// reader picks the replica for plain reads when configured.
func (r *Repository[T, ID]) reader(q sqlgen.Query) Executor {
	if r.cfg.replica != nil && !q.Locked && isRead(q.SQL) {
		return r.cfg.replica
	}
	return r.exec
}

// IncludingDeleted returns a copy that ignores the entity's SoftDelete filter
// and hard-deletes.
func (r *Repository[T, ID]) IncludingDeleted() *Repository[T, ID] {
	c := *r
	c.includeDeleted = true
	return &c
}

func (r *Repository[T, ID]) Meta() *entity.Meta      { return r.meta }
func (r *Repository[T, ID]) Dialect() sqlgen.Dialect { return r.d }
func (r *Repository[T, ID]) Executor() Executor      { return r.exec }

func (r *Repository[T, ID]) run(ctx context.Context, name string, q sqlgen.Query, fn func(ctx context.Context, q sqlgen.Query) (int64, error)) (int64, error) {
	if r.cfg.comments {
		q.SQL = "/* " + r.cfg.name + "." + name + " */ " + q.SQL
	}
	attempts := 1
	if p := r.cfg.retry; p != nil && p.Attempts > 1 && (p.Writes || isRead(q.SQL)) {
		attempts = p.Attempts
	}
	var n int64
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			d := r.cfg.retry.Backoff << (i - 1)
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return n, ctx.Err()
			}
		}
		n, err = r.once(ctx, name, q, fn)
		if err == nil || i == attempts-1 || !r.cfg.retry.Retryable(err) || ctx.Err() != nil {
			return n, err
		}
	}
	return n, err
}

func isRead(sql string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT")
}

func (r *Repository[T, ID]) once(ctx context.Context, name string, q sqlgen.Query, fn func(ctx context.Context, q sqlgen.Query) (int64, error)) (int64, error) {
	query := Query{Name: r.cfg.name + "." + name, SQL: q.SQL, Args: q.Args}
	var dones []func(int64, error)
	for _, h := range r.cfg.hooks {
		var done func(int64, error)
		ctx, done = h.Before(ctx, query)
		if done != nil {
			dones = append(dones, done)
		}
	}
	n, err := fn(ctx, q)
	for i := len(dones) - 1; i >= 0; i-- {
		dones[i](n, err)
	}
	return n, err
}

func (r *Repository[T, ID]) query(ctx context.Context, name string, q sqlgen.Query) ([]T, error) {
	var key string
	if c := r.cfg.cache; c != nil && !q.Locked {
		key = q.SQL + fmt.Sprint(q.Args)
		if v, ok := c.store.Get(key); ok {
			return v.([]T), nil
		}
	}
	var out []T
	exec := r.reader(q)
	_, err := r.run(ctx, name, q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
		rows, err := exec.QueryContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return 0, err
		}
		out, err = entity.ScanAll[T](rows)
		return int64(len(out)), err
	})
	if err == nil && key != "" {
		r.cfg.cache.store.Set(key, out, r.cfg.cache.ttl)
	}
	return out, err
}

func (r *Repository[T, ID]) scalar(ctx context.Context, name string, q sqlgen.Query, dest any) error {
	exec := r.reader(q)
	_, err := r.run(ctx, name, q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
		rows, err := exec.QueryContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		if !rows.Next() {
			return 0, rows.Err()
		}
		return 1, rows.Scan(dest)
	})
	return err
}

func (r *Repository[T, ID]) execute(ctx context.Context, name string, q sqlgen.Query) (int64, error) {
	r.InvalidateCache()
	return r.run(ctx, name, q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
		res, err := r.exec.ExecContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

// ---- derived queries (used by generated code) ---------------------------

func (r *Repository[T, ID]) tree(method string) (*parser.Tree, error) {
	if t, ok := r.trees.Load(method); ok {
		return t.(*parser.Tree), nil
	}
	t, err := parser.Parse(method, func(p string) bool { _, ok := r.meta.ResolveProperty(p); return ok })
	if err != nil {
		return nil, err
	}
	r.trees.Store(method, t)
	return t, nil
}

// splitSpecial pulls a trailing Pageable/Sort/Limit off args.
func (r *Repository[T, ID]) splitSpecial(args []any) ([]any, sqlgen.Options) {
	opt := sqlgen.Options{IncludeDeleted: r.includeDeleted}
	for len(args) > 0 {
		switch v := args[len(args)-1].(type) {
		case Pageable:
			opt.OrderBy, opt.Limit, opt.Offset = v.Sort, v.Size, v.Page*v.Size
		case Sort:
			opt.OrderBy = v
		case Limit:
			opt.Limit = int(v)
		case LockMode:
			opt.Lock = v
		default:
			return args, opt
		}
		args = args[:len(args)-1]
	}
	return args, opt
}

func (r *Repository[T, ID]) build(ctx context.Context, method string, args []any) (*parser.Tree, sqlgen.Query, sqlgen.Options, error) {
	t, err := r.tree(method)
	if err != nil {
		return nil, sqlgen.Query{}, sqlgen.Options{}, err
	}
	args, opt := r.splitSpecial(args)
	opt.Cond = r.withFilter(ctx, opt.Cond)
	q, err := sqlgen.Build(r.d, r.meta, t, args, opt)
	return t, q, opt, err
}

func (r *Repository[T, ID]) withFilter(ctx context.Context, c Cond) Cond {
	f := ctxFilter(ctx)
	switch {
	case f == nil:
		return c
	case c == nil:
		return f
	}
	return And(c, f)
}

// SQL renders the statement a derived method would run, without executing
// it (for review, logging, EXPLAIN).
func (r *Repository[T, ID]) SQL(method string, args ...any) (sqlgen.Query, error) {
	_, q, _, err := r.build(context.Background(), method, args)
	return q, err
}

// Plan is a query plan from ExplainBy. FullScan is true when the dialect's
// EXPLAIN reports a full table scan (MySQL type=ALL, Postgres "Seq Scan",
// SQLite "SCAN" without an index) — assert on it in tests to keep queries
// indexed.
type Plan struct {
	Rows     []map[string]any
	FullScan bool
}

// ExplainBy runs EXPLAIN on a derived method's statement.
func (r *Repository[T, ID]) ExplainBy(ctx context.Context, method string, args ...any) (Plan, error) {
	_, q, _, err := r.build(ctx, method, args)
	if err != nil {
		return Plan{}, err
	}
	return r.explain(ctx, method, q)
}

func (r *Repository[T, ID]) explain(ctx context.Context, name string, q sqlgen.Query) (Plan, error) {
	q.SQL = r.d.ExplainPrefix() + q.SQL
	var plan Plan
	_, err := r.run(ctx, "explain."+name, q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
		rows, err := r.exec.QueryContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return 0, err
			}
			row := map[string]any{}
			for i, c := range cols {
				if b, ok := vals[i].([]byte); ok {
					vals[i] = string(b)
				}
				row[c] = vals[i]
			}
			plan.Rows = append(plan.Rows, row)
		}
		return int64(len(plan.Rows)), rows.Err()
	})
	if err != nil {
		return Plan{}, err
	}
	for _, row := range plan.Rows {
		for _, v := range row {
			s := strings.ToUpper(fmt.Sprint(v))
			if s == "ALL" || strings.Contains(s, "SEQ SCAN") || (strings.HasPrefix(s, "SCAN ") && !strings.Contains(s, "USING")) {
				plan.FullScan = true
			}
		}
	}
	return plan, nil
}

// Verify checks that every mapped column exists by running a zero-row
// SELECT. Call it at startup to fail fast on schema drift.
func (r *Repository[T, ID]) Verify(ctx context.Context) error {
	cols := make([]string, len(r.meta.Columns))
	for i, c := range r.meta.Columns {
		cols[i] = r.d.Quote(c.Name)
	}
	q := sqlgen.Query{SQL: fmt.Sprintf("SELECT %s FROM %s WHERE 1=0", strings.Join(cols, ", "), r.d.Quote(r.meta.Table))}
	_, err := r.run(ctx, "verify", q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
		rows, err := r.exec.QueryContext(ctx, q.SQL)
		if err != nil {
			return 0, fmt.Errorf("jpa: verify %s: %w", r.meta.Table, err)
		}
		defer rows.Close()
		return 0, rows.Err()
	})
	return err
}

// FindBy runs a derived find method, e.g. FindBy(ctx, "FindByCustomerIdAndStatusIdIn", 7, []int{1,2}).
func (r *Repository[T, ID]) FindBy(ctx context.Context, method string, args ...any) ([]T, error) {
	_, q, _, err := r.build(ctx, method, args)
	if err != nil {
		return nil, err
	}
	return r.query(ctx, method, q)
}

// EachBy streams a derived find row by row without loading the result set;
// fn returning an error stops the iteration. (Spring's Stream<T>.)
func (r *Repository[T, ID]) EachBy(ctx context.Context, method string, fn func(T) error, args ...any) error {
	_, q, _, err := r.build(ctx, method, args)
	if err != nil {
		return err
	}
	_, err = r.run(ctx, method, q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
		rows, err := r.exec.QueryContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return 0, err
		}
		var n int64
		for rows.Next() {
			var item T
			if err := rows.Scan(r.meta.ScanTargets(cols, reflect.ValueOf(&item))...); err != nil {
				return n, err
			}
			n++
			if err := fn(item); err != nil {
				return n, err
			}
		}
		return n, rows.Err()
	})
	return err
}

// FindOneBy returns the first row or ErrNotFound.
func (r *Repository[T, ID]) FindOneBy(ctx context.Context, method string, args ...any) (*T, error) {
	rows, err := r.FindBy(ctx, method, append(args, Limit(1))...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// PageBy runs a derived find with a Pageable and a COUNT for the total.
func (r *Repository[T, ID]) PageBy(ctx context.Context, method string, p Pageable, args ...any) (Page[T], error) {
	if p.Size <= 0 {
		return Page[T]{}, fmt.Errorf("jpa: page size must be > 0")
	}
	content, err := r.FindBy(ctx, method, append(args, p)...)
	if err != nil {
		return Page[T]{}, err
	}
	t, err := r.tree(method)
	if err != nil {
		return Page[T]{}, err
	}
	ct := *t
	ct.Verb, ct.Limit, ct.OrderBy, ct.Distinct = parser.Count, 0, nil, false
	q, err := sqlgen.Build(r.d, r.meta, &ct, args, sqlgen.Options{IncludeDeleted: r.includeDeleted, Cond: r.withFilter(ctx, nil)})
	if err != nil {
		return Page[T]{}, err
	}
	var total int64
	if err := r.scalar(ctx, method+".count", q, &total); err != nil {
		return Page[T]{}, err
	}
	return Page[T]{Content: content, Page: p.Page, Size: p.Size, Total: total, TotalPages: int((total + int64(p.Size) - 1) / int64(p.Size))}, nil
}

// SliceBy runs a derived find fetching size+1 rows to know whether a next
// page exists, without a COUNT.
func (r *Repository[T, ID]) SliceBy(ctx context.Context, method string, p Pageable, args ...any) (Slice[T], error) {
	if p.Size <= 0 {
		return Slice[T]{}, fmt.Errorf("jpa: page size must be > 0")
	}
	t, err := r.tree(method)
	if err != nil {
		return Slice[T]{}, err
	}
	opt := sqlgen.Options{OrderBy: p.Sort, Limit: p.Size + 1, Offset: p.Page * p.Size, IncludeDeleted: r.includeDeleted, Cond: r.withFilter(ctx, nil)}
	q, err := sqlgen.Build(r.d, r.meta, t, args, opt)
	if err != nil {
		return Slice[T]{}, err
	}
	rows, err := r.query(ctx, method, q)
	if err != nil {
		return Slice[T]{}, err
	}
	s := Slice[T]{Content: rows, Page: p.Page, Size: p.Size}
	if len(rows) > p.Size {
		s.Content, s.HasNext = rows[:p.Size], true
	}
	return s, nil
}

func (r *Repository[T, ID]) CountBy(ctx context.Context, method string, args ...any) (int64, error) {
	_, q, _, err := r.build(ctx, method, args)
	if err != nil {
		return 0, err
	}
	var n int64
	return n, r.scalar(ctx, method, q, &n)
}

func (r *Repository[T, ID]) ExistsBy(ctx context.Context, method string, args ...any) (bool, error) {
	_, q, _, err := r.build(ctx, method, args)
	if err != nil {
		return false, err
	}
	var ok bool
	return ok, r.scalar(ctx, method, q, &ok)
}

// DeleteBy runs a derived delete (soft if the entity declares SoftDelete).
func (r *Repository[T, ID]) DeleteBy(ctx context.Context, method string, args ...any) (int64, error) {
	_, q, _, err := r.build(ctx, method, args)
	if err != nil {
		return 0, err
	}
	return r.execute(ctx, method, q)
}

// ---- CRUD (SimpleJpaRepository) ----------------------------------------

// ErrNoPrimaryKey is returned by *ByID methods on entities without a single
// primary key (e.g. composite-key tables).
var ErrNoPrimaryKey = errors.New("jpa: entity has no primary key")

func (r *Repository[T, ID]) pkMethod(prefix string) (string, error) {
	if r.meta.PK == nil {
		return "", fmt.Errorf("%w: %s", ErrNoPrimaryKey, r.meta.Table)
	}
	var b strings.Builder
	for _, w := range strings.Split(r.meta.PK.Name, "_") {
		if w != "" {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	return prefix + "By" + b.String(), nil
}

func (r *Repository[T, ID]) FindByID(ctx context.Context, id ID) (*T, error) {
	m, err := r.pkMethod("find")
	if err != nil {
		return nil, err
	}
	return r.FindOneBy(ctx, m, id)
}

func (r *Repository[T, ID]) FindAllByID(ctx context.Context, ids []ID) ([]T, error) {
	m, err := r.pkMethod("find")
	if err != nil {
		return nil, err
	}
	return r.FindBy(ctx, m+"In", ids)
}

func (r *Repository[T, ID]) FindAll(ctx context.Context, sort ...Order) ([]T, error) {
	return r.FindBy(ctx, "findAll", Sort(sort))
}

func (r *Repository[T, ID]) FindPage(ctx context.Context, p Pageable) (Page[T], error) {
	return r.PageBy(ctx, "findAll", p)
}

func (r *Repository[T, ID]) Count(ctx context.Context) (int64, error) {
	return r.CountBy(ctx, "countAll")
}

func (r *Repository[T, ID]) ExistsByID(ctx context.Context, id ID) (bool, error) {
	m, err := r.pkMethod("exists")
	if err != nil {
		return false, err
	}
	return r.ExistsBy(ctx, m, id)
}

func (r *Repository[T, ID]) DeleteByID(ctx context.Context, id ID) error {
	m, err := r.pkMethod("delete")
	if err != nil {
		return err
	}
	n, err := r.DeleteBy(ctx, m, id)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (r *Repository[T, ID]) DeleteAllByID(ctx context.Context, ids []ID) (int64, error) {
	m, err := r.pkMethod("delete")
	if err != nil {
		return 0, err
	}
	return r.DeleteBy(ctx, m+"In", ids)
}

// insertColumns returns the column list and, per row, a placeholder list.
func (r *Repository[T, ID]) insertRow(b *sqlgen.Query, e *T, actor any) []string {
	v := reflect.ValueOf(e)
	var phs []string
	for i := range r.meta.Columns {
		c := &r.meta.Columns[i]
		switch {
		case c.Auto:
			continue
		case c.Created || c.Updated:
			phs = append(phs, r.d.Now())
		case (c.CreatedBy || c.UpdatedBy) && actor != nil:
			b.Args = append(b.Args, actor)
			phs = append(phs, r.d.Placeholder(len(b.Args)))
		default:
			b.Args = append(b.Args, c.Value(v).Interface())
			phs = append(phs, r.d.Placeholder(len(b.Args)))
		}
	}
	return phs
}

func (r *Repository[T, ID]) insertCols() []string {
	var cols []string
	for _, c := range r.meta.Columns {
		if !c.Auto {
			cols = append(cols, r.d.Quote(c.Name))
		}
	}
	return cols
}

// Save inserts e; a DB-generated key is written back into e.
func (r *Repository[T, ID]) Save(ctx context.Context, e *T) error {
	return r.SaveAll(ctx, []*T{e})
}

// SaveAll inserts in multi-row statements of 500. Generated keys are written
// back: Postgres via RETURNING in one statement; on dialects without
// RETURNING (MySQL) rows with an auto key are inserted one by one so each
// LAST_INSERT_ID is exact. Use SaveAllNoKeys for a single multi-row INSERT
// when you don't need the ids.
func (r *Repository[T, ID]) SaveAll(ctx context.Context, es []*T) error {
	pk := r.meta.PK
	if pk != nil && pk.Auto && !r.d.Returning() {
		for _, e := range es {
			if err := r.saveAll(ctx, []*T{e}); err != nil {
				return err
			}
		}
		return nil
	}
	return r.saveAll(ctx, es)
}

// SaveAllNoKeys always uses multi-row INSERTs; generated keys are not
// written back on dialects without RETURNING.
func (r *Repository[T, ID]) SaveAllNoKeys(ctx context.Context, es []*T) error {
	return r.saveAll(ctx, es)
}

func (r *Repository[T, ID]) saveAll(ctx context.Context, es []*T) error {
	actor := Actor(ctx)
	cols := r.insertCols()
	pk := r.meta.PK
	for start := 0; start < len(es); start += batchSize {
		chunk := es[start:min(start+batchSize, len(es))]
		q := sqlgen.Query{}
		rows := make([]string, len(chunk))
		for i, e := range chunk {
			rows[i] = "(" + strings.Join(r.insertRow(&q, e, actor), ", ") + ")"
		}
		q.SQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", r.d.Quote(r.meta.Table), strings.Join(cols, ", "), strings.Join(rows, ", "))
		if pk != nil && pk.Auto && r.d.Returning() {
			q.SQL += " RETURNING " + r.d.Quote(pk.Name)
			_, err := r.run(ctx, "save", q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
				rs, err := r.exec.QueryContext(ctx, q.SQL, q.Args...)
				if err != nil {
					return 0, err
				}
				defer rs.Close()
				for i := 0; rs.Next(); i++ {
					if err := rs.Scan(pk.Value(reflect.ValueOf(chunk[i])).Addr().Interface()); err != nil {
						return 0, err
					}
				}
				return int64(len(chunk)), rs.Err()
			})
			if err != nil {
				return err
			}
			continue
		}
		_, err := r.run(ctx, "save", q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
			res, err := r.exec.ExecContext(ctx, q.SQL, q.Args...)
			if err != nil {
				return 0, err
			}
			if pk != nil && pk.Auto && len(chunk) == 1 {
				if id, err := res.LastInsertId(); err == nil && id > 0 {
					f := pk.Value(reflect.ValueOf(chunk[0]))
					if f.CanInt() {
						f.SetInt(id)
					} else if f.CanUint() {
						f.SetUint(uint64(id))
					}
				}
			}
			return int64(len(chunk)), nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ErrOptimisticLock is returned by Update when the entity has an
// orm:"version" column and the row's version no longer matches.
var ErrOptimisticLock = errors.New("jpa: optimistic lock failed (row changed concurrently)")

// SaveAllUpsert inserts rows, updating the existing row on a unique-key
// conflict (MySQL ON DUPLICATE KEY UPDATE; Postgres/SQLite ON CONFLICT on
// conflictProps, which must name a unique index). All non-key, non-created
// columns are overwritten; updated/updated_by audit columns are set.
// Generated keys are not written back.
func (r *Repository[T, ID]) SaveAllUpsert(ctx context.Context, es []*T, conflictProps ...string) error {
	if r.d.Name() != "mysql" && len(conflictProps) == 0 {
		return fmt.Errorf("jpa: SaveAllUpsert needs conflict columns on %s", r.d.Name())
	}
	var conflict []string
	isConflict := map[string]bool{}
	for _, p := range conflictProps {
		c, ok := r.meta.ResolveProperty(p)
		if !ok {
			return fmt.Errorf("jpa: unknown property %q on %s", p, r.meta.Table)
		}
		conflict = append(conflict, r.d.Quote(c.Name))
		isConflict[c.Name] = true
	}
	var update []string
	for _, c := range r.meta.Columns {
		if c.Auto || c.PK || c.Created || c.CreatedBy || isConflict[c.Name] {
			continue
		}
		update = append(update, r.d.Quote(c.Name))
	}
	actor := Actor(ctx)
	cols := r.insertCols()
	for start := 0; start < len(es); start += batchSize {
		chunk := es[start:min(start+batchSize, len(es))]
		q := sqlgen.Query{}
		rows := make([]string, len(chunk))
		for i, e := range chunk {
			rows[i] = "(" + strings.Join(r.insertRow(&q, e, actor), ", ") + ")"
		}
		q.SQL = r.d.Upsert(fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", r.d.Quote(r.meta.Table), strings.Join(cols, ", "), strings.Join(rows, ", ")), conflict, update)
		if _, err := r.execute(ctx, "upsert", q); err != nil {
			return err
		}
	}
	return nil
}

// Update writes every non-key, non-created column of e by primary key. With
// an orm:"version" column it also checks and increments the version
// (optimistic locking) and bumps e's version on success.
func (r *Repository[T, ID]) Update(ctx context.Context, e *T) error {
	pk := r.meta.PK
	if pk == nil {
		return fmt.Errorf("%w: %s", ErrNoPrimaryKey, r.meta.Table)
	}
	actor := Actor(ctx)
	v := reflect.ValueOf(e)
	q := sqlgen.Query{}
	var sets []string
	for i := range r.meta.Columns {
		c := &r.meta.Columns[i]
		name := r.d.Quote(c.Name)
		switch {
		case c.PK || c.Created || c.CreatedBy:
			continue
		case c.Version:
			sets = append(sets, name+" = "+name+" + 1")
		case c.Updated:
			sets = append(sets, name+" = "+r.d.Now())
		case c.UpdatedBy && actor != nil:
			q.Args = append(q.Args, actor)
			sets = append(sets, name+" = "+r.d.Placeholder(len(q.Args)))
		default:
			q.Args = append(q.Args, c.Value(v).Interface())
			sets = append(sets, name+" = "+r.d.Placeholder(len(q.Args)))
		}
	}
	q.Args = append(q.Args, pk.Value(v).Interface())
	q.SQL = fmt.Sprintf("UPDATE %s SET %s WHERE %s = %s", r.d.Quote(r.meta.Table), strings.Join(sets, ", "), r.d.Quote(pk.Name), r.d.Placeholder(len(q.Args)))
	if ver := r.meta.Version; ver != nil {
		q.Args = append(q.Args, ver.Value(v).Interface())
		q.SQL += " AND " + r.d.Quote(ver.Name) + " = " + r.d.Placeholder(len(q.Args))
	}
	n, err := r.execute(ctx, "update", q)
	if err != nil {
		return err
	}
	if ver := r.meta.Version; ver != nil {
		if n == 0 {
			return ErrOptimisticLock
		}
		f := ver.Value(v)
		if f.CanInt() {
			f.SetInt(f.Int() + 1)
		} else if f.CanUint() {
			f.SetUint(f.Uint() + 1)
		}
	}
	return nil
}

// ---- batch update (CASE WHEN) -------------------------------------------

// BatchRow is one row of a BatchUpdate: Key identifies the row (one or more
// columns, e.g. product_code or product_code+warehouse_id); Set holds the
// columns to change. Columns absent from a row's Set are left unchanged.
type BatchRow struct {
	Key map[string]any
	Set map[string]any
}

// BatchUpdate updates many rows with per-row values in one statement per
// chunk of 500, rendering
//
//	col = CASE WHEN k1 = ? AND k2 = ? THEN ? ... ELSE col END
//
// for every column set by at least one row, plus updated/updated_by audit
// columns, WHERE (k1 = ? AND k2 = ?) OR .... Property names resolve like
// method names (ProductCode, product_code and WarehouseId all work).
// Returns rows affected.
func (r *Repository[T, ID]) BatchUpdate(ctx context.Context, rows []BatchRow) (int64, error) {
	var total int64
	for start := 0; start < len(rows); start += batchSize {
		n, err := r.batchChunk(ctx, rows[start:min(start+batchSize, len(rows))])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (r *Repository[T, ID]) batchChunk(ctx context.Context, rows []BatchRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	resolve := func(p string) (string, error) {
		c, ok := r.meta.ResolveProperty(p)
		if !ok {
			return "", fmt.Errorf("jpa: unknown property %q on %s", p, r.meta.Table)
		}
		return r.d.Quote(c.Name), nil
	}
	// stable key column order from the first row
	var keyProps []string
	for k := range rows[0].Key {
		keyProps = append(keyProps, k)
	}
	sortStrings(keyProps)
	keyCols := make([]string, len(keyProps))
	for i, k := range keyProps {
		c, err := resolve(k)
		if err != nil {
			return 0, err
		}
		keyCols[i] = c
	}
	// union of set columns, stable order
	seen := map[string]bool{}
	var setProps []string
	for _, row := range rows {
		for k := range row.Set {
			if !seen[k] {
				seen[k] = true
				setProps = append(setProps, k)
			}
		}
	}
	sortStrings(setProps)

	q := sqlgen.Query{}
	bind := func(v any) string {
		q.Args = append(q.Args, v)
		return r.d.Placeholder(len(q.Args))
	}
	keyMatch := func(row BatchRow) (string, error) {
		parts := make([]string, len(keyProps))
		for i, k := range keyProps {
			v, ok := row.Key[k]
			if !ok {
				return "", fmt.Errorf("jpa: batch row missing key %q", k)
			}
			parts[i] = keyCols[i] + " = " + bind(v)
		}
		return strings.Join(parts, " AND "), nil
	}
	var sets []string
	for _, p := range setProps {
		col, err := resolve(p)
		if err != nil {
			return 0, err
		}
		var whens []string
		for _, row := range rows {
			v, ok := row.Set[p]
			if !ok {
				continue
			}
			km, err := keyMatch(row)
			if err != nil {
				return 0, err
			}
			whens = append(whens, "WHEN "+km+" THEN "+bind(v))
		}
		sets = append(sets, fmt.Sprintf("%s = CASE %s ELSE %s END", col, strings.Join(whens, " "), col))
	}
	if len(sets) == 0 {
		return 0, nil
	}
	actor := Actor(ctx)
	for _, c := range r.meta.Columns {
		switch {
		case c.Updated:
			sets = append(sets, r.d.Quote(c.Name)+" = "+r.d.Now())
		case c.UpdatedBy && actor != nil:
			sets = append(sets, r.d.Quote(c.Name)+" = "+bind(actor))
		}
	}
	where := make([]string, len(rows))
	for i, row := range rows {
		km, err := keyMatch(row)
		if err != nil {
			return 0, err
		}
		where[i] = "(" + km + ")"
	}
	q.SQL = fmt.Sprintf("UPDATE %s SET %s WHERE %s", r.d.Quote(r.meta.Table), strings.Join(sets, ", "), strings.Join(where, " OR "))
	if len(q.Args) > r.d.MaxParams() {
		return 0, fmt.Errorf("%w: %d", sqlgen.ErrTooManyParams, len(q.Args))
	}
	return r.execute(ctx, "batchUpdate", q)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Chunk splits items into slices of at most n for statements with large IN
// lists (see sqlgen.ErrTooManyParams).
func Chunk[V any](items []V, n int) [][]V {
	if n <= 0 || len(items) == 0 {
		return nil
	}
	var out [][]V
	for start := 0; start < len(items); start += n {
		out = append(out, items[start:min(start+n, len(items))])
	}
	return out
}

// ---- raw queries (the @Query / @Modifying escape hatch) -----------------

// QueryRows runs a :name-parameterised SELECT and hands the rows to scan
// while hooks (timeouts, logging) are still active. scan returns the row
// count for hooks. Prefer Select/SelectOne/SelectScalar.
func (r *Repository[T, ID]) QueryRows(ctx context.Context, name, query string, params map[string]any, scan func(*sql.Rows) (int64, error)) error {
	var q sqlgen.Query
	if args, ok := params["__positional__"].([]any); ok && len(params) == 1 {
		q = sqlgen.Query{SQL: query, Args: args}
	} else {
		var err error
		if q, err = sqlgen.Named(r.d, query, params); err != nil {
			return err
		}
	}
	exec := r.reader(q)
	_, err := r.run(ctx, name, q, func(ctx context.Context, q sqlgen.Query) (int64, error) {
		rows, err := exec.QueryContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		n, err := scan(rows)
		if err != nil {
			return n, err
		}
		return n, rows.Err()
	})
	return err
}

// ExecArgs runs a "?"-placeholder statement; slice args expand like sqlx.In.
// Use it for hand-built SQL (CASE WHEN batches) from *Impl fragments.
func (r *Repository[T, ID]) ExecArgs(ctx context.Context, name, query string, args ...any) (int64, error) {
	q, err := sqlgen.Positional(r.d, query, args)
	if err != nil {
		return 0, err
	}
	return r.execute(ctx, name, q)
}

// Exec runs a :name-parameterised UPDATE/INSERT/DELETE and returns rows affected.
func (r *Repository[T, ID]) Exec(ctx context.Context, name, query string, params map[string]any) (int64, error) {
	q, err := sqlgen.Named(r.d, query, params)
	if err != nil {
		return 0, err
	}
	return r.execute(ctx, name, q)
}

// Raw is implemented by every Repository; lets Select work for any row type.
type Raw interface {
	QueryRows(ctx context.Context, name, query string, params map[string]any, scan func(*sql.Rows) (int64, error)) error
	Dialect() sqlgen.Dialect
}

// Select runs a raw query and scans rows into R (entity or DTO struct).
func Select[R any](ctx context.Context, r Raw, name, query string, params map[string]any) ([]R, error) {
	var out []R
	err := r.QueryRows(ctx, name, query, params, func(rows *sql.Rows) (int64, error) {
		var err error
		out, err = entity.ScanAll[R](rows)
		return int64(len(out)), err
	})
	return out, err
}

// SelectOne returns the first row or ErrNotFound.
func SelectOne[R any](ctx context.Context, r Raw, name, query string, params map[string]any) (*R, error) {
	rows, err := Select[R](ctx, r, name, query, params)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// SelectScalars scans the first column of every row into []V.
func SelectScalars[V any](ctx context.Context, r Raw, name, query string, params map[string]any) ([]V, error) {
	var out []V
	err := r.QueryRows(ctx, name, query, params, func(rows *sql.Rows) (int64, error) {
		for rows.Next() {
			var v V
			if err := rows.Scan(&v); err != nil {
				return 0, err
			}
			out = append(out, v)
		}
		return int64(len(out)), rows.Err()
	})
	return out, err
}

// SelectScalar scans the first column of the first row into a value of type V.
func SelectScalar[V any](ctx context.Context, r Raw, name, query string, params map[string]any) (V, error) {
	var v V
	err := r.QueryRows(ctx, name, query, params, func(rows *sql.Rows) (int64, error) {
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return 0, err
			}
			return 0, ErrNotFound
		}
		return 1, rows.Scan(&v)
	})
	return v, err
}

// ---- transactions -------------------------------------------------------

// Transactional runs fn in a transaction, rolling back on error or panic.
func Transactional(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
			return
		}
		err = tx.Commit()
	}()
	return fn(tx)
}

// CRUD is the interface generated repositories embed (JpaRepository<T, ID>).
// *Repository[T, ID] implements it.
type CRUD[T any, ID any] interface {
	FindByID(ctx context.Context, id ID) (*T, error)
	FindAllByID(ctx context.Context, ids []ID) ([]T, error)
	FindAll(ctx context.Context, sort ...Order) ([]T, error)
	FindPage(ctx context.Context, p Pageable) (Page[T], error)
	Count(ctx context.Context) (int64, error)
	ExistsByID(ctx context.Context, id ID) (bool, error)
	DeleteByID(ctx context.Context, id ID) error
	DeleteAllByID(ctx context.Context, ids []ID) (int64, error)
	Save(ctx context.Context, e *T) error
	SaveAll(ctx context.Context, es []*T) error
	Update(ctx context.Context, e *T) error
	Exec(ctx context.Context, name, query string, params map[string]any) (int64, error)
	ExecArgs(ctx context.Context, name, query string, args ...any) (int64, error)
	QueryRows(ctx context.Context, name, query string, params map[string]any, scan func(*sql.Rows) (int64, error)) error
}

// ---- Specification (Cond) queries -----------------------------------------

// Cond re-exports the sqlgen predicate builder: jpa.Eq, jpa.In, jpa.And ...
type Cond = sqlgen.Cond

var (
	Eq        = sqlgen.Eq
	Ne        = sqlgen.Ne
	Gt        = sqlgen.Gt
	Ge        = sqlgen.Ge
	Lt        = sqlgen.Lt
	Le        = sqlgen.Le
	Like      = sqlgen.Like
	NotLike   = sqlgen.NotLike
	In        = sqlgen.In
	NotIn     = sqlgen.NotIn
	IsNull    = sqlgen.IsNull
	IsNotNull = sqlgen.IsNotNull
	Between   = sqlgen.Between
	And       = sqlgen.And
	Or        = sqlgen.Or
	Not       = sqlgen.Not
	RawCond   = sqlgen.Raw
)

func (r *Repository[T, ID]) condQuery(ctx context.Context, verb parser.Verb, c Cond, opt sqlgen.Options) (sqlgen.Query, error) {
	opt.Cond = r.withFilter(ctx, c)
	opt.IncludeDeleted = opt.IncludeDeleted || r.includeDeleted
	return sqlgen.Build(r.d, r.meta, &parser.Tree{Verb: verb}, nil, opt)
}

// FindWhere returns rows matching c; a trailing Sort/Limit/Pageable applies.
func (r *Repository[T, ID]) FindWhere(ctx context.Context, c Cond, special ...any) ([]T, error) {
	_, opt := r.splitSpecial(special)
	q, err := r.condQuery(ctx, parser.Find, c, opt)
	if err != nil {
		return nil, err
	}
	return r.query(ctx, "findWhere", q)
}

func (r *Repository[T, ID]) CountWhere(ctx context.Context, c Cond) (int64, error) {
	q, err := r.condQuery(ctx, parser.Count, c, sqlgen.Options{})
	if err != nil {
		return 0, err
	}
	var n int64
	return n, r.scalar(ctx, "countWhere", q, &n)
}

func (r *Repository[T, ID]) ExistsWhere(ctx context.Context, c Cond) (bool, error) {
	q, err := r.condQuery(ctx, parser.Exists, c, sqlgen.Options{})
	if err != nil {
		return false, err
	}
	var ok bool
	return ok, r.scalar(ctx, "existsWhere", q, &ok)
}

func (r *Repository[T, ID]) DeleteWhere(ctx context.Context, c Cond) (int64, error) {
	q, err := r.condQuery(ctx, parser.Delete, c, sqlgen.Options{})
	if err != nil {
		return 0, err
	}
	return r.execute(ctx, "deleteWhere", q)
}

func (r *Repository[T, ID]) PageWhere(ctx context.Context, c Cond, p Pageable) (Page[T], error) {
	if p.Size <= 0 {
		return Page[T]{}, fmt.Errorf("jpa: page size must be > 0")
	}
	content, err := r.FindWhere(ctx, c, p)
	if err != nil {
		return Page[T]{}, err
	}
	total, err := r.CountWhere(ctx, c)
	if err != nil {
		return Page[T]{}, err
	}
	return Page[T]{Content: content, Page: p.Page, Size: p.Size, Total: total, TotalPages: int((total + int64(p.Size) - 1) / int64(p.Size))}, nil
}

// ---- keyset scrolling (Window) ---------------------------------------------

// ScrollPosition is a keyset cursor. Keyset(size, sort...) is the start;
// Window.Next continues. Sort must end in a unique key (e.g. Id).
type ScrollPosition struct {
	Size int
	Sort Sort
	Keys []any // values of Sort properties on the last row; nil = start
}

func Keyset(size int, sort ...Order) ScrollPosition {
	return ScrollPosition{Size: size, Sort: sort}
}

// Window is one keyset page.
type Window[T any] struct {
	Content []T            `json:"content"`
	HasNext bool           `json:"has_next"`
	Next    ScrollPosition `json:"-"`
}

// WindowBy runs a derived find as a keyset scroll. The method's own OrderBy is
// used when pos.Sort is empty.
func (r *Repository[T, ID]) WindowBy(ctx context.Context, method string, pos ScrollPosition, args ...any) (Window[T], error) {
	if pos.Size <= 0 {
		return Window[T]{}, fmt.Errorf("jpa: window size must be > 0")
	}
	t, err := r.tree(method)
	if err != nil {
		return Window[T]{}, err
	}
	return r.window(ctx, method, t, nil, pos, args)
}

// WindowWhere is the Cond variant of WindowBy.
func (r *Repository[T, ID]) WindowWhere(ctx context.Context, c Cond, pos ScrollPosition) (Window[T], error) {
	if pos.Size <= 0 {
		return Window[T]{}, fmt.Errorf("jpa: window size must be > 0")
	}
	return r.window(ctx, "windowWhere", &parser.Tree{Verb: parser.Find}, c, pos, nil)
}

func (r *Repository[T, ID]) window(ctx context.Context, name string, t *parser.Tree, c Cond, pos ScrollPosition, args []any) (Window[T], error) {
	sort := pos.Sort
	if len(sort) == 0 {
		sort = t.OrderBy
	}
	if len(sort) == 0 {
		return Window[T]{}, fmt.Errorf("jpa: keyset scrolling needs a sort order")
	}
	opt := sqlgen.Options{OrderBy: sort, Limit: pos.Size + 1, IncludeDeleted: r.includeDeleted, Cond: r.withFilter(ctx, c)}
	if pos.Keys != nil {
		opt.Keyset = &sqlgen.Keyset{Orders: sort, Values: pos.Keys}
	}
	q, err := sqlgen.Build(r.d, r.meta, t, args, opt)
	if err != nil {
		return Window[T]{}, err
	}
	rows, err := r.query(ctx, name, q)
	if err != nil {
		return Window[T]{}, err
	}
	w := Window[T]{Content: rows, Next: ScrollPosition{Size: pos.Size, Sort: sort}}
	if len(rows) > pos.Size {
		w.Content, w.HasNext = rows[:pos.Size], true
	}
	if n := len(w.Content); n > 0 {
		last := reflect.ValueOf(&w.Content[n-1])
		for _, o := range sort {
			col, _ := r.meta.ResolveProperty(o.Property)
			w.Next.Keys = append(w.Next.Keys, col.Value(last).Interface())
		}
	}
	return w, nil
}

// ---- declared queries with paging ------------------------------------------

func pagedSQL[R any](d sqlgen.Dialect, query string, sort Sort, limit, offset int) (string, error) {
	if len(sort) > 0 {
		m, err := entity.Of[R]()
		if err != nil {
			return "", err
		}
		parts := make([]string, len(sort))
		for i, o := range sort {
			c, ok := m.ResolveProperty(o.Property)
			if !ok {
				return "", fmt.Errorf("jpa: unknown sort property %q", o.Property)
			}
			parts[i] = c.Name + " ASC"
			if o.Desc {
				parts[i] = c.Name + " DESC"
			}
		}
		query += " ORDER BY " + strings.Join(parts, ", ")
	}
	query += fmt.Sprintf(" LIMIT %d", limit)
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}
	return query, nil
}

// SelectPage runs a raw query with Pageable and countQuery (jpa:count).
func SelectPage[R any](ctx context.Context, r Raw, name, query, countQuery string, p Pageable, params map[string]any) (Page[R], error) {
	if p.Size <= 0 {
		return Page[R]{}, fmt.Errorf("jpa: page size must be > 0")
	}
	q, err := pagedSQL[R](r.Dialect(), query, p.Sort, p.Size, p.Page*p.Size)
	if err != nil {
		return Page[R]{}, err
	}
	content, err := Select[R](ctx, r, name, q, params)
	if err != nil {
		return Page[R]{}, err
	}
	total, err := SelectScalar[int64](ctx, r, name+".count", countQuery, params)
	if err != nil {
		return Page[R]{}, err
	}
	return Page[R]{Content: content, Page: p.Page, Size: p.Size, Total: total, TotalPages: int((total + int64(p.Size) - 1) / int64(p.Size))}, nil
}

// SelectWindow keyset-scrolls a raw query by wrapping it as a subquery:
// SELECT * FROM (<query>) q WHERE <keyset> ORDER BY ... LIMIT size+1.
// pos.Sort is required and must end in a unique column of the result.
func SelectWindow[R any](ctx context.Context, r Raw, name, query string, pos ScrollPosition, params map[string]any) (Window[R], error) {
	if pos.Size <= 0 || len(pos.Sort) == 0 {
		return Window[R]{}, fmt.Errorf("jpa: SelectWindow needs a size and a sort")
	}
	m, err := entity.Of[R]()
	if err != nil {
		return Window[R]{}, err
	}
	d := r.Dialect()
	inner, err := sqlgen.Named(d, query, params) // user placeholders come first
	if err != nil {
		return Window[R]{}, err
	}
	args := append([]any{}, inner.Args...)
	ph := func(v any) string { args = append(args, v); return d.Placeholder(len(args)) }
	cols := make([]string, len(pos.Sort))
	order := make([]string, len(pos.Sort))
	for i, o := range pos.Sort {
		c, ok := m.ResolveProperty(o.Property)
		if !ok {
			return Window[R]{}, fmt.Errorf("jpa: unknown sort property %q", o.Property)
		}
		cols[i] = d.Quote(c.Name)
		order[i] = cols[i] + " ASC"
		if o.Desc {
			order[i] = cols[i] + " DESC"
		}
	}
	sqlText := "SELECT * FROM (" + inner.SQL + ") q"
	if pos.Keys != nil {
		if len(pos.Keys) != len(pos.Sort) {
			return Window[R]{}, fmt.Errorf("jpa: keyset has %d keys for %d sort columns", len(pos.Keys), len(pos.Sort))
		}
		var ors []string
		for i := range pos.Sort {
			var ands []string
			for j := 0; j < i; j++ {
				ands = append(ands, cols[j]+" = "+ph(pos.Keys[j]))
			}
			op := " > "
			if pos.Sort[i].Desc {
				op = " < "
			}
			ands = append(ands, cols[i]+op+ph(pos.Keys[i]))
			ors = append(ors, "("+strings.Join(ands, " AND ")+")")
		}
		sqlText += " WHERE " + strings.Join(ors, " OR ")
	}
	sqlText += fmt.Sprintf(" ORDER BY %s LIMIT %d", strings.Join(order, ", "), pos.Size+1)
	var out []R
	err = r.QueryRows(ctx, name, sqlText, positionalParams(args), func(rows *sql.Rows) (int64, error) {
		var err error
		out, err = entity.ScanAll[R](rows)
		return int64(len(out)), err
	})
	if err != nil {
		return Window[R]{}, err
	}
	w := Window[R]{Content: out, Next: ScrollPosition{Size: pos.Size, Sort: pos.Sort}}
	if len(out) > pos.Size {
		w.Content, w.HasNext = out[:pos.Size], true
	}
	if n := len(w.Content); n > 0 {
		last := reflect.ValueOf(&w.Content[n-1])
		for _, o := range pos.Sort {
			c, _ := m.ResolveProperty(o.Property)
			w.Next.Keys = append(w.Next.Keys, c.Value(last).Interface())
		}
	}
	return w, nil
}

// positionalParams wraps already-bound args so QueryRows (which expects
// :name params) can run a fully rendered statement.
func positionalParams(args []any) map[string]any { return map[string]any{"__positional__": args} }

// SelectSlice runs a raw query with Pageable, fetching size+1 (no count).
func SelectSlice[R any](ctx context.Context, r Raw, name, query string, p Pageable, params map[string]any) (Slice[R], error) {
	if p.Size <= 0 {
		return Slice[R]{}, fmt.Errorf("jpa: page size must be > 0")
	}
	q, err := pagedSQL[R](r.Dialect(), query, p.Sort, p.Size+1, p.Page*p.Size)
	if err != nil {
		return Slice[R]{}, err
	}
	rows, err := Select[R](ctx, r, name, q, params)
	if err != nil {
		return Slice[R]{}, err
	}
	s := Slice[R]{Content: rows, Page: p.Page, Size: p.Size}
	if len(rows) > p.Size {
		s.Content, s.HasNext = rows[:p.Size], true
	}
	return s, nil
}

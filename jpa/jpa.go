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
	hooks []Hook
	name  string
}

func WithHook(h Hook) Option   { return func(c *config) { c.hooks = append(c.hooks, h) } }
func WithName(n string) Option { return func(c *config) { c.name = n } }

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
	return &c
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

func (r *Repository[T, ID]) run(ctx context.Context, name string, q sqlgen.Query, fn func(ctx context.Context) (int64, error)) (int64, error) {
	query := Query{Name: r.cfg.name + "." + name, SQL: q.SQL, Args: q.Args}
	var dones []func(int64, error)
	for _, h := range r.cfg.hooks {
		var done func(int64, error)
		ctx, done = h.Before(ctx, query)
		if done != nil {
			dones = append(dones, done)
		}
	}
	n, err := fn(ctx)
	for i := len(dones) - 1; i >= 0; i-- {
		dones[i](n, err)
	}
	return n, err
}

func (r *Repository[T, ID]) query(ctx context.Context, name string, q sqlgen.Query) ([]T, error) {
	var out []T
	_, err := r.run(ctx, name, q, func(ctx context.Context) (int64, error) {
		rows, err := r.exec.QueryContext(ctx, q.SQL, q.Args...)
		if err != nil {
			return 0, err
		}
		out, err = entity.ScanAll[T](rows)
		return int64(len(out)), err
	})
	return out, err
}

func (r *Repository[T, ID]) scalar(ctx context.Context, name string, q sqlgen.Query, dest any) error {
	_, err := r.run(ctx, name, q, func(ctx context.Context) (int64, error) {
		rows, err := r.exec.QueryContext(ctx, q.SQL, q.Args...)
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
	return r.run(ctx, name, q, func(ctx context.Context) (int64, error) {
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
		default:
			return args, opt
		}
		args = args[:len(args)-1]
	}
	return args, opt
}

func (r *Repository[T, ID]) build(method string, args []any) (*parser.Tree, sqlgen.Query, sqlgen.Options, error) {
	t, err := r.tree(method)
	if err != nil {
		return nil, sqlgen.Query{}, sqlgen.Options{}, err
	}
	args, opt := r.splitSpecial(args)
	q, err := sqlgen.Build(r.d, r.meta, t, args, opt)
	return t, q, opt, err
}

// FindBy runs a derived find method, e.g. FindBy(ctx, "FindByCustomerIdAndStatusIdIn", 7, []int{1,2}).
func (r *Repository[T, ID]) FindBy(ctx context.Context, method string, args ...any) ([]T, error) {
	_, q, _, err := r.build(method, args)
	if err != nil {
		return nil, err
	}
	return r.query(ctx, method, q)
}

// FindOneBy returns the first row or ErrNotFound.
func (r *Repository[T, ID]) FindOneBy(ctx context.Context, method string, args ...any) (*T, error) {
	args, _ = r.splitSpecial(args)
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
	q, err := sqlgen.Build(r.d, r.meta, &ct, args, sqlgen.Options{IncludeDeleted: r.includeDeleted})
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
	opt := sqlgen.Options{OrderBy: p.Sort, Limit: p.Size + 1, Offset: p.Page * p.Size, IncludeDeleted: r.includeDeleted}
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
	_, q, _, err := r.build(method, args)
	if err != nil {
		return 0, err
	}
	var n int64
	return n, r.scalar(ctx, method, q, &n)
}

func (r *Repository[T, ID]) ExistsBy(ctx context.Context, method string, args ...any) (bool, error) {
	_, q, _, err := r.build(method, args)
	if err != nil {
		return false, err
	}
	var ok bool
	return ok, r.scalar(ctx, method, q, &ok)
}

// DeleteBy runs a derived delete (soft if the entity declares SoftDelete).
func (r *Repository[T, ID]) DeleteBy(ctx context.Context, method string, args ...any) (int64, error) {
	_, q, _, err := r.build(method, args)
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
			cols = append(cols, c.Name)
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
		q.SQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", r.meta.Table, strings.Join(cols, ", "), strings.Join(rows, ", "))
		if pk != nil && pk.Auto && r.d.Returning() {
			q.SQL += " RETURNING " + pk.Name
			_, err := r.run(ctx, "save", q, func(ctx context.Context) (int64, error) {
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
		_, err := r.run(ctx, "save", q, func(ctx context.Context) (int64, error) {
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

// Update writes every non-key, non-created column of e by primary key.
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
		switch {
		case c.PK || c.Created || c.CreatedBy:
			continue
		case c.Updated:
			sets = append(sets, c.Name+" = "+r.d.Now())
		case c.UpdatedBy && actor != nil:
			q.Args = append(q.Args, actor)
			sets = append(sets, c.Name+" = "+r.d.Placeholder(len(q.Args)))
		default:
			q.Args = append(q.Args, c.Value(v).Interface())
			sets = append(sets, c.Name+" = "+r.d.Placeholder(len(q.Args)))
		}
	}
	q.Args = append(q.Args, pk.Value(v).Interface())
	q.SQL = fmt.Sprintf("UPDATE %s SET %s WHERE %s = %s", r.meta.Table, strings.Join(sets, ", "), pk.Name, r.d.Placeholder(len(q.Args)))
	_, err := r.execute(ctx, "update", q)
	return err
}

// ---- raw queries (the @Query / @Modifying escape hatch) -----------------

// QueryRows runs a :name-parameterised SELECT and hands the rows to scan
// while hooks (timeouts, logging) are still active. scan returns the row
// count for hooks. Prefer Select/SelectOne/SelectScalar.
func (r *Repository[T, ID]) QueryRows(ctx context.Context, name, query string, params map[string]any, scan func(*sql.Rows) (int64, error)) error {
	q, err := sqlgen.Named(r.d, query, params)
	if err != nil {
		return err
	}
	_, err = r.run(ctx, name, q, func(ctx context.Context) (int64, error) {
		rows, err := r.exec.QueryContext(ctx, q.SQL, q.Args...)
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

func (r *Repository[T, ID]) condQuery(verb parser.Verb, c Cond, opt sqlgen.Options) (sqlgen.Query, error) {
	opt.Cond = c
	opt.IncludeDeleted = opt.IncludeDeleted || r.includeDeleted
	return sqlgen.Build(r.d, r.meta, &parser.Tree{Verb: verb}, nil, opt)
}

// FindWhere returns rows matching c; a trailing Sort/Limit/Pageable applies.
func (r *Repository[T, ID]) FindWhere(ctx context.Context, c Cond, special ...any) ([]T, error) {
	_, opt := r.splitSpecial(special)
	q, err := r.condQuery(parser.Find, c, opt)
	if err != nil {
		return nil, err
	}
	return r.query(ctx, "findWhere", q)
}

func (r *Repository[T, ID]) CountWhere(ctx context.Context, c Cond) (int64, error) {
	q, err := r.condQuery(parser.Count, c, sqlgen.Options{})
	if err != nil {
		return 0, err
	}
	var n int64
	return n, r.scalar(ctx, "countWhere", q, &n)
}

func (r *Repository[T, ID]) ExistsWhere(ctx context.Context, c Cond) (bool, error) {
	q, err := r.condQuery(parser.Exists, c, sqlgen.Options{})
	if err != nil {
		return false, err
	}
	var ok bool
	return ok, r.scalar(ctx, "existsWhere", q, &ok)
}

func (r *Repository[T, ID]) DeleteWhere(ctx context.Context, c Cond) (int64, error) {
	q, err := r.condQuery(parser.Delete, c, sqlgen.Options{})
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
	opt := sqlgen.Options{OrderBy: sort, Limit: pos.Size + 1, IncludeDeleted: r.includeDeleted, Cond: c}
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

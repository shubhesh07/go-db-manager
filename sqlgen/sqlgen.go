// Package sqlgen turns a parser.Tree plus entity metadata into dialect-specific
// SQL. It is the JpaQueryCreator + Hibernate dialect of Spring Data.
package sqlgen

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/shubhesh07/gojpa/entity"
	"github.com/shubhesh07/gojpa/parser"
)

type Dialect interface {
	Name() string
	Placeholder(n int) string // 1-based
	Now() string
	Returning() bool // INSERT ... RETURNING pk supported
	// Quote wraps an identifier that needs quoting (reserved word or unusual
	// characters). Plain snake_case identifiers are emitted bare so generated
	// SQL stays readable and stable.
	Quote(ident string) string
	// MaxParams is the driver/server bind-parameter ceiling; Build returns
	// ErrTooManyParams beyond it instead of failing inside the driver.
	MaxParams() int
}

// ErrTooManyParams is returned when a statement would exceed Dialect.MaxParams.
var ErrTooManyParams = errors.New("sqlgen: too many bind parameters")

type mysql struct{}

func (mysql) Name() string           { return "mysql" }
func (mysql) Placeholder(int) string { return "?" }
func (mysql) Now() string            { return "NOW()" }
func (mysql) Returning() bool        { return false }
func (mysql) Quote(id string) string { return quoteWith(id, '`') }
func (mysql) MaxParams() int         { return 65535 }

type postgres struct{}

func (postgres) Name() string             { return "postgres" }
func (postgres) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }
func (postgres) Now() string              { return "NOW()" }
func (postgres) Returning() bool          { return true }
func (postgres) Quote(id string) string   { return quoteWith(id, '"') }
func (postgres) MaxParams() int           { return 65535 }

type sqlite struct{}

func (sqlite) Name() string           { return "sqlite" }
func (sqlite) Placeholder(int) string { return "?" }
func (sqlite) Now() string            { return "CURRENT_TIMESTAMP" }
func (sqlite) Returning() bool        { return true } // SQLite >= 3.35
func (sqlite) Quote(id string) string { return quoteWith(id, '"') }
func (sqlite) MaxParams() int         { return 32766 }

var (
	MySQL    Dialect = mysql{}
	Postgres Dialect = postgres{}
	SQLite   Dialect = sqlite{}
)

// reserved lists identifiers that must be quoted on at least one supported
// dialect. Deliberately small: common column names that are keywords.
var reserved = map[string]bool{
	"add": true, "all": true, "alter": true, "and": true, "as": true, "asc": true, "between": true, "by": true,
	"case": true, "check": true, "column": true, "constraint": true, "create": true, "cross": true,
	"current_date": true, "current_time": true, "current_timestamp": true, "default": true, "delete": true,
	"desc": true, "distinct": true, "drop": true, "else": true, "end": true, "exists": true, "for": true,
	"foreign": true, "from": true, "group": true, "grouping": true, "having": true, "in": true, "index": true,
	"inner": true, "insert": true, "into": true, "is": true, "join": true, "key": true, "keys": true,
	"lateral": true, "left": true, "like": true, "limit": true, "lock": true, "natural": true, "not": true,
	"null": true, "of": true, "offset": true, "on": true, "or": true, "order": true, "outer": true,
	"primary": true, "rank": true, "references": true, "right": true, "row": true, "rows": true,
	"select": true, "set": true, "system": true, "table": true, "then": true, "to": true, "union": true,
	"unique": true, "update": true, "user": true, "using": true, "values": true, "when": true, "where": true,
	"window": true, "with": true,
}

func needsQuote(id string) bool {
	if id == "" || reserved[strings.ToLower(id)] {
		return true
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || (c >= '0' && c <= '9' && i > 0)
		if !ok {
			return true
		}
	}
	return false
}

func quoteWith(id string, q byte) string {
	if !needsQuote(id) {
		return id
	}
	return string(q) + strings.ReplaceAll(id, string(q), string(q)+string(q)) + string(q)
}

// EscapeLike escapes %, _ and \ in a LIKE operand so user text matches
// literally. MySQL, Postgres and SQLite default the escape character to \.
func EscapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	return r.Replace(s)
}

// Options are the call-time special parameters (Pageable, Sort, Limit).
type Options struct {
	OrderBy        []parser.Order
	Limit, Offset  int
	IncludeDeleted bool
	Cond           Cond    // extra condition ANDed with the tree (Specification)
	Keyset         *Keyset // keyset-scroll boundary, requires OrderBy
	Lock           Lock    // row lock appended to SELECTs
}

// Lock is a row-level lock clause for SELECT (Spring @Lock).
type Lock string

const (
	NoLock    Lock = ""
	ForUpdate Lock = "FOR UPDATE"
	ForShare  Lock = "FOR SHARE" // MySQL 8+/Postgres; dropped on SQLite
)

// Keyset is the "after this row" boundary for keyset scrolling: Values align
// with Orders; the rendered predicate is the expanded row-value comparison
// so mixed ASC/DESC works on every dialect.
type Keyset struct {
	Orders []parser.Order
	Values []any
}

// Query is a rendered statement.
type Query struct {
	SQL  string
	Args []any
}

type builder struct {
	d    Dialect
	args []any
	sb   strings.Builder
}

func (b *builder) bind(v any) string {
	b.args = append(b.args, v)
	return b.d.Placeholder(len(b.args))
}

func (b *builder) q(ident string) string { return b.d.Quote(ident) }

func (b *builder) finish() (Query, error) {
	if len(b.args) > b.d.MaxParams() {
		return Query{}, fmt.Errorf("%w: %d > %d (chunk the input)", ErrTooManyParams, len(b.args), b.d.MaxParams())
	}
	return Query{SQL: b.sb.String(), Args: b.args}, nil
}

// Build renders tree for meta. args are the method arguments in order; a
// slice argument for In/NotIn is expanded.
func Build(d Dialect, m *entity.Meta, t *parser.Tree, args []any, opt Options) (Query, error) {
	if len(args) != t.NumArgs {
		return Query{}, fmt.Errorf("sqlgen: expected %d args, got %d", t.NumArgs, len(args))
	}
	b := &builder{d: d}
	where, err := b.where(m, t, args, opt)
	if err != nil {
		return Query{}, err
	}
	table := b.q(m.Table)
	switch t.Verb {
	case parser.Count:
		fmt.Fprintf(&b.sb, "SELECT COUNT(*) FROM %s%s", table, where)
	case parser.Exists:
		fmt.Fprintf(&b.sb, "SELECT EXISTS(SELECT 1 FROM %s%s)", table, where)
	case parser.Delete:
		if m.SoftDeleteSet != "" && !opt.IncludeDeleted {
			set := m.SoftDeleteSet
			for _, c := range m.Columns {
				if c.Updated {
					set += ", " + b.q(c.Name) + " = " + d.Now()
				}
			}
			fmt.Fprintf(&b.sb, "UPDATE %s SET %s%s", table, set, where)
		} else {
			fmt.Fprintf(&b.sb, "DELETE FROM %s%s", table, where)
		}
	default:
		distinct := ""
		if t.Distinct {
			distinct = "DISTINCT "
		}
		cols := make([]string, len(m.Columns))
		for i, c := range m.Columns {
			cols[i] = b.q(c.Name)
		}
		fmt.Fprintf(&b.sb, "SELECT %s%s FROM %s%s", distinct, strings.Join(cols, ", "), table, where)
		order := t.OrderBy
		if len(opt.OrderBy) > 0 {
			order = opt.OrderBy
		}
		if len(order) > 0 {
			parts := make([]string, len(order))
			for i, o := range order {
				c, ok := m.ResolveProperty(o.Property)
				if !ok {
					return Query{}, fmt.Errorf("sqlgen: unknown sort property %q", o.Property)
				}
				parts[i] = b.q(c.Name) + " ASC"
				if o.Desc {
					parts[i] = b.q(c.Name) + " DESC"
				}
			}
			b.sb.WriteString(" ORDER BY " + strings.Join(parts, ", "))
		}
		limit := t.Limit
		if opt.Limit > 0 {
			limit = opt.Limit
		}
		if limit > 0 {
			fmt.Fprintf(&b.sb, " LIMIT %d", limit)
		}
		if opt.Offset > 0 {
			fmt.Fprintf(&b.sb, " OFFSET %d", opt.Offset)
		}
		if opt.Lock != NoLock && d.Name() != "sqlite" {
			b.sb.WriteString(" " + string(opt.Lock))
		}
	}
	return b.finish()
}

func (b *builder) where(m *entity.Meta, t *parser.Tree, args []any, opt Options) (string, error) {
	var ors []string
	ai := 0
	for _, ands := range t.Or {
		var conds []string
		for _, p := range ands {
			c, ok := m.ResolveProperty(p.Property)
			if !ok {
				return "", fmt.Errorf("sqlgen: unknown property %q on %s", p.Property, m.Table)
			}
			cond, err := b.cond(b.q(c.Name), p, args[ai:ai+p.NumArgs])
			if err != nil {
				return "", err
			}
			ai += p.NumArgs
			conds = append(conds, cond)
		}
		ors = append(ors, strings.Join(conds, " AND "))
	}
	var w []string
	switch len(ors) {
	case 0:
	case 1:
		w = append(w, ors[0])
	default:
		for i := range ors {
			ors[i] = "(" + ors[i] + ")"
		}
		w = append(w, strings.Join(ors, " OR "))
	}
	if opt.Cond != nil {
		c, err := opt.Cond.render(b, m)
		if err != nil {
			return "", err
		}
		if c != "" {
			w = append(w, c)
		}
	}
	if opt.Keyset != nil && len(opt.Keyset.Orders) > 0 {
		k, err := b.keyset(m, opt.Keyset)
		if err != nil {
			return "", err
		}
		w = append(w, k)
	}
	if m.SoftDeleteFilter != "" && !opt.IncludeDeleted {
		w = append(w, m.SoftDeleteFilter)
	}
	if len(w) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(w, " AND "), nil
}

func (b *builder) cond(col string, p parser.Part, args []any) (string, error) {
	lhs := col
	ph := b.bind
	if p.IgnoreCase {
		lhs = "UPPER(" + col + ")"
		ph = func(v any) string { return "UPPER(" + b.bind(v) + ")" }
	}
	switch p.Op {
	case "Equals":
		if args[0] == nil {
			return col + " IS NULL", nil
		}
		return lhs + " = " + ph(args[0]), nil
	case "Not":
		if args[0] == nil {
			return col + " IS NOT NULL", nil
		}
		return lhs + " <> " + ph(args[0]), nil
	case "IsNull":
		return col + " IS NULL", nil
	case "IsNotNull":
		return col + " IS NOT NULL", nil
	case "True":
		return col + " = TRUE", nil
	case "False":
		return col + " = FALSE", nil
	case "In", "NotIn":
		vals := Expand(args[0])
		if len(vals) == 0 {
			if p.Op == "In" {
				return "1=0", nil
			}
			return "1=1", nil
		}
		phs := make([]string, len(vals))
		for i, v := range vals {
			phs[i] = b.bind(v)
		}
		op := " IN ("
		if p.Op == "NotIn" {
			op = " NOT IN ("
		}
		return col + op + strings.Join(phs, ", ") + ")", nil
	case "Like":
		return lhs + " LIKE " + ph(args[0]), nil
	case "NotLike":
		return lhs + " NOT LIKE " + ph(args[0]), nil
	case "StartingWith":
		return lhs + " LIKE " + ph(EscapeLike(fmt.Sprint(args[0]))+"%"), nil
	case "EndingWith":
		return lhs + " LIKE " + ph("%"+EscapeLike(fmt.Sprint(args[0]))), nil
	case "Containing":
		return lhs + " LIKE " + ph("%"+EscapeLike(fmt.Sprint(args[0]))+"%"), nil
	case "NotContaining":
		return lhs + " NOT LIKE " + ph("%"+EscapeLike(fmt.Sprint(args[0]))+"%"), nil
	case "LessThan":
		return col + " < " + b.bind(args[0]), nil
	case "LessThanEqual":
		return col + " <= " + b.bind(args[0]), nil
	case "GreaterThan":
		return col + " > " + b.bind(args[0]), nil
	case "GreaterThanEqual":
		return col + " >= " + b.bind(args[0]), nil
	case "Between":
		return col + " BETWEEN " + b.bind(args[0]) + " AND " + b.bind(args[1]), nil
	}
	return "", fmt.Errorf("sqlgen: unsupported operator %q", p.Op)
}

func isList(v any) bool {
	rv := reflect.ValueOf(v)
	return v != nil && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Type().Elem().Kind() != reflect.Uint8
}

// Expand flattens a slice/array argument into []any; non-slices become a
// single-element list. []byte is a scalar.
func Expand(v any) []any {
	if !isList(v) {
		return []any{v}
	}
	rv := reflect.ValueOf(v)
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

// Named rewrites a query using :name parameters into dialect placeholders.
// Slice values are expanded (sqlx.In semantics); "::" is left alone (casts).
func Named(d Dialect, sql string, params map[string]any) (Query, error) {
	b := &builder{d: d}
	out, err := b.named(sql, params)
	if err != nil {
		return Query{}, err
	}
	b.sb.WriteString(out)
	return b.finish()
}

func (b *builder) named(sql string, params map[string]any) (string, error) {
	var out strings.Builder
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch != ':' {
			out.WriteByte(ch)
			continue
		}
		if i+1 < len(sql) && sql[i+1] == ':' { // cast
			out.WriteString("::")
			i++
			continue
		}
		j := i + 1
		for j < len(sql) && (sql[j] == '_' || sql[j] >= 'a' && sql[j] <= 'z' || sql[j] >= 'A' && sql[j] <= 'Z' || sql[j] >= '0' && sql[j] <= '9') {
			j++
		}
		name := sql[i+1 : j]
		if name == "" {
			out.WriteByte(ch)
			continue
		}
		v, ok := params[name]
		if !ok {
			return "", fmt.Errorf("sqlgen: missing parameter :%s", name)
		}
		if isList(v) {
			vals := Expand(v)
			if len(vals) == 0 {
				return "", fmt.Errorf("sqlgen: empty slice for :%s", name)
			}
			phs := make([]string, len(vals))
			for k, x := range vals {
				phs[k] = b.bind(x)
			}
			out.WriteString(strings.Join(phs, ", "))
		} else {
			out.WriteString(b.bind(v))
		}
		i = j - 1
	}
	return out.String(), nil
}

func (b *builder) keyset(m *entity.Meta, k *Keyset) (string, error) {
	if len(k.Values) != len(k.Orders) {
		return "", fmt.Errorf("sqlgen: keyset has %d values for %d sort keys", len(k.Values), len(k.Orders))
	}
	cols := make([]string, len(k.Orders))
	for i, o := range k.Orders {
		c, ok := m.ResolveProperty(o.Property)
		if !ok {
			return "", fmt.Errorf("sqlgen: unknown keyset property %q", o.Property)
		}
		cols[i] = b.q(c.Name)
	}
	var ors []string
	for i := range k.Orders {
		var ands []string
		for j := 0; j < i; j++ {
			ands = append(ands, cols[j]+" = "+b.bind(k.Values[j]))
		}
		op := " > "
		if k.Orders[i].Desc {
			op = " < "
		}
		ands = append(ands, cols[i]+op+b.bind(k.Values[i]))
		ors = append(ors, "("+strings.Join(ands, " AND ")+")")
	}
	return "(" + strings.Join(ors, " OR ") + ")", nil
}

// ---- Cond: the Specification builder --------------------------------------

// Cond is a composable predicate resolved against the entity's columns.
type Cond interface {
	render(b *builder, m *entity.Meta) (string, error)
}

type cmp struct {
	prop, op string
	val      any
}
type inCond struct {
	prop string
	vals any
	not  bool
}
type nullCond struct {
	prop string
	not  bool
}
type between struct {
	prop   string
	lo, hi any
}
type logic struct {
	op    string
	conds []Cond
}
type notCond struct{ c Cond }
type rawCond struct {
	sql    string
	params map[string]any
}

func Eq(prop string, v any) Cond      { return cmp{prop, "=", v} }
func Ne(prop string, v any) Cond      { return cmp{prop, "<>", v} }
func Gt(prop string, v any) Cond      { return cmp{prop, ">", v} }
func Ge(prop string, v any) Cond      { return cmp{prop, ">=", v} }
func Lt(prop string, v any) Cond      { return cmp{prop, "<", v} }
func Le(prop string, v any) Cond      { return cmp{prop, "<=", v} }
func Like(prop string, v any) Cond    { return cmp{prop, "LIKE", v} }
func NotLike(prop string, v any) Cond { return cmp{prop, "NOT LIKE", v} }
func In(prop string, vals any) Cond   { return inCond{prop, vals, false} }
func NotIn(prop string, vals any) Cond {
	return inCond{prop, vals, true}
}
func IsNull(prop string) Cond              { return nullCond{prop, false} }
func IsNotNull(prop string) Cond           { return nullCond{prop, true} }
func Between(prop string, lo, hi any) Cond { return between{prop, lo, hi} }
func And(conds ...Cond) Cond               { return logic{"AND", conds} }
func Or(conds ...Cond) Cond                { return logic{"OR", conds} }
func Not(c Cond) Cond                      { return notCond{c} }

// Raw embeds a :name-parameterised SQL fragment (column names are not
// validated — use for expressions the builder can't express).
func Raw(sql string, params map[string]any) Cond { return rawCond{sql, params} }

func col(b *builder, m *entity.Meta, prop string) (string, error) {
	c, ok := m.ResolveProperty(prop)
	if !ok {
		return "", fmt.Errorf("sqlgen: unknown property %q on %s", prop, m.Table)
	}
	return b.q(c.Name), nil
}

func (c cmp) render(b *builder, m *entity.Meta) (string, error) {
	n, err := col(b, m, c.prop)
	if err != nil {
		return "", err
	}
	if c.val == nil {
		switch c.op {
		case "=":
			return n + " IS NULL", nil
		case "<>":
			return n + " IS NOT NULL", nil
		}
	}
	return n + " " + c.op + " " + b.bind(c.val), nil
}

func (c inCond) render(b *builder, m *entity.Meta) (string, error) {
	n, err := col(b, m, c.prop)
	if err != nil {
		return "", err
	}
	vals := Expand(c.vals)
	if len(vals) == 0 {
		if c.not {
			return "1=1", nil
		}
		return "1=0", nil
	}
	phs := make([]string, len(vals))
	for i, v := range vals {
		phs[i] = b.bind(v)
	}
	op := " IN ("
	if c.not {
		op = " NOT IN ("
	}
	return n + op + strings.Join(phs, ", ") + ")", nil
}

func (c nullCond) render(b *builder, m *entity.Meta) (string, error) {
	n, err := col(b, m, c.prop)
	if err != nil {
		return "", err
	}
	if c.not {
		return n + " IS NOT NULL", nil
	}
	return n + " IS NULL", nil
}

func (c between) render(b *builder, m *entity.Meta) (string, error) {
	n, err := col(b, m, c.prop)
	if err != nil {
		return "", err
	}
	return n + " BETWEEN " + b.bind(c.lo) + " AND " + b.bind(c.hi), nil
}

func (c logic) render(b *builder, m *entity.Meta) (string, error) {
	var parts []string
	for _, x := range c.conds {
		if x == nil {
			continue
		}
		s, err := x.render(b, m)
		if err != nil {
			return "", err
		}
		if s != "" {
			parts = append(parts, s)
		}
	}
	switch len(parts) {
	case 0:
		return "", nil
	case 1:
		return parts[0], nil
	}
	return "(" + strings.Join(parts, " "+c.op+" ") + ")", nil
}

func (c notCond) render(b *builder, m *entity.Meta) (string, error) {
	s, err := c.c.render(b, m)
	if err != nil || s == "" {
		return s, err
	}
	return "NOT (" + s + ")", nil
}

func (c rawCond) render(b *builder, m *entity.Meta) (string, error) {
	s, err := b.named(c.sql, c.params)
	if err != nil {
		return "", err
	}
	return "(" + s + ")", nil
}

// Positional rewrites a "?"-style query into dialect placeholders, expanding
// slice arguments into "?, ?, ..." (sqlx.In + Rebind semantics).
func Positional(d Dialect, sql string, args []any) (Query, error) {
	b := &builder{d: d}
	var out strings.Builder
	ai := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] != '?' {
			out.WriteByte(sql[i])
			continue
		}
		if ai >= len(args) {
			return Query{}, fmt.Errorf("sqlgen: more placeholders than arguments")
		}
		v := args[ai]
		ai++
		if isList(v) {
			vals := Expand(v)
			if len(vals) == 0 {
				return Query{}, fmt.Errorf("sqlgen: empty slice for placeholder %d", ai)
			}
			phs := make([]string, len(vals))
			for k, x := range vals {
				phs[k] = b.bind(x)
			}
			out.WriteString(strings.Join(phs, ", "))
			continue
		}
		out.WriteString(b.bind(v))
	}
	if ai != len(args) {
		return Query{}, fmt.Errorf("sqlgen: %d arguments for %d placeholders", len(args), ai)
	}
	b.sb.WriteString(out.String())
	return b.finish()
}

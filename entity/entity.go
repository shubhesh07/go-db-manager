// Package entity extracts table metadata from Go structs (the "metamodel" in
// Spring Data terms) and scans rows back into them.
//
// Tags:
//
//	db:"col"          column name (sqlx convention); default snake_case(Field)
//	db:"-"            ignored
//	orm:"pk"          primary key
//	orm:"auto"        DB-generated (auto-increment); skipped on insert
//	orm:"created"     set to NOW() on insert
//	orm:"updated"     set to NOW() on insert and update
//	orm:"created_by"  set from jpa.WithActor(ctx) on insert
//	orm:"updated_by"  set from jpa.WithActor(ctx) on insert and update
//
// Optional methods on *T:
//
//	TableName() string
//	SoftDelete() (filter, set string)   e.g. "active = 1", "active = 0"
package entity

import (
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Column struct {
	Name      string
	Field     string // Go field name (last segment)
	Index     []int  // reflect index path (handles embedded structs)
	Type      reflect.Type
	PK        bool
	Auto      bool
	Created   bool
	Updated   bool
	CreatedBy bool
	UpdatedBy bool
}

type Meta struct {
	Type             reflect.Type
	Table            string
	Columns          []Column
	PK               *Column
	SoftDeleteFilter string // "" = none
	SoftDeleteSet    string
	byName           map[string]*Column
}

var (
	cache   sync.Map // reflect.Type -> *Meta
	scanner = reflect.TypeOf((*sql.Scanner)(nil)).Elem()
	timeT   = reflect.TypeOf(time.Time{})
)

// Of returns the cached metadata for struct type T.
func Of[T any]() (*Meta, error) {
	var zero T
	return OfType(reflect.TypeOf(zero))
}

func OfType(t reflect.Type) (*Meta, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if m, ok := cache.Load(t); ok {
		return m.(*Meta), nil
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("entity: %s is not a struct", t)
	}
	m := &Meta{Type: t, Table: SnakeCase(t.Name()), byName: map[string]*Column{}}
	collect(m, t, nil)
	if len(m.Columns) == 0 {
		return nil, fmt.Errorf("entity: %s has no columns", t)
	}
	for i := range m.Columns {
		c := &m.Columns[i]
		m.byName[c.Name] = c
		if c.PK {
			m.PK = c
		}
	}
	if m.PK == nil {
		if c := m.byName["id"]; c != nil {
			c.PK = true
			m.PK = c
		}
	}
	ptr := reflect.New(t)
	if tn, ok := ptr.Interface().(interface{ TableName() string }); ok {
		m.Table = tn.TableName()
	}
	if sd, ok := ptr.Interface().(interface{ SoftDelete() (string, string) }); ok {
		m.SoftDeleteFilter, m.SoftDeleteSet = sd.SoftDelete()
	}
	cache.Store(t, m)
	return m, nil
}

func collect(m *Meta, t reflect.Type, prefix []int) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		idx := append(append([]int{}, prefix...), i)
		dbTag := f.Tag.Get("db")
		if dbTag == "-" {
			continue
		}
		ft := f.Type
		if f.Anonymous && ft.Kind() == reflect.Struct && !scannable(ft) {
			collect(m, ft, idx) // embedded (even unexported) structs are flattened
			continue
		}
		if !f.IsExported() {
			continue
		}
		if !scannable(ft) {
			continue // relationships, nested structs, maps: not columns
		}
		c := Column{Name: dbTag, Field: f.Name, Index: idx, Type: ft}
		if c.Name == "" {
			c.Name = SnakeCase(f.Name)
		}
		for _, opt := range strings.Split(f.Tag.Get("orm"), ",") {
			switch strings.TrimSpace(opt) {
			case "pk":
				c.PK = true
			case "auto":
				c.Auto = true
			case "created":
				c.Created = true
			case "updated":
				c.Updated = true
			case "created_by":
				c.CreatedBy = true
			case "updated_by":
				c.UpdatedBy = true
			}
		}
		m.Columns = append(m.Columns, c)
	}
}

// scannable reports whether database/sql can scan into a value of type t.
func scannable(t reflect.Type) bool {
	if t.Implements(scanner) || reflect.PointerTo(t).Implements(scanner) {
		return true
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8
	case reflect.Struct:
		return t == timeT
	}
	return false
}

// Column looks a column up by name.
func (m *Meta) Column(name string) *Column { return m.byName[name] }

// ColumnNames returns all column names for SELECT lists.
func (m *Meta) ColumnNames() []string {
	out := make([]string, len(m.Columns))
	for i, c := range m.Columns {
		out[i] = c.Name
	}
	return out
}

// ResolveProperty maps a CamelCase property from a method name (e.g.
// "WarehouseId", "ProductCode") to a column. Matching is case-insensitive
// against the Go field name and against the column name with underscores
// removed, so WarehouseId, WarehouseID and warehouse_id all resolve.
func (m *Meta) ResolveProperty(prop string) (*Column, bool) {
	p := strings.ToLower(strings.ReplaceAll(prop, "_", ""))
	for i := range m.Columns {
		c := &m.Columns[i]
		if strings.ToLower(c.Field) == p || strings.ReplaceAll(c.Name, "_", "") == p {
			return c, true
		}
	}
	return nil, false
}

// Value returns the field value for column c on entity v (struct or pointer).
func (c *Column) Value(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	return v.FieldByIndex(c.Index)
}

// ScanTargets returns scan destinations for the given result columns on dest
// (a pointer to struct). Result columns with no matching field are discarded.
func (m *Meta) ScanTargets(cols []string, dest reflect.Value) []any {
	dest = dest.Elem()
	targets := make([]any, len(cols))
	for i, name := range cols {
		if c := m.byName[name]; c != nil {
			targets[i] = target(dest.FieldByIndex(c.Index))
		} else {
			targets[i] = new(any)
		}
	}
	return targets
}

// target returns a scan destination for field f. Plain bool, integer and
// time.Time fields (and pointers to them) get a coercing scanner so MySQL
// BIT(n) (raw big-endian bytes such as []byte{1}), TINYINT and
// DATETIME-as-text (no parseTime) still scan.
func target(f reflect.Value) any {
	ptr := f.Addr().Interface()
	switch ptr.(type) {
	case *bool, **bool, *time.Time, **time.Time,
		*int, *int8, *int16, *int32, *int64, *uint, *uint8, *uint16, *uint32, *uint64,
		**int, **int8, **int16, **int32, **int64, **uint, **uint8, **uint16, **uint32, **uint64:
		return &coerce{f}
	}
	return ptr
}

type coerce struct{ f reflect.Value }

var timeLayouts = []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", time.RFC3339Nano, "2006-01-02"}

func (c *coerce) Scan(src any) error {
	f := c.f
	if f.Kind() == reflect.Pointer {
		if src == nil {
			f.Set(reflect.Zero(f.Type()))
			return nil
		}
		f.Set(reflect.New(f.Type().Elem()))
		f = f.Elem()
	} else if src == nil {
		f.Set(reflect.Zero(f.Type()))
		return nil
	}
	if f.Type() == timeT {
		switch v := src.(type) {
		case time.Time:
			f.Set(reflect.ValueOf(v))
			return nil
		case []byte:
			return c.parseTime(f, string(v))
		case string:
			return c.parseTime(f, v)
		}
		return fmt.Errorf("entity: cannot scan %T into time.Time", src)
	}
	if k := f.Kind(); k >= reflect.Int && k <= reflect.Uint64 {
		return c.scanInt(f, src)
	}
	switch v := src.(type) {
	case bool:
		f.SetBool(v)
	case int64:
		f.SetBool(v != 0)
	case []byte:
		f.SetBool(len(v) > 0 && v[0] != 0 && string(v) != "0" && !strings.EqualFold(string(v), "false"))
	case string:
		f.SetBool(v != "" && v != "0" && !strings.EqualFold(v, "false"))
	default:
		return fmt.Errorf("entity: cannot scan %T into bool", src)
	}
	return nil
}

// scanInt handles what database/sql does plus raw BIT bytes.
func (c *coerce) scanInt(f reflect.Value, src any) error {
	var n int64
	var u uint64
	signed := f.Kind() <= reflect.Int64
	switch v := src.(type) {
	case int64:
		n, u = v, uint64(v)
	case uint64:
		n, u = int64(v), v
	case float64:
		n, u = int64(v), uint64(v)
	case bool:
		if v {
			n, u = 1, 1
		}
	case []byte:
		if x, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			n, u = x, uint64(x)
		} else if len(v) <= 8 { // BIT(n): big-endian bytes
			for _, b := range v {
				u = u<<8 | uint64(b)
			}
			n = int64(u)
		} else {
			return fmt.Errorf("entity: cannot scan %q into %s", v, f.Type())
		}
	case string:
		x, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("entity: cannot scan %q into %s", v, f.Type())
		}
		n, u = x, uint64(x)
	default:
		return fmt.Errorf("entity: cannot scan %T into %s", src, f.Type())
	}
	if signed {
		f.SetInt(n)
	} else {
		f.SetUint(u)
	}
	return nil
}

func (c *coerce) parseTime(f reflect.Value, s string) error {
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			f.Set(reflect.ValueOf(t))
			return nil
		}
	}
	return fmt.Errorf("entity: cannot parse time %q", s)
}

// ScanAll scans every row into a []T. Mapping is by column name, so
// projections and extra columns are fine. Works for any struct (DTOs too).
func ScanAll[T any](rows *sql.Rows) ([]T, error) {
	defer rows.Close()
	m, err := Of[T]()
	if err != nil {
		return nil, err
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []T
	for rows.Next() {
		var item T
		if err := rows.Scan(m.ScanTargets(cols, reflect.ValueOf(&item))...); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// SnakeCase converts CamelCase to snake_case, keeping acronyms together:
// ProductCode → product_code, WarehouseID → warehouse_id, HTTPCode → http_code.
func SnakeCase(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && (rs[i-1] < 'A' || rs[i-1] > 'Z' || (i+1 < len(rs) && rs[i+1] >= 'a' && rs[i+1] <= 'z')) {
				b.WriteByte('_')
			}
			b.WriteRune(r + 'a' - 'A')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

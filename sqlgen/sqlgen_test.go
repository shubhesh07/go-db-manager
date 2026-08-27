package sqlgen

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shubhesh07/gojpa/entity"
	"github.com/shubhesh07/gojpa/parser"
)

type Order struct {
	ID         int64     `db:"id" orm:"pk,auto"`
	OrderID    int64     `db:"order_id"`
	CustomerID int64     `db:"customer_id"`
	StatusID   int       `db:"status_id"`
	Name       string    `db:"name"`
	Active     bool      `db:"active"`
	CreatedOn  time.Time `db:"created_on" orm:"created"`
	UpdatedOn  time.Time `db:"updated_on" orm:"updated"`
}

func (*Order) TableName() string            { return "order_details" }
func (*Order) SoftDelete() (string, string) { return "active = 1", "active = 0" }

const cols = "id, order_id, customer_id, status_id, name, active, created_on, updated_on"

func TestBuild(t *testing.T) {
	m, err := entity.Of[Order]()
	if err != nil {
		t.Fatal(err)
	}
	res := func(p string) bool { _, ok := m.ResolveProperty(p); return ok }
	cases := []struct {
		name string
		args []any
		d    Dialect
		sql  string
		out  []any
	}{
		{"FindByOrderId", []any{1}, MySQL, "SELECT " + cols + " FROM order_details WHERE order_id = ? AND active = 1", []any{1}},
		{"FindByOrderId", []any{1}, Postgres, "SELECT " + cols + " FROM order_details WHERE order_id = $1 AND active = 1", []any{1}},
		{"FindByCustomerIdAndStatusIdIn", []any{7, []int{1, 2}}, MySQL, "SELECT " + cols + " FROM order_details WHERE customer_id = ? AND status_id IN (?, ?) AND active = 1", []any{7, 1, 2}},
		{"FindByCustomerIdOrStatusId", []any{7, 2}, MySQL, "SELECT " + cols + " FROM order_details WHERE (customer_id = ?) OR (status_id = ?) AND active = 1", nil},
		{"FindByStatusIdIn", []any{[]int{}}, MySQL, "SELECT " + cols + " FROM order_details WHERE 1=0 AND active = 1", nil},
		{"FindByNameContainingIgnoreCase", []any{"ab"}, MySQL, "SELECT " + cols + " FROM order_details WHERE UPPER(name) LIKE UPPER(?) AND active = 1", []any{"%ab%"}},
		{"FindByNameStartingWith", []any{"ab"}, MySQL, "SELECT " + cols + " FROM order_details WHERE name LIKE ? AND active = 1", []any{"ab%"}},
		{"FindTop5ByCustomerIdOrderByCreatedOnDescIdAsc", []any{7}, MySQL, "SELECT " + cols + " FROM order_details WHERE customer_id = ? AND active = 1 ORDER BY created_on DESC, id ASC LIMIT 5", nil},
		{"FindByOrderIdBetween", []any{1, 9}, Postgres, "SELECT " + cols + " FROM order_details WHERE order_id BETWEEN $1 AND $2 AND active = 1", []any{1, 9}},
		{"FindByNameIsNullAndActiveTrue", nil, MySQL, "SELECT " + cols + " FROM order_details WHERE name IS NULL AND active = TRUE AND active = 1", nil},
		{"CountByStatusId", []any{1}, MySQL, "SELECT COUNT(*) FROM order_details WHERE status_id = ? AND active = 1", nil},
		{"ExistsByOrderId", []any{1}, MySQL, "SELECT EXISTS(SELECT 1 FROM order_details WHERE order_id = ? AND active = 1)", nil},
		{"DeleteByOrderId", []any{1}, MySQL, "UPDATE order_details SET active = 0, updated_on = NOW() WHERE order_id = ? AND active = 1", nil},
		{"FindDistinctByCustomerId", []any{1}, MySQL, "SELECT DISTINCT " + cols + " FROM order_details WHERE customer_id = ? AND active = 1", nil},
		{"FindAll", nil, MySQL, "SELECT " + cols + " FROM order_details WHERE active = 1", nil},
	}
	for _, c := range cases {
		tree, err := parser.Parse(c.name, res)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		q, err := Build(c.d, m, tree, c.args, Options{})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if q.SQL != c.sql {
			t.Errorf("%s:\n got %s\nwant %s", c.name, q.SQL, c.sql)
		}
		if c.out != nil && !reflect.DeepEqual(q.Args, c.out) {
			t.Errorf("%s: args %v want %v", c.name, q.Args, c.out)
		}
	}

	tree, _ := parser.Parse("FindByCustomerId", res)
	q, _ := Build(MySQL, m, tree, []any{1}, Options{OrderBy: []parser.Order{{Property: "OrderId", Desc: true}}, Limit: 10, Offset: 20, IncludeDeleted: true})
	want := "SELECT " + cols + " FROM order_details WHERE customer_id = ? ORDER BY order_id DESC LIMIT 10 OFFSET 20"
	if q.SQL != want {
		t.Errorf("pageable:\n got %s\nwant %s", q.SQL, want)
	}
	tree, _ = parser.Parse("DeleteByOrderId", res)
	q, _ = Build(MySQL, m, tree, []any{1}, Options{IncludeDeleted: true})
	if q.SQL != "DELETE FROM order_details WHERE order_id = ?" {
		t.Errorf("hard delete: %s", q.SQL)
	}
}

func TestNamed(t *testing.T) {
	q, err := Named(MySQL, "UPDATE t SET a = :a, x = NOW() WHERE code IN (:codes) AND b = :a", map[string]any{"a": 1, "codes": []string{"x", "y"}})
	if err != nil {
		t.Fatal(err)
	}
	if q.SQL != "UPDATE t SET a = ?, x = NOW() WHERE code IN (?, ?) AND b = ?" || !reflect.DeepEqual(q.Args, []any{1, "x", "y", 1}) {
		t.Errorf("got %s %v", q.SQL, q.Args)
	}
	q, _ = Named(Postgres, "SELECT x::text FROM t WHERE id = :id", map[string]any{"id": 5})
	if q.SQL != "SELECT x::text FROM t WHERE id = $1" {
		t.Errorf("got %s", q.SQL)
	}
	if _, err := Named(MySQL, "SELECT :missing", nil); err == nil {
		t.Error("expected missing param error")
	}
}

func TestPositional(t *testing.T) {
	q, err := Positional(Postgres, "UPDATE t SET a = ? WHERE c IN (?) AND w IN (?)", []any{1, []string{"x", "y"}, []int64{7}})
	if err != nil || q.SQL != "UPDATE t SET a = $1 WHERE c IN ($2, $3) AND w IN ($4)" || !reflect.DeepEqual(q.Args, []any{1, "x", "y", int64(7)}) {
		t.Errorf("%v %s %v", err, q.SQL, q.Args)
	}
	if _, err := Positional(MySQL, "? ?", []any{1}); err == nil {
		t.Error("expected arg-count error")
	}
}

type Reserved struct {
	ID    int64  `db:"id" orm:"pk,auto"`
	Order int    `db:"order"`
	Group string `db:"group"`
	Odd   string `db:"odd-name"`
}

func (*Reserved) TableName() string { return "user" }

func TestQuotingAndLocks(t *testing.T) {
	m, _ := entity.Of[Reserved]()
	res := func(p string) bool { _, ok := m.ResolveProperty(p); return ok }
	tree, err := parser.Parse("FindByOrderOrderByGroupDesc", res)
	if err != nil {
		t.Fatal(err)
	}
	q, err := Build(MySQL, m, tree, []any{1}, Options{Lock: ForUpdate})
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, `order`, `group`, `odd-name` FROM `user` WHERE `order` = ? ORDER BY `group` DESC FOR UPDATE"
	if q.SQL != want {
		t.Errorf("\n got %s\nwant %s", q.SQL, want)
	}
	q, _ = Build(Postgres, m, tree, []any{1}, Options{Lock: ForShare})
	want = `SELECT id, "order", "group", "odd-name" FROM "user" WHERE "order" = $1 ORDER BY "group" DESC FOR SHARE`
	if q.SQL != want {
		t.Errorf("\n got %s\nwant %s", q.SQL, want)
	}
	q, _ = Build(SQLite, m, tree, []any{1}, Options{Lock: ForUpdate})
	if strings.Contains(q.SQL, "FOR UPDATE") {
		t.Errorf("sqlite must drop locks: %s", q.SQL)
	}
	q, _ = Build(MySQL, m, &parser.Tree{Verb: parser.Count}, nil, Options{Cond: Eq("Group", "x")})
	if q.SQL != "SELECT COUNT(*) FROM `user` WHERE `group` = ?" {
		t.Errorf("cond quoting: %s", q.SQL)
	}
}

func TestLikeEscapeAndParamLimit(t *testing.T) {
	m, _ := entity.Of[Order]()
	res := func(p string) bool { _, ok := m.ResolveProperty(p); return ok }
	tree, _ := parser.Parse("FindByNameContaining", res)
	q, _ := Build(MySQL, m, tree, []any{"50%_off\\"}, Options{})
	if q.Args[0] != `%50\%\_off\\%` {
		t.Errorf("escape: %q", q.Args[0])
	}
	tree, _ = parser.Parse("FindByOrderIdIn", res)
	big := make([]int, 70000)
	if _, err := Build(MySQL, m, tree, []any{big}, Options{}); !errors.Is(err, ErrTooManyParams) {
		t.Errorf("want ErrTooManyParams, got %v", err)
	}
	if _, err := Build(Postgres, m, tree, []any{big[:65535]}, Options{}); err != nil {
		t.Errorf("65535 params must be allowed: %v", err)
	}
}

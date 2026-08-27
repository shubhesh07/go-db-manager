package jpa

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shubhesh07/gojpa/sqlgen"
)

type Warehouse struct {
	ID          int64           `db:"id" orm:"pk,auto"`
	ProductCode string          `db:"product_code"`
	WarehouseID int64           `db:"warehouse_id"`
	Price       sql.NullFloat64 `db:"price"`
	Active      bool            `db:"active"`
	UpdatedBy   int64           `db:"updated_by" orm:"updated_by"`
	UpdatedOn   time.Time       `db:"updated_on" orm:"updated"`
}

func (*Warehouse) TableName() string            { return "medicine_warehouse_master" }
func (*Warehouse) SoftDelete() (string, string) { return "active = 1", "active = 0" }

var _ CRUD[Warehouse, int64] = (*Repository[Warehouse, int64])(nil)

const cols = "id, product_code, warehouse_id, price, active, updated_by, updated_on"

func newRepo(t *testing.T) (*Repository[Warehouse, int64], sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New[Warehouse, int64](db, sqlgen.MySQL, WithHook(Timeout(time.Second))), mock
}

func TestFindBy(t *testing.T) {
	r, mock := newRepo(t)
	mock.ExpectQuery("SELECT "+cols+" FROM medicine_warehouse_master WHERE product_code IN (?, ?) AND warehouse_id = ? AND active = 1").
		WithArgs("A", "B", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "product_code", "warehouse_id", "price", "active", "updated_by", "updated_on"}).
			AddRow(1, "A", 3, []byte("12.50"), true, 9, time.Now()).
			AddRow(2, "B", 3, nil, true, 9, time.Now()))
	rows, err := r.FindBy(context.Background(), "FindByProductCodeInAndWarehouseId", []string{"A", "B"}, int64(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Price.Float64 != 12.5 || rows[1].Price.Valid || !rows[0].Active {
		t.Errorf("bad scan: %+v", rows)
	}
}

func TestFindOneNotFound(t *testing.T) {
	r, mock := newRepo(t)
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE id = ? AND active = 1 LIMIT 1").
		WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err := r.FindByID(context.Background(), 7)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestPage(t *testing.T) {
	r, mock := newRepo(t)
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE warehouse_id = ? AND active = 1 ORDER BY id DESC LIMIT 2 OFFSET 2").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5).AddRow(4))
	mock.ExpectQuery("SELECT COUNT(*) FROM medicine_warehouse_master WHERE warehouse_id = ? AND active = 1").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(7))
	p, err := r.PageBy(context.Background(), "FindByWarehouseId", PageRequest(1, 2, Desc("Id")), int64(3))
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 7 || p.TotalPages != 4 || len(p.Content) != 2 || !p.HasNext() {
		t.Errorf("%+v", p)
	}
}

func TestSaveUpdateDelete(t *testing.T) {
	r, mock := newRepo(t)
	ctx := WithActor(context.Background(), int64(42))
	const ins = "INSERT INTO medicine_warehouse_master (product_code, warehouse_id, price, active, updated_by, updated_on) VALUES (?, ?, ?, ?, ?, NOW())"
	mock.ExpectExec(ins).WithArgs("A", int64(1), sql.NullFloat64{}, true, int64(42)).WillReturnResult(sqlmock.NewResult(10, 1))
	mock.ExpectExec(ins).WithArgs("B", int64(1), sql.NullFloat64{}, true, int64(42)).WillReturnResult(sqlmock.NewResult(17, 1))
	ws := []*Warehouse{{ProductCode: "A", WarehouseID: 1, Active: true}, {ProductCode: "B", WarehouseID: 1, Active: true}}
	if err := r.SaveAll(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if ws[0].ID != 10 || ws[1].ID != 17 {
		t.Errorf("ids not written back: %d %d", ws[0].ID, ws[1].ID)
	}
	mock.ExpectExec(ins+", (?, ?, ?, ?, ?, NOW())").
		WithArgs("A", int64(1), sql.NullFloat64{}, true, int64(42), "B", int64(1), sql.NullFloat64{}, true, int64(42)).
		WillReturnResult(sqlmock.NewResult(20, 2))
	if err := r.SaveAllNoKeys(ctx, ws); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("UPDATE medicine_warehouse_master SET product_code = ?, warehouse_id = ?, price = ?, active = ?, updated_by = ?, updated_on = NOW() WHERE id = ?").
		WithArgs("A", int64(1), sql.NullFloat64{}, true, int64(42), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Update(ctx, ws[0]); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("UPDATE medicine_warehouse_master SET active = 0, updated_on = NOW() WHERE id = ? AND active = 1").
		WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.DeleteByID(ctx, 10); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("DELETE FROM medicine_warehouse_master WHERE id = ?").
		WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.IncludingDeleted().DeleteByID(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

type soldCount struct {
	ProductCode string `db:"product_code"`
	Max         int64  `db:"subs_taken_count"`
}

func TestRaw(t *testing.T) {
	r, mock := newRepo(t)
	mock.ExpectQuery("SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (?, ?) GROUP BY product_code").
		WithArgs("A", "B").WillReturnRows(sqlmock.NewRows([]string{"product_code", "subs_taken_count"}).AddRow("A", 3))
	out, err := Select[soldCount](context.Background(), r, "sold", "SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (:codes) GROUP BY product_code", map[string]any{"codes": []string{"A", "B"}})
	if err != nil || len(out) != 1 || out[0].Max != 3 {
		t.Fatalf("%v %+v", err, out)
	}
	mock.ExpectExec("UPDATE medicine_warehouse_master SET is_searchable = ?, updated_on = NOW() WHERE product_code IN (?) AND warehouse_id = ?").
		WithArgs(true, "A", int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	n, err := r.Exec(context.Background(), "upd", "UPDATE medicine_warehouse_master SET is_searchable = :v, updated_on = NOW() WHERE product_code IN (:codes) AND warehouse_id = :wh", map[string]any{"v": true, "codes": []string{"A"}, "wh": int64(2)})
	if err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
}

func TestTransactional(t *testing.T) {
	db, mock, _ := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	defer db.Close()
	r := New[Warehouse, int64](db, sqlgen.MySQL)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM medicine_warehouse_master WHERE id = ?").WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()
	err := Transactional(context.Background(), db, func(tx *sql.Tx) error {
		if err := r.WithTx(tx).IncludingDeleted().DeleteByID(context.Background(), 1); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Errorf("got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestBitBoolAndTextTime(t *testing.T) {
	r, mock := newRepo(t)
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE active = 1").
		WillReturnRows(sqlmock.NewRows([]string{"active", "updated_on"}).
			AddRow([]byte{1}, []byte("2026-08-27 10:00:00")).
			AddRow([]byte{0}, nil))
	rows, err := r.FindAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].Active || rows[1].Active || rows[0].UpdatedOn.Hour() != 10 || !rows[1].UpdatedOn.IsZero() {
		t.Errorf("%+v", rows)
	}
}

func TestWhere(t *testing.T) {
	r, mock := newRepo(t)
	ctx := context.Background()
	mock.ExpectQuery("SELECT "+cols+" FROM medicine_warehouse_master WHERE ((product_code = ? AND warehouse_id = ?) OR (product_code = ? AND warehouse_id = ?)) AND active = 1 ORDER BY id DESC LIMIT 5").
		WithArgs("A", int64(1), "B", int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	c := Or(And(Eq("ProductCode", "A"), Eq("WarehouseId", int64(1))), And(Eq("ProductCode", "B"), Eq("WarehouseId", int64(2))))
	rows, err := r.FindWhere(ctx, c, Sort{Desc("Id")}, Limit(5))
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", err, rows)
	}
	mock.ExpectQuery("SELECT COUNT(*) FROM medicine_warehouse_master WHERE (product_code IN (?, ?) AND (price IS NULL OR price > ?)) AND active = 1").
		WithArgs("A", "B", 5).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(2))
	n, err := r.CountWhere(ctx, And(In("ProductCode", []string{"A", "B"}), RawCond("price IS NULL OR price > :p", map[string]any{"p": 5})))
	if err != nil || n != 2 {
		t.Fatalf("%v %d", err, n)
	}
	mock.ExpectExec("UPDATE medicine_warehouse_master SET active = 0, updated_on = NOW() WHERE NOT (updated_by IS NULL) AND active = 1").
		WillReturnResult(sqlmock.NewResult(0, 3))
	if n, err := r.DeleteWhere(ctx, Not(IsNull("UpdatedBy"))); err != nil || n != 3 {
		t.Fatalf("%v %d", err, n)
	}
	if _, err := r.FindWhere(ctx, Eq("Nope", 1)); err == nil {
		t.Error("unknown property must fail")
	}
}

func TestWindow(t *testing.T) {
	r, mock := newRepo(t)
	ctx := context.Background()
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE warehouse_id = ? AND active = 1 ORDER BY product_code ASC, id DESC LIMIT 3").
		WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "product_code"}).AddRow(3, "A").AddRow(2, "B").AddRow(1, "C"))
	w, err := r.WindowBy(ctx, "FindByWarehouseId", Keyset(2, Asc("ProductCode"), Desc("Id")), int64(1))
	if err != nil || len(w.Content) != 2 || !w.HasNext {
		t.Fatalf("%v %+v", err, w)
	}
	mock.ExpectQuery("SELECT "+cols+" FROM medicine_warehouse_master WHERE warehouse_id = ? AND ((product_code > ?) OR (product_code = ? AND id < ?)) AND active = 1 ORDER BY product_code ASC, id DESC LIMIT 3").
		WithArgs(int64(1), "B", "B", int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "product_code"}).AddRow(1, "C"))
	w, err = r.WindowBy(ctx, "FindByWarehouseId", w.Next, int64(1))
	if err != nil || len(w.Content) != 1 || w.HasNext {
		t.Fatalf("%v %+v", err, w)
	}
}

func TestSelectPage(t *testing.T) {
	r, mock := newRepo(t)
	mock.ExpectQuery("SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (?) GROUP BY product_code ORDER BY product_code DESC LIMIT 10 OFFSET 10").
		WithArgs("A").WillReturnRows(sqlmock.NewRows([]string{"product_code", "subs_taken_count"}).AddRow("A", 1))
	mock.ExpectQuery("SELECT COUNT(DISTINCT product_code) FROM product_sold_count WHERE product_code IN (?)").
		WithArgs("A").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(11))
	p, err := SelectPage[soldCount](context.Background(), r, "sold",
		"SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (:codes) GROUP BY product_code",
		"SELECT COUNT(DISTINCT product_code) FROM product_sold_count WHERE product_code IN (:codes)",
		PageRequest(1, 10, Desc("ProductCode")), map[string]any{"codes": []string{"A"}})
	if err != nil || p.Total != 11 || p.TotalPages != 2 || len(p.Content) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
}

type Versioned struct {
	ID      int64  `db:"id" orm:"pk,auto"`
	Name    string `db:"name"`
	Version int64  `db:"version" orm:"version"`
}

func (*Versioned) TableName() string { return "versioned" }

func TestOptimisticLock(t *testing.T) {
	db, mock, _ := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	defer db.Close()
	r := New[Versioned, int64](db, sqlgen.MySQL)
	v := &Versioned{ID: 1, Name: "a", Version: 3}
	mock.ExpectExec("UPDATE versioned SET name = ?, version = version + 1 WHERE id = ? AND version = ?").
		WithArgs("a", int64(1), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Update(context.Background(), v); err != nil || v.Version != 4 {
		t.Fatalf("%v version=%d", err, v.Version)
	}
	mock.ExpectExec("UPDATE versioned SET name = ?, version = version + 1 WHERE id = ? AND version = ?").
		WithArgs("a", int64(1), int64(4)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.Update(context.Background(), v); !errors.Is(err, ErrOptimisticLock) {
		t.Fatalf("want ErrOptimisticLock, got %v", err)
	}
}

func TestRetry(t *testing.T) {
	db, mock, _ := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	defer db.Close()
	calls := 0
	r := New[Warehouse, int64](db, sqlgen.MySQL,
		WithRetry(RetryPolicy{Attempts: 3, Backoff: time.Millisecond, Retryable: IsDeadlock}),
		WithHook(Metrics(func(string, time.Duration, int64, error) { calls++ })))
	q := "SELECT " + cols + " FROM medicine_warehouse_master WHERE id = ? AND active = 1 LIMIT 1"
	mock.ExpectQuery(q).WithArgs(int64(1)).WillReturnError(errors.New("Error 1213: Deadlock found when trying to get lock"))
	mock.ExpectQuery(q).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	if _, err := r.FindByID(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("hook calls = %d, want 2 (one per attempt)", calls)
	}
	// writes are not retried unless Writes: true
	mock.ExpectExec("UPDATE medicine_warehouse_master SET active = 0, updated_on = NOW() WHERE id = ? AND active = 1").
		WithArgs(int64(1)).WillReturnError(errors.New("Error 1213: Deadlock"))
	if err := r.DeleteByID(context.Background(), 1); err == nil {
		t.Error("write must not be retried by default")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestBatchUpdateAndLock(t *testing.T) {
	r, mock := newRepo(t)
	ctx := WithActor(context.Background(), int64(9))
	mock.ExpectExec("UPDATE medicine_warehouse_master SET active = CASE WHEN product_code = ? AND warehouse_id = ? THEN ? ELSE active END, price = CASE WHEN product_code = ? AND warehouse_id = ? THEN ? ELSE price END, updated_by = ?, updated_on = NOW() WHERE (product_code = ? AND warehouse_id = ?) OR (product_code = ? AND warehouse_id = ?)").
		WithArgs("A", int64(1), true, "B", int64(1), 9.5, int64(9), "A", int64(1), "B", int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	n, err := r.BatchUpdate(ctx, []BatchRow{
		{Key: map[string]any{"ProductCode": "A", "WarehouseId": int64(1)}, Set: map[string]any{"Active": true}},
		{Key: map[string]any{"ProductCode": "B", "WarehouseId": int64(1)}, Set: map[string]any{"Price": 9.5}},
	})
	if err != nil || n != 2 {
		t.Fatalf("%v %d", err, n)
	}
	if _, err := r.BatchUpdate(ctx, []BatchRow{{Key: map[string]any{"Nope": 1}, Set: map[string]any{"Active": true}}}); err == nil {
		t.Error("unknown property must fail")
	}
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE id = ? AND active = 1 LIMIT 1 FOR UPDATE").
		WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	if _, err := r.FindOneBy(ctx, "FindById", int64(1), ForUpdate); err != nil {
		t.Fatal(err)
	}
	var seen []int64
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE warehouse_id = ? AND active = 1").
		WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2).AddRow(3))
	err = r.EachBy(ctx, "FindByWarehouseId", func(w Warehouse) error {
		seen = append(seen, w.ID)
		if w.ID == 2 {
			return errors.New("stop")
		}
		return nil
	}, int64(3))
	if err == nil || len(seen) != 2 {
		t.Errorf("each: %v %v", err, seen)
	}
	if got := Chunk([]int{1, 2, 3, 4, 5}, 2); len(got) != 3 || len(got[2]) != 1 {
		t.Errorf("chunk: %v", got)
	}
}

func TestCommentsFilterCacheReplica(t *testing.T) {
	primary, pmock, _ := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	replica, rmock, _ := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	defer primary.Close()
	defer replica.Close()
	r := New[Warehouse, int64](primary, sqlgen.MySQL, WithComments(), WithReplica(replica), WithCache(MemoryCache(), time.Minute))
	ctx := WithFilter(context.Background(), Eq("WarehouseId", int64(7)))

	// read → replica, comment prefix, request filter ANDed in
	q := "/* Warehouse.FindByProductCode */ SELECT " + cols + " FROM medicine_warehouse_master WHERE product_code = ? AND warehouse_id = ? AND active = 1"
	rmock.ExpectQuery(q).WithArgs("A", int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	rows, err := r.FindBy(ctx, "FindByProductCode", "A")
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", err, rows)
	}
	// second call served from cache: no expectation set, must not hit any DB
	if rows, err = r.FindBy(ctx, "FindByProductCode", "A"); err != nil || len(rows) != 1 {
		t.Fatalf("cache: %v %v", err, rows)
	}
	// locked read → primary, bypasses cache
	pmock.ExpectQuery(q+" LIMIT 1 FOR UPDATE").WithArgs("A", int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	if _, err := r.FindOneBy(ctx, "FindByProductCode", "A", ForUpdate); err != nil {
		t.Fatal(err)
	}
	// write → primary and clears cache
	pmock.ExpectExec("/* Warehouse.DeleteByProductCode */ UPDATE medicine_warehouse_master SET active = 0, updated_on = NOW() WHERE product_code = ? AND warehouse_id = ? AND active = 1").
		WithArgs("A", int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := r.DeleteBy(ctx, "DeleteByProductCode", "A"); err != nil {
		t.Fatal(err)
	}
	rmock.ExpectQuery(q).WithArgs("A", int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if rows, err = r.FindBy(ctx, "FindByProductCode", "A"); err != nil || len(rows) != 0 {
		t.Fatalf("after invalidate: %v %v", err, rows)
	}
	// inside a tx: everything on the tx, replica ignored
	pmock.ExpectBegin()
	pmock.ExpectQuery("/* Warehouse.countAll */ SELECT COUNT(*) FROM medicine_warehouse_master WHERE warehouse_id = ? AND active = 1").
		WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(3))
	pmock.ExpectCommit()
	_ = Transactional(ctx, primary, func(tx *sql.Tx) error { _, err := r.WithTx(tx).Count(ctx); return err })
	for _, m := range []sqlmock.Sqlmock{pmock, rmock} {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	}
}

func TestSQLExplainVerifyUpsert(t *testing.T) {
	r, mock := newRepo(t)
	ctx := context.Background()
	q, err := r.SQL("FindByWarehouseIdOrderByIdDesc", int64(1))
	if err != nil || q.SQL != "SELECT "+cols+" FROM medicine_warehouse_master WHERE warehouse_id = ? AND active = 1 ORDER BY id DESC" {
		t.Errorf("SQL(): %v %s", err, q.SQL)
	}
	mock.ExpectQuery("EXPLAIN SELECT " + cols + " FROM medicine_warehouse_master WHERE warehouse_id = ? AND active = 1").
		WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "select_type", "type", "key"}).AddRow(1, "SIMPLE", "ALL", nil))
	plan, err := r.ExplainBy(ctx, "FindByWarehouseId", int64(1))
	if err != nil || !plan.FullScan || len(plan.Rows) != 1 {
		t.Errorf("explain: %v %+v", err, plan)
	}
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE 1=0").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if err := r.Verify(ctx); err != nil {
		t.Errorf("verify: %v", err)
	}
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE 1=0").WillReturnError(errors.New("Unknown column 'price'"))
	if err := r.Verify(ctx); err == nil {
		t.Error("verify must surface missing columns")
	}
	mock.ExpectExec("INSERT INTO medicine_warehouse_master (product_code, warehouse_id, price, active, updated_by, updated_on) VALUES (?, ?, ?, ?, ?, NOW()) ON DUPLICATE KEY UPDATE price = VALUES(price), active = VALUES(active), updated_by = VALUES(updated_by), updated_on = VALUES(updated_on)").
		WithArgs("A", int64(1), sql.NullFloat64{}, true, int64(0)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.SaveAllUpsert(ctx, []*Warehouse{{ProductCode: "A", WarehouseID: 1, Active: true}}, "ProductCode", "WarehouseId"); err != nil {
		t.Errorf("upsert mysql: %v", err)
	}
	pg := New[Warehouse, int64](r.exec, sqlgen.Postgres)
	mock.ExpectExec(`INSERT INTO medicine_warehouse_master (product_code, warehouse_id, price, active, updated_by, updated_on) VALUES ($1, $2, $3, $4, $5, NOW()) ON CONFLICT (product_code, warehouse_id) DO UPDATE SET price = EXCLUDED.price, active = EXCLUDED.active, updated_by = EXCLUDED.updated_by, updated_on = EXCLUDED.updated_on`).
		WithArgs("A", int64(1), sql.NullFloat64{}, true, int64(0)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := pg.SaveAllUpsert(ctx, []*Warehouse{{ProductCode: "A", WarehouseID: 1, Active: true}}, "ProductCode", "WarehouseId"); err != nil {
		t.Errorf("upsert postgres: %v", err)
	}
	if err := pg.SaveAllUpsert(ctx, nil); err == nil {
		t.Error("postgres upsert without conflict columns must fail")
	}
}

func TestSelectWindow(t *testing.T) {
	r, mock := newRepo(t)
	ctx := context.Background()
	inner := "SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (?, ?) GROUP BY product_code"
	mock.ExpectQuery("SELECT * FROM ("+inner+") q ORDER BY product_code ASC LIMIT 2").
		WithArgs("A", "B").WillReturnRows(sqlmock.NewRows([]string{"product_code", "subs_taken_count"}).AddRow("A", 1).AddRow("B", 2))
	w, err := SelectWindow[soldCount](ctx, r, "w", "SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (:codes) GROUP BY product_code",
		Keyset(1, Asc("ProductCode")), map[string]any{"codes": []string{"A", "B"}})
	if err != nil || len(w.Content) != 1 || !w.HasNext || w.Next.Keys[0] != "A" {
		t.Fatalf("%v %+v", err, w)
	}
	mock.ExpectQuery("SELECT * FROM ("+inner+") q WHERE (product_code > ?) ORDER BY product_code ASC LIMIT 2").
		WithArgs("A", "B", "A").WillReturnRows(sqlmock.NewRows([]string{"product_code", "subs_taken_count"}).AddRow("B", 2))
	w, err = SelectWindow[soldCount](ctx, r, "w", "SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (:codes) GROUP BY product_code",
		w.Next, map[string]any{"codes": []string{"A", "B"}})
	if err != nil || len(w.Content) != 1 || w.HasNext {
		t.Fatalf("%v %+v", err, w)
	}
}

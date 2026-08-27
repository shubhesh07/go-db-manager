package warehouse

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shubhesh07/gojpa/jpa"
	"github.com/shubhesh07/gojpa/sqlgen"
)

const cols = "id, product_code, warehouse_id, availability, is_searchable, supplied_by_tm, mrp, updated_by, updated_on"

func setup(t *testing.T) (Repository, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepository(db, sqlgen.MySQL), mock
}

func TestGenerated(t *testing.T) {
	r, mock := setup(t)
	ctx := context.Background()

	mock.ExpectQuery("SELECT "+cols+" FROM medicine_warehouse_master WHERE product_code IN (?, ?) AND warehouse_id IN (?) AND is_searchable = TRUE").
		WithArgs("A", "B", int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "product_code"}).AddRow(1, "A"))
	rows, err := r.FindByProductCodeInAndWarehouseIdIn(ctx, []string{"A", "B"}, []int64{2})
	if err != nil || len(rows) != 1 || rows[0].ProductCode != "A" {
		t.Fatalf("%v %+v", err, rows)
	}

	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE warehouse_id = ? AND is_searchable = TRUE ORDER BY product_code ASC LIMIT 20").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT(*) FROM medicine_warehouse_master WHERE warehouse_id = ? AND is_searchable = TRUE").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	page, err := r.FindByWarehouseIdOrderByProductCodeAsc(ctx, 2, jpa.PageRequest(0, 20))
	if err != nil || page.Total != 1 {
		t.Fatalf("%v %+v", err, page)
	}

	mock.ExpectQuery("SELECT COUNT(*) FROM medicine_warehouse_master WHERE warehouse_id = ? AND availability = TRUE AND is_searchable = TRUE").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(5))
	if n, err := r.CountByWarehouseIdAndAvailabilityTrue(ctx, 2); err != nil || n != 5 {
		t.Fatalf("%v %d", err, n)
	}

	mock.ExpectExec("UPDATE medicine_warehouse_master SET availability = ?, updated_by = ?, updated_on = NOW() WHERE product_code IN (?, ?) AND warehouse_id = ?").
		WithArgs(false, int64(9), "A", "B", int64(2)).WillReturnResult(sqlmock.NewResult(0, 2))
	if n, err := r.UpdateAvailabilityByProductCodesAndWarehouseID(ctx, false, 9, []string{"A", "B"}, 2); err != nil || n != 2 {
		t.Fatalf("%v %d", err, n)
	}

	mock.ExpectQuery("SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (?) GROUP BY product_code").
		WithArgs("A").WillReturnRows(sqlmock.NewRows([]string{"product_code", "subs_taken_count"}).AddRow("A", 7))
	sc, err := r.GetSoldCounts(ctx, []string{"A"})
	if err != nil || sc[0].Count != 7 {
		t.Fatalf("%v %+v", err, sc)
	}

	mock.ExpectQuery("SELECT COUNT(*) FROM medicine_warehouse_master WHERE warehouse_id = ?").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(11))
	if n, err := r.CountAllInWarehouse(ctx, 2); err != nil || n != 11 {
		t.Fatalf("%v %d", err, n)
	}

	mock.ExpectExec("UPDATE medicine_warehouse_master SET is_searchable = FALSE, updated_on = NOW() WHERE product_code IN (?) AND is_searchable = TRUE").
		WithArgs("A").WillReturnResult(sqlmock.NewResult(0, 1))
	if n, err := r.DeleteByProductCodeIn(ctx, []string{"A"}); err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}

	tr := true
	mrp := 9.5
	mock.ExpectExec("UPDATE medicine_warehouse_master SET availability = CASE WHEN product_code = ? AND warehouse_id = ? THEN ? ELSE availability END, mrp = CASE WHEN product_code = ? AND warehouse_id = ? THEN ? ELSE mrp END, updated_by = ?, updated_on = NOW() WHERE (product_code = ? AND warehouse_id = ?) OR (product_code = ? AND warehouse_id = ?)").
		WithArgs("A", int64(2), true, "B", int64(2), 9.5, int64(9), "A", int64(2), "B", int64(2)).WillReturnResult(sqlmock.NewResult(0, 2))
	n, err := r.UpdateBatch(ctx, 9, []Update{{Key: Key{"A", 2}, Availability: &tr}, {Key: Key{"B", 2}, MRP: &mrp}})
	if err != nil || n != 2 {
		t.Fatalf("%v %d", err, n)
	}
	mock.ExpectQuery("SELECT " + cols + " FROM medicine_warehouse_master WHERE warehouse_id = ? AND is_searchable = TRUE ORDER BY product_code ASC, id ASC LIMIT 2").
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "product_code"}).AddRow(1, "A"))
	w, err := r.FindByWarehouseIdOrderByProductCodeAscIdAsc(ctx, 2, jpa.Keyset(1))
	if err != nil || w.HasNext || len(w.Next.Keys) != 2 {
		t.Fatalf("%v %+v", err, w)
	}

	mock.ExpectQuery("SELECT product_code, MAX(subs_taken_count) AS subs_taken_count FROM product_sold_count WHERE product_code IN (?) GROUP BY product_code LIMIT 5").
		WithArgs("A").WillReturnRows(sqlmock.NewRows([]string{"product_code", "subs_taken_count"}).AddRow("A", 1))
	mock.ExpectQuery("SELECT COUNT(DISTINCT product_code) FROM product_sold_count WHERE product_code IN (?)").
		WithArgs("A").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	if pg, err := r.PageSoldCounts(ctx, []string{"A"}, jpa.PageRequest(0, 5)); err != nil || pg.Total != 1 {
		t.Fatalf("%v %+v", err, pg)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestMockAndLock(t *testing.T) {
	m := &RepositoryMock{CountByWarehouseIdAndAvailabilityTrueFunc: func(ctx context.Context, wh int64) (int64, error) { return 42, nil }}
	var repo Repository = m
	if n, _ := repo.CountByWarehouseIdAndAvailabilityTrue(context.Background(), 1); n != 42 {
		t.Errorf("mock: %d", n)
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = repo.FindByID(context.Background(), 1)
		t.Error("unset mock func must panic")
	}()

	r, mock := setup(t)
	mock.ExpectQuery("SELECT "+cols+" FROM medicine_warehouse_master WHERE product_code = ? AND warehouse_id = ? AND is_searchable = TRUE LIMIT 1 FOR UPDATE").
		WithArgs("A", int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	if _, err := r.FindLockedByProductCodeAndWarehouseId(context.Background(), "A", 2, jpa.ForUpdate); err != nil {
		t.Fatal(err)
	}
}

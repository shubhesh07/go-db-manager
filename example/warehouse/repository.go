package warehouse

import (
	"context"

	"github.com/shubhesh07/gojpa/jpa"
)

//go:generate go run github.com/shubhesh07/gojpa/cmd/jpagen -type Repository

// Repository is written exactly like a Spring Data interface; jpagen emits
// repository_gen.go. UpdateBatch is hand-written in impl.go.
type Repository interface {
	jpa.CRUD[MedicineWarehouse, int64]

	// derived
	FindByProductCodeInAndWarehouseId(ctx context.Context, codes []string, warehouseID int64) ([]MedicineWarehouse, error)
	FindByProductCodeInAndWarehouseIdIn(ctx context.Context, codes []string, warehouseIDs []int64) ([]MedicineWarehouse, error)
	FindByProductCodeAndWarehouseId(ctx context.Context, code string, warehouseID int64) (*MedicineWarehouse, error)
	FindByWarehouseIdOrderByProductCodeAsc(ctx context.Context, warehouseID int64, p jpa.Pageable) (jpa.Page[MedicineWarehouse], error)
	CountByWarehouseIdAndAvailabilityTrue(ctx context.Context, warehouseID int64) (int64, error)
	ExistsByProductCodeAndWarehouseId(ctx context.Context, code string, warehouseID int64) (bool, error)
	DeleteByProductCodeIn(ctx context.Context, codes []string) (int64, error)

	// declared (@Modifying @Query)
	// jpa:query UPDATE medicine_warehouse_master
	//   SET availability = :availability, updated_by = :userID, updated_on = NOW()
	//   WHERE product_code IN (:codes) AND warehouse_id = :warehouseID
	// jpa:modifying
	UpdateAvailabilityByProductCodesAndWarehouseID(ctx context.Context, availability bool, userID int64, codes []string, warehouseID int64) (int64, error)

	// declared (@Query with DTO projection)
	// jpa:query SELECT product_code, MAX(subs_taken_count) AS subs_taken_count
	//   FROM product_sold_count WHERE product_code IN (:codes) GROUP BY product_code
	GetSoldCounts(ctx context.Context, codes []string) ([]SoldCount, error)

	// jpa:query SELECT COUNT(*) FROM medicine_warehouse_master WHERE warehouse_id = :warehouseID
	CountAllInWarehouse(ctx context.Context, warehouseID int64) (int64, error)

	// keyset scrolling (Window)
	FindByWarehouseIdOrderByProductCodeAscIdAsc(ctx context.Context, warehouseID int64, pos jpa.ScrollPosition) (jpa.Window[MedicineWarehouse], error)

	// declared query with paging (@Query + countQuery)
	// jpa:query SELECT product_code, MAX(subs_taken_count) AS subs_taken_count
	//   FROM product_sold_count WHERE product_code IN (:codes) GROUP BY product_code
	// jpa:count SELECT COUNT(DISTINCT product_code) FROM product_sold_count WHERE product_code IN (:codes)
	PageSoldCounts(ctx context.Context, codes []string, p jpa.Pageable) (jpa.Page[SoldCount], error)

	// hand-written fragment (impl.go)
	UpdateBatch(ctx context.Context, userID int64, updates []Update) (int64, error)
}

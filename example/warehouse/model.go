// Package warehouse mirrors catalog-service's medicinewarehousemaster DAL on
// top of gojpa. It is the acceptance test for the generator.
package warehouse

import (
	"database/sql"
	"time"
)

// MedicineWarehouse is one row of medicine_warehouse_master.
type MedicineWarehouse struct {
	ID           int64           `db:"id" orm:"pk,auto"`
	ProductCode  string          `db:"product_code"`
	WarehouseID  int64           `db:"warehouse_id"`
	Availability bool            `db:"availability"`
	IsSearchable bool            `db:"is_searchable"`
	SuppliedByTM bool            `db:"supplied_by_tm"`
	MRP          sql.NullFloat64 `db:"mrp"`
	UpdatedBy    int64           `db:"updated_by" orm:"updated_by"`
	UpdatedOn    time.Time       `db:"updated_on" orm:"updated"`
}

func (*MedicineWarehouse) TableName() string { return "medicine_warehouse_master" }

// SoftDelete: catalog-service treats is_searchable as the "active" flag.
func (*MedicineWarehouse) SoftDelete() (string, string) {
	return "is_searchable = 1", "is_searchable = 0"
}

// Key is the composite business key.
type Key struct {
	ProductCode string
	WarehouseID int64
}

// Update carries per-row changes; nil means "leave unchanged" (CSV blank cell).
type Update struct {
	Key
	Availability *bool
	SuppliedByTM *bool
	MRP          *float64
}

// SoldCount is a DTO projection for an aggregate query.
type SoldCount struct {
	ProductCode string `db:"product_code"`
	Count       int64  `db:"subs_taken_count"`
}

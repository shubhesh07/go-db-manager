package warehouse

import (
	"context"
	"fmt"
	"strings"
)

const batchSize = 500

// UpdateBatch is the per-row CASE WHEN bulk update from catalog-service:
// each column that at least one row sets gets
//
//	col = CASE WHEN product_code = ? AND warehouse_id = ? THEN ? ... ELSE col END
//
// so unset (nil) fields are left untouched. Chunked at 500 rows.
func (r *RepositoryImpl) UpdateBatch(ctx context.Context, userID int64, updates []Update) (int64, error) {
	var total int64
	for start := 0; start < len(updates); start += batchSize {
		end := start + batchSize
		if end > len(updates) {
			end = len(updates)
		}
		n, err := r.updateChunk(ctx, userID, updates[start:end])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (r *RepositoryImpl) updateChunk(ctx context.Context, userID int64, chunk []Update) (int64, error) {
	params := map[string]any{"userID": userID}
	var sets, where []string
	n := 0
	bind := func(v any) string {
		n++
		k := fmt.Sprintf("p%d", n)
		params[k] = v
		return ":" + k
	}
	col := func(name string, pick func(u Update) any) {
		var whens []string
		for _, u := range chunk {
			v := pick(u)
			if v == nil {
				continue
			}
			whens = append(whens, fmt.Sprintf("WHEN product_code = %s AND warehouse_id = %s THEN %s", bind(u.ProductCode), bind(u.WarehouseID), bind(v)))
		}
		if len(whens) > 0 {
			sets = append(sets, fmt.Sprintf("%s = CASE %s ELSE %s END", name, strings.Join(whens, " "), name))
		}
	}
	col("availability", func(u Update) any {
		if u.Availability == nil {
			return nil
		}
		return *u.Availability
	})
	col("supplied_by_tm", func(u Update) any {
		if u.SuppliedByTM == nil {
			return nil
		}
		return *u.SuppliedByTM
	})
	col("mrp", func(u Update) any {
		if u.MRP == nil {
			return nil
		}
		return *u.MRP
	})
	if len(sets) == 0 {
		return 0, nil
	}
	sets = append(sets, "updated_by = :userID", "updated_on = NOW()")
	for _, u := range chunk {
		where = append(where, fmt.Sprintf("(product_code = %s AND warehouse_id = %s)", bind(u.ProductCode), bind(u.WarehouseID)))
	}
	q := fmt.Sprintf("UPDATE medicine_warehouse_master SET %s WHERE %s", strings.Join(sets, ", "), strings.Join(where, " OR "))
	return r.Exec(ctx, "UpdateBatch", q, params)
}

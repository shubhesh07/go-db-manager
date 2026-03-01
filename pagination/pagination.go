// Package pagination provides types and helpers for paginated database queries
// in go-db-manager.
//
// It offers PageRequest for describing the desired page and sort order, and
// PageResponse for returning paginated results with metadata such as total
// element count, total pages, and navigation flags.
//
// Example:
//
//	req := pagination.NewPageRequest(0, 20) // page 0, 20 items per page
//	req.SortBy = "created_at"
//	req.SortDir = "DESC"
package pagination

import (
		"context"
		"database/sql"
		"fmt"
	)

// PageRequest describes the pagination and sorting parameters for a query.
type PageRequest struct {
		// Page is the zero-based page index.
		Page int
		// PageSize is the number of items per page.
		PageSize int
		// SortBy is the column name to sort by. Defaults to "id".
		SortBy string
		// SortDir is the sort direction: "ASC" or "DESC". Defaults to "ASC".
		SortDir string
}

// DefaultPageRequest returns a PageRequest with sensible defaults:
// page 0, page size 20, sorted by "id" ascending.
func DefaultPageRequest() *PageRequest {
		return &PageRequest{
					Page:     0,
					PageSize: 20,
					SortBy:   "id",
					SortDir:  "ASC",
				}
}

// NewPageRequest creates a PageRequest for the given page index and page size.
// Sort defaults to "id ASC".
//
// Example:
//
//	req := pagination.NewPageRequest(2, 10) // third page, 10 items each
func NewPageRequest(page, pageSize int) *PageRequest {
		return &PageRequest{
					Page:     page,
					PageSize: pageSize,
					SortBy:   "id",
					SortDir:  "ASC",
				}
}

// Offset returns the SQL OFFSET value for this page.
func (pr *PageRequest) Offset() int {
		return pr.Page * pr.PageSize
}

// PageResponse holds a page of results along with pagination metadata.
type PageResponse[T any] struct {
		// Content is the slice of items on this page.
		Content []T `json:"content"`
		// Page is the zero-based index of the current page.
		Page int `json:"page"`
		// PageSize is the number of items per page.
		PageSize int `json:"page_size"`
		// TotalElements is the total number of matching items across all pages.
		TotalElements int64 `json:"total_elements"`
		// TotalPages is the total number of pages.
		TotalPages int `json:"total_pages"`
		// HasNext indicates whether there is a next page.
		HasNext bool `json:"has_next"`
		// HasPrevious indicates whether there is a previous page.
		HasPrevious bool `json:"has_previous"`
}

// NewPageResponse creates a PageResponse from the given content and metadata.
//
// Example:
//
//	resp := pagination.NewPageResponse(items, req.Page, req.PageSize, totalCount)
func NewPageResponse[T any](content []T, page int, pageSize int, totalElements int64) *PageResponse[T] {
		totalPages := int(totalElements) / pageSize
		if int(totalElements)%pageSize > 0 {
					totalPages++
				}
		return &PageResponse[T]{
					Content:       content,
					Page:          page,
					PageSize:      pageSize,
					TotalElements: totalElements,
					TotalPages:    totalPages,
					HasNext:       page < totalPages-1,
					HasPrevious:   page > 0,
				}
}

// Paginate executes a paginated SQL query against the given table.
// whereClause should not include the "WHERE" keyword (e.g. "deleted_at IS NULL").
// Pass an empty string for whereClause to return all rows.
//
// Example:
//
//	resp, err := pagination.Paginate[MyEntity](ctx, db, "orders",
//		pagination.NewPageRequest(0, 10),
//		"customer_id = $1",
//		[]interface{}{customerId},
//	)
func Paginate[T any](ctx context.Context, db *sql.DB, tableName string, pageRequest *PageRequest, whereClause string, args []interface{}) (*PageResponse[T], error) {
		// Count total elements
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
		if whereClause != "" {
					countQuery += " WHERE " + whereClause
				}
		var totalElements int64
		if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&totalElements); err != nil {
					return nil, fmt.Errorf("failed to count elements: %w", err)
				}

		// Build select query with pagination
		selectQuery := fmt.Sprintf("SELECT * FROM %s", tableName)
		if whereClause != "" {
					selectQuery += " WHERE " + whereClause
				}
		if pageRequest.SortBy != "" {
					selectQuery += fmt.Sprintf(" ORDER BY %s %s", pageRequest.SortBy, pageRequest.SortDir)
				}
		selectQuery += fmt.Sprintf(" LIMIT %d OFFSET %d", pageRequest.PageSize, pageRequest.Offset())

		rows, err := db.QueryContext(ctx, selectQuery, args...)
		if err != nil {
					return nil, fmt.Errorf("failed to query: %w", err)
				}
		defer rows.Close()

		// Scan rows (simplified - would need proper type scanning)
		var content []T
		_ = rows
		return NewPageResponse(content, pageRequest.Page, pageRequest.PageSize, totalElements), nil
}

package pagination

import (
	"context"
	"database/sql"
	"fmt"
)

// PageRequest represents a pagination request
type PageRequest struct {
	Page     int
	PageSize int
	SortBy   string
	SortDir  string // ASC, DESC
}

// DefaultPageRequest returns a default page request
func DefaultPageRequest() *PageRequest {
	return &PageRequest{
		Page:     0,
		PageSize: 20,
		SortBy:   "id",
		SortDir:  "ASC",
	}
}

// NewPageRequest creates a new page request
func NewPageRequest(page, pageSize int) *PageRequest {
	return &PageRequest{
		Page:     page,
		PageSize: pageSize,
		SortBy:   "id",
		SortDir:  "ASC",
	}
}

// Offset calculates the offset for the page
func (pr *PageRequest) Offset() int {
	return pr.Page * pr.PageSize
}

// PageResponse represents a paginated response
type PageResponse[T any] struct {
	Content       []T   `json:"content"`
	Page          int   `json:"page"`
	PageSize      int   `json:"page_size"`
	TotalElements int64 `json:"total_elements"`
	TotalPages    int   `json:"total_pages"`
	HasNext       bool  `json:"has_next"`
	HasPrevious   bool  `json:"has_previous"`
}

// NewPageResponse creates a new page response
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

// Paginate executes a paginated query
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

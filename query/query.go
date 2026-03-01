// Package query provides a fluent SQL query builder for go-db-manager.
//
// It allows constructing type-safe SQL queries programmatically using
// a chainable API. Supports SELECT, WHERE, JOIN, ORDER BY, GROUP BY,
// HAVING, LIMIT, and OFFSET clauses.
//
// Example:
//
//	sql, args := query.NewQueryBuilder("orders").
//		Select("id", "customer_id", "total").
//		Where("status", "=", "active").
//		OrderBy("created_at", "DESC").
//		Limit(10).
//		Build()
package query

import (
		"context"
		"database/sql"
		"fmt"
		"strings"
	)

// QueryBuilder provides a fluent interface for building SQL queries.
// Use NewQueryBuilder to create a new instance.
type QueryBuilder struct {
		table      string
		selectCols []string
		where      []condition
		joins      []join
		orderBy    []order
		groupBy    []string
		having     []condition
		limitVal   *int
		offsetVal  *int
		args       []interface{}
		argIndex   int
}

type condition struct {
		column   string
		operator string
		value    interface{}
		logic    string // AND, OR
}

type join struct {
		table     string
		condition string
		joinType  string // INNER, LEFT, RIGHT, FULL
}

type order struct {
		column string
		dir    string // ASC, DESC
}

// NewQueryBuilder creates a new QueryBuilder targeting the given table.
//
// Example:
//
//	qb := query.NewQueryBuilder("users")
func NewQueryBuilder(table string) *QueryBuilder {
		return &QueryBuilder{
					table:      table,
					selectCols: []string{"*"},
					where:      []condition{},
					joins:      []join{},
					orderBy:    []order{},
					groupBy:    []string{},
					having:     []condition{},
					args:       []interface{}{},
					argIndex:   1,
				}
}

// Select specifies which columns to include in the SELECT clause.
// Defaults to "*" if not called.
//
// Example:
//
//	qb.Select("id", "name", "email")
func (qb *QueryBuilder) Select(columns ...string) *QueryBuilder {
		qb.selectCols = columns
		return qb
}

// Where adds an AND WHERE condition. Supported operators: =, !=, >, >=, <, <=, LIKE.
//
// Example:
//
//	qb.Where("status", "=", "active").Where("age", ">", 18)
func (qb *QueryBuilder) Where(column string, operator string, value interface{}) *QueryBuilder {
		qb.where = append(qb.where, condition{
					column:   column,
					operator: operator,
					value:    value,
					logic:    "AND",
				})
		qb.args = append(qb.args, value)
		qb.argIndex++
		return qb
}

// OrWhere adds an OR WHERE condition.
//
// Example:
//
//	qb.Where("status", "=", "active").OrWhere("status", "=", "pending")
func (qb *QueryBuilder) OrWhere(column string, operator string, value interface{}) *QueryBuilder {
		qb.where = append(qb.where, condition{
					column:   column,
					operator: operator,
					value:    value,
					logic:    "OR",
				})
		qb.args = append(qb.args, value)
		qb.argIndex++
		return qb
}

// WhereIn adds a WHERE column IN (...) condition.
//
// Example:
//
//	qb.WhereIn("status_id", []interface{}{1, 2, 3})
func (qb *QueryBuilder) WhereIn(column string, values []interface{}) *QueryBuilder {
		placeholders := make([]string, len(values))
		for i, v := range values {
					placeholders[i] = fmt.Sprintf("$%d", qb.argIndex)
					qb.args = append(qb.args, v)
					qb.argIndex++
				}
		qb.where = append(qb.where, condition{
					column:   column,
					operator: fmt.Sprintf("IN (%s)", strings.Join(placeholders, ", ")),
					value:    nil,
					logic:    "AND",
				})
		return qb
}

// WhereNull adds a WHERE column IS NULL condition.
//
// Example:
//
//	qb.WhereNull("deleted_at")
func (qb *QueryBuilder) WhereNull(column string) *QueryBuilder {
		qb.where = append(qb.where, condition{
					column:   column,
					operator: "IS NULL",
					value:    nil,
					logic:    "AND",
				})
		return qb
}

// WhereNotNull adds a WHERE column IS NOT NULL condition.
//
// Example:
//
//	qb.WhereNotNull("email")
func (qb *QueryBuilder) WhereNotNull(column string) *QueryBuilder {
		qb.where = append(qb.where, condition{
					column:   column,
					operator: "IS NOT NULL",
					value:    nil,
					logic:    "AND",
				})
		return qb
}

// Join adds an INNER JOIN clause.
//
// Example:
//
//	qb.Join("order_items", "orders.id = order_items.order_id")
func (qb *QueryBuilder) Join(table string, condition string) *QueryBuilder {
		qb.joins = append(qb.joins, join{
					table:     table,
					condition: condition,
					joinType:  "INNER",
				})
		return qb
}

// LeftJoin adds a LEFT JOIN clause.
//
// Example:
//
//	qb.LeftJoin("profiles", "users.id = profiles.user_id")
func (qb *QueryBuilder) LeftJoin(table string, condition string) *QueryBuilder {
		qb.joins = append(qb.joins, join{
					table:     table,
					condition: condition,
					joinType:  "LEFT",
				})
		return qb
}

// RightJoin adds a RIGHT JOIN clause.
func (qb *QueryBuilder) RightJoin(table string, condition string) *QueryBuilder {
		qb.joins = append(qb.joins, join{
					table:     table,
					condition: condition,
					joinType:  "RIGHT",
				})
		return qb
}

// OrderBy adds an ORDER BY clause. direction should be "ASC" or "DESC".
//
// Example:
//
//	qb.OrderBy("created_at", "DESC")
func (qb *QueryBuilder) OrderBy(column string, direction string) *QueryBuilder {
		qb.orderBy = append(qb.orderBy, order{
					column: column,
					dir:    strings.ToUpper(direction),
				})
		return qb
}

// GroupBy adds a GROUP BY clause.
//
// Example:
//
//	qb.GroupBy("customer_id", "status")
func (qb *QueryBuilder) GroupBy(columns ...string) *QueryBuilder {
		qb.groupBy = append(qb.groupBy, columns...)
		return qb
}

// Having adds a HAVING condition, typically used after GroupBy.
//
// Example:
//
//	qb.GroupBy("customer_id").Having("COUNT(*)", ">", 5)
func (qb *QueryBuilder) Having(column string, operator string, value interface{}) *QueryBuilder {
		qb.having = append(qb.having, condition{
					column:   column,
					operator: operator,
					value:    value,
					logic:    "AND",
				})
		qb.args = append(qb.args, value)
		qb.argIndex++
		return qb
}

// Limit sets the maximum number of rows to return.
//
// Example:
//
//	qb.Limit(10)
func (qb *QueryBuilder) Limit(limit int) *QueryBuilder {
		qb.limitVal = &limit
		return qb
}

// Offset sets the number of rows to skip. Use with Limit for pagination.
//
// Example:
//
//	qb.Limit(10).Offset(20) // third page
func (qb *QueryBuilder) Offset(offset int) *QueryBuilder {
		qb.offsetVal = &offset
		return qb
}

// Build constructs and returns the final SQL string and its positional arguments.
// Placeholders use the $1, $2, ... format (PostgreSQL compatible).
//
// Example:
//
//	sql, args := qb.Build()
//	rows, err := db.QueryContext(ctx, sql, args...)
func (qb *QueryBuilder) Build() (string, []interface{}) {
		var parts []string

		// SELECT
		parts = append(parts, fmt.Sprintf("SELECT %s FROM %s",
										  		strings.Join(qb.selectCols, ", "), qb.table))

		// JOINs
		for _, j := range qb.joins {
					parts = append(parts, fmt.Sprintf("%s JOIN %s ON %s", j.joinType, j.table, j.condition))
				}

		// WHERE
		if len(qb.where) > 0 {
					var whereParts []string
					for i, w := range qb.where {
									if i == 0 {
														whereParts = append(whereParts, qb.buildCondition(w))
													} else {
														whereParts = append(whereParts, fmt.Sprintf("%s %s", w.logic, qb.buildCondition(w)))
													}
								}
					parts = append(parts, fmt.Sprintf("WHERE %s", strings.Join(whereParts, " ")))
				}

		// GROUP BY
		if len(qb.groupBy) > 0 {
					parts = append(parts, fmt.Sprintf("GROUP BY %s", strings.Join(qb.groupBy, ", ")))
				}

		// HAVING
		if len(qb.having) > 0 {
					var havingParts []string
					for i, h := range qb.having {
									if i == 0 {
														havingParts = append(havingParts, qb.buildCondition(h))
													} else {
														havingParts = append(havingParts, fmt.Sprintf("%s %s", h.logic, qb.buildCondition(h)))
													}
								}
					parts = append(parts, fmt.Sprintf("HAVING %s", strings.Join(havingParts, " ")))
				}

		// ORDER BY
		if len(qb.orderBy) > 0 {
					var orderParts []string
					for _, o := range qb.orderBy {
									orderParts = append(orderParts, fmt.Sprintf("%s %s", o.column, o.dir))
								}
					parts = append(parts, fmt.Sprintf("ORDER BY %s", strings.Join(orderParts, ", ")))
				}

		// LIMIT
		if qb.limitVal != nil {
					parts = append(parts, fmt.Sprintf("LIMIT %d", *qb.limitVal))
				}

		// OFFSET
		if qb.offsetVal != nil {
					parts = append(parts, fmt.Sprintf("OFFSET %d", *qb.offsetVal))
				}

		return strings.Join(parts, " "), qb.args
}

func (qb *QueryBuilder) buildCondition(c condition) string {
		if c.operator == "IS NULL" || c.operator == "IS NOT NULL" {
					return fmt.Sprintf("%s %s", c.column, c.operator)
				}
		if strings.Contains(c.operator, "IN") {
					return fmt.Sprintf("%s %s", c.column, c.operator)
				}
		argIndex := len(qb.args)
		for i, arg := range qb.args {
					if arg == c.value {
									argIndex = i + 1
									break
								}
				}
		return fmt.Sprintf("%s %s $%d", c.column, c.operator, argIndex)
}

// Execute builds and runs the query, returning all matching rows.
// The caller must close the returned *sql.Rows.
//
// Example:
//
//	rows, err := qb.Execute(ctx, db)
//	if err != nil { ... }
//	defer rows.Close()
func (qb *QueryBuilder) Execute(ctx context.Context, db *sql.DB) (*sql.Rows, error) {
		query, args := qb.Build()
		return db.QueryContext(ctx, query, args...)
}

// ExecuteRow builds and runs the query, returning a single row.
// Use this when you expect exactly one result.
//
// Example:
//
//	row := qb.Where("id", "=", 1).ExecuteRow(ctx, db)
func (qb *QueryBuilder) ExecuteRow(ctx context.Context, db *sql.DB) *sql.Row {
		query, args := qb.Build()
		return db.QueryRowContext(ctx, query, args...)
}

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// QueryBuilder provides a fluent interface for building SQL queries
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

// NewQueryBuilder creates a new query builder
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

// Select specifies columns to select
func (qb *QueryBuilder) Select(columns ...string) *QueryBuilder {
	qb.selectCols = columns
	return qb
}

// Where adds a WHERE condition
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

// OrWhere adds an OR WHERE condition
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

// WhereIn adds a WHERE IN condition
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

// WhereNull adds a WHERE IS NULL condition
func (qb *QueryBuilder) WhereNull(column string) *QueryBuilder {
	qb.where = append(qb.where, condition{
		column:   column,
		operator: "IS NULL",
		value:    nil,
		logic:    "AND",
	})
	return qb
}

// WhereNotNull adds a WHERE IS NOT NULL condition
func (qb *QueryBuilder) WhereNotNull(column string) *QueryBuilder {
	qb.where = append(qb.where, condition{
		column:   column,
		operator: "IS NOT NULL",
		value:    nil,
		logic:    "AND",
	})
	return qb
}

// Join adds an INNER JOIN
func (qb *QueryBuilder) Join(table string, condition string) *QueryBuilder {
	qb.joins = append(qb.joins, join{
		table:     table,
		condition: condition,
		joinType:  "INNER",
	})
	return qb
}

// LeftJoin adds a LEFT JOIN
func (qb *QueryBuilder) LeftJoin(table string, condition string) *QueryBuilder {
	qb.joins = append(qb.joins, join{
		table:     table,
		condition: condition,
		joinType:  "LEFT",
	})
	return qb
}

// RightJoin adds a RIGHT JOIN
func (qb *QueryBuilder) RightJoin(table string, condition string) *QueryBuilder {
	qb.joins = append(qb.joins, join{
		table:     table,
		condition: condition,
		joinType:  "RIGHT",
	})
	return qb
}

// OrderBy adds an ORDER BY clause
func (qb *QueryBuilder) OrderBy(column string, direction string) *QueryBuilder {
	qb.orderBy = append(qb.orderBy, order{
		column: column,
		dir:    strings.ToUpper(direction),
	})
	return qb
}

// GroupBy adds a GROUP BY clause
func (qb *QueryBuilder) GroupBy(columns ...string) *QueryBuilder {
	qb.groupBy = append(qb.groupBy, columns...)
	return qb
}

// Having adds a HAVING condition
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

// Limit sets the LIMIT clause
func (qb *QueryBuilder) Limit(limit int) *QueryBuilder {
	qb.limitVal = &limit
	return qb
}

// Offset sets the OFFSET clause
func (qb *QueryBuilder) Offset(offset int) *QueryBuilder {
	qb.offsetVal = &offset
	return qb
}

// Build constructs the SQL query string
func (qb *QueryBuilder) Build() (string, []interface{}) {
	var parts []string

	// SELECT
	parts = append(parts, fmt.Sprintf("SELECT %s FROM %s", strings.Join(qb.selectCols, ", "), qb.table))

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

// Execute executes the query and returns rows
func (qb *QueryBuilder) Execute(ctx context.Context, db *sql.DB) (*sql.Rows, error) {
	query, args := qb.Build()
	return db.QueryContext(ctx, query, args...)
}

// ExecuteRow executes the query and returns a single row
func (qb *QueryBuilder) ExecuteRow(ctx context.Context, db *sql.DB) *sql.Row {
	query, args := qb.Build()
	return db.QueryRowContext(ctx, query, args...)
}

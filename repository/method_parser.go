package repository

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// QueryPart represents a part of a parsed query
type QueryPart struct {
	Field     string
	Operator  string
	Logic     string // AND, OR
	IsNot     bool
	IsNull    bool
	IsNotNull bool
}

// ParsedQuery represents a fully parsed method name query
type ParsedQuery struct {
	Action      string // find, count, exists, delete
	Limit       int    // for Top/First
	OrderBy     []OrderClause
	WhereClause []QueryPart
	ReturnType  reflect.Type
}

// OrderClause represents an ORDER BY clause
type OrderClause struct {
	Field     string
	Direction string // ASC, DESC
}

// MethodParser parses Spring JPA-style method names
type MethodParser struct {
	entityType reflect.Type
	tableName  string
}

// NewMethodParser creates a new method parser
func NewMethodParser(entityType reflect.Type, tableName string) *MethodParser {
	return &MethodParser{
		entityType: entityType,
		tableName:  tableName,
	}
}

// ParseMethodName parses a Spring JPA-style method name into a SQL query
func (mp *MethodParser) ParseMethodName(methodName string, args []interface{}) (*ParsedQuery, error) {
	query := &ParsedQuery{
		WhereClause: []QueryPart{},
		OrderBy:     []OrderClause{},
	}

	// Extract action (find, count, exists, delete)
	action, remaining := mp.extractAction(methodName)
	query.Action = action

	// Extract limit (Top, First)
	limit, remaining := mp.extractLimit(remaining)
	query.Limit = limit

	// Extract OrderBy clause
	orderBy, remaining := mp.extractOrderBy(remaining)
	query.OrderBy = orderBy

	// Parse WHERE clause
	whereClause, err := mp.parseWhereClause(remaining, args)
	if err != nil {
		return nil, err
	}
	query.WhereClause = whereClause

	return query, nil
}

// extractAction extracts the action from method name
func (mp *MethodParser) extractAction(methodName string) (string, string) {
	methodName = strings.ToLower(methodName)

	if strings.HasPrefix(methodName, "find") {
		return "find", strings.TrimPrefix(methodName, "find")
	}
	if strings.HasPrefix(methodName, "count") {
		return "count", strings.TrimPrefix(methodName, "count")
	}
	if strings.HasPrefix(methodName, "exists") {
		return "exists", strings.TrimPrefix(methodName, "exists")
	}
	if strings.HasPrefix(methodName, "delete") {
		return "delete", strings.TrimPrefix(methodName, "delete")
	}

	return "find", methodName
}

// extractLimit extracts Top/First limit
func (mp *MethodParser) extractLimit(methodName string) (int, string) {
	// Check for TopN or FirstN
	re := regexp.MustCompile(`(?i)^(top|first)(\d+)?`)
	matches := re.FindStringSubmatch(methodName)

	if len(matches) > 0 {
		limit := 1 // default
		if len(matches) > 2 && matches[2] != "" {
			fmt.Sscanf(matches[2], "%d", &limit)
		}
		remaining := strings.TrimPrefix(methodName, matches[0])
		remaining = strings.TrimPrefix(remaining, "by")
		return limit, remaining
	}

	return 0, methodName
}

// extractOrderBy extracts OrderBy clauses
func (mp *MethodParser) extractOrderBy(methodName string) ([]OrderClause, string) {
	var orderBy []OrderClause

	// Look for OrderBy pattern
	re := regexp.MustCompile(`(?i)orderby([a-z0-9]+)(asc|desc)?`)
	matches := re.FindAllStringSubmatch(methodName, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			field := mp.toSnakeCase(match[1])
			direction := "ASC"
			if len(match) >= 3 && strings.ToUpper(match[2]) == "DESC" {
				direction = "DESC"
			}
			orderBy = append(orderBy, OrderClause{
				Field:     field,
				Direction: direction,
			})
		}
	}

	// Remove OrderBy from method name
	methodName = re.ReplaceAllString(methodName, "")

	return orderBy, methodName
}

// parseWhereClause parses the WHERE clause from method name
func (mp *MethodParser) parseWhereClause(methodName string, args []interface{}) ([]QueryPart, error) {
	var parts []QueryPart

	// Remove "By" prefix if present
	methodName = strings.TrimPrefix(strings.ToLower(methodName), "by")
	if methodName == "" {
		return parts, nil
	}

	// Split by And/Or
	clauses := mp.splitByLogic(methodName)

	argIndex := 0
	for _, clause := range clauses {
		part, consumedArgs, err := mp.parseClause(clause, args, argIndex)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
		argIndex += consumedArgs
	}

	return parts, nil
}

// splitByLogic splits method name by And/Or
func (mp *MethodParser) splitByLogic(methodName string) []string {
	var clauses []string
	var current strings.Builder

	upper := strings.ToUpper(methodName)
	i := 0

	for i < len(methodName) {
		if i+3 < len(upper) && upper[i:i+3] == "AND" {
			if current.Len() > 0 {
				clauses = append(clauses, current.String())
				current.Reset()
			}
			i += 3
			continue
		}
		if i+2 < len(upper) && upper[i:i+2] == "OR" {
			if current.Len() > 0 {
				clauses = append(clauses, current.String())
				current.Reset()
			}
			i += 2
			continue
		}
		current.WriteByte(methodName[i])
		i++
	}

	if current.Len() > 0 {
		clauses = append(clauses, current.String())
	}

	return clauses
}

// parseClause parses a single clause (e.g., "OrderId", "CustomerIdIn", "StatusIdNotIn")
func (mp *MethodParser) parseClause(clause string, args []interface{}, argIndex int) (QueryPart, int, error) {
	part := QueryPart{
		Logic: "AND",
	}

	clause = strings.TrimSpace(clause)

	// Check for IsNull/IsNotNull
	if strings.HasSuffix(strings.ToLower(clause), "isnull") {
		field := strings.TrimSuffix(clause, "IsNull")
		part.Field = mp.toSnakeCase(field)
		part.IsNull = true
		part.Operator = "IS NULL"
		return part, 0, nil
	}

	if strings.HasSuffix(strings.ToLower(clause), "isnotnull") {
		field := strings.TrimSuffix(clause, "IsNotNull")
		part.Field = mp.toSnakeCase(field)
		part.IsNotNull = true
		part.Operator = "IS NOT NULL"
		return part, 0, nil
	}

	// Check for NotIn
	if strings.HasSuffix(strings.ToLower(clause), "notin") {
		field := strings.TrimSuffix(clause, "NotIn")
		part.Field = mp.toSnakeCase(field)
		part.Operator = "NOT IN"
		part.IsNot = true
		if argIndex < len(args) {
			// Count elements in slice/array
			argValue := reflect.ValueOf(args[argIndex])
			if argValue.Kind() == reflect.Slice || argValue.Kind() == reflect.Array {
				return part, 1, nil
			}
		}
		return part, 1, nil
	}

	// Check for In
	if strings.HasSuffix(strings.ToLower(clause), "in") {
		field := strings.TrimSuffix(clause, "In")
		part.Field = mp.toSnakeCase(field)
		part.Operator = "IN"
		if argIndex < len(args) {
			return part, 1, nil
		}
		return part, 1, nil
	}

	// Check for Not
	if strings.HasPrefix(strings.ToLower(clause), "not") {
		part.IsNot = true
		clause = clause[3:]
	}

	// Check for comparison operators
	operators := map[string]string{
		"GreaterThanEqual": ">=",
		"GreaterThan":      ">",
		"LessThanEqual":    "<=",
		"LessThan":         "<",
		"Equal":            "=",
		"NotEqual":         "!=",
		"Like":             "LIKE",
		"NotLike":          "NOT LIKE",
		"Between":          "BETWEEN",
		"StartingWith":     "LIKE",
		"EndingWith":       "LIKE",
		"Containing":       "LIKE",
	}

	for opName, opSymbol := range operators {
		if strings.HasSuffix(clause, opName) {
			field := strings.TrimSuffix(clause, opName)
			part.Field = mp.toSnakeCase(field)
			part.Operator = opSymbol

			// Special handling for Like operators
			if opSymbol == "LIKE" {
				if strings.HasSuffix(clause, "StartingWith") {
					// Will add % at end
				} else if strings.HasSuffix(clause, "EndingWith") {
					// Will add % at start
				} else if strings.HasSuffix(clause, "Containing") {
					// Will add % at both ends
				}
			}

			return part, 1, nil
		}
	}

	// Default: equality
	part.Field = mp.toSnakeCase(clause)
	part.Operator = "="
	return part, 1, nil
}

// BuildSQL builds SQL query from parsed query
func (mp *MethodParser) BuildSQL(parsed *ParsedQuery, args []interface{}) (string, []interface{}) {
	var sql strings.Builder
	var sqlArgs []interface{}
	argIndex := 1

	switch parsed.Action {
	case "find", "exists":
		sql.WriteString("SELECT * FROM ")
	case "count":
		sql.WriteString("SELECT COUNT(*) FROM ")
	case "delete":
		sql.WriteString("UPDATE ")
	}

	sql.WriteString(mp.tableName)

	// Build WHERE clause
	if len(parsed.WhereClause) > 0 {
		sql.WriteString(" WHERE ")
		argIndex = mp.buildWhereClause(&sql, parsed.WhereClause, args, &sqlArgs, argIndex)
		sql.WriteString(" AND deleted_at IS NULL")
	} else {
		sql.WriteString(" WHERE deleted_at IS NULL")
	}

	// Build ORDER BY
	if len(parsed.OrderBy) > 0 {
		sql.WriteString(" ORDER BY ")
		var orderParts []string
		for _, order := range parsed.OrderBy {
			orderParts = append(orderParts, fmt.Sprintf("%s %s", order.Field, order.Direction))
		}
		sql.WriteString(strings.Join(orderParts, ", "))
	}

	// Build LIMIT
	if parsed.Limit > 0 {
		sql.WriteString(fmt.Sprintf(" LIMIT %d", parsed.Limit))
	}

	// For delete, add SET clause
	if parsed.Action == "delete" {
		// This would be handled differently - soft delete
		sql.Reset()
		sql.WriteString(fmt.Sprintf("UPDATE %s SET deleted_at = $%d WHERE ", mp.tableName, argIndex))
		sqlArgs = append(sqlArgs, "NOW()")
		argIndex++
		argIndex = mp.buildWhereClause(&sql, parsed.WhereClause, args, &sqlArgs, argIndex)
		sql.WriteString(" AND deleted_at IS NULL")
	}

	return sql.String(), sqlArgs
}

// buildWhereClause builds WHERE clause from query parts
func (mp *MethodParser) buildWhereClause(sql *strings.Builder, parts []QueryPart, args []interface{}, sqlArgs *[]interface{}, argIndex int) int {
	currentArgIndex := 0

	for i, part := range parts {
		if i > 0 {
			sql.WriteString(" " + part.Logic + " ")
		}

		if part.IsNull {
			sql.WriteString(fmt.Sprintf("%s IS NULL", part.Field))
		} else if part.IsNotNull {
			sql.WriteString(fmt.Sprintf("%s IS NOT NULL", part.Field))
		} else if part.Operator == "IN" || part.Operator == "NOT IN" {
			if currentArgIndex < len(args) {
				arg := args[currentArgIndex]
				argValue := reflect.ValueOf(arg)

				if argValue.Kind() == reflect.Slice || argValue.Kind() == reflect.Array {
					var placeholders []string
					for j := 0; j < argValue.Len(); j++ {
						placeholders = append(placeholders, fmt.Sprintf("$%d", argIndex))
						*sqlArgs = append(*sqlArgs, argValue.Index(j).Interface())
						argIndex++
					}
					sql.WriteString(fmt.Sprintf("%s %s (%s)", part.Field, part.Operator, strings.Join(placeholders, ", ")))
				} else {
					sql.WriteString(fmt.Sprintf("%s %s ($%d)", part.Field, part.Operator, argIndex))
					*sqlArgs = append(*sqlArgs, arg)
					argIndex++
				}
				currentArgIndex++
			}
		} else if part.Operator == "LIKE" {
			if currentArgIndex < len(args) {
				arg := args[currentArgIndex]
				argStr := fmt.Sprintf("%v", arg)

				// Handle Like variations
				if strings.Contains(part.Field, "StartingWith") {
					argStr = argStr + "%"
				} else if strings.Contains(part.Field, "EndingWith") {
					argStr = "%" + argStr
				} else if strings.Contains(part.Field, "Containing") {
					argStr = "%" + argStr + "%"
				}

				sql.WriteString(fmt.Sprintf("%s %s $%d", part.Field, part.Operator, argIndex))
				*sqlArgs = append(*sqlArgs, argStr)
				argIndex++
				currentArgIndex++
			}
		} else if part.Operator == "BETWEEN" {
			if currentArgIndex+1 < len(args) {
				sql.WriteString(fmt.Sprintf("%s BETWEEN $%d AND $%d", part.Field, argIndex, argIndex+1))
				*sqlArgs = append(*sqlArgs, args[currentArgIndex], args[currentArgIndex+1])
				argIndex += 2
				currentArgIndex += 2
			}
		} else {
			if currentArgIndex < len(args) {
				sql.WriteString(fmt.Sprintf("%s %s $%d", part.Field, part.Operator, argIndex))
				*sqlArgs = append(*sqlArgs, args[currentArgIndex])
				argIndex++
				currentArgIndex++
			}
		}
	}

	return argIndex
}

// toSnakeCase converts CamelCase to snake_case
func (mp *MethodParser) toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

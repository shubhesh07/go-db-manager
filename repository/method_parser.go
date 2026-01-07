package repository

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/shubhesh07/go-db-manager/connection"
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
	entityType   reflect.Type
	tableName    string
	dbType       string // "postgres", "mysql", "sqlite3"
	hasDeletedAt bool   // Whether entity has deleted_at field for soft deletes
	deletedAtCol string // Column name for deleted_at (default: "deleted_at")
}

// NewMethodParser creates a new method parser
func NewMethodParser(entityType reflect.Type, tableName string) *MethodParser {
	// Get DB type from connection manager
	connMgr := connection.GetInstance()
	dbType := string(connMgr.GetDBType())
	if dbType == "" {
		dbType = "postgres" // Default
	}

	// Check if entity has deleted_at field for soft deletes
	hasDeletedAt, deletedAtCol := checkDeletedAtField(entityType)

	return &MethodParser{
		entityType:   entityType,
		tableName:    tableName,
		dbType:       dbType,
		hasDeletedAt: hasDeletedAt,
		deletedAtCol: deletedAtCol,
	}
}

// checkDeletedAtField checks if the entity has a deleted_at field and returns its column name
func checkDeletedAtField(entityType reflect.Type) (bool, string) {
	if entityType == nil {
		return false, ""
	}

	t := entityType
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Search for deleted_at field (case-insensitive)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Check field name (case-insensitive)
		if strings.EqualFold(field.Name, "DeletedAt") {
			// Get column name from tag
			tag := field.Tag.Get("orm")
			if tag != "" && tag != "-" {
				parts := strings.Split(tag, ";")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if strings.HasPrefix(part, "column:") {
						colName := strings.TrimPrefix(part, "column:")
						// Remove any additional attributes after column name
						if idx := strings.Index(colName, ";"); idx >= 0 {
							colName = colName[:idx]
						}
						if idx := strings.Index(colName, " "); idx >= 0 {
							colName = colName[:idx]
						}
						return true, strings.TrimSpace(colName)
					}
				}
			}
			// No column tag, use snake_case of field name
			return true, "deleted_at"
		}

		// Also check embedded structs (like BaseEntity)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			hasDeletedAt, deletedAtCol := checkDeletedAtField(field.Type)
			if hasDeletedAt {
				return true, deletedAtCol
			}
		}
	}

	return false, ""
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
			field := mp.getColumnNameFromField(match[1])
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

	// Check for True/False (e.g., ActiveTrue, ActiveFalse)
	// Must check before other suffixes to avoid false matches
	clauseLower := strings.ToLower(clause)
	if strings.HasSuffix(clauseLower, "true") && len(clause) > 4 {
		field := strings.TrimSuffix(clause, "True")
		field = strings.TrimSuffix(field, "true")                // Handle both cases
		part.Field = mp.getColumnNameFromField(field) + "::true" // Use :: as marker
		part.Operator = "="
		// We'll set the value to true in buildWhereClause
		return part, 0, nil
	}

	if strings.HasSuffix(clauseLower, "false") && len(clause) > 5 {
		field := strings.TrimSuffix(clause, "False")
		field = strings.TrimSuffix(field, "false")                // Handle both cases
		part.Field = mp.getColumnNameFromField(field) + "::false" // Use :: as marker
		part.Operator = "="
		// We'll set the value to false in buildWhereClause
		return part, 0, nil
	}

	// Check for IsNull/IsNotNull
	if strings.HasSuffix(strings.ToLower(clause), "isnull") {
		field := strings.TrimSuffix(clause, "IsNull")
		part.Field = mp.getColumnNameFromField(field)
		part.IsNull = true
		part.Operator = "IS NULL"
		return part, 0, nil
	}

	if strings.HasSuffix(strings.ToLower(clause), "isnotnull") {
		field := strings.TrimSuffix(clause, "IsNotNull")
		part.Field = mp.getColumnNameFromField(field)
		part.IsNotNull = true
		part.Operator = "IS NOT NULL"
		return part, 0, nil
	}

	// Check for NotIn
	if strings.HasSuffix(strings.ToLower(clause), "notin") {
		field := strings.TrimSuffix(clause, "NotIn")
		part.Field = mp.getColumnNameFromField(field)
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
		part.Field = mp.getColumnNameFromField(field)
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
			part.Field = mp.getColumnNameFromField(field)
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
	// Special handling for "id" - convert to "ID" for field matching
	fieldName := clause
	if strings.ToLower(clause) == "id" {
		fieldName = "ID" // Try "ID" first, then fall back to "Id" if needed
	}
	part.Field = mp.getColumnNameFromField(fieldName)
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
		// Only add soft delete filter if entity has deleted_at field
		if mp.hasDeletedAt {
			sql.WriteString(fmt.Sprintf(" AND %s IS NULL", mp.deletedAtCol))
		}
	} else {
		// Only add soft delete filter if entity has deleted_at field
		if mp.hasDeletedAt {
			sql.WriteString(fmt.Sprintf(" WHERE %s IS NULL", mp.deletedAtCol))
		} else {
			// No WHERE clause needed if no conditions and no soft delete
			// But we need WHERE for consistency, so use 1=1
			sql.WriteString(" WHERE 1=1")
		}
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

	// For delete, add SET clause (only if soft delete is supported)
	if parsed.Action == "delete" {
		if mp.hasDeletedAt {
			// Soft delete
			sql.Reset()
			sql.WriteString(fmt.Sprintf("UPDATE %s SET %s = %s WHERE ", mp.tableName, mp.deletedAtCol, mp.getPlaceholder(argIndex)))
			sqlArgs = append(sqlArgs, "NOW()")
			argIndex++
			argIndex = mp.buildWhereClause(&sql, parsed.WhereClause, args, &sqlArgs, argIndex)
			sql.WriteString(fmt.Sprintf(" AND %s IS NULL", mp.deletedAtCol))
		} else {
			// Hard delete (if soft delete not supported)
			sql.Reset()
			sql.WriteString(fmt.Sprintf("DELETE FROM %s WHERE ", mp.tableName))
			argIndex = mp.buildWhereClause(&sql, parsed.WhereClause, args, &sqlArgs, argIndex)
		}
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
						placeholders = append(placeholders, mp.getPlaceholder(argIndex))
						*sqlArgs = append(*sqlArgs, argValue.Index(j).Interface())
						argIndex++
					}
					sql.WriteString(fmt.Sprintf("%s %s (%s)", part.Field, part.Operator, strings.Join(placeholders, ", ")))
				} else {
					sql.WriteString(fmt.Sprintf("%s %s %s", part.Field, part.Operator, mp.getPlaceholder(argIndex)))
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

				sql.WriteString(fmt.Sprintf("%s %s %s", part.Field, part.Operator, mp.getPlaceholder(argIndex)))
				*sqlArgs = append(*sqlArgs, argStr)
				argIndex++
				currentArgIndex++
			}
		} else if part.Operator == "BETWEEN" {
			if currentArgIndex+1 < len(args) {
				placeholder1 := mp.getPlaceholder(argIndex)
				placeholder2 := mp.getPlaceholder(argIndex + 1)
				sql.WriteString(fmt.Sprintf("%s BETWEEN %s AND %s", part.Field, placeholder1, placeholder2))
				*sqlArgs = append(*sqlArgs, args[currentArgIndex], args[currentArgIndex+1])
				argIndex += 2
				currentArgIndex += 2
			}
		} else {
			// Check if this is a True/False clause (no args consumed)
			fieldName := part.Field
			if strings.HasSuffix(fieldName, "::true") {
				// Field name ends with "::true", remove suffix and set value to true
				fieldName = strings.TrimSuffix(fieldName, "::true")
				sql.WriteString(fmt.Sprintf("%s %s %s", fieldName, part.Operator, mp.getPlaceholder(argIndex)))
				*sqlArgs = append(*sqlArgs, true)
				argIndex++
			} else if strings.HasSuffix(fieldName, "::false") {
				// Field name ends with "::false", remove suffix and set value to false
				fieldName = strings.TrimSuffix(fieldName, "::false")
				sql.WriteString(fmt.Sprintf("%s %s %s", fieldName, part.Operator, mp.getPlaceholder(argIndex)))
				*sqlArgs = append(*sqlArgs, false)
				argIndex++
			} else if currentArgIndex < len(args) {
				sql.WriteString(fmt.Sprintf("%s %s %s", part.Field, part.Operator, mp.getPlaceholder(argIndex)))
				*sqlArgs = append(*sqlArgs, args[currentArgIndex])
				argIndex++
				currentArgIndex++
			}
		}
	}

	return argIndex
}

// getPlaceholder returns the correct placeholder for the current database type
func (mp *MethodParser) getPlaceholder(index int) string {
	switch mp.dbType {
	case "mysql", "sqlite3":
		return "?"
	case "postgres":
		return fmt.Sprintf("$%d", index)
	default:
		return fmt.Sprintf("$%d", index) // Default to PostgreSQL style
	}
}

// toSnakeCase converts CamelCase to snake_case
// Handles common abbreviations like ID, URL, API, etc.
// Also handles mixed case like "Id", "WarehouseId" correctly
func (mp *MethodParser) toSnakeCase(s string) string {
	if s == "" {
		return ""
	}

	// Handle common abbreviations that should be lowercase
	abbreviations := map[string]string{
		"ID":    "id",
		"URL":   "url",
		"API":   "api",
		"HTTP":  "http",
		"HTTPS": "https",
		"JSON":  "json",
		"XML":   "xml",
		"HTML":  "html",
		"CSS":   "css",
		"SQL":   "sql",
		"UUID":  "uuid",
	}

	// Check if the entire string is a known abbreviation (uppercase)
	if lower, ok := abbreviations[strings.ToUpper(s)]; ok {
		return lower
	}

	// Special case: if string is "id" or "ID", return "id" directly
	if s == "id" || s == "ID" || s == "Id" {
		return "id"
	}

	var result strings.Builder
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Check if this is an uppercase letter
		if r >= 'A' && r <= 'Z' {
			// Look ahead to see if we have consecutive uppercase letters (abbreviation)
			abbrevEnd := i
			for j := i; j < len(runes); j++ {
				if runes[j] >= 'A' && runes[j] <= 'Z' {
					abbrevEnd = j
				} else {
					break
				}
			}

			// If we have 2+ consecutive uppercase letters, treat as abbreviation
			if abbrevEnd > i {
				abbrev := string(runes[i : abbrevEnd+1])
				if lower, ok := abbreviations[abbrev]; ok {
					// Known abbreviation (e.g., "ID", "URL")
					if i > 0 {
						result.WriteRune('_')
					}
					result.WriteString(lower)
					i = abbrevEnd // Skip processed runes
					continue
				} else {
					// Unknown abbreviation - treat as single unit, lowercase all
					if i > 0 {
						result.WriteRune('_')
					}
					result.WriteString(strings.ToLower(abbrev))
					i = abbrevEnd // Skip processed runes
					continue
				}
			}

			// Single uppercase letter
			// Check if next character is lowercase (e.g., "Id" in "WarehouseId")
			// In this case, the uppercase letter is the start of a word
			if i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z' {
				// Special case: "Id" should become "id" (not "i_d")
				// If this is "I" followed by "d" and it's at the end or followed by uppercase, treat as "Id"
				isId := (r == 'I' && runes[i+1] == 'd') && (i+2 >= len(runes) || runes[i+2] >= 'A' && runes[i+2] <= 'Z')
				if isId {
					// This is "Id" - write both characters together as a unit
					// Add underscore before the whole "Id" if not at start
					if i > 0 {
						result.WriteRune('_')
					}
					result.WriteRune(r)          // Write 'I'
					result.WriteRune(runes[i+1]) // Write 'd'
					i++                          // Skip the 'd' since we've already written it
					continue
				}
				// This is a word boundary (e.g., "Name" in "UserName")
				if i > 0 {
					result.WriteRune('_')
				}
				result.WriteRune(r)
				// Continue to process the lowercase part
				continue
			}

			// Single uppercase letter followed by uppercase or end
			// Add underscore before it if previous was lowercase
			if i > 0 && runes[i-1] >= 'a' && runes[i-1] <= 'z' {
				result.WriteRune('_')
			}
			result.WriteRune(r)
		} else {
			result.WriteRune(r)
		}
	}

	return strings.ToLower(result.String())
}

// getColumnNameFromField looks up the column name for a field in the entity
// It first tries to find the field by name, then uses the struct tag, then falls back to snake_case
func (mp *MethodParser) getColumnNameFromField(fieldName string) string {
	if mp.entityType == nil {
		return mp.toSnakeCase(fieldName)
	}

	// Try to find the field in the entity
	t := mp.entityType
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Normalize field name for matching (handle Id vs ID)
	normalizedFieldName := normalizeFieldNameForMatching(fieldName)

	// Search for field by name (case-insensitive, with normalization)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		normalizedFieldNameFromStruct := normalizeFieldNameForMatching(field.Name)

		// Try exact match first (case-insensitive)
		if strings.EqualFold(field.Name, fieldName) {
			return mp.getColumnNameFromTag(field)
		}

		// Try normalized match (handles Id vs ID)
		if strings.EqualFold(normalizedFieldNameFromStruct, normalizedFieldName) {
			return mp.getColumnNameFromTag(field)
		}
	}

	// Also check embedded structs
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			// Recursively check embedded struct
			embeddedParser := &MethodParser{
				entityType:   field.Type,
				tableName:    mp.tableName,
				dbType:       mp.dbType,
				hasDeletedAt: mp.hasDeletedAt,
				deletedAtCol: mp.deletedAtCol,
			}
			colName := embeddedParser.getColumnNameFromField(fieldName)
			if colName != mp.toSnakeCase(fieldName) {
				// Found in embedded struct
				return colName
			}
		}
	}

	// Field not found, fall back to snake_case conversion
	// Special handling: if fieldName is "ID" or "Id", return "id" directly
	fieldNameUpper := strings.ToUpper(fieldName)
	if fieldNameUpper == "ID" {
		return "id"
	}
	return mp.toSnakeCase(fieldName)
}

// getColumnNameFromTag extracts column name from struct tag
func (mp *MethodParser) getColumnNameFromTag(field reflect.StructField) string {
	tag := field.Tag.Get("orm")
	if tag != "" && tag != "-" {
		// Parse column name from tag
		parts := strings.Split(tag, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "column:") {
				colName := strings.TrimPrefix(part, "column:")
				// Remove any additional attributes after column name
				if idx := strings.Index(colName, ";"); idx >= 0 {
					colName = colName[:idx]
				}
				if idx := strings.Index(colName, " "); idx >= 0 {
					colName = colName[:idx]
				}
				return strings.TrimSpace(colName)
			}
		}
	}
	// No column tag, use snake_case of field name
	// Special handling: if field name is "ID" or "Id", return "id" directly
	fieldNameUpper := strings.ToUpper(field.Name)
	if fieldNameUpper == "ID" {
		return "id"
	}
	return mp.toSnakeCase(field.Name)
}

// normalizeFieldNameForMatching normalizes field names for matching
// Converts "Id" to "ID", "WarehouseId" to "WarehouseID", etc.
// This handles the case where method names use "Id" but struct fields use "ID"
func normalizeFieldNameForMatching(fieldName string) string {
	// First, replace "Id" at the end with "ID" (e.g., "Id" -> "ID", "WarehouseId" -> "WarehouseID")
	fieldName = regexp.MustCompile(`Id$`).ReplaceAllString(fieldName, "ID")

	// Then replace "Id" in the middle when followed by uppercase (e.g., "UserIdName" -> "UserIDName")
	fieldName = regexp.MustCompile(`([a-z])Id([A-Z])`).ReplaceAllString(fieldName, `${1}ID${2}`)

	return fieldName
}

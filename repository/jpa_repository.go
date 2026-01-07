package repository

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/shubhesh07/go-db-manager/connection"
	"github.com/shubhesh07/go-db-manager/entity"
)

// JPARepository is the main interface that repositories should extend
// It provides Spring JPA-style method name query generation
type JPARepository[T any] interface {
	// Standard CRUD methods
	FindByID(ctx context.Context, id interface{}) (*T, error)
	FindAll(ctx context.Context) ([]*T, error)
	Save(ctx context.Context, entity *T) error
	SaveAll(ctx context.Context, entities []*T) error
	Update(ctx context.Context, entity *T) error
	Delete(ctx context.Context, id interface{}) error
	Count(ctx context.Context) (int64, error)
	Exists(ctx context.Context, id interface{}) (bool, error)
}

// JpaRepositoryImpl provides the implementation with method name parsing
type JpaRepositoryImpl[T any] struct {
	db         *sql.DB
	tableName  string
	primaryKey string
	entityType reflect.Type
	parser     *MethodParser
}

// NewJpaRepository creates a new JPA-style repository
func NewJpaRepository[T any]() *JpaRepositoryImpl[T] {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	instanceValue := reflect.New(t)
	instance, ok := instanceValue.Interface().(entity.Entity)
	if !ok {
		panic("type T must implement entity.Entity interface")
	}

	tableName := entity.GetTableName(instance)
	primaryKey := entity.GetPrimaryKey(instance)

	return &JpaRepositoryImpl[T]{
		db:         connection.GetInstance().GetDB(),
		tableName:  tableName,
		primaryKey: primaryKey,
		entityType: t,
		parser:     NewMethodParser(t, tableName),
	}
}

// ExecuteMethod executes a method by parsing its name
func (r *JpaRepositoryImpl[T]) ExecuteMethod(ctx context.Context, methodName string, args []interface{}) (interface{}, error) {
	// Parse method name
	parsed, err := r.parser.ParseMethodName(methodName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to parse method name: %w", err)
	}

	// Build SQL
	sql, sqlArgs := r.parser.BuildSQL(parsed, args)

	// Execute based on action
	switch parsed.Action {
	case "find":
		return r.executeFind(ctx, sql, sqlArgs, parsed)
	case "count":
		return r.executeCount(ctx, sql, sqlArgs)
	case "exists":
		return r.executeExists(ctx, sql, sqlArgs)
	case "delete":
		return r.executeDelete(ctx, sql, sqlArgs)
	default:
		return nil, fmt.Errorf("unknown action: %s", parsed.Action)
	}
}

// executeFind executes a find query
func (r *JpaRepositoryImpl[T]) executeFind(ctx context.Context, sql string, args []interface{}, parsed *ParsedQuery) (interface{}, error) {
	rows, err := r.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results, err := r.scanRows(rows)
	if err != nil {
		return nil, err
	}

	// Handle return type
	if parsed.Limit == 1 {
		if len(results) > 0 {
			return results[0], nil
		}
		return nil, fmt.Errorf("no rows found")
	}

	return results, nil
}

// executeCount executes a count query
func (r *JpaRepositoryImpl[T]) executeCount(ctx context.Context, sql string, args []interface{}) (interface{}, error) {
	var count int64
	err := r.db.QueryRowContext(ctx, sql, args...).Scan(&count)
	return count, err
}

// executeExists executes an exists query
func (r *JpaRepositoryImpl[T]) executeExists(ctx context.Context, sql string, args []interface{}) (interface{}, error) {
	var exists bool
	// Modify SQL to use EXISTS
	sql = strings.Replace(sql, "SELECT *", "SELECT EXISTS(SELECT 1", 1)
	sql = sql + ")"

	err := r.db.QueryRowContext(ctx, sql, args...).Scan(&exists)
	return exists, err
}

// executeDelete executes a delete query
func (r *JpaRepositoryImpl[T]) executeDelete(ctx context.Context, sql string, args []interface{}) (interface{}, error) {
	result, err := r.db.ExecContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

// Standard CRUD methods

// FindByID finds an entity by its ID
func (r *JpaRepositoryImpl[T]) FindByID(ctx context.Context, id interface{}) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findBy"+strings.Title(r.primaryKey), []interface{}{id})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, sql.ErrNoRows
	}
	return result.(*T), nil
}

// FindAll finds all entities
func (r *JpaRepositoryImpl[T]) FindAll(ctx context.Context) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findAll", []interface{}{})
	if err != nil {
		return nil, err
	}
	return result.([]*T), nil
}

// Save saves a new entity
func (r *JpaRepositoryImpl[T]) Save(ctx context.Context, entity *T) error {
	columns, values, placeholders := r.buildInsertQuery(entity)
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *", r.tableName, columns, placeholders)

	row := r.db.QueryRowContext(ctx, query, values...)
	return r.scanRow(row, entity)
}

// SaveAll saves multiple entities
func (r *JpaRepositoryImpl[T]) SaveAll(ctx context.Context, entities []*T) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, e := range entities {
		columns, values, placeholders := r.buildInsertQuery(e)
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *", r.tableName, columns, placeholders)

		row := tx.QueryRowContext(ctx, query, values...)
		if err := r.scanRow(row, e); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Update updates an existing entity
func (r *JpaRepositoryImpl[T]) Update(ctx context.Context, entity *T) error {
	columns, values, id := r.buildUpdateQuery(entity)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $%d AND deleted_at IS NULL RETURNING *",
		r.tableName, columns, r.primaryKey, len(values))

	values = append(values, id)
	row := r.db.QueryRowContext(ctx, query, values...)
	return r.scanRow(row, entity)
}

// Delete soft deletes an entity by ID
func (r *JpaRepositoryImpl[T]) Delete(ctx context.Context, id interface{}) error {
	_, err := r.ExecuteMethod(ctx, "deleteBy"+strings.Title(r.primaryKey), []interface{}{id})
	return err
}

// Count counts all entities
func (r *JpaRepositoryImpl[T]) Count(ctx context.Context) (int64, error) {
	result, err := r.ExecuteMethod(ctx, "count", []interface{}{})
	if err != nil {
		return 0, err
	}
	return result.(int64), nil
}

// Exists checks if an entity exists by ID
func (r *JpaRepositoryImpl[T]) Exists(ctx context.Context, id interface{}) (bool, error) {
	result, err := r.ExecuteMethod(ctx, "existsBy"+strings.Title(r.primaryKey), []interface{}{id})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// Helper methods (reuse from BaseRepository)

func (r *JpaRepositoryImpl[T]) buildInsertQuery(entity *T) (string, []interface{}, string) {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	var columns []string
	var values []interface{}
	var placeholders []string
	argIndex := 1

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")

		if tag == "-" || field.Anonymous || (contains(tag, "primary_key") && contains(tag, "auto_increment")) {
			continue
		}

		colName := r.getColumnName(field, tag)
		if colName == "" {
			colName = toSnakeCase(field.Name)
		}

		fieldValue := v.Field(i).Interface()

		if fieldValue == nil || (reflect.ValueOf(fieldValue).IsZero() && !contains(tag, "not_null")) {
			continue
		}

		columns = append(columns, colName)
		values = append(values, fieldValue)
		placeholders = append(placeholders, fmt.Sprintf("$%d", argIndex))
		argIndex++
	}

	return strings.Join(columns, ", "), values, strings.Join(placeholders, ", ")
}

func (r *JpaRepositoryImpl[T]) buildUpdateQuery(entity *T) (string, []interface{}, interface{}) {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	var updates []string
	var values []interface{}
	var id interface{}
	argIndex := 1

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")

		if tag == "-" || field.Anonymous {
			continue
		}

		if contains(tag, "primary_key") {
			id = v.Field(i).Interface()
			continue
		}

		colName := r.getColumnName(field, tag)
		if colName == "" {
			colName = toSnakeCase(field.Name)
		}

		fieldValue := v.Field(i).Interface()

		if colName == "updated_at" {
			fieldValue = "NOW()"
		}

		updates = append(updates, fmt.Sprintf("%s = $%d", colName, argIndex))
		values = append(values, fieldValue)
		argIndex++
	}

	return strings.Join(updates, ", "), values, id
}

func (r *JpaRepositoryImpl[T]) scanRow(row *sql.Row, entity *T) error {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	var fieldCount int
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")
		if tag != "-" && !field.Anonymous {
			fieldCount++
		}
	}

	values := make([]interface{}, fieldCount)
	valuePtrs := make([]interface{}, fieldCount)

	for i := range values {
		valuePtrs[i] = &values[i]
	}

	if err := row.Scan(valuePtrs...); err != nil {
		return err
	}

	valueIndex := 0
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")

		if tag == "-" || field.Anonymous {
			continue
		}

		if valueIndex < len(values) {
			fieldValue := v.Field(i)
			if fieldValue.IsValid() && fieldValue.CanSet() {
				if err := r.setFieldValue(fieldValue, values[valueIndex]); err != nil {
					return err
				}
			}
			valueIndex++
		}
	}

	return nil
}

func (r *JpaRepositoryImpl[T]) scanRows(rows *sql.Rows) ([]*T, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []*T
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))

		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		var entity T
		if err := r.mapValuesToEntityByColumns(values, columns, &entity); err != nil {
			return nil, err
		}

		results = append(results, &entity)
	}

	return results, rows.Err()
}

func (r *JpaRepositoryImpl[T]) mapValuesToEntityByColumns(values []interface{}, columns []string, entity *T) error {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	columnMap := make(map[string]int)
	for i, col := range columns {
		columnMap[col] = i
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")

		colName := r.getColumnName(field, tag)
		if colName == "" {
			colName = toSnakeCase(field.Name)
		}

		if idx, ok := columnMap[colName]; ok {
			fieldValue := v.Field(i)
			if fieldValue.IsValid() && fieldValue.CanSet() {
				if err := r.setFieldValue(fieldValue, values[idx]); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *JpaRepositoryImpl[T]) setFieldValue(field reflect.Value, value interface{}) error {
	if value == nil {
		return nil
	}

	fieldType := field.Type()
	valueType := reflect.TypeOf(value)

	if fieldType == reflect.TypeOf((*interface{})(nil)).Elem() {
		field.Set(reflect.ValueOf(value))
		return nil
	}

	if valueType.AssignableTo(fieldType) {
		field.Set(reflect.ValueOf(value))
		return nil
	}

	if valueType.ConvertibleTo(fieldType) {
		field.Set(reflect.ValueOf(value).Convert(fieldType))
		return nil
	}

	return nil
}

func (r *JpaRepositoryImpl[T]) getColumnName(field reflect.StructField, tag string) string {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return ""
}

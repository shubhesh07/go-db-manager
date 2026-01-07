package repository

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/shubhesh07/go-db-manager/connection"
	"github.com/shubhesh07/go-db-manager/entity"
)

// Repository defines the base repository interface
// T must be a pointer type that implements entity.Entity
type Repository[T any] interface {
	FindByID(ctx context.Context, id interface{}) (*T, error)
	FindAll(ctx context.Context) ([]*T, error)
	FindBy(ctx context.Context, conditions map[string]interface{}) ([]*T, error)
	Save(ctx context.Context, entity *T) error
	SaveAll(ctx context.Context, entities []*T) error
	Update(ctx context.Context, entity *T) error
	Delete(ctx context.Context, id interface{}) error
	DeleteBy(ctx context.Context, conditions map[string]interface{}) error
	Count(ctx context.Context) (int64, error)
	CountBy(ctx context.Context, conditions map[string]interface{}) (int64, error)
	Exists(ctx context.Context, id interface{}) (bool, error)
	ExistsBy(ctx context.Context, conditions map[string]interface{}) (bool, error)
}

// BaseRepository provides default implementation of Repository
// T must be a pointer type that implements entity.Entity
type BaseRepository[T any] struct {
	db         *sql.DB
	tableName  string
	primaryKey string
	entityType reflect.Type
}

// NewRepository creates a new repository instance
func NewRepository[T any]() *BaseRepository[T] {
	var zero T
	// Get the underlying type if T is a pointer
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Create an instance to get metadata
	instanceValue := reflect.New(t)
	instance, ok := instanceValue.Interface().(entity.Entity)
	if !ok {
		panic("type T must implement entity.Entity interface")
	}

	return &BaseRepository[T]{
		db:         connection.GetInstance().GetDB(),
		tableName:  entity.GetTableName(instance),
		primaryKey: entity.GetPrimaryKey(instance),
		entityType: t,
	}
}

// FindByID finds an entity by its ID
func (r *BaseRepository[T]) FindByID(ctx context.Context, id interface{}) (*T, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s = $1 AND deleted_at IS NULL", r.tableName, r.primaryKey)
	row := r.db.QueryRowContext(ctx, query, id)

	var result T
	if err := r.scanRow(row, &result); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entity not found with id: %v", id)
		}
		return nil, err
	}

	return &result, nil
}

// FindAll finds all entities
func (r *BaseRepository[T]) FindAll(ctx context.Context) ([]*T, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE deleted_at IS NULL", r.tableName)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanRows(rows)
}

// FindBy finds entities by conditions
func (r *BaseRepository[T]) FindBy(ctx context.Context, conditions map[string]interface{}) ([]*T, error) {
	query, args := r.buildSelectQuery(conditions)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanRows(rows)
}

// Save saves a new entity
func (r *BaseRepository[T]) Save(ctx context.Context, entity *T) error {
	columns, values, placeholders := r.buildInsertQuery(entity)
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *", r.tableName, columns, placeholders)

	row := r.db.QueryRowContext(ctx, query, values...)
	return r.scanRow(row, entity)
}

// SaveAll saves multiple entities in a transaction
func (r *BaseRepository[T]) SaveAll(ctx context.Context, entities []*T) error {
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
func (r *BaseRepository[T]) Update(ctx context.Context, entity *T) error {
	columns, values, id := r.buildUpdateQuery(entity)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $%d AND deleted_at IS NULL RETURNING *",
		r.tableName, columns, r.primaryKey, len(values))

	values = append(values, id)
	row := r.db.QueryRowContext(ctx, query, values...)
	return r.scanRow(row, entity)
}

// Delete soft deletes an entity by ID
func (r *BaseRepository[T]) Delete(ctx context.Context, id interface{}) error {
	query := fmt.Sprintf("UPDATE %s SET deleted_at = $1 WHERE %s = $2 AND deleted_at IS NULL",
		r.tableName, r.primaryKey)

	result, err := r.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("entity not found with id: %v", id)
	}

	return nil
}

// DeleteBy soft deletes entities by conditions
func (r *BaseRepository[T]) DeleteBy(ctx context.Context, conditions map[string]interface{}) error {
	whereClause, args := r.buildWhereClause(conditions)
	query := fmt.Sprintf("UPDATE %s SET deleted_at = $1 WHERE %s AND deleted_at IS NULL",
		r.tableName, whereClause)

	args = append([]interface{}{time.Now()}, args...)
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// Count counts all entities
func (r *BaseRepository[T]) Count(ctx context.Context) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE deleted_at IS NULL", r.tableName)
	var count int64
	err := r.db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

// CountBy counts entities by conditions
func (r *BaseRepository[T]) CountBy(ctx context.Context, conditions map[string]interface{}) (int64, error) {
	whereClause, args := r.buildWhereClause(conditions)
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s AND deleted_at IS NULL",
		r.tableName, whereClause)

	var count int64
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// Exists checks if an entity exists by ID
func (r *BaseRepository[T]) Exists(ctx context.Context, id interface{}) (bool, error) {
	query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s = $1 AND deleted_at IS NULL)",
		r.tableName, r.primaryKey)

	var exists bool
	err := r.db.QueryRowContext(ctx, query, id).Scan(&exists)
	return exists, err
}

// ExistsBy checks if entities exist by conditions
func (r *BaseRepository[T]) ExistsBy(ctx context.Context, conditions map[string]interface{}) (bool, error) {
	whereClause, args := r.buildWhereClause(conditions)
	query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s AND deleted_at IS NULL)",
		r.tableName, whereClause)

	var exists bool
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	return exists, err
}

// Helper methods

func (r *BaseRepository[T]) buildSelectQuery(conditions map[string]interface{}) (string, []interface{}) {
	whereClause, args := r.buildWhereClause(conditions)
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s AND deleted_at IS NULL", r.tableName, whereClause)
	return query, args
}

func (r *BaseRepository[T]) buildWhereClause(conditions map[string]interface{}) (string, []interface{}) {
	if len(conditions) == 0 {
		return "1=1", []interface{}{}
	}

	var clauses []string
	var args []interface{}
	argIndex := 1

	for column, value := range conditions {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, argIndex))
		args = append(args, value)
		argIndex++
	}

	return strings.Join(clauses, " AND "), args
}

func (r *BaseRepository[T]) buildInsertQuery(entity *T) (string, []interface{}, string) {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	var columns []string
	var values []interface{}
	var placeholders []string
	argIndex := 1

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")

		if tag == "-" || field.Anonymous || contains(tag, "primary_key") && contains(tag, "auto_increment") {
			continue
		}

		colName := r.getColumnName(field, tag)
		if colName == "" {
			colName = toSnakeCase(field.Name)
		}

		fieldValue := v.Field(i).Interface()

		// Skip zero values for nullable fields
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

func (r *BaseRepository[T]) buildUpdateQuery(entity *T) (string, []interface{}, interface{}) {
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

		// Always update UpdatedAt
		if colName == "updated_at" {
			fieldValue = time.Now()
		}

		updates = append(updates, fmt.Sprintf("%s = $%d", colName, argIndex))
		values = append(values, fieldValue)
		argIndex++
	}

	return strings.Join(updates, ", "), values, id
}

func (r *BaseRepository[T]) scanRow(row *sql.Row, entity *T) error {
	// Get all fields from entity type
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	// Count non-ignored fields
	var fieldCount int
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")
		if tag != "-" && !field.Anonymous {
			fieldCount++
		}
	}

	// Create values array for scanning
	values := make([]interface{}, fieldCount)
	valuePtrs := make([]interface{}, fieldCount)

	for i := range values {
		valuePtrs[i] = &values[i]
	}

	// Scan the row
	if err := row.Scan(valuePtrs...); err != nil {
		return err
	}

	// Map values back to entity fields
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

func (r *BaseRepository[T]) scanRows(rows *sql.Rows) ([]*T, error) {
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

func (r *BaseRepository[T]) mapValuesToEntityByColumns(values []interface{}, columns []string, entity *T) error {
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

func (r *BaseRepository[T]) setFieldValue(field reflect.Value, value interface{}) error {
	if value == nil {
		return nil
	}

	fieldType := field.Type()
	valueType := reflect.TypeOf(value)

	// Handle time.Time
	if fieldType == reflect.TypeOf(time.Time{}) {
		if t, ok := value.(time.Time); ok {
			field.Set(reflect.ValueOf(t))
			return nil
		}
		if bytes, ok := value.([]byte); ok {
			if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", string(bytes)); err == nil {
				field.Set(reflect.ValueOf(t))
				return nil
			}
		}
	}

	// Handle *time.Time
	if fieldType == reflect.TypeOf((*time.Time)(nil)) {
		if t, ok := value.(time.Time); ok {
			field.Set(reflect.ValueOf(&t))
			return nil
		}
		if bytes, ok := value.([]byte); ok {
			if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", string(bytes)); err == nil {
				field.Set(reflect.ValueOf(&t))
				return nil
			}
		}
	}

	// Direct assignment if types match
	if valueType.AssignableTo(fieldType) {
		field.Set(reflect.ValueOf(value))
		return nil
	}

	// Convert if possible
	if valueType.ConvertibleTo(fieldType) {
		field.Set(reflect.ValueOf(value).Convert(fieldType))
		return nil
	}

	return nil
}

func (r *BaseRepository[T]) getColumnName(field reflect.StructField, tag string) string {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return ""
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

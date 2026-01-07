package entity

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Entity represents the base interface for all entities
type Entity interface {
	GetID() interface{}
	SetID(interface{})
}

// BaseEntity provides common fields for all entities
type BaseEntity struct {
	ID        uint       `orm:"primary_key;auto_increment" json:"id"`
	CreatedAt time.Time  `orm:"column:created_at;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time  `orm:"column:updated_at;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP" json:"updated_at"`
	DeletedAt *time.Time `orm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// GetID returns the entity ID
func (e *BaseEntity) GetID() interface{} {
	return e.ID
}

// SetID sets the entity ID
func (e *BaseEntity) SetID(id interface{}) {
	if idUint, ok := id.(uint); ok {
		e.ID = idUint
	}
}

// IsDeleted checks if entity is soft deleted
func (e *BaseEntity) IsDeleted() bool {
	return e.DeletedAt != nil
}

// SoftDelete marks entity as deleted
func (e *BaseEntity) SoftDelete() {
	now := time.Now()
	e.DeletedAt = &now
}

// Restore restores a soft deleted entity
func (e *BaseEntity) Restore() {
	e.DeletedAt = nil
}

// JSONB is a type for PostgreSQL JSONB columns
type JSONB map[string]interface{}

// Value implements the driver.Valuer interface
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal JSONB value: %v", value)
	}
	return json.Unmarshal(bytes, j)
}

// GetTableName extracts table name from struct tag or uses struct name
func GetTableName(entity Entity) string {
	t := reflect.TypeOf(entity)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Check for TableName method
	if method, ok := t.MethodByName("TableName"); ok {
		v := reflect.New(t)
		result := method.Func.Call([]reflect.Value{v})
		if len(result) > 0 && result[0].Kind() == reflect.String {
			return result[0].String()
		}
	}

	// Use struct name as default
	return toSnakeCase(t.Name())
}

// GetPrimaryKey extracts primary key field name from struct
func GetPrimaryKey(entity Entity) string {
	t := reflect.TypeOf(entity)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")
		if contains(tag, "primary_key") {
			return toSnakeCase(field.Name)
		}
	}

	return "id"
}

// GetColumns extracts all column names from struct tags
func GetColumns(entity Entity) []ColumnInfo {
	t := reflect.TypeOf(entity)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	var columns []ColumnInfo
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")

		if tag == "-" || field.Anonymous {
			continue
		}

		colName := getColumnName(field, tag)
		if colName == "" {
			colName = toSnakeCase(field.Name)
		}

		columns = append(columns, ColumnInfo{
			Name:      colName,
			FieldName: field.Name,
			Type:      field.Type,
			Tag:       tag,
			IsPrimary: contains(tag, "primary_key"),
			IsUnique:  contains(tag, "unique"),
			IsIndex:   contains(tag, "index"),
			IsNotNull: !contains(tag, "nullable"),
		})
	}

	return columns
}

// ColumnInfo holds information about a database column
type ColumnInfo struct {
	Name      string
	FieldName string
	Type      reflect.Type
	Tag       string
	IsPrimary bool
	IsUnique  bool
	IsIndex   bool
	IsNotNull bool
}

// Helper functions
func getColumnName(field reflect.StructField, tag string) string {
	parts := parseTag(tag)
	for _, part := range parts {
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return ""
}

func parseTag(tag string) []string {
	if tag == "" {
		return []string{}
	}
	return strings.Split(tag, ";")
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

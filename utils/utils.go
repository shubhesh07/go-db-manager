package utils

import (
	"database/sql"
	"reflect"
	"strings"
	"time"
)

// ToSnakeCase converts a string to snake_case
func ToSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// ParseTag parses an ORM tag string
func ParseTag(tag string) map[string]string {
	result := make(map[string]string)
	if tag == "" {
		return result
	}

	parts := strings.Split(tag, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, ":") {
			kv := strings.SplitN(part, ":", 2)
			if len(kv) == 2 {
				result[kv[0]] = kv[1]
			}
		} else {
			result[part] = ""
		}
	}

	return result
}

// Contains checks if a string contains a substring
func Contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// SetFieldValue sets a field value using reflection with proper type conversion
func SetFieldValue(field reflect.Value, value interface{}) error {
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
			formats := []string{
				time.RFC3339,
				time.RFC3339Nano,
				"2006-01-02 15:04:05.999999999-07:00",
				"2006-01-02 15:04:05",
				"2006-01-02",
			}
			for _, format := range formats {
				if t, err := time.Parse(format, string(bytes)); err == nil {
					field.Set(reflect.ValueOf(t))
					return nil
				}
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
			formats := []string{
				time.RFC3339,
				time.RFC3339Nano,
				"2006-01-02 15:04:05.999999999-07:00",
				"2006-01-02 15:04:05",
				"2006-01-02",
			}
			for _, format := range formats {
				if t, err := time.Parse(format, string(bytes)); err == nil {
					field.Set(reflect.ValueOf(&t))
					return nil
				}
			}
		}
		if value == nil {
			field.Set(reflect.Zero(fieldType))
			return nil
		}
	}

	// Handle sql.NullString, sql.NullInt64, etc.
	if fieldType == reflect.TypeOf(sql.NullString{}) {
		if ns, ok := value.(sql.NullString); ok {
			field.Set(reflect.ValueOf(ns))
			return nil
		}
		if s, ok := value.(string); ok {
			field.Set(reflect.ValueOf(sql.NullString{String: s, Valid: true}))
			return nil
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

	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		elemType := fieldType.Elem()
		if valueType.AssignableTo(elemType) {
			elemValue := reflect.New(elemType)
			elemValue.Elem().Set(reflect.ValueOf(value))
			field.Set(elemValue)
			return nil
		}
		if valueType.ConvertibleTo(elemType) {
			elemValue := reflect.New(elemType)
			elemValue.Elem().Set(reflect.ValueOf(value).Convert(elemType))
			field.Set(elemValue)
			return nil
		}
	}

	// Handle non-pointer field with pointer value
	if valueType.Kind() == reflect.Ptr {
		valueValue := reflect.ValueOf(value)
		if !valueValue.IsNil() {
			return SetFieldValue(field, valueValue.Elem().Interface())
		}
	}

	return nil
}

// GetColumnName extracts column name from struct field and tag
func GetColumnName(field reflect.StructField, tag string) string {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return ToSnakeCase(field.Name)
}

package migration

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/shubhesh07/go-db-manager/connection"
	"github.com/shubhesh07/go-db-manager/entity"
)

// Migration represents a database migration
type Migration struct {
	Version     string
	Description string
	Up          func(*sql.DB) error
	Down        func(*sql.DB) error
}

// Migrator handles database migrations
type Migrator struct {
	db         *sql.DB
	migrations []Migration
}

// NewMigrator creates a new migrator instance
func NewMigrator() *Migrator {
	return &Migrator{
		db:         connection.GetInstance().GetDB(),
		migrations: []Migration{},
	}
}

// AddMigration adds a migration to the migrator
func (m *Migrator) AddMigration(migration Migration) {
	m.migrations = append(m.migrations, migration)
}

// AutoMigrate automatically creates/updates tables based on entity structs
func (m *Migrator) AutoMigrate(entities ...entity.Entity) error {
	for _, e := range entities {
		if err := m.createTable(e); err != nil {
			return fmt.Errorf("failed to create table for %T: %w", e, err)
		}
	}
	return nil
}

// createTable creates a table based on entity struct
func (m *Migrator) createTable(e entity.Entity) error {
	tableName := entity.GetTableName(e)
	columns := entity.GetColumns(e)

	var columnDefs []string
	for _, col := range columns {
		colDef := m.buildColumnDefinition(col)
		columnDefs = append(columnDefs, colDef)
	}

	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			%s
		)
	`, tableName, strings.Join(columnDefs, ",\n\t\t"))

	_, err := m.db.Exec(createSQL)
	return err
}

// buildColumnDefinition builds SQL column definition from ColumnInfo
func (m *Migrator) buildColumnDefinition(col entity.ColumnInfo) string {
	var parts []string

	// Column name
	parts = append(parts, col.Name)

	// Column type
	parts = append(parts, m.getSQLType(col.Type))

	// Primary key
	if col.IsPrimary {
		parts = append(parts, "PRIMARY KEY")
	}

	// Auto increment
	if strings.Contains(col.Tag, "auto_increment") {
		parts = append(parts, "AUTO_INCREMENT")
	}

	// Not null
	if col.IsNotNull {
		parts = append(parts, "NOT NULL")
	}

	// Unique
	if col.IsUnique {
		parts = append(parts, "UNIQUE")
	}

	// Default value
	if defaultVal := m.getDefaultValue(col.Tag); defaultVal != "" {
		parts = append(parts, fmt.Sprintf("DEFAULT %s", defaultVal))
	}

	return strings.Join(parts, " ")
}

// getSQLType converts Go type to SQL type
func (m *Migrator) getSQLType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "VARCHAR(255)"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return "INTEGER"
	case reflect.Int64:
		return "BIGINT"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return "INTEGER"
	case reflect.Uint64:
		return "BIGINT"
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Float32:
		return "REAL"
	case reflect.Float64:
		return "DOUBLE PRECISION"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "BYTEA" // PostgreSQL
		}
		return "TEXT"
	case reflect.Map:
		return "JSONB" // PostgreSQL
	default:
		if t == reflect.TypeOf((*entity.JSONB)(nil)).Elem() {
			return "JSONB"
		}
		return "TEXT"
	}
}

// getDefaultValue extracts default value from tag
func (m *Migrator) getDefaultValue(tag string) string {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "default:") {
			return strings.TrimPrefix(part, "default:")
		}
	}
	return ""
}

// DropTable drops a table
func (m *Migrator) DropTable(tableName string) error {
	_, err := m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
	return err
}

// CreateIndex creates an index
func (m *Migrator) CreateIndex(tableName string, indexName string, columns ...string) error {
	_, err := m.db.Exec(fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
		indexName, tableName, strings.Join(columns, ", "),
	))
	return err
}

// DropIndex drops an index
func (m *Migrator) DropIndex(tableName string, indexName string) error {
	_, err := m.db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", indexName))
	return err
}

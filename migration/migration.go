// Package migration provides automatic database schema migration for go-db-manager.
//
// It inspects entity structs and their orm struct tags to create or update
// database tables automatically, similar to JPA's auto-DDL feature.
//
// Example:
//
//	migrator := migration.NewMigrator()
//	err := migrator.AutoMigrate(&OrderDetails{}, &Customer{})
package migration

import (
		"database/sql"
		"fmt"
		"reflect"
		"strings"

		"github.com/shubhesh07/go-db-manager/connection"
		"github.com/shubhesh07/go-db-manager/entity"
	)

// Migration represents a single versioned database migration with
// Up and Down functions for applying and reverting the change.
type Migration struct {
		// Version is a unique identifier for this migration (e.g. "001", "20260101").
		Version string
		// Description is a human-readable summary of what this migration does.
		Description string
		// Up applies the migration.
		Up func(*sql.DB) error
		// Down reverts the migration.
		Down func(*sql.DB) error
}

// Migrator manages schema migrations and auto-migration for entity structs.
type Migrator struct {
		db         *sql.DB
		migrations []Migration
}

// NewMigrator creates a new Migrator using the active database connection.
func NewMigrator() *Migrator {
		return &Migrator{
					db:         connection.GetInstance().GetDB(),
					migrations: []Migration{},
				}
}

// AddMigration registers a manual Migration to be run later.
func (m *Migrator) AddMigration(migration Migration) {
		m.migrations = append(m.migrations, migration)
}

// AutoMigrate inspects each entity struct and creates the corresponding
// database table if it does not already exist. Column definitions are
// derived from orm struct tags.
//
// Example:
//
//	err := migrator.AutoMigrate(&OrderDetails{}, &User{})
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

// DropTable drops the named table if it exists.
//
// Example:
//
//	err := migrator.DropTable("old_orders")
func (m *Migrator) DropTable(tableName string) error {
		_, err := m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
		return err
}

// CreateIndex creates a named index on the specified columns of a table.
//
// Example:
//
//	err := migrator.CreateIndex("orders", "idx_customer", "customer_id")
func (m *Migrator) CreateIndex(tableName string, indexName string, columns ...string) error {
		_, err := m.db.Exec(fmt.Sprintf(
					"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
					indexName, tableName, strings.Join(columns, ", "),
				))
		return err
}

// DropIndex drops the named index if it exists.
//
// Example:
//
//	err := migrator.DropIndex("orders", "idx_customer")
func (m *Migrator) DropIndex(tableName string, indexName string) error {
		_, err := m.db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", indexName))
		return err
}

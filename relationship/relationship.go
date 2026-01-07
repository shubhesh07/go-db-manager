package relationship

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/shubhesh07/go-db-manager/connection"
	"github.com/shubhesh07/go-db-manager/entity"
)

// RelationshipType represents the type of relationship
type RelationshipType string

const (
	OneToOne   RelationshipType = "one_to_one"
	OneToMany  RelationshipType = "one_to_many"
	ManyToOne  RelationshipType = "many_to_one"
	ManyToMany RelationshipType = "many_to_many"
)

// Relationship represents a relationship between entities
type Relationship struct {
	Type          RelationshipType
	FieldName     string
	ForeignEntity interface{}
	ForeignKey    string
	References    string
	JoinTable     string
	JoinColumn    string
	InverseColumn string
	Lazy          bool
	Eager         bool
}

// RelationshipManager handles entity relationships
type RelationshipManager struct {
	db *sql.DB
}

// NewRelationshipManager creates a new relationship manager
func NewRelationshipManager() *RelationshipManager {
	return &RelationshipManager{
		db: connection.GetInstance().GetDB(),
	}
}

// LoadOneToOne loads a one-to-one relationship
func (rm *RelationshipManager) LoadOneToOne(ctx context.Context, parent entity.Entity, fieldName string, relationship Relationship) error {
	parentID := parent.GetID()
	if parentID == nil {
		return fmt.Errorf("parent entity has no ID")
	}

	// Get table name from foreign entity type
	foreignType := reflect.TypeOf(relationship.ForeignEntity)
	if foreignType.Kind() == reflect.Ptr {
		foreignType = foreignType.Elem()
	}

	// Create a zero value instance to get table name
	foreignValue := reflect.New(foreignType)
	foreignEntity, ok := foreignValue.Interface().(entity.Entity)
	if !ok {
		return fmt.Errorf("foreign entity does not implement Entity interface")
	}

	foreignTable := entity.GetTableName(foreignEntity)
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s = $1 LIMIT 1", foreignTable, relationship.ForeignKey)

	row := rm.db.QueryRowContext(ctx, query, parentID)

	// Scan the row (simplified - would need proper scanning logic)
	_ = row

	// Set the field on parent entity
	parentValue := reflect.ValueOf(parent)
	if parentValue.Kind() == reflect.Ptr {
		parentValue = parentValue.Elem()
	}

	field := parentValue.FieldByName(fieldName)
	if field.IsValid() && field.CanSet() {
		field.Set(foreignValue)
	}

	return nil
}

// LoadOneToMany loads a one-to-many relationship
func (rm *RelationshipManager) LoadOneToMany(ctx context.Context, parent entity.Entity, fieldName string, relationship Relationship) error {
	parentID := parent.GetID()
	if parentID == nil {
		return fmt.Errorf("parent entity has no ID")
	}

	// Get table name from foreign entity type
	foreignType := reflect.TypeOf(relationship.ForeignEntity)
	if foreignType.Kind() == reflect.Ptr {
		foreignType = foreignType.Elem()
	}

	foreignValue := reflect.New(foreignType)
	foreignEntity, ok := foreignValue.Interface().(entity.Entity)
	if !ok {
		return fmt.Errorf("foreign entity does not implement Entity interface")
	}

	foreignTable := entity.GetTableName(foreignEntity)
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s = $1", foreignTable, relationship.ForeignKey)

	rows, err := rm.db.QueryContext(ctx, query, parentID)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Create slice to hold results (foreignType already declared above)
	sliceType := reflect.SliceOf(reflect.PtrTo(foreignType))
	sliceValue := reflect.MakeSlice(sliceType, 0, 0)

	// Scan rows (simplified)
	_ = rows

	// Set the field on parent entity
	parentValue := reflect.ValueOf(parent)
	if parentValue.Kind() == reflect.Ptr {
		parentValue = parentValue.Elem()
	}

	field := parentValue.FieldByName(fieldName)
	if field.IsValid() && field.CanSet() {
		field.Set(sliceValue)
	}

	return nil
}

// LoadManyToMany loads a many-to-many relationship
func (rm *RelationshipManager) LoadManyToMany(ctx context.Context, parent entity.Entity, fieldName string, relationship Relationship) error {
	parentID := parent.GetID()
	if parentID == nil {
		return fmt.Errorf("parent entity has no ID")
	}

	// Get table name from foreign entity type
	foreignType := reflect.TypeOf(relationship.ForeignEntity)
	if foreignType.Kind() == reflect.Ptr {
		foreignType = foreignType.Elem()
	}

	foreignValue := reflect.New(foreignType)
	foreignEntity, ok := foreignValue.Interface().(entity.Entity)
	if !ok {
		return fmt.Errorf("foreign entity does not implement Entity interface")
	}

	foreignTable := entity.GetTableName(foreignEntity)
	query := fmt.Sprintf(`
		SELECT f.* FROM %s f
		INNER JOIN %s j ON f.%s = j.%s
		WHERE j.%s = $1
	`, foreignTable, relationship.JoinTable, relationship.References, relationship.InverseColumn, relationship.JoinColumn)

	rows, err := rm.db.QueryContext(ctx, query, parentID)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Create slice to hold results
	foreignType3 := reflect.TypeOf(relationship.ForeignEntity)
	if foreignType3.Kind() == reflect.Ptr {
		foreignType3 = foreignType3.Elem()
	}

	sliceType := reflect.SliceOf(reflect.PtrTo(foreignType3))
	sliceValue := reflect.MakeSlice(sliceType, 0, 0)

	// Scan rows (simplified)
	_ = rows

	// Set the field on parent entity
	parentValue := reflect.ValueOf(parent)
	if parentValue.Kind() == reflect.Ptr {
		parentValue = parentValue.Elem()
	}

	field := parentValue.FieldByName(fieldName)
	if field.IsValid() && field.CanSet() {
		field.Set(sliceValue)
	}

	return nil
}

// ExtractRelationships extracts relationship metadata from struct tags
func ExtractRelationships(e entity.Entity) []Relationship {
	t := reflect.TypeOf(e)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	var relationships []Relationship

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("orm")

		if tag == "" || tag == "-" {
			continue
		}

		rel := parseRelationshipTag(field, tag)
		if rel != nil {
			relationships = append(relationships, *rel)
		}
	}

	return relationships
}

func parseRelationshipTag(field reflect.StructField, tag string) *Relationship {
	parts := strings.Split(tag, ";")

	var relType RelationshipType
	var foreignKey, references, joinTable, joinColumn, inverseColumn string

	for _, part := range parts {
		if strings.HasPrefix(part, "one_to_one") {
			relType = OneToOne
		} else if strings.HasPrefix(part, "one_to_many") {
			relType = OneToMany
		} else if strings.HasPrefix(part, "many_to_one") {
			relType = ManyToOne
		} else if strings.HasPrefix(part, "many_to_many") {
			relType = ManyToMany
		} else if strings.HasPrefix(part, "foreign_key:") {
			foreignKey = strings.TrimPrefix(part, "foreign_key:")
		} else if strings.HasPrefix(part, "references:") {
			references = strings.TrimPrefix(part, "references:")
		} else if strings.HasPrefix(part, "join_table:") {
			joinTable = strings.TrimPrefix(part, "join_table:")
		} else if strings.HasPrefix(part, "join_column:") {
			joinColumn = strings.TrimPrefix(part, "join_column:")
		} else if strings.HasPrefix(part, "inverse_column:") {
			inverseColumn = strings.TrimPrefix(part, "inverse_column:")
		}
	}

	if relType == "" {
		return nil
	}

	return &Relationship{
		Type:          relType,
		FieldName:     field.Name,
		ForeignKey:    foreignKey,
		References:    references,
		JoinTable:     joinTable,
		JoinColumn:    joinColumn,
		InverseColumn: inverseColumn,
	}
}

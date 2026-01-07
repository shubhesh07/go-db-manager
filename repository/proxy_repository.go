package repository

import (
	"context"
	"database/sql"
)

// DynamicRepository provides dynamic method invocation like Spring JPA
// Users can define methods like FindByOrderId, FindByCustomerIdAndStatusId, etc.
// and they will be automatically implemented
type DynamicRepository[T any] struct {
	*JpaRepositoryImpl[T]
}

// NewDynamicRepository creates a new dynamic repository
func NewDynamicRepository[T any]() *DynamicRepository[T] {
	return &DynamicRepository[T]{
		JpaRepositoryImpl: NewJpaRepository[T](),
	}
}

// CallMethod allows calling methods by name dynamically
// This is used internally by the proxy mechanism
func (r *DynamicRepository[T]) CallMethod(ctx context.Context, methodName string, args ...interface{}) (interface{}, error) {
	return r.ExecuteMethod(ctx, methodName, args)
}

// FindByID finds by ID - common method used by repositories
func (r *DynamicRepository[T]) FindByID(ctx context.Context, id interface{}) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findById", []interface{}{id})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, sql.ErrNoRows
	}
	if single, ok := result.(*T); ok {
		return single, nil
	}
	if list, ok := result.([]*T); ok && len(list) > 0 {
		return list[0], nil
	}
	return nil, sql.ErrNoRows
}

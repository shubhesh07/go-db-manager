/ Package repository provides the core repository pattern implementation
// for go-db-manager, including Spring JPA-style dynamic query generation.
//
// The main entry point is DynamicRepository, which automatically implements
// repository methods based on their names — just like Spring Data JPA.
//
// Defining a repository:
//
//	type OrderRepository interface {
//		FindByOrderId(ctx context.Context, orderId uint) (*Order, error)
//		FindByCustomerIdAndStatusId(ctx context.Context, customerId, statusId uint) ([]*Order, error)
//	}
//
//	type orderRepositoryImpl struct {
//		*repository.DynamicRepository[Order]
//	}
//
//	func NewOrderRepository() OrderRepository {
//		return &orderRepositoryImpl{
//			DynamicRepository: repository.NewDynamicRepository[Order](),
//		}
//	}
//
//	func (r *orderRepositoryImpl) FindByOrderId(ctx context.Context, orderId uint) (*Order, error) {
//		return r.DynamicRepository.FindByOrderId(ctx, orderId)
//	}
package repository

import (
		"context"
		"database/sql"
	)

// DynamicRepository provides automatic Spring JPA-style query generation.
// Method names like FindByOrderId, FindByCustomerIdAndStatusIdNotIn, or
// FindTop5ByCustomerIdOrderByCreatedOnDesc are automatically translated
// into the correct SQL queries at runtime.
//
// Embed *DynamicRepository[T] in your repository implementation struct
// and delegate method calls to it.
type DynamicRepository[T any] struct {
		*JpaRepositoryImpl[T]
}

// NewDynamicRepository creates a new DynamicRepository for the given entity type T.
// T must implement entity.Entity (e.g. by embedding entity.BaseEntity).
//
// Example:
//
//	repo := repository.NewDynamicRepository[OrderDetails]()
func NewDynamicRepository[T any]() *DynamicRepository[T] {
		return &DynamicRepository[T]{
					JpaRepositoryImpl: NewJpaRepository[T](),
				}
}

// CallMethod invokes a repository method by name with the given arguments.
// This is the underlying mechanism used by the dynamic proxy to dispatch
// Spring JPA-style method calls.
//
// Example:
//
//	result, err := repo.CallMethod(ctx, "FindByCustomerId", 42)
func (r *DynamicRepository[T]) CallMethod(ctx context.Context, methodName string, args ...interface{}) (interface{}, error) {
		return r.ExecuteMethod(ctx, methodName, args)
}

// FindByID finds a single entity by its primary key.
// Returns sql.ErrNoRows if no entity is found.
//
// Example:
//
//	order, err := repo.FindByID(ctx, 123)
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

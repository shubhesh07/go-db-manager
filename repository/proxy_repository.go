package repository

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
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

// Helper methods for common Spring JPA patterns
// These can be called directly or used as examples for code generation

// FindByOrderId finds by order ID
func (r *DynamicRepository[T]) FindByOrderId(ctx context.Context, orderId interface{}) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByOrderId", []interface{}{orderId})
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

// FindByOrderIdIn finds by order IDs
func (r *DynamicRepository[T]) FindByOrderIdIn(ctx context.Context, orderIds interface{}) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByOrderIdIn", []interface{}{orderIds})
	if err != nil {
		return nil, err
	}
	if list, ok := result.([]*T); ok {
		return list, nil
	}
	return []*T{}, nil
}

// FindByCustomerId finds by customer ID
func (r *DynamicRepository[T]) FindByCustomerId(ctx context.Context, customerId interface{}) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByCustomerId", []interface{}{customerId})
	if err != nil {
		return nil, err
	}
	if list, ok := result.([]*T); ok {
		return list, nil
	}
	return []*T{}, nil
}

// FindByCustomerIdAndStatusId finds by customer ID and status ID
func (r *DynamicRepository[T]) FindByCustomerIdAndStatusId(ctx context.Context, customerId interface{}, statusId interface{}) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByCustomerIdAndStatusId", []interface{}{customerId, statusId})
	if err != nil {
		return nil, err
	}
	if list, ok := result.([]*T); ok {
		return list, nil
	}
	return []*T{}, nil
}

// FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc finds with complex conditions
func (r *DynamicRepository[T]) FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
	ctx context.Context, customerIds interface{}, statusIds interface{}, orderValue float64) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc",
		[]interface{}{customerIds, statusIds, orderValue})
	if err != nil {
		return nil, err
	}
	if list, ok := result.([]*T); ok {
		return list, nil
	}
	return []*T{}, nil
}

// FindByCustomerIdAndOrderValueGreaterThanEqual finds by customer ID and order value
func (r *DynamicRepository[T]) FindByCustomerIdAndOrderValueGreaterThanEqual(
	ctx context.Context, customerId interface{}, orderValue float64) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByCustomerIdAndOrderValueGreaterThanEqual",
		[]interface{}{customerId, orderValue})
	if err != nil {
		return nil, err
	}
	if list, ok := result.([]*T); ok {
		return list, nil
	}
	return []*T{}, nil
}

// FindTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc finds top 1
func (r *DynamicRepository[T]) FindTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc(
	ctx context.Context, customerId interface{}, statusId interface{}) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc",
		[]interface{}{customerId, statusId})
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

// FindByOrderIdAndCustomerId finds by order ID and customer ID
func (r *DynamicRepository[T]) FindByOrderIdAndCustomerId(ctx context.Context, orderId interface{}, customerId interface{}) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByOrderIdAndCustomerId", []interface{}{orderId, customerId})
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

// FindByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual finds with date comparison
func (r *DynamicRepository[T]) FindByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual(
	ctx context.Context, customerId interface{}, statusIds interface{}, createdOn interface{}) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual",
		[]interface{}{customerId, statusIds, createdOn})
	if err != nil {
		return nil, err
	}
	if list, ok := result.([]*T); ok {
		return list, nil
	}
	return []*T{}, nil
}

// FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc finds top 5
func (r *DynamicRepository[T]) FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc(
	ctx context.Context, customerId interface{}, statusIds interface{}) ([]*T, error) {
	result, err := r.ExecuteMethod(ctx, "findTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc",
		[]interface{}{customerId, statusIds})
	if err != nil {
		return nil, err
	}
	if list, ok := result.([]*T); ok {
		return list, nil
	}
	return []*T{}, nil
}

// RepositoryProxy creates a proxy that intercepts method calls
// This allows users to define custom repository interfaces with Spring JPA-style methods
type RepositoryProxy struct {
	repo interface{}
}

// NewRepositoryProxy creates a new repository proxy
func NewRepositoryProxy(repo interface{}) *RepositoryProxy {
	return &RepositoryProxy{repo: repo}
}

// Invoke invokes a method on the repository by name
func (p *RepositoryProxy) Invoke(ctx context.Context, methodName string, args ...interface{}) (interface{}, error) {
	repoValue := reflect.ValueOf(p.repo)
	method := repoValue.MethodByName(methodName)

	if !method.IsValid() {
		// Try to use ExecuteMethod if available
		if execMethod := repoValue.MethodByName("ExecuteMethod"); execMethod.IsValid() {
			return execMethod.Call([]reflect.Value{
				reflect.ValueOf(ctx),
				reflect.ValueOf(methodName),
				reflect.ValueOf(args),
			})[0].Interface(), nil
		}
		return nil, fmt.Errorf("method %s not found", methodName)
	}

	// Prepare arguments
	methodType := method.Type()
	numIn := methodType.NumIn()
	callArgs := make([]reflect.Value, numIn)

	// First argument is context
	callArgs[0] = reflect.ValueOf(ctx)

	// Map remaining arguments
	for i := 1; i < numIn && i-1 < len(args); i++ {
		argType := methodType.In(i)
		argValue := reflect.ValueOf(args[i-1])

		if argValue.Type().AssignableTo(argType) {
			callArgs[i] = argValue
		} else if argValue.Type().ConvertibleTo(argType) {
			callArgs[i] = argValue.Convert(argType)
		} else {
			return nil, fmt.Errorf("cannot convert argument %d to %v", i-1, argType)
		}
	}

	// Call the method
	results := method.Call(callArgs)

	// Handle return values
	if len(results) == 0 {
		return nil, nil
	}

	if len(results) == 1 {
		return results[0].Interface(), nil
	}

	// Multiple return values (result, error)
	result := results[0].Interface()
	var err error
	if !results[1].IsNil() {
		err = results[1].Interface().(error)
	}

	return result, err
}

// Additional methods for search-service

// FindByExperimentType finds by experiment type
func (r *DynamicRepository[T]) FindByExperimentType(ctx context.Context, experimentType string) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByExperimentType", []interface{}{experimentType})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if single, ok := result.(*T); ok {
		return single, nil
	}
	if list, ok := result.([]*T); ok && len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}

// FindByExperimentMasterId finds by experiment master ID
func (r *DynamicRepository[T]) FindByExperimentMasterId(ctx context.Context, experimentMasterId interface{}) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByExperimentMasterId", []interface{}{experimentMasterId})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if single, ok := result.(*T); ok {
		return single, nil
	}
	if list, ok := result.([]*T); ok && len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}

// FindByWarehouseIdAndType finds by warehouse ID and type
func (r *DynamicRepository[T]) FindByWarehouseIdAndType(ctx context.Context, warehouseId interface{}, searchType string) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByWarehouseIdAndType", []interface{}{warehouseId, searchType})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if single, ok := result.(*T); ok {
		return single, nil
	}
	if list, ok := result.([]*T); ok && len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}

// FindByWarehouseIdAndTypeAndIsMultiSearch finds by warehouse ID, type, and isMultiSearch
func (r *DynamicRepository[T]) FindByWarehouseIdAndTypeAndIsMultiSearch(ctx context.Context, warehouseId interface{}, searchType string, isMultiSearch bool) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByWarehouseIdAndTypeAndIsMultiSearch", []interface{}{warehouseId, searchType, isMultiSearch})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if single, ok := result.(*T); ok {
		return single, nil
	}
	if list, ok := result.([]*T); ok && len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}

// FindByWarehouseIdAndRequestType finds by warehouse ID and request type
func (r *DynamicRepository[T]) FindByWarehouseIdAndRequestType(ctx context.Context, warehouseId interface{}, requestType string) (*T, error) {
	result, err := r.ExecuteMethod(ctx, "findByWarehouseIdAndRequestType", []interface{}{warehouseId, requestType})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if single, ok := result.(*T); ok {
		return single, nil
	}
	if list, ok := result.([]*T); ok && len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}

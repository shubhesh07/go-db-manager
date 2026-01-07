package main

import (
	"context"
	"time"

	"github.com/shubhesh07/go-db-manager/entity"
	"github.com/shubhesh07/go-db-manager/repository"
)

// OrderDetails represents an order details entity (similar to your Java example)
type OrderDetails struct {
	entity.BaseEntity
	OrderID          uint      `orm:"column:order_id;not_null;index" json:"order_id"`
	CustomerID       uint      `orm:"column:customer_id;not_null;index" json:"customer_id"`
	StatusID         uint      `orm:"column:status_id;not_null;index" json:"status_id"`
	OrderValue       float64   `orm:"column:order_value" json:"order_value"`
	DoctorDigitizeID *uint     `orm:"column:doctor_digitize_id" json:"doctor_digitize_id"`
	OrgSubsID        *uint     `orm:"column:org_subs_id" json:"org_subs_id"`
	CreatedOn        time.Time `orm:"column:created_on;default:CURRENT_TIMESTAMP" json:"created_on"`
}

// TableName returns the table name
func (o *OrderDetails) TableName() string {
	return "order_details"
}

// OrderDetailsRepository defines Spring JPA-style repository methods
// All these methods are automatically implemented by DynamicRepository
type OrderDetailsRepository interface {
	// Standard CRUD
	FindByID(ctx context.Context, id interface{}) (*OrderDetails, error)
	FindAll(ctx context.Context) ([]*OrderDetails, error)
	Save(ctx context.Context, entity *OrderDetails) error
	Update(ctx context.Context, entity *OrderDetails) error
	Delete(ctx context.Context, id interface{}) error

	// Spring JPA-style query methods (automatically generated)
	FindByOrderId(ctx context.Context, orderId uint) (*OrderDetails, error)
	FindByOrderIdIn(ctx context.Context, orderIds []uint) ([]*OrderDetails, error)
	FindByCustomerId(ctx context.Context, customerId uint) ([]*OrderDetails, error)
	FindByCustomerIdAndStatusId(ctx context.Context, customerId uint, statusId uint) ([]*OrderDetails, error)
	FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
		ctx context.Context, customerIds []uint, statusIds []uint, orderValue float64) ([]*OrderDetails, error)
	FindByCustomerIdAndOrderValueGreaterThanEqual(ctx context.Context, customerId uint, orderValue float64) ([]*OrderDetails, error)
	FindTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc(
		ctx context.Context, customerId uint, statusId uint) (*OrderDetails, error)
	FindByOrderIdAndCustomerId(ctx context.Context, orderId uint, customerId uint) (*OrderDetails, error)
	FindByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual(
		ctx context.Context, customerId uint, statusIds []uint, createdOn time.Time) ([]*OrderDetails, error)
	FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc(
		ctx context.Context, customerId uint, statusIds []uint) ([]*OrderDetails, error)
}

// orderDetailsRepositoryImpl implements OrderDetailsRepository
type orderDetailsRepositoryImpl struct {
	*repository.DynamicRepository[OrderDetails]
}

// NewOrderDetailsRepository creates a new OrderDetailsRepository
func NewOrderDetailsRepository() OrderDetailsRepository {
	return &orderDetailsRepositoryImpl{
		DynamicRepository: repository.NewDynamicRepository[OrderDetails](),
	}
}

// Implement interface methods with correct signatures

func (r *orderDetailsRepositoryImpl) FindByOrderId(ctx context.Context, orderId uint) (*OrderDetails, error) {
	return r.DynamicRepository.FindByOrderId(ctx, orderId)
}

func (r *orderDetailsRepositoryImpl) FindByOrderIdIn(ctx context.Context, orderIds []uint) ([]*OrderDetails, error) {
	return r.DynamicRepository.FindByOrderIdIn(ctx, orderIds)
}

func (r *orderDetailsRepositoryImpl) FindByCustomerId(ctx context.Context, customerId uint) ([]*OrderDetails, error) {
	return r.DynamicRepository.FindByCustomerId(ctx, customerId)
}

func (r *orderDetailsRepositoryImpl) FindByCustomerIdAndStatusId(ctx context.Context, customerId uint, statusId uint) ([]*OrderDetails, error) {
	return r.DynamicRepository.FindByCustomerIdAndStatusId(ctx, customerId, statusId)
}

func (r *orderDetailsRepositoryImpl) FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
	ctx context.Context, customerIds []uint, statusIds []uint, orderValue float64) ([]*OrderDetails, error) {
	return r.DynamicRepository.FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
		ctx, customerIds, statusIds, orderValue)
}

func (r *orderDetailsRepositoryImpl) FindByCustomerIdAndOrderValueGreaterThanEqual(ctx context.Context, customerId uint, orderValue float64) ([]*OrderDetails, error) {
	return r.DynamicRepository.FindByCustomerIdAndOrderValueGreaterThanEqual(ctx, customerId, orderValue)
}

func (r *orderDetailsRepositoryImpl) FindTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc(
	ctx context.Context, customerId uint, statusId uint) (*OrderDetails, error) {
	return r.DynamicRepository.FindTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc(
		ctx, customerId, statusId)
}

func (r *orderDetailsRepositoryImpl) FindByOrderIdAndCustomerId(ctx context.Context, orderId uint, customerId uint) (*OrderDetails, error) {
	return r.DynamicRepository.FindByOrderIdAndCustomerId(ctx, orderId, customerId)
}

func (r *orderDetailsRepositoryImpl) FindByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual(
	ctx context.Context, customerId uint, statusIds []uint, createdOn time.Time) ([]*OrderDetails, error) {
	return r.DynamicRepository.FindByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual(
		ctx, customerId, statusIds, createdOn)
}

func (r *orderDetailsRepositoryImpl) FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc(
	ctx context.Context, customerId uint, statusIds []uint) ([]*OrderDetails, error) {
	return r.DynamicRepository.FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc(ctx, customerId, statusIds)
}

// Package godbmanager is a Spring Data JPA-inspired ORM framework for Go
// that automatically generates SQL queries from method names — no query
// writing required.
//
// Define a repository interface with methods like FindByCustomerId,
// FindByStatusIdNotIn, or FindTop5ByCustomerIdOrderByCreatedOnDesc,
// and the framework generates the correct SQL at runtime, just like
// Spring Data JPA does in Java.
//
// # Features
//
//   - Spring JPA-style query generation from method names
//   - Auto-migration: automatic table creation from entity structs
//   - Relationship support: OneToOne, OneToMany, ManyToOne, ManyToMany
//   - Soft deletes, transaction management, and pagination
//   - Fluent query builder for complex ad-hoc queries
//   - Full type safety with Go generics
//   - Connection pooling for PostgreSQL and MySQL
//
// # Quick Start
//
// Install the module:
//
//	go get github.com/shubhesh07/go-db-manager
//
// Connect to a database:
//
//	config := connection.LoadConfigFromEnv()
//	connMgr := connection.GetInstance()
//	if err := connMgr.Connect(config); err != nil {
//		log.Fatal(err)
//	}
//	defer connMgr.Close()
//
// Define an entity:
//
//	type Order struct {
//		entity.BaseEntity
//		CustomerID uint    `orm:"column:customer_id;not_null;index" json:"customer_id"`
//		StatusID   uint    `orm:"column:status_id;not_null;index" json:"status_id"`
//		Total      float64 `orm:"column:total" json:"total"`
//	}
//
//	func (Order) TableName() string { return "orders" }
//
// Define and use a repository:
//
//	type OrderRepository interface {
//		FindByCustomerID(ctx context.Context, customerID uint) ([]*Order, error)
//		FindByStatusIDNotIn(ctx context.Context, statusIDs []uint) ([]*Order, error)
//	}
//
// See the repository, migration, query, pagination, and transaction
// sub-packages for full API documentation.
package godbmanager

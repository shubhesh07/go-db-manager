// Package godbmanager is a Go ORM and database manager inspired by Spring Data JPA.
// It provides automatic SQL query generation from method names, making database
// access in Go as effortless as Spring Data JPA in Java — with full type safety
// via Go generics, no code generation, and no raw SQL required.
//
// # Overview
//
// go-db-manager is a Go ORM (Object-Relational Mapper) and database framework
// for MySQL and PostgreSQL. Define repository interfaces with method names like
// FindByCustomerId, FindByStatusIdNotIn, or FindTop5ByOrderIdOrderByCreatedOnDesc,
// and the framework generates the correct SQL at runtime automatically.
//
// It is designed for Go developers who want the productivity of Spring Data JPA
// without leaving the Go ecosystem. The library uses Go generics (1.21+) for
// full compile-time type safety.
//
// # Key Features
//
//   - Golang ORM with Spring JPA-style query generation from method names
//   - Supports MySQL and PostgreSQL databases out of the box
//   - Auto-migration: create and update database tables from Go structs
//   - Relationship mapping: OneToOne, OneToMany, ManyToOne, ManyToMany
//   - Soft deletes, transaction management, and cursor/offset pagination
//   - Fluent query builder for complex ad-hoc SQL queries
//   - Full type safety with Go generics — no interface{} or reflection hacks
//   - Connection pooling with configurable pool sizes
//   - No code generation — everything happens at runtime
//
// # Supported Databases
//
// go-db-manager currently supports:
//
//   - MySQL (5.7+, 8.0+)
//   - PostgreSQL (12+)
//
// # Installation
//
//	go get github.com/shubhesh07/go-db-manager
//
// # Quick Start
//
// 1. Connect to your database:
//
//	config := connection.Config{
//	    Driver:   "mysql",
//	    Host:     "localhost",
//	    Port:     3306,
//	    User:     "root",
//	    Password: "secret",
//	    DBName:   "mydb",
//	}
//	connMgr := connection.GetInstance()
//	if err := connMgr.Connect(config); err != nil {
//	    log.Fatal(err)
//	}
//	defer connMgr.Close()
//
// 2. Define an entity (database table):
//
//	type Order struct {
//	    entity.BaseEntity
//	    CustomerID uint    `orm:"column:customer_id;not_null;index" json:"customer_id"`
//	    StatusID   uint    `orm:"column:status_id;not_null;index"  json:"status_id"`
//	    Total      float64 `orm:"column:total"                     json:"total"`
//	}
//
//	func (Order) TableName() string { return "orders" }
//
// 3. Auto-migrate the schema:
//
//	if err := migration.AutoMigrate(db, &Order{}); err != nil {
//	    log.Fatal(err)
//	}
//
// 4. Define a repository interface:
//
//	type OrderRepository interface {
//	    FindByCustomerID(ctx context.Context, customerID uint) ([]*Order, error)
//	    FindByStatusIDNotIn(ctx context.Context, statusIDs []uint) ([]*Order, error)
//	    FindTop5ByCustomerIDOrderByCreatedOnDesc(ctx context.Context, customerID uint) ([]*Order, error)
//	}
//
// 5. Create the repository and call methods — SQL is generated automatically:
//
//	repo := repository.NewDynamicRepository[Order](db)
//	orders, err := repo.CallMethod("FindByCustomerID", ctx, uint(42))
//
// # Search Tags
//
// golang orm, go orm, database orm, mysql orm, postgresql orm, go database,
// sql query builder, spring jpa go, spring data jpa golang, go generics orm,
// auto migration, database manager, db manager, go db manager, godbmanager
//
// # Sub-packages
//
//   - [github.com/shubhesh07/go-db-manager/connection] — database connection and pooling
//   - [github.com/shubhesh07/go-db-manager/repository] — dynamic repository with JPA-style query generation
//   - [github.com/shubhesh07/go-db-manager/migration] — auto-migration and schema management
//   - [github.com/shubhesh07/go-db-manager/query] — fluent query builder
//   - [github.com/shubhesh07/go-db-manager/transaction] — transaction management
//   - [github.com/shubhesh07/go-db-manager/pagination] — cursor and offset pagination
//   - [github.com/shubhesh07/go-db-manager/entity] — base entity with common fields
//   - [github.com/shubhesh07/go-db-manager/relationship] — relationship mapping helpers
package godbmanager

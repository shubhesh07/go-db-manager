# Go ORM Framework - Spring JPA Style

A powerful, Spring JPA-inspired ORM framework for Go that provides automatic query generation from method names, similar to Spring Data JPA.

## Features

- 🎯 **Spring JPA-Style Queries**: Write methods like `FindByOrderId` and queries are automatically generated
- 🔄 **Auto-Migration**: Automatic table creation and schema management
- 🔗 **Relationship Support**: OneToOne, OneToMany, ManyToOne, and ManyToMany relationships
- 💾 **Soft Deletes**: Built-in soft delete functionality
- 🔒 **Transaction Management**: Easy transaction handling
- 📊 **Pagination**: Built-in pagination support
- 🏷️ **Struct Tags**: Use struct tags for entity mapping (similar to JPA annotations)
- 🚀 **Query Builder**: Fluent query builder for complex queries
- ⚡ **Connection Pooling**: Optimized database connection management
- 🎨 **Type Safety**: Full type safety with Go generics

## Installation

```bash
go get github.com/shubhesh07/go-db-manager
```

## Quick Start

### 1. Define Your Entity

```go
package main

import (
    "time"
    "github.com/shubhesh07/go-db-manager/entity"
)

type OrderDetails struct {
    entity.BaseEntity
    OrderID    uint      `orm:"column:order_id;not_null;index" json:"order_id"`
    CustomerID uint      `orm:"column:customer_id;not_null;index" json:"customer_id"`
    StatusID   uint      `orm:"column:status_id;not_null;index" json:"status_id"`
    OrderValue float64   `orm:"column:order_value" json:"order_value"`
    CreatedOn  time.Time `orm:"column:created_on;default:CURRENT_TIMESTAMP" json:"created_on"`
}

func (o *OrderDetails) TableName() string {
    return "order_details"
}
```

### 2. Create Repository Interface

```go
type OrderDetailsRepository interface {
    // Standard CRUD
    FindByID(ctx context.Context, id interface{}) (*OrderDetails, error)
    FindAll(ctx context.Context) ([]*OrderDetails, error)
    Save(ctx context.Context, entity *OrderDetails) error
    
    // Spring JPA-style methods - queries are automatically generated!
    FindByOrderId(ctx context.Context, orderId uint) (*OrderDetails, error)
    FindByOrderIdIn(ctx context.Context, orderIds []uint) ([]*OrderDetails, error)
    FindByCustomerId(ctx context.Context, customerId uint) ([]*OrderDetails, error)
    FindByCustomerIdAndStatusId(ctx context.Context, customerId uint, statusId uint) ([]*OrderDetails, error)
    FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
        ctx context.Context, customerIds []uint, statusIds []uint, orderValue float64) ([]*OrderDetails, error)
    FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc(
        ctx context.Context, customerId uint, statusIds []uint) ([]*OrderDetails, error)
}
```

### 3. Implement Repository

```go
type orderDetailsRepositoryImpl struct {
    *repository.DynamicRepository[OrderDetails]
}

func NewOrderDetailsRepository() OrderDetailsRepository {
    return &orderDetailsRepositoryImpl{
        DynamicRepository: repository.NewDynamicRepository[OrderDetails](),
    }
}

// Implement interface methods (they delegate to DynamicRepository)
func (r *orderDetailsRepositoryImpl) FindByOrderId(ctx context.Context, orderId uint) (*OrderDetails, error) {
    return r.DynamicRepository.FindByOrderId(ctx, orderId)
}
// ... implement other methods similarly
```

### 4. Initialize Database

#### Option 1: Using Environment Variables (Recommended)

Set environment variables in your project (or use a `.env` file):

```bash
export DB_TYPE=postgres
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=yourpassword
export DB_NAME=mydb
export DB_SSLMODE=disable
```

Then in your code:

```go
import (
    "github.com/shubhesh07/go-db-manager/connection"
    "github.com/shubhesh07/go-db-manager/migration"
)

// Load config from environment variables
// Falls back to DefaultConfig if env vars are not set
config := connection.LoadConfigFromEnv()

connMgr := connection.GetInstance()
if err := connMgr.Connect(config); err != nil {
    log.Fatal(err)
}
defer connMgr.Close()

// Run migrations
migrator := migration.NewMigrator()
if err := migrator.AutoMigrate(&OrderDetails{}); err != nil {
    log.Fatal(err)
}
```

#### Option 2: Direct Configuration

```go
config := connection.DefaultConfig()
config.Type = connection.PostgreSQL
config.Host = "localhost"
config.Port = 5432
config.User = "postgres"
config.Password = "postgres"
config.Database = "mydb"

connMgr := connection.GetInstance()
if err := connMgr.Connect(config); err != nil {
    log.Fatal(err)
}
defer connMgr.Close()
```

**Note**: The library is designed to work with any project's configuration. Use `LoadConfigFromEnv()` to automatically read from environment variables, or pass your own `Config` object.

### 5. Use Repository

```go
ctx := context.Background()
repo := NewOrderDetailsRepository()

// These methods automatically generate SQL queries!
order, err := repo.FindByOrderId(ctx, 123)
// Generates: SELECT * FROM order_details WHERE order_id = $1

orders, err := repo.FindByOrderIdIn(ctx, []uint{123, 456, 789})
// Generates: SELECT * FROM order_details WHERE order_id IN ($1, $2, $3)

orders, err = repo.FindByCustomerIdAndStatusId(ctx, 100, 1)
// Generates: SELECT * FROM order_details WHERE customer_id = $1 AND status_id = $2

orders, err = repo.FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
    ctx, []uint{100, 200}, []uint{5, 6}, 1000.0)
// Generates complex SQL with IN, NOT IN, >=, and ORDER BY
```

## Supported Spring JPA Query Keywords

### Find Methods
- `FindBy...` - Find entities by conditions
- `FindAll` - Find all entities
- `FindTopN` or `FindFirstN` - Find top N entities

### Comparison Operators
- `And` - AND condition
- `Or` - OR condition
- `GreaterThan` - `>`
- `GreaterThanEqual` - `>=`
- `LessThan` - `<`
- `LessThanEqual` - `<=`
- `Equal` - `=`
- `NotEqual` - `!=`

### Collection Operators
- `In` - IN clause
- `NotIn` - NOT IN clause

### Null Checks
- `IsNull` - IS NULL
- `IsNotNull` - IS NOT NULL

### String Operations
- `Like` - LIKE operator
- `StartingWith` - LIKE 'value%'
- `EndingWith` - LIKE '%value'
- `Containing` - LIKE '%value%'

### Ordering
- `OrderBy...Asc` - ORDER BY ... ASC
- `OrderBy...Desc` - ORDER BY ... DESC

### Examples

```go
// Simple find
FindByOrderId(ctx, orderId uint) (*OrderDetails, error)
// SQL: SELECT * FROM order_details WHERE order_id = $1

// Find with IN
FindByOrderIdIn(ctx, orderIds []uint) ([]*OrderDetails, error)
// SQL: SELECT * FROM order_details WHERE order_id IN ($1, $2, $3)

// Find with multiple conditions
FindByCustomerIdAndStatusId(ctx, customerId uint, statusId uint) ([]*OrderDetails, error)
// SQL: SELECT * FROM order_details WHERE customer_id = $1 AND status_id = $2

// Find with NOT IN
FindByStatusIdNotIn(ctx, statusIds []uint) ([]*OrderDetails, error)
// SQL: SELECT * FROM order_details WHERE status_id NOT IN ($1, $2)

// Find with comparison
FindByOrderValueGreaterThanEqual(ctx, value float64) ([]*OrderDetails, error)
// SQL: SELECT * FROM order_details WHERE order_value >= $1

// Find with null check
FindByDoctorDigitizeByIdIsNotNull(ctx) ([]*OrderDetails, error)
// SQL: SELECT * FROM order_details WHERE doctor_digitize_id IS NOT NULL

// Find top N
FindTop5ByCustomerIdOrderByCreatedOnDesc(ctx, customerId uint) ([]*OrderDetails, error)
// SQL: SELECT * FROM order_details WHERE customer_id = $1 ORDER BY created_on DESC LIMIT 5

// Complex query
FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
    ctx, customerIds []uint, statusIds []uint, orderValue float64) ([]*OrderDetails, error)
// SQL: SELECT * FROM order_details 
//      WHERE customer_id IN ($1, $2) 
//      AND status_id NOT IN ($3, $4) 
//      AND order_value >= $5 
//      ORDER BY created_on ASC
```

## Struct Tags Reference

### Entity Tags

- `orm:"primary_key"` - Marks field as primary key
- `orm:"auto_increment"` - Auto-increment primary key
- `orm:"column:column_name"` - Custom column name
- `orm:"unique"` - Unique constraint
- `orm:"index"` - Create index
- `orm:"not_null"` - NOT NULL constraint
- `orm:"nullable"` - Allow NULL values
- `orm:"default:value"` - Default value
- `orm:"type:text"` - Custom column type
- `orm:"-"` - Ignore field

### Relationship Tags

- `orm:"one_to_one;foreign_key:user_id"` - One-to-one relationship
- `orm:"one_to_many;foreign_key:user_id"` - One-to-many relationship
- `orm:"many_to_one;foreign_key:user_id;references:id"` - Many-to-one relationship
- `orm:"many_to_many;join_table:post_tags;join_column:post_id;inverse_column:tag_id"` - Many-to-many relationship

## Standard Repository Methods

### Basic CRUD

- `FindByID(ctx, id)` - Find entity by ID
- `FindAll(ctx)` - Find all entities
- `Save(ctx, entity)` - Save new entity
- `SaveAll(ctx, entities)` - Save multiple entities
- `Update(ctx, entity)` - Update entity
- `Delete(ctx, id)` - Soft delete entity

### Query Methods

- `Count(ctx)` - Count all entities
- `Exists(ctx, id)` - Check if entity exists

## Supported Databases

- PostgreSQL (full support)
- MySQL (full support)
- SQLite (coming soon)

## Project Structure

```
go-orm/
├── connection/      # Database connection management
├── entity/          # Entity definitions and metadata
├── migration/       # Auto-migration and schema management
├── pagination/      # Pagination support
├── query/           # Query builder
├── relationship/    # Relationship handling
├── repository/      # Repository pattern with Spring JPA-style queries
│   ├── repository.go        # Base repository
│   ├── jpa_repository.go    # JPA-style repository implementation
│   ├── method_parser.go     # Method name parser
│   └── proxy_repository.go  # Dynamic repository proxy
├── transaction/     # Transaction management
├── utils/           # Utility functions
└── examples/        # Usage examples
```

## Best Practices

1. **Define Clear Interfaces**: Create repository interfaces with Spring JPA-style method names
2. **Use Type Safety**: Leverage Go generics for type-safe repositories
3. **Handle Errors**: Always check and handle errors properly
4. **Use Context**: Pass context for cancellation and timeout support
5. **Index Frequently Queried Columns**: Use `orm:"index"` tag
6. **Use Transactions**: Wrap multiple operations in transactions

## Performance Tips

- Use connection pooling (configured automatically)
- Use indexes on frequently queried columns
- Use pagination for large result sets
- Use transactions for batch operations
- Leverage Spring JPA-style methods for type-safe queries

## License

MIT License

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

---

## Also by Shubhesh

- **[DB Connect](https://github.com/shubhesh07/db-connect)** — Free desktop database IDE for MySQL, Redshift & DynamoDB. Alternative to DataGrip, DBeaver, TablePlus. ([Download](https://shubhesh07.github.io/db-connect))
- **[Claude Code Reviewer](https://github.com/shubhesh07/claude-code-reviewer)** — Automated PR/MR code reviews powered by Claude Code CLI. Works with GitHub & GitLab.

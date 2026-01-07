# Project Structure

This document describes the industry-standard project structure for the Go ORM framework.

## Directory Layout

```
go-db-manager/
├── cmd/                  # Application entry points
│   └── example/          # Example application
│       ├── main.go           # Main entry point with basic CRUD examples
│       ├── user.go           # User entity example
│       ├── order_repository.go  # OrderDetails repository with Spring JPA methods
│       └── jpa_example.go    # Spring JPA-style query examples
│
├── connection/          # Database connection management
│   └── connection.go    # Connection pool, configuration, health checks
│
├── entity/              # Entity definitions and metadata
│   └── entity.go        # Base entity, column metadata, table naming
│
├── migration/           # Database migration and schema management
│   └── migration.go      # Auto-migration, table creation, index management
│
├── pagination/          # Pagination support
│   └── pagination.go     # Page request/response, pagination utilities
│
├── query/                # Query builder
│   └── query.go          # Fluent query builder API
│
├── relationship/         # Entity relationship handling
│   └── relationship.go   # OneToOne, OneToMany, ManyToOne, ManyToMany
│
├── repository/           # Repository pattern implementation
│   ├── repository.go           # Base repository interface and implementation
│   ├── jpa_repository.go       # JPA-style repository with method parsing
│   ├── method_parser.go         # Spring JPA method name parser
│   └── proxy_repository.go      # Dynamic repository proxy
│
├── transaction/          # Transaction management
│   └── transaction.go    # Transaction lifecycle, rollback handling
│
├── utils/                # Utility functions
│   └── utils.go          # Helper functions for reflection, type conversion
│
├── go.mod                # Go module definition
├── go.sum                 # Go module checksums
├── Makefile              # Build and development commands
├── README.md             # Main documentation
├── PROJECT_STRUCTURE.md  # This file
└── .gitignore           # Git ignore rules
```

## Package Organization

### Core Packages

1. **connection** - Database connection management
   - Singleton pattern for connection pool
   - Multi-database support (PostgreSQL, MySQL, SQLite)
   - Connection health checks
   - Configurable pool settings

2. **entity** - Entity definitions
   - Base entity with common fields (ID, timestamps, soft delete)
   - Entity interface contract
   - Column metadata extraction
   - Table name resolution

3. **repository** - Repository pattern
   - Base repository with standard CRUD
   - JPA-style repository with method name parsing
   - Dynamic query generation
   - Type-safe generic repositories

4. **migration** - Schema management
   - Auto-migration from structs
   - Table creation/updates
   - Index management
   - Type mapping (Go → SQL)

### Supporting Packages

5. **query** - Query builder
   - Fluent API for building queries
   - Conditional queries (WHERE, JOIN, etc.)
   - Parameter binding

6. **transaction** - Transaction management
   - Transaction lifecycle
   - Automatic rollback on error
   - Context support

7. **relationship** - Entity relationships
   - Relationship metadata extraction
   - Lazy/eager loading
   - Join table management

8. **pagination** - Pagination utilities
   - Page request/response
   - Sorting support

9. **utils** - Utility functions
   - Reflection helpers
   - Type conversion
   - String utilities

### Application Entry Points

10. **cmd/example** - Example application
    - Demonstrates basic CRUD operations
    - Shows Spring JPA-style query usage
    - Complete working examples

## Design Patterns Used

1. **Repository Pattern** - Abstraction over data access
2. **Singleton Pattern** - Connection manager
3. **Generic Programming** - Type-safe repositories with Go generics
4. **Proxy Pattern** - Dynamic method invocation
5. **Builder Pattern** - Query builder API

## Industry Standards Followed

1. **Package Organization**
   - Clear separation of concerns
   - Single responsibility per package
   - Logical grouping of related functionality
   - `cmd/` directory for application entry points (Go standard)

2. **Naming Conventions**
   - Package names: lowercase, single word
   - File names: lowercase, snake_case
   - Public types: PascalCase
   - Private types: camelCase

3. **Error Handling**
   - Explicit error returns
   - Context-aware operations
   - Proper error wrapping

4. **Documentation**
   - Package-level documentation
   - Public API documentation
   - Usage examples

5. **Testing Structure**
   - Unit tests alongside code
   - Integration test examples
   - Testable design

## Extension Points

1. **Custom Repositories** - Extend base repository for custom logic
2. **Query Builder** - Build complex queries programmatically
3. **Migration Hooks** - Custom migration logic
4. **Relationship Loaders** - Custom relationship loading strategies

## Future Enhancements

1. Code generation for repository methods
2. Caching layer
3. Audit trail
4. Validation framework
5. Event system (before/after hooks)

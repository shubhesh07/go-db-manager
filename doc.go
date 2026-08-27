// Package gojpa brings the Spring Data JPA repository model to Go: you write
// a repository interface with method names such as FindByCustomerIdAndStatusIdIn
// or FindTop5ByWarehouseIdOrderByCreatedOnDesc, and the jpagen code generator
// emits a typed implementation that builds and executes the SQL.
//
// Sub-packages:
//
//   - jpa:     runtime — Repository[T, ID] CRUD, derived queries, paging
//     (Page/Slice/keyset Window), Specification-style Cond builder, raw
//     :name queries, hooks, transactions.
//   - sqlgen:  MySQL/Postgres dialects and SQL rendering.
//   - parser:  the method-name grammar (a port of Spring's PartTree).
//   - entity:  struct metadata from db/orm tags and row scanning.
//   - cmd/jpagen: the go:generate tool. Unknown properties or wrong argument
//     counts fail at generate time, the way Spring fails at startup.
//
// Install:
//
//	go get github.com/shubhesh07/gojpa
//
// See README.md for the grammar and examples, and example/warehouse for a
// complete repository with derived, declared (jpa:query) and hand-written
// methods.
package gojpa

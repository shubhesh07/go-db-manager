// Package transaction provides utilities for managing database transactions
// in go-db-manager.
//
// It supports both manual transaction management and a convenient
// ExecuteInTransaction helper that handles commit/rollback automatically.
//
// Example:
//
//	err := transaction.ExecuteInTransaction(ctx, func(tm *transaction.TransactionManager) error {
//		// perform multiple operations using tm.GetTx()
//		return nil
//	})
package transaction

import (
		"context"
		"database/sql"
		"fmt"

		"github.com/shubhesh07/go-db-manager/connection"
	)

// TransactionManager wraps a database transaction and provides
// commit and rollback methods.
type TransactionManager struct {
		tx *sql.Tx
}

// Begin starts a new database transaction and returns a TransactionManager.
// The caller must call Commit or Rollback when done.
//
// Example:
//
//	tm, err := transaction.Begin(ctx)
//	if err != nil { ... }
//	defer tm.Rollback()
//	// ... do work ...
//	return tm.Commit()
func Begin(ctx context.Context) (*TransactionManager, error) {
		db := connection.GetInstance().GetDB()
		if db == nil {
					return nil, fmt.Errorf("database connection is not initialized")
				}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
					return nil, fmt.Errorf("failed to begin transaction: %w", err)
				}
		return &TransactionManager{tx: tx}, nil
}

// GetTx returns the underlying *sql.Tx for use in raw queries.
func (tm *TransactionManager) GetTx() *sql.Tx {
		return tm.tx
}

// Commit commits the transaction. Returns an error if the transaction
// has not been started.
func (tm *TransactionManager) Commit() error {
		if tm.tx == nil {
					return fmt.Errorf("transaction is not initialized")
				}
		return tm.tx.Commit()
}

// Rollback rolls back the transaction. It is safe to call Rollback
// even after a successful Commit (it becomes a no-op).
func (tm *TransactionManager) Rollback() error {
		if tm.tx == nil {
					return nil
				}
		return tm.tx.Rollback()
}

// ExecuteInTransaction runs the given function within a database transaction.
// It automatically commits if fn returns nil, or rolls back if fn returns an error
// or panics. This is the recommended way to run transactional operations.
//
// Example:
//
//	err := transaction.ExecuteInTransaction(ctx, func(tm *transaction.TransactionManager) error {
//		tx := tm.GetTx()
//		_, err := tx.ExecContext(ctx, "UPDATE orders SET status=$1 WHERE id=$2", "shipped", 42)
//		return err
//	})
func ExecuteInTransaction(ctx context.Context, fn func(*TransactionManager) error) error {
		tm, err := Begin(ctx)
		if err != nil {
					return err
				}
		defer func() {
					if p := recover(); p != nil {
									tm.Rollback()
									panic(p)
								}
				}()
		if err := fn(tm); err != nil {
					if rbErr := tm.Rollback(); rbErr != nil {
									return fmt.Errorf("transaction error: %w, rollback error: %v", err, rbErr)
								}
					return err
				}
		return tm.Commit()
}

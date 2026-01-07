package transaction

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shubhesh07/go-db-manager/connection"
)

// TransactionManager manages database transactions
type TransactionManager struct {
	tx *sql.Tx
}

// Begin starts a new transaction
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

// GetTx returns the underlying transaction
func (tm *TransactionManager) GetTx() *sql.Tx {
	return tm.tx
}

// Commit commits the transaction
func (tm *TransactionManager) Commit() error {
	if tm.tx == nil {
		return fmt.Errorf("transaction is not initialized")
	}
	return tm.tx.Commit()
}

// Rollback rolls back the transaction
func (tm *TransactionManager) Rollback() error {
	if tm.tx == nil {
		return nil
	}
	return tm.tx.Rollback()
}

// ExecuteInTransaction executes a function within a transaction
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

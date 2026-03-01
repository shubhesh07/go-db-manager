// Package connection manages database connections for go-db-manager.
//
// It provides a singleton ConnectionManager that supports PostgreSQL and MySQL,
// with built-in connection pooling and health checks. Configuration can be
// loaded from environment variables using LoadConfigFromEnv or set manually
// using DefaultConfig.
//
// Example:
//
//	config := connection.LoadConfigFromEnv()
//	connMgr := connection.GetInstance()
//	if err := connMgr.Connect(config); err != nil {
//		log.Fatal(err)
//	}
//	defer connMgr.Close()
package connection

import (
		"database/sql"
		"fmt"
		"sync"
		"time"
	)

// DBType represents the type of database to connect to.
type DBType string

const (
		// PostgreSQL is the identifier for a PostgreSQL database.
		PostgreSQL DBType = "postgres"
		// MySQL is the identifier for a MySQL database.
		MySQL DBType = "mysql"
		// SQLite is the identifier for a SQLite database.
		SQLite DBType = "sqlite3"
	)

// Config holds all configuration needed to establish a database connection,
// including credentials, pool settings, and timeouts.
type Config struct {
		// Type is the database driver type (PostgreSQL, MySQL, or SQLite).
		Type DBType
		// Host is the database server hostname or IP address.
		Host string
		// Port is the database server port.
		Port int
		// User is the database username.
		User string
		// Password is the database password.
		Password string
		// Database is the name of the database to connect to.
		Database string
		// SSLMode controls SSL for PostgreSQL (e.g. "disable", "require").
		SSLMode string
		// MaxOpenConns is the maximum number of open connections in the pool.
		MaxOpenConns int
		// MaxIdleConns is the maximum number of idle connections in the pool.
		MaxIdleConns int
		// ConnMaxLifetime is the maximum time a connection may be reused.
		ConnMaxLifetime time.Duration
		// ConnMaxIdleTime is the maximum time a connection may remain idle.
		ConnMaxIdleTime time.Duration
}

// DefaultConfig returns a Config with sensible defaults for connection pooling.
// Type, Host, Port, User, Password, and Database must be set before use.
func DefaultConfig() *Config {
		return &Config{
					MaxOpenConns:    25,
					MaxIdleConns:    5,
					ConnMaxLifetime: 5 * time.Minute,
					ConnMaxIdleTime: 10 * time.Minute,
					SSLMode:         "disable",
				}
}

// ConnectionManager manages a single database connection with pooling.
// Use GetInstance to obtain the global singleton.
type ConnectionManager struct {
		db     *sql.DB
		config *Config
		mu     sync.RWMutex
}

var (
		instance *ConnectionManager
		once     sync.Once
	)

// GetInstance returns the global singleton ConnectionManager.
// It is safe to call from multiple goroutines.
func GetInstance() *ConnectionManager {
		once.Do(func() {
					instance = &ConnectionManager{}
				})
		return instance
}

// Connect establishes the database connection using the provided Config.
// It configures connection pool settings and verifies the connection with a ping.
//
// Example:
//
//	err := connection.GetInstance().Connect(config)
func (cm *ConnectionManager) Connect(config *Config) error {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		cm.config = config
		dsn := cm.buildDSN()

		db, err := sql.Open(string(config.Type), dsn)
		if err != nil {
					return fmt.Errorf("failed to open database: %w", err)
				}

		// Configure connection pool
		db.SetMaxOpenConns(config.MaxOpenConns)
		db.SetMaxIdleConns(config.MaxIdleConns)
		db.SetConnMaxLifetime(config.ConnMaxLifetime)
		db.SetConnMaxIdleTime(config.ConnMaxIdleTime)

		// Test connection
		if err := db.Ping(); err != nil {
					return fmt.Errorf("failed to ping database: %w", err)
				}

		cm.db = db
		return nil
}

// GetDB returns the underlying *sql.DB instance.
// Returns nil if Connect has not been called successfully.
func (cm *ConnectionManager) GetDB() *sql.DB {
		cm.mu.RLock()
		defer cm.mu.RUnlock()
		return cm.db
}

// GetDBType returns the configured database type.
// Defaults to PostgreSQL if no configuration has been set.
func (cm *ConnectionManager) GetDBType() DBType {
		cm.mu.RLock()
		defer cm.mu.RUnlock()
		if cm.config == nil {
					return PostgreSQL
				}
		return cm.config.Type
}

// Close closes the database connection and releases all pooled connections.
func (cm *ConnectionManager) Close() error {
		cm.mu.Lock()
		defer cm.mu.Unlock()
		if cm.db != nil {
					return cm.db.Close()
				}
		return nil
}

// buildDSN constructs the database connection string
func (cm *ConnectionManager) buildDSN() string {
		config := cm.config
		switch config.Type {
				case PostgreSQL:
					return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
									   			config.Host, config.Port, config.User, config.Password, config.Database, config.SSLMode)
				case MySQL:
					return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
									   			config.User, config.Password, config.Host, config.Port, config.Database)
				case SQLite:
					return config.Database
				default:
					return ""
				}
}

// HealthCheck pings the database to verify the connection is alive.
// Returns an error if the connection has not been initialized or if the ping fails.
func (cm *ConnectionManager) HealthCheck() error {
		cm.mu.RLock()
		defer cm.mu.RUnlock()
		if cm.db == nil {
					return fmt.Errorf("database connection is not initialized")
				}
		return cm.db.Ping()
}

package connection

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// DBType represents the database type
type DBType string

const (
	PostgreSQL DBType = "postgres"
	MySQL      DBType = "mysql"
	SQLite     DBType = "sqlite3"
)

// Config holds database configuration
type Config struct {
	Type            DBType
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 10 * time.Minute,
		SSLMode:         "disable",
	}
}

// ConnectionManager manages database connections
type ConnectionManager struct {
	db     *sql.DB
	config *Config
	mu     sync.RWMutex
}

var (
	instance *ConnectionManager
	once     sync.Once
)

// GetInstance returns a singleton instance of ConnectionManager
func GetInstance() *ConnectionManager {
	once.Do(func() {
		instance = &ConnectionManager{}
	})
	return instance
}

// Connect establishes a database connection
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

// GetDB returns the underlying *sql.DB
func (cm *ConnectionManager) GetDB() *sql.DB {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.db
}

// Close closes the database connection
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

// HealthCheck performs a health check on the database connection
func (cm *ConnectionManager) HealthCheck() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.db == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	return cm.db.Ping()
}

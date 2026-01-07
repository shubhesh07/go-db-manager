package connection

import (
	"os"
	"strconv"
)

// LoadConfigFromEnv loads database configuration from environment variables
// Falls back to DefaultConfig values if environment variables are not set
// This allows the library to be used in any project without hardcoded values
func LoadConfigFromEnv() *Config {
	config := DefaultConfig()

	// Database type
	if dbType := os.Getenv("DB_TYPE"); dbType != "" {
		switch dbType {
		case "postgres", "postgresql":
			config.Type = PostgreSQL
		case "mysql":
			config.Type = MySQL
		case "sqlite", "sqlite3":
			config.Type = SQLite
		}
	}

	// Database host
	if host := os.Getenv("DB_HOST"); host != "" {
		config.Host = host
	}

	// Database port
	if portStr := os.Getenv("DB_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
			config.Port = port
		}
	} else {
		// Set default port based on database type if not specified
		switch config.Type {
		case PostgreSQL:
			config.Port = 5432
		case MySQL:
			config.Port = 3306
		case SQLite:
			config.Port = 0
		}
	}

	// Database user
	if user := os.Getenv("DB_USER"); user != "" {
		config.User = user
	}

	// Database password
	if password := os.Getenv("DB_PASSWORD"); password != "" {
		config.Password = password
	}

	// Database name
	if database := os.Getenv("DB_NAME"); database != "" {
		config.Database = database
	}

	// SSL mode (for PostgreSQL)
	if sslMode := os.Getenv("DB_SSLMODE"); sslMode != "" {
		config.SSLMode = sslMode
	}

	// Connection pool settings
	if maxOpenConnsStr := os.Getenv("DB_MAX_OPEN_CONNS"); maxOpenConnsStr != "" {
		if maxOpenConns, err := strconv.Atoi(maxOpenConnsStr); err == nil && maxOpenConns > 0 {
			config.MaxOpenConns = maxOpenConns
		}
	}

	if maxIdleConnsStr := os.Getenv("DB_MAX_IDLE_CONNS"); maxIdleConnsStr != "" {
		if maxIdleConns, err := strconv.Atoi(maxIdleConnsStr); err == nil && maxIdleConns > 0 {
			config.MaxIdleConns = maxIdleConns
		}
	}

	return config
}

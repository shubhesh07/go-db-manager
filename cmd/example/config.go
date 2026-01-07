package main

import (
	"github.com/shubhesh07/go-db-manager/connection"
)

// LoadConfig loads database configuration from environment variables
// Falls back to DefaultConfig if environment variables are not set
// This demonstrates how to use the library in your project
func LoadConfig() *connection.Config {
	// Use the built-in LoadConfigFromEnv which handles all environment variables
	// and falls back to DefaultConfig values
	return connection.LoadConfigFromEnv()
}

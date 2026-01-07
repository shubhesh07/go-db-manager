package main

import (
	"context"
	"fmt"
	"log"

	"github.com/shubhesh07/go-db-manager/connection"
	"github.com/shubhesh07/go-db-manager/migration"
	"github.com/shubhesh07/go-db-manager/repository"
	"github.com/shubhesh07/go-db-manager/transaction"
)

func main() {
	// Load configuration from environment variables
	// Falls back to DefaultConfig if environment variables are not set
	// Set these environment variables in your project:
	//   DB_TYPE=postgres (or mysql, sqlite)
	//   DB_HOST=localhost
	//   DB_PORT=5432
	//   DB_USER=postgres
	//   DB_PASSWORD=yourpassword
	//   DB_NAME=yourdatabase
	//   DB_SSLMODE=disable (for PostgreSQL)
	//   DB_MAX_OPEN_CONNS=25 (optional)
	//   DB_MAX_IDLE_CONNS=5 (optional)
	config := LoadConfig()

	connMgr := connection.GetInstance()
	if err := connMgr.Connect(config); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer connMgr.Close()

	// Run migrations
	migrator := migration.NewMigrator()
	if err := migrator.AutoMigrate(
		&User{},
		&Profile{},
		&Post{},
		&Tag{},
	); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	ctx := context.Background()

	// Create repositories
	userRepo := repository.NewRepository[User]()
	postRepo := repository.NewRepository[Post]()

	// Example 1: Save a new user
	user := &User{
		Username:  "johndoe",
		Email:     "john@example.com",
		Password:  "hashedpassword",
		FirstName: "John",
		LastName:  "Doe",
		IsActive:  true,
	}

	if err := userRepo.Save(ctx, user); err != nil {
		log.Fatalf("Failed to save user: %v", err)
	}
	fmt.Printf("Created user with ID: %d\n", user.ID)

	// Example 2: Find user by ID
	foundUser, err := userRepo.FindByID(ctx, user.ID)
	if err != nil {
		log.Fatalf("Failed to find user: %v", err)
	}
	fmt.Printf("Found user: %s (%s)\n", foundUser.Username, foundUser.Email)

	// Example 3: Find users by conditions
	users, err := userRepo.FindBy(ctx, map[string]interface{}{
		"is_active": true,
	})
	if err != nil {
		log.Fatalf("Failed to find users: %v", err)
	}
	fmt.Printf("Found %d active users\n", len(users))

	// Example 4: Update user
	foundUser.FirstName = "Jane"
	if err := userRepo.Update(ctx, foundUser); err != nil {
		log.Fatalf("Failed to update user: %v", err)
	}
	fmt.Printf("Updated user: %s\n", foundUser.FirstName)

	// Example 5: Create a post
	post := &Post{
		UserID:    user.ID,
		Title:     "My First Post",
		Content:   "This is the content of my first post.",
		Published: true,
	}

	if err := postRepo.Save(ctx, post); err != nil {
		log.Fatalf("Failed to save post: %v", err)
	}
	fmt.Printf("Created post with ID: %d\n", post.ID)

	// Example 6: Count entities
	count, err := userRepo.Count(ctx)
	if err != nil {
		log.Fatalf("Failed to count users: %v", err)
	}
	fmt.Printf("Total users: %d\n", count)

	// Example 7: Transaction example
	err = transaction.ExecuteInTransaction(ctx, func(tm *transaction.TransactionManager) error {
		// Create multiple users in a transaction
		user1 := &User{
			Username: "user1",
			Email:    "user1@example.com",
			Password: "pass1",
		}

		user2 := &User{
			Username: "user2",
			Email:    "user2@example.com",
			Password: "pass2",
		}

		// In a real implementation, you would use the transaction manager
		// to execute queries within the transaction
		_ = user1
		_ = user2

		return nil
	})

	if err != nil {
		log.Fatalf("Transaction failed: %v", err)
	}

	// Example 8: Soft delete
	if err := userRepo.Delete(ctx, user.ID); err != nil {
		log.Fatalf("Failed to delete user: %v", err)
	}
	fmt.Printf("Soft deleted user with ID: %d\n", user.ID)

	// Example 9: Check existence
	exists, err := userRepo.Exists(ctx, user.ID)
	if err != nil {
		log.Fatalf("Failed to check existence: %v", err)
	}
	fmt.Printf("User exists (should be false after soft delete): %v\n", exists)

	fmt.Println("\nAll examples completed successfully!")

	// Run Spring JPA-style examples
	fmt.Println("\n=== Running Spring JPA-style Examples ===")
	runJPAExample()
}

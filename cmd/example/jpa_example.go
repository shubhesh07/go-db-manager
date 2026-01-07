package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/shubhesh07/go-db-manager/migration"
)

func runJPAExample() {
	// Load configuration from environment variables
	// Uses the same configuration as main() - no need to reconnect if already connected
	// If you need a separate connection, uncomment below:
	// config := LoadConfig()
	// connMgr := connection.GetInstance()
	// if err := connMgr.Connect(config); err != nil {
	// 	log.Fatalf("Failed to connect to database: %v", err)
	// }
	// defer connMgr.Close()

	// Run migrations
	migrator := migration.NewMigrator()
	if err := migrator.AutoMigrate(&OrderDetails{}); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	ctx := context.Background()
	repo := NewOrderDetailsRepository()

	// Example 1: FindByOrderId - automatically generates: SELECT * FROM order_details WHERE order_id = $1
	order, err := repo.FindByOrderId(ctx, 123)
	if err != nil {
		fmt.Printf("Order not found: %v\n", err)
	} else {
		fmt.Printf("Found order: %+v\n", order)
	}

	// Example 2: FindByOrderIdIn - automatically generates: SELECT * FROM order_details WHERE order_id IN ($1, $2, $3)
	orders, err := repo.FindByOrderIdIn(ctx, []uint{123, 456, 789})
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found %d orders\n", len(orders))
	}

	// Example 3: FindByCustomerId - automatically generates: SELECT * FROM order_details WHERE customer_id = $1
	orders, err = repo.FindByCustomerId(ctx, 100)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found %d orders for customer\n", len(orders))
	}

	// Example 4: FindByCustomerIdAndStatusId
	// Automatically generates: SELECT * FROM order_details WHERE customer_id = $1 AND status_id = $2
	orders, err = repo.FindByCustomerIdAndStatusId(ctx, 100, 1)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found %d orders with customer and status\n", len(orders))
	}

	// Example 5: Complex query - FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc
	// Automatically generates complex SQL with IN, NOT IN, >=, and ORDER BY
	orders, err = repo.FindByCustomerIdInAndStatusIdNotInAndOrderValueGreaterThanEqualOrderByCreatedOnAsc(
		ctx, []uint{100, 200}, []uint{5, 6}, 1000.0)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found %d orders with complex conditions\n", len(orders))
	}

	// Example 6: FindByCustomerIdAndOrderValueGreaterThanEqual
	orders, err = repo.FindByCustomerIdAndOrderValueGreaterThanEqual(ctx, 100, 500.0)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found %d orders with value >= 500\n", len(orders))
	}

	// Example 7: FindTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc
	// Automatically generates: SELECT * FROM order_details WHERE customer_id = $1 AND status_id = $2
	// AND doctor_digitize_id IS NOT NULL AND org_subs_id IS NOT NULL ORDER BY order_id DESC LIMIT 1
	order, err = repo.FindTop1ByCustomerIdAndStatusIdAndDoctorDigitizeByIdIsNotNullAndOrgSubsIdIsNotNullOrderByOrderIdDesc(
		ctx, 100, 1)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found top order: %+v\n", order)
	}

	// Example 8: FindByOrderIdAndCustomerId
	order, err = repo.FindByOrderIdAndCustomerId(ctx, 123, 100)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found order: %+v\n", order)
	}

	// Example 9: FindByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual
	orders, err = repo.FindByCustomerIdAndStatusIdNotInAndCreatedOnGreaterThanEqual(
		ctx, 100, []uint{5, 6}, time.Now().AddDate(0, -1, 0))
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found %d orders in last month\n", len(orders))
	}

	// Example 10: FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc
	orders, err = repo.FindTop5ByCustomerIdAndStatusIdNotInOrderByCreatedOnDesc(ctx, 100, []uint{5, 6})
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found top 5 orders: %d\n", len(orders))
	}

	fmt.Println("\nAll Spring JPA-style examples completed!")
}

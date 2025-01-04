package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/streadway/amqp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	AmqpURL   = os.Getenv("AMQP_URL")
	QueueName = "events"
)

var DB *gorm.DB

type Event struct {
	ID        uint                   `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `gorm:"type:jsonb" json:"data"`
}

type OrderDetails struct {
	OrderID   uint `gorm:"primaryKey" json:"order_id"`
	ProductID uint `json:"product_id"`
	Quantity  uint `json:"quantity"`
}

func InitDB() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect to DB: %w", err)
	}

	if err := db.AutoMigrate(&Event{}, &OrderDetails{}); err != nil {
		return fmt.Errorf("failed to migrate DB: %w", err)
	}

	DB = db
	log.Println("Database initialized & migrated successfully")
	return nil
}

func createOrderDetails(data map[string]interface{}) error {
	orderIDFloat, ok := data["order_id"].(float64)
	if !ok {
		return fmt.Errorf("cannot parse order_id")
	}
	productIDFloat, ok := data["product_id"].(float64)
	if !ok {
		return fmt.Errorf("cannot parse product_id")
	}
	quantityFloat, ok := data["quantity"].(float64)
	if !ok {
		return fmt.Errorf("cannot parse quantity")
	}

	orderID := uint(orderIDFloat)

	var existing OrderDetails
	if err := DB.First(&existing, orderID).Error; err == nil {
		return fmt.Errorf("order details already exists for order_id %v", orderID)
	}

	op := OrderDetails{
		OrderID:   orderID,
		ProductID: uint(productIDFloat),
		Quantity:  uint(quantityFloat),
	}
	if err := DB.Create(&op).Error; err != nil {
		return fmt.Errorf("failed to create order details: %w", err)
	}
	log.Printf("Order details created: %+v\n", op)
	return nil
}

func updateOrderDetails(data map[string]interface{}) error {
	orderIDFloat, ok := data["order_id"].(float64)
	if !ok {
		return fmt.Errorf("cannot parse order_id")
	}
	quantityFloat, ok := data["quantity"].(float64)
	if !ok {
		return fmt.Errorf("cannot parse quantity")
	}

	orderID := uint(orderIDFloat)
	var existing OrderDetails
	if err := DB.First(&existing, orderID).Error; err != nil {
		return fmt.Errorf("order details not found for order_id %v", orderID)
	}

	existing.Quantity = uint(quantityFloat)
	if err := DB.Save(&existing).Error; err != nil {
		return fmt.Errorf("failed to update order details: %w", err)
	}
	log.Printf("Order details updated: %+v\n", existing)
	return nil
}

func deleteOrderDetails(data map[string]interface{}) error {
	orderIDFloat, ok := data["order_id"].(float64)
	if !ok {
		return fmt.Errorf("cannot parse order_id")
	}

	orderID := uint(orderIDFloat)
	var existing OrderDetails
	if err := DB.First(&existing, orderID).Error; err != nil {
		return fmt.Errorf("order details not found for order_id %v", orderID)
	}

	if err := DB.Delete(&existing).Error; err != nil {
		return fmt.Errorf("failed to delete order details: %w", err)
	}
	log.Printf("Order details deleted: %+v\n", existing)
	return nil
}

type ReceivedEvent struct {
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
}

func processEvent(evt ReceivedEvent) {
	log.Printf("Received event: %v\n", evt)

	switch evt.EventType {
	case "order_created":
		if err := createOrderDetails(evt.Data); err != nil {
			log.Println("Error creating order details:", err)
		}
	case "order_updated":
		if err := updateOrderDetails(evt.Data); err != nil {
			log.Println("Error updating order details:", err)
		}
	case "order_deleted":
		if err := deleteOrderDetails(evt.Data); err != nil {
			log.Println("Error deleting order details:", err)
		}
	default:
		log.Println("Unknown event type:", evt.EventType)
	}
}

func startConsumer() (*amqp.Connection, error) {
	conn, err := amqp.Dial(AmqpURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	_, err = ch.QueueDeclare(
		QueueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	msgs, err := ch.Consume(
		QueueName, // queue
		"",        // consumer
		false,     // autoAck
		false,     // exclusive
		false,     // noLocal
		false,     // noWait
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register consumer: %w", err)
	}

	go func() {
		for d := range msgs {
			var evt ReceivedEvent
			if err := json.Unmarshal(d.Body, &evt); err != nil {
				log.Println("Error unmarshaling event:", err)
				d.Nack(false, false)
				continue
			}
			processEvent(evt)
			d.Ack(false)
		}
	}()

	log.Printf("Waiting for messages on queue: %s\n", QueueName)
	return conn, nil
}

func main() {
	if err := InitDB(); err != nil {
		log.Fatalf("Cannot init DB: %v", err)
	}

	conn, err := startConsumer()
	if err != nil {
		log.Fatalf("Error in consumer: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	log.Println("Shutting down...")

	if conn != nil {
		_ = conn.Close()
	}

	sqlDB, err := DB.DB()
	if err == nil {
		_ = sqlDB.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done()
	log.Println("Stopped gracefully")
}

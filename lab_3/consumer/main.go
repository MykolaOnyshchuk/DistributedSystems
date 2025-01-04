package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/datatypes"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType string         `json:"event_type"`
	Data      datatypes.JSON `gorm:"type:jsonb" json:"data"`
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
		return fmt.Errorf("failed to connect DB: %w", err)
	}

	if err := db.AutoMigrate(&Event{}, &OrderDetails{}); err != nil {
		return fmt.Errorf("failed to migrate DB: %w", err)
	}

	DB = db
	log.Println("Database initialized and migrated successfully")
	return nil
}

type CreateEventRequest struct {
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
}

func publishEvent(evt Event) error {
	conn, err := amqp.Dial(AmqpURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open a channel: %w", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		QueueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	body, err := json.Marshal(map[string]interface{}{
		"event_type": evt.EventType,
		"data":       evt.Data,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	err = ch.Publish(
		"",
		QueueName,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	log.Printf("Event published: %v\n", string(body))
	return nil
}

func handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Cannot decode JSON", http.StatusBadRequest)
		return
	}

	marshalledData, err := json.Marshal(req.Data)
	if err != nil {
		http.Error(w, "Failed to marshal Data", http.StatusInternalServerError)
		return
	}

	newEvent := Event{
		EventType: req.EventType,
		Data:      datatypes.JSON(marshalledData),
	}
	if err := DB.Create(&newEvent).Error; err != nil {
		http.Error(w, "Failed to save event to DB", http.StatusInternalServerError)
		return
	}

	if err := publishEvent(newEvent); err != nil {
		http.Error(w, "Failed to publish event", http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"message": "Event created and published",
		"event":   newEvent,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleGetEvents(w http.ResponseWriter, r *http.Request) {
	var events []Event
	if err := DB.Find(&events).Error; err != nil {
		http.Error(w, "Failed to query events", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
	})
}

func handleGetOrders(w http.ResponseWriter, r *http.Request) {
	var orderDetails []OrderDetails
	if err := DB.Find(&orderDetails).Error; err != nil {
		http.Error(w, "Failed to query orderDetails", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"orderDetails": orderDetails,
	})
}

func main() {
	if err := InitDB(); err != nil {
		log.Fatalf("Cannot init DB: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/post_events", handleCreateEvent)
	mux.HandleFunc("/get_events", handleGetEvents)
	mux.HandleFunc("/get_orders_details", handleGetOrders)

	srv := &http.Server{
		Addr:    ":5050",
		Handler: mux,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Server is running on port %s\n", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server Shutdown Failed:%+v", err)
	}

	sqlDB, err := DB.DB()
	if err == nil {
		_ = sqlDB.Close()
	}

	wg.Wait()
	log.Println("Server stopped gracefully")
}

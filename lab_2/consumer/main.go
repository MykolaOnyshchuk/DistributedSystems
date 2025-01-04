package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/streadway/amqp"
)

var (
	amqpURL     = os.Getenv("AMQP_URL")
	serviceName = getEnvOrDefault("SERVICE_NAME", "consumer")
	port        = getEnvOrDefault("PORT", "8000")
)

var (
	conn    *amqp.Connection
	channel *amqp.Channel
)

var (
	callbackQueue string
	responseMap   = sync.Map{}
)

type consumerResponse struct {
	Response    interface{} `json:"response"`
	RequestTime float64     `json:"request_time"`
}

type providerResponse struct {
	Result          string  `json:"result"`
	ComputationTime float64 `json:"computation_time"`
	Provider        string  `json:"provider"`
}

func main() {
	// Підключаємося до RabbitMQ
	err := connectRabbitMQ()
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer closeRabbitMQ()

	// HTTP-сервер
	mux := http.NewServeMux()
	mux.HandleFunc("/add_task", handleCreateTask) // POST

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, os.Kill)

	go func() {
		log.Printf("Consumer service is running on port %s\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server ListenAndServe error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server Shutdown error: %v", err)
	}
	log.Println("HTTP server stopped")
}

func connectRabbitMQ() error {
	if amqpURL == "" {
		return errors.New("AMQP_URL is not set")
	}
	log.Println("Connecting to RabbitMQ...")

	var err error
	conn, err = amqp.Dial(amqpURL)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	channel, err = conn.Channel()
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}

	_, err = channel.QueueDeclare(
		"priority_queue",
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("queue declare: %w", err)
	}

	q, err := channel.QueueDeclare(
		"",
		false, // durable
		false, // autoDelete
		true,  // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		return fmt.Errorf("callbackQueue declare: %w", err)
	}
	callbackQueue = q.Name

	msgs, err := channel.Consume(
		callbackQueue,
		"",
		true,  // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("consume callbackQueue: %w", err)
	}

	go func() {
		for msg := range msgs {
			corrId := msg.CorrelationId
			resp := msg.Body

			if chInterface, ok := responseMap.Load(corrId); ok {
				if ch, ok := chInterface.(chan []byte); ok {
					ch <- resp
				}
			}
		}
	}()

	log.Println("Connected to RabbitMQ.")
	return nil
}

func closeRabbitMQ() {
	if channel != nil {
		channel.Close()
	}
	if conn != nil {
		conn.Close()
	}
}

func handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if channel == nil {
		http.Error(w, "Consumer is not connected to RabbitMQ", http.StatusInternalServerError)
		return
	}

	var requestBody struct {
		Task     int   `json:"task"`
		Priority uint8 `json:"priority"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Bad Request: %v", err), http.StatusBadRequest)
		return
	}

	startTime := time.Now()

	resp, err := sendTask(requestBody.Task, requestBody.Priority)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send task: %v", err), http.StatusInternalServerError)
		return
	}

	requestTime := time.Since(startTime).Seconds()

	response := consumerResponse{
		Response:    resp,
		RequestTime: requestTime,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("Processed request for task=%d in %.6f seconds\n", requestBody.Task, requestTime)
}

func sendTask(task int, priority uint8) (*providerResponse, error) {
	if channel == nil {
		return nil, errors.New("RabbitMQ channel is not initialized")
	}

	corrId := uuid.New().String()

	respCh := make(chan []byte, 1)
	responseMap.Store(corrId, respCh)
	defer responseMap.Delete(corrId)

	taskBytes, _ := json.Marshal(task)

	err := channel.Publish(
		"", // exchange
		"priority_queue",
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: corrId,
			ReplyTo:       callbackQueue,
			Priority:      priority,
			Body:          taskBytes,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to publish: %w", err)
	}

	select {
	case data := <-respCh:
		var provResp providerResponse
		if err := json.Unmarshal(data, &provResp); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		log.Printf("Response: %+v, Consumer: %s\n", provResp, serviceName)
		return &provResp, nil
	case <-time.After(60 * time.Second):
		return nil, errors.New("timeout waiting for response from provider")
	}
}

func getEnvOrDefault(key, defVal string) string {
	val := os.Getenv(key)
	if val == "" {
		return defVal
	}
	return val
}

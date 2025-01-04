package main

import (
	"encoding/json"
	"log"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/streadway/amqp"
)

var (
	amqpURL     = os.Getenv("AMQP_URL")
	serviceName = getEnvOrDefault("SERVICE_NAME", "provider")
)

type providerResponse struct {
	Result          string  `json:"result"`
	ComputationTime float64 `json:"computation_time"`
	Provider        string  `json:"provider"`
}

func main() {
	if amqpURL == "" {
		log.Fatal("AMQP_URL is not set")
	}

	log.Printf("Connecting to RabbitMQ at %s\n", amqpURL)
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		"priority_queue",
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,   // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	log.Println("Provider is waiting for messages...")

	msgs, err := ch.Consume(
		"priority_queue",
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to consume: %v", err)
	}

	go func() {
		for msg := range msgs {
			body := msg.Body
			log.Printf("Received task: %s", string(body))

			number, err := strconv.ParseInt(string(body), 10, 64)
			if err != nil {
				number = 0
			}

			startTime := time.Now()

			bigNumber := bigIntFromInt64(number)

			sum := new(big.Int).Set(bigNumber)
			currentTerm := new(big.Int).Set(bigNumber)

			for i := 0; i < 100; i++ {
				sum.Add(sum, currentTerm)
				currentTerm.Mul(currentTerm, big.NewInt(2))
			}

			modulus := new(big.Int).Exp(big.NewInt(3), big.NewInt(60), nil) // 3^60
			sum.Mod(sum, modulus)

			computationTime := time.Since(startTime).Seconds()

			response := providerResponse{
				Result:          sum.String(),
				ComputationTime: computationTime,
				Provider:        serviceName,
			}

			respBytes, err := json.Marshal(response)
			if err != nil {
				log.Printf("Failed to marshal response: %v", err)
				msg.Nack(false, false)
				continue
			}

			err = ch.Publish(
				"", // exchange
				msg.ReplyTo,
				false,
				false,
				amqp.Publishing{
					ContentType:   "application/json",
					CorrelationId: msg.CorrelationId,
					Body:          respBytes,
				},
			)
			if err != nil {
				log.Printf("Failed to publish response: %v", err)
				msg.Nack(false, false)
				continue
			}

			msg.Ack(false)

			log.Printf("Task '%s' processed in %.6f seconds and response sent. Result: %s\n", string(body), computationTime, sum.String())
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, os.Kill)
	<-stop
	log.Println("Provider shutting down...")
}

func bigIntFromInt64(n int64) *big.Int {
	return big.NewInt(n)
}

func getEnvOrDefault(key, defVal string) string {
	val := os.Getenv(key)
	if val == "" {
		return defVal
	}
	return val
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apache/rocketmq-client-go/v2"

	"notification-system/pkg/config"
	"notification-system/pkg/event"
	"notification-system/pkg/mq"
)

func main() {
	// 1. Load and Validate Configuration
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Println("Configuration loaded and validated.")

	// 2. Initialize Producer (for Event Ingestion)
	producer, err := mq.NewProducer(cfg.MQ.NameServer, cfg.MQ.AccessKey, cfg.MQ.SecretKey)
	if err != nil {
		log.Fatalf("Failed to start producer: %v", err)
	}
	defer producer.Shutdown()
	log.Println("RocketMQ Producer initialized.")

	// 3. Setup HTTP Server (Event Ingestion API)
	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		handleEventIngestion(w, r, producer, cfg)
	})

	// 4. Start Server
	server := &http.Server{Addr: ":8080"}
	go func() {
		log.Println("API Server started on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// 5. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down API Server...")
	
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("API Server exited")
}

func handleEventIngestion(w http.ResponseWriter, r *http.Request, producer rocketmq.Producer, cfg *config.Config) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var evt event.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Basic validation
	if evt.Type == "" {
		http.Error(w, "Event type is required", http.StatusBadRequest)
		return
	}

	// Find config to get Topic (QueueName)
	notifyConfig := cfg.FindNotificationConfig(evt.Type)
	if notifyConfig == nil {
		http.Error(w, "Unknown event type: "+evt.Type, http.StatusBadRequest)
		return
	}
	topic := notifyConfig.QueueName
	
	// Ensure timestamp is set
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	body, _ := json.Marshal(evt)
	
	if err := mq.SendMessage(context.Background(), producer, topic, body); err != nil {
		log.Printf("Failed to send message: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "Event accepted")
}

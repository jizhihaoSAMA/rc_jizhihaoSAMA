package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"notification-system/pkg/config"
	"notification-system/pkg/worker"
)

func main() {
	// 1. Load and Validate Configuration
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Println("Configuration loaded and validated.")

	// 2. Initialize Worker (Core Processing Logic & RocketMQ Consumer)
	w, err := worker.NewWorker(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize worker: %v", err)
	}

	// 3. Start Worker (Subscribe and Consume)
	if err := w.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start worker: %v", err)
	}
	defer w.Shutdown()
	log.Println("RocketMQ Subscriber (Worker) started.")

	// 4. Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Worker...")
	log.Println("Worker exited")
}

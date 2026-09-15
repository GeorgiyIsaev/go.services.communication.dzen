package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"

	"syscall"

	"go.services.communication.dzen/internal/StatsKeeper/handlers"
	"go.services.communication.dzen/internal/StatsKeeper/service"
)

func main() {
	brokers := []string{"localhost:9092", "localhost:9093", "localhost:9094"}
	topic := "clicks"
	groupID := "stats-keeper"
	port := ":8081"

	tracker := service.NewTracker()
	consumer := service.NewConsumer(brokers, topic, groupID, tracker)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Kafka-консьюмер в отдельной горутине
	go func() {
		if err := consumer.Run(ctx); err != nil {
			log.Printf("consumer stopped: %v", err)
		}
	}()

	http.HandleFunc("/stats", handlers.StatsHandler(tracker))
	go func() {
		log.Printf("Server starting on %s", port)
		if err := http.ListenAndServe(port, nil); err != nil &&
			err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")
	if err := consumer.Close(); err != nil {
		log.Printf("consumer close: %v", err)
	}
}

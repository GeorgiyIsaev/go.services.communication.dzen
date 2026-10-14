package main

import (
	"log"
	"net/http"

	"go.services.communication.dzen/internal/ClickDashboard/config"
	"go.services.communication.dzen/internal/ClickDashboard/db"
	"go.services.communication.dzen/internal/ClickDashboard/handlers"
)

func main() {
	cfg := config.Load()
	db, err := db.NewDB(cfg)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	handler := handlers.NewStatsHandler(db)

	http.HandleFunc("/stats", handler.GetStats)

	log.Printf("Starting server on :%s", cfg.ServerPort)
	if err := http.ListenAndServe(":"+cfg.ServerPort, nil); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"log"
	"net/http"

	"go.services.communication.dzen/internal/StatsKeeper/handlers"
	"go.services.communication.dzen/internal/StatsKeeper/service"
)

func main() {
	tracker := service.NewTracker()

	http.HandleFunc("/click", handlers.ClickHandler(tracker))
	http.HandleFunc("/stats", handlers.StatsHandler(tracker))

	log.Println("Server starting on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

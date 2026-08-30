package main

import (
	"encoding/json"
	"log"
	"os"

	"go.services.communication.dzen/internal/ClickGenerator/client"
	"go.services.communication.dzen/internal/ClickGenerator/domain"
	"go.services.communication.dzen/internal/ClickGenerator/usecase"
)

func main() {
	Run()
}

func Run() {
	cfgFile, err := os.Open("cmd/ClickGenerator/config.json")
	if err != nil {
		log.Fatalf("Не удалось открыть config.json: %v", err)
	}
	defer cfgFile.Close()

	var cfg domain.Config
	if err := json.NewDecoder(cfgFile).Decode(&cfg); err != nil {
		log.Fatalf("Не удалось распарсить config.json: %v", err)
	}

	// client: создаем конкретную реализацию отправщика
	httpSender := client.NewHTTPClickSender(cfg.TargetURL)

	// UseCase: передаем в симулятор только интерфейс из domain (Dependency Inversion)
	sim := usecase.NewSimulator(cfg, httpSender)
	sim.Run()
}

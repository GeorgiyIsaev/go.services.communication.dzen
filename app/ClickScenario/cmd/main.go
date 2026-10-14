package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.services.communication.dzen/internal/ClickScenario/client"
	"go.services.communication.dzen/internal/ClickScenario/domain"
	"go.services.communication.dzen/internal/ClickScenario/usecase"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfgFile, err := os.Open("cmd/ClickScenario/config.json")
	if err != nil {
		log.Fatalf("Не удалось открыть config.json: %v", err)
	}
	defer cfgFile.Close()

	var cfg domain.Config
	if err := json.NewDecoder(cfgFile).Decode(&cfg); err != nil {
		log.Fatalf("Не удалось распарсить config.json: %v", err)
	}

	// Читаем сценарии из папки. Файлы обрабатываются в алфавитном порядке.
	scenarios, err := usecase.LoadScenarios(cfg.ScenarioDir)
	if err != nil {
		log.Fatalf("Не удалось загрузить сценарии: %v", err)
	}
	log.Printf("Загружено сценариев: %d (папка %q)", len(scenarios), cfg.ScenarioDir)

	// Гарантируем, что топик существует с нужным числом партиций
	if err := client.EnsureTopic(
		cfg.KafkaBrokers,
		cfg.KafkaTopic,
		cfg.KafkaPartitions,
		cfg.KafkaReplicationFactor,
	); err != nil {
		log.Fatalf("Не удалось создать/проверить топик: %v", err)
	}
	log.Printf("Топик %q готов (%d партиций, RF=%d)",
		cfg.KafkaTopic, cfg.KafkaPartitions, cfg.KafkaReplicationFactor)

	kafkaSender, err := client.NewKafkaClickSender(cfg.KafkaBrokers, cfg.KafkaTopic)
	if err != nil {
		log.Fatalf("Не удалось создать Kafka sender: %v", err)
	}
	defer func() {
		if err := kafkaSender.Close(); err != nil {
			log.Printf("Ошибка закрытия Kafka writer: %v", err)
		}
	}()

	sim := usecase.NewSimulator(cfg, kafkaSender, scenarios)
	sim.Run(ctx)
}

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
	cfgFile, err := os.Open("cmd/ClickGenerator/config.json")
	if err != nil {
		log.Fatalf("Не удалось открыть config.json: %v", err)
	}
	defer cfgFile.Close()

	var cfg domain.Config
	if err := json.NewDecoder(cfgFile).Decode(&cfg); err != nil {
		log.Fatalf("Не удалось распарсить config.json: %v", err)
	}

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

	sim := usecase.NewSimulator(cfg, kafkaSender)
	sim.Run()
}

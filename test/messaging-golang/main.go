package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/diwise/messaging-golang/pkg/messaging"
	"github.com/diwise/messaging-golang/pkg/messaging/telemetry"
	amqp "github.com/rabbitmq/amqp091-go"

	"log/slog"
)

func messageHandler(ctx context.Context, message amqp.Delivery, logger *slog.Logger) {
	logger.Info("message received from queue", "body", string(message.Body))
	msg := &telemetry.Temperature{}

	err := json.Unmarshal(message.Body, msg)
	if err != nil {
		logger.Error("failed to unmarshal message", "err", err.Error())
	}
}

func main() {

	serviceName := "messaging-golang-test"

	ctx := context.Background()
	logger := slog.Default().With(slog.String("service", serviceName))
	logger.Info("starting up ...")

	config := messaging.LoadConfiguration(ctx, serviceName, logger)

	messenger, _ := messaging.Initialize(ctx, config)
	defer messenger.Close()

	testMessage := &telemetry.Temperature{
		Temp: 37.0,
	}

	messenger.RegisterTopicMessageHandler(testMessage.TopicName(), messageHandler)
	messenger.PublishOnTopic(context.Background(), testMessage)

	time.Sleep(5 * time.Second)

}

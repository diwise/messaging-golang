package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/diwise/messaging-golang/pkg/messaging"
	amqp "github.com/rabbitmq/amqp091-go"

	"log/slog"
)

type TestMessage struct{}

func (m *TestMessage) ContentType() string { return "application/json" }
func (m *TestMessage) TopicName() string   { return "test.topic" }

func messageHandler(ctx context.Context, message amqp.Delivery, logger *slog.Logger) {
	logger.Info("message received from queue", "body", string(message.Body))
	msg := &TestMessage{}

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

	msg := &TestMessage{}

	messenger.RegisterTopicMessageHandler(msg.TopicName(), messageHandler)
	messenger.PublishOnTopic(ctx, msg)

	time.Sleep(5 * time.Second)
}

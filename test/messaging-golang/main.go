package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/diwise/messaging-golang/pkg/messaging"

	"log/slog"
)

type TestMessage struct{}

func (m *TestMessage) ContentType() string { return "application/json" }
func (m *TestMessage) TopicName() string   { return "test.topic" }
func (m *TestMessage) Body() []byte        { return []byte{} }

func messageHandler(ctx context.Context, message messaging.IncomingTopicMessage, logger *slog.Logger) {
	body := message.Body()
	logger.Info("message received from queue", "body", string(body))
	msg := &TestMessage{}

	err := json.Unmarshal(body, msg)
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

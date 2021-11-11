package main

import (
	"encoding/json"
	"time"

	"github.com/diwise/messaging-golang/pkg/messaging"
	"github.com/diwise/messaging-golang/pkg/messaging/telemetry"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func messageHandler(message amqp.Delivery, logger zerolog.Logger) {
	logger.Info().Str("body", string(message.Body)).Msg("message received from queue")
	msg := &telemetry.Temperature{}

	err := json.Unmarshal(message.Body, msg)
	if err != nil {
		logger.Error().Err(err).Msg("failed to unmarshal message")
	}
}

func main() {

	serviceName := "messaging-golang-test"

	logger := log.With().Str("service", serviceName).Logger()
	logger.Info().Msg("starting up ...")

	config := messaging.LoadConfiguration(serviceName, logger)

	messenger, _ := messaging.Initialize(config)
	defer messenger.Close()

	testMessage := &telemetry.Temperature{
		Temp: 37.0,
	}

	messenger.RegisterTopicMessageHandler(testMessage.TopicName(), messageHandler)
	messenger.PublishOnTopic(testMessage)

	time.Sleep(5 * time.Second)

}

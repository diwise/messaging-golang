package main

import (
	"encoding/json"
	"time"

	"github.com/diwise/messaging-golang/pkg/messaging"
	"github.com/diwise/messaging-golang/pkg/messaging/telemetry"
	"github.com/streadway/amqp"

	log "github.com/sirupsen/logrus"
)

func messageHandler(message amqp.Delivery) {
	log.Info("message received from queue: " + string(message.Body))
	msg := &telemetry.Temperature{}

	err := json.Unmarshal(message.Body, msg)
	if err != nil {
		log.Error("failed to unmarshal message: " + err.Error())
	}
}

func main() {

	log.Info("starting up ...")

	serviceName := "messaging-golang-test"
	config := messaging.LoadConfiguration(serviceName)

	messenger, _ := messaging.Initialize(config)
	defer messenger.Close()

	testMessage := &telemetry.Temperature{
		Temp: 37.0,
	}

	messenger.RegisterTopicMessageHandler(testMessage.TopicName(), messageHandler)
	messenger.PublishOnTopic(testMessage)

	time.Sleep(5 * time.Second)

}

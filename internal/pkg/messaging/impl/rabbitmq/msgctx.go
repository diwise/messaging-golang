package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

type rmqCtx struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	cfg        Config

	commandQueueName string
	commandChannel   <-chan amqp.Delivery
	commandLogger    *slog.Logger

	responseQueueName string
	responseChannel   <-chan amqp.Delivery
	responseLogger    *slog.Logger

	connectionClosedError chan *amqp.Error
}

func createMessageQueueChannel(ctx context.Context, msgctx *rmqCtx) (*rmqCtx, error) {
	connectionString := fmt.Sprintf(
		"amqp://%s:%s@%s:%d/%s",
		msgctx.cfg.User, msgctx.cfg.Password, msgctx.cfg.Host, msgctx.cfg.Port,
		strings.TrimPrefix(msgctx.cfg.VirtualHost, "/"),
	)

	conn, err := amqp.Dial(connectionString)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to message queue: (%w)", err)
	}

	amqpChannel, err := conn.Channel()

	if err != nil {
		return nil, fmt.Errorf("unable to create an amqp channel to message queue: (%w)", err)
	}

	msgctx.connection = conn
	msgctx.connection.NotifyClose(msgctx.connectionClosedError)
	msgctx.channel = amqpChannel

	return msgctx, nil
}

func createCommandExchange(ctx context.Context, msgctx *rmqCtx) error {
	err := msgctx.channel.ExchangeDeclare(commandExchange, amqp.ExchangeDirect, false, false, false, false, nil)

	if err != nil {
		return fmt.Errorf("unable to declare command exchange %s: (%w)", commandExchange, err)
	}

	return nil
}

func createTopicExchange(ctx context.Context, msgctx *rmqCtx) error {
	err := msgctx.channel.ExchangeDeclare(topicExchange, amqp.ExchangeTopic, false, false, false, false, nil)

	if err != nil {
		return fmt.Errorf("unable to declare topic exchange %s: (%w)", topicExchange, err)
	}

	return nil
}

func createCommandAndResponseQueues(ctx context.Context, msgctx *rmqCtx) error {
	err := createCommandExchange(ctx, msgctx)
	if err != nil {
		return err
	}

	serviceName := msgctx.serviceName()

	commandQueue, err := msgctx.channel.QueueDeclare(serviceName, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("failed to declare command queue for %s: (%w)", serviceName, err)
	}

	cmdlog := msgctx.cfg.logger.With(slog.String("queue", commandQueue.Name))
	cmdlog.Info("declared command queue")

	err = msgctx.channel.QueueBind(commandQueue.Name, serviceName, commandExchange, false, nil)
	if err != nil {
		return fmt.Errorf("failed to bind command queue %s to exchange %s: (%w)", commandQueue.Name, commandExchange, err)
	}

	cmdlog.Info("bound command queue to command exchange")

	responseQueue, err := msgctx.channel.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return fmt.Errorf("failed to declare response queue for %s: (%w)", serviceName, err)
	}

	resplog := msgctx.cfg.logger.With(slog.String("queue", responseQueue.Name))
	resplog.Info("declared response queue")

	err = msgctx.channel.QueueBind(responseQueue.Name, responseQueue.Name, commandExchange, false, nil)
	if err != nil {
		return fmt.Errorf("failed to bind response queue %s to exchange %s: (%w)", responseQueue.Name, commandExchange, err)
	}

	resplog.Info("bound response queue to command exchange")

	msgctx.commandQueueName = commandQueue.Name

	cmdchan, err := msgctx.channel.Consume(commandQueue.Name, "command-consumer", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("unable to start consuming commands from %s: (%w)", commandQueue.Name, err)
	}
	msgctx.commandChannel = make(chan AcknowledgeableIncomingCommand, 32)

	go func() {
		for cmd := range cmdchan {
			w := newAMQPDeliveryWrapper(msgctx, &cmd)
			msgctx.commandChannel <- w
		}
	}()

	msgctx.commandLogger = cmdlog
	msgctx.responseLogger = resplog

	msgctx.responseQueueName = responseQueue.Name
	respchan, err := msgctx.channel.Consume(responseQueue.Name, "response-consumer", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("unable to start consuming responses from %s: (%w)", responseQueue.Name, err)
	}
	msgctx.responseChannel = make(chan AcknowledgeableIncomingResponse, 32)

	go func() {
		for resp := range respchan {
			w := newAMQPDeliveryWrapper(msgctx, &resp)
			msgctx.responseChannel <- w
		}
	}()

	go func() {
		msgctx.RegisterCommandHandler(MatchContentType(PingCommandContentType), NewPingCommandHandler(msgctx))

		err := msgctx.NoteToSelf(context.Background(), NewPingCommand())
		if err != nil {
			cmdlog.Error("failed to publish a ping command to ourselves!", "err", err.Error())
		}
	}()

	return nil
}

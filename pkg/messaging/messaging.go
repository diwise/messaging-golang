package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/diwise/messaging-golang/pkg/messaging/tracing"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type IoTHubMessageOrigin struct {
	Device    string  `json:"device"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type IoTHubMessage struct {
	Origin    IoTHubMessageOrigin `json:"origin"`
	Timestamp string              `json:"timestamp"`
}

// Config is an encapsulating context that wraps configuration information
// used by the initialization methods of the messaging library
type Config struct {
	ServiceName string
	Host        string
	User        string
	Password    string
	logger      zerolog.Logger
}

// MsgContext encapsulates the underlying messaging primitives, as well as
// their associated configuration
type MsgContext interface {
	NoteToSelf(ctx context.Context, command CommandMessage) error
	SendCommandTo(ctx context.Context, command CommandMessage, key string) error
	SendResponseTo(ctx context.Context, response CommandMessage, key string) error
	PublishOnTopic(ctx context.Context, message TopicMessage) error

	Close()

	RegisterCommandHandler(contentType string, handler CommandHandler) error
	RegisterTopicMessageHandler(routingKey string, handler TopicMessageHandler)
}

type rabbitMQContext struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	cfg        Config

	commandHandlers   map[string]CommandHandler
	responseQueueName string

	connectionClosedError chan *amqp.Error
}

// CommandMessage is an interface used when sending commands
type CommandMessage interface {
	ContentType() string
}

// CommandMessageWrapper is used to wrap an incoming command message
type CommandMessageWrapper interface {
	Body() []byte
	Context() context.Context
	RespondWith(context.Context, CommandMessage) error
}

var tracer = otel.Tracer("messaging-rmq")

// NoteToSelf enqueues a command to the same routing key as the calling service
// which means that the sender or one of its replicas will receive the command
func (rmq *rabbitMQContext) NoteToSelf(ctx context.Context, command CommandMessage) error {
	return rmq.SendCommandTo(ctx, command, rmq.serviceName())
}

// SendCommandTo enqueues a command to given routing key via the command exchange
func (rmq *rabbitMQContext) SendCommandTo(ctx context.Context, command CommandMessage, key string) error {
	var err error

	messageBytes, err := json.MarshalIndent(command, "", " ")
	if err != nil {
		return &Error{"unable to marshal command to json!", err}
	}

	ctx, span := tracer.Start(
		ctx, commandExchange+" command",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attribute.String("messaging.rabbitmq.routing_key", key)),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()

	err = rmq.channel.Publish(commandExchange, key, true, false, amqp.Publishing{
		Headers:     tracing.InjectAMQPHeaders(ctx),
		ContentType: command.ContentType(),
		ReplyTo:     rmq.responseQueueName,
		Body:        messageBytes,
	})
	if err != nil {
		return &Error{"failed to publish a command to " + key + "!", err}
	}

	return nil
}

// SendResponseTo enqueues a response to a given routing key via the command exchange
func (rmq *rabbitMQContext) SendResponseTo(ctx context.Context, response CommandMessage, key string) error {
	var err error

	messageBytes, err := json.MarshalIndent(response, "", " ")
	if err != nil {
		return &Error{"unable to marshal response to json!", err}
	}

	ctx, span := tracer.Start(
		ctx, commandExchange+" response",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attribute.String("messaging.rabbitmq.routing_key", key)),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()

	err = rmq.channel.Publish(commandExchange, key, true, false, amqp.Publishing{
		Headers:     tracing.InjectAMQPHeaders(ctx),
		ContentType: response.ContentType(),
		Body:        messageBytes,
	})
	if err != nil {
		return &Error{"failed to publish a command to " + key + "!", err}
	}

	return nil
}

// TopicMessage is an interface used when sending messages to make sure
// that messages are sent to the correct topic with correct content type
type TopicMessage interface {
	ContentType() string
	TopicName() string
}

// PublishOnTopic takes a TopicMessage, reads its TopicName property,
// and publishes it to the correct topic with the correct content type
func (rmq *rabbitMQContext) PublishOnTopic(ctx context.Context, message TopicMessage) error {
	var err error

	messageBytes, err := json.MarshalIndent(message, "", " ")
	if err != nil {
		return &Error{"unable to marshal telemetry message to json!", err}
	}

	ctx, span := tracer.Start(
		ctx, topicExchange+" publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attribute.String("messaging.rabbitmq.routing_key", message.TopicName())),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()

	err = rmq.channel.Publish(topicExchange, message.TopicName(), false, false,
		amqp.Publishing{
			Headers:     tracing.InjectAMQPHeaders(ctx),
			ContentType: message.ContentType(),
			Body:        messageBytes,
		})

	return err
}

// Close is a wrapper method to close both the underlying AMQP
// connection as well as the channel
func (rmq *rabbitMQContext) Close() {
	rmq.channel.Close()
	rmq.connection.Close()
}

//CommandHandler is a callback type to be used for dispatching incoming commands
type CommandHandler func(context.Context, CommandMessageWrapper, zerolog.Logger) error

//RegisterCommandHandler registers a handler to be called when a command with a given
//content type is received
func (rmq *rabbitMQContext) RegisterCommandHandler(contentType string, handler CommandHandler) error {
	//TODO: Return an error if a handler has already been registered
	//TODO: Mutex protection
	if rmq.commandHandlers == nil {
		rmq.commandHandlers = map[string]CommandHandler{}
	}

	rmq.commandHandlers[contentType] = handler
	return nil
}

func (rmq *rabbitMQContext) serviceName() string {
	return rmq.cfg.ServiceName
}

// Error encapsulates a lower level error together with an error
// message provided by the caller that experienced the error
type Error struct {
	msg string
	err error
}

func (err *Error) Error() string {
	if err.err != nil {
		return err.msg + " (" + err.err.Error() + ")"
	}

	return err.msg
}

const (
	commandExchange = "iot-cmd-exchange-direct"
	topicExchange   = "iot-msg-exchange-topic"
)

func getEnvironmentVariableOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// LoadConfiguration loads configuration values from RABBITMQ_HOST, RABBITMQ_USER
// and RABBITMQ_PASS. The username and password defaults to the bitnami ootb
// values for local testing.
func LoadConfiguration(serviceName string, log zerolog.Logger) Config {
	rabbitMQHostEnvVar := "RABBITMQ_HOST"
	rabbitMQHost := os.Getenv(rabbitMQHostEnvVar)
	rabbitMQUser := getEnvironmentVariableOrDefault("RABBITMQ_USER", "user")
	rabbitMQPass := getEnvironmentVariableOrDefault("RABBITMQ_PASS", "bitnami")
	rabbitMQDisabled := getEnvironmentVariableOrDefault("RABBITMQ_DISABLED", "false")

	if rabbitMQDisabled != "true" {
		if rabbitMQHost == "" {
			log.Fatal().Msg("Rabbit MQ host missing. Please set " + rabbitMQHostEnvVar + " to a valid host name or IP.")
		}

		return Config{
			ServiceName: serviceName,
			Host:        rabbitMQHost,
			User:        rabbitMQUser,
			Password:    rabbitMQPass,
			logger:      log,
		}
	}

	return Config{
		ServiceName: serviceName,
		Host:        "",
		User:        "",
		Password:    "",
		logger:      log,
	}
}

// Initialize takes a Config parameter and initializes a connection,
// channel, topic exchange, command exchange and service specific
// command and response queues. Retries every 2 seconds until successfull.
func Initialize(cfg Config) (MsgContext, error) {

	if cfg.Host == "" {
		cfg.logger.Info().Msg("host name empty, returning mocked context instead.")
		return &MsgContextMock{}, nil
	}

	var connClosedError = make(chan *amqp.Error)
	var context *rabbitMQContext
	var err error

	for {

		time.Sleep(2 * time.Second)

		context, err = createMessageQueueChannel(&rabbitMQContext{
			cfg:                   cfg,
			connectionClosedError: connClosedError,
		})
		if err != nil {
			cfg.logger.Error().Err(err).Msg("")
			continue
		}

		err = createTopicExchange(context)
		if err != nil {
			cfg.logger.Error().Err(err).Msg("")
			continue
		}

		err = createCommandAndResponseQueues(context)
		if err != nil {
			cfg.logger.Error().Err(err).Msg("")
			continue
		}

		return context, nil
	}
}

// TopicMessageHandler is a callback type that should be passed
// to RegisterTopicMessageHandler to receive messages from topics.
type TopicMessageHandler func(context.Context, amqp.Delivery, zerolog.Logger)

// RegisterTopicMessageHandler creates a subscription queue that is bound
// to the topic exchange with the provided routing key, starts a consumer
// for that queue and hands off any received messages to the provided
// TopicMessageHandler
func (ctx *rabbitMQContext) RegisterTopicMessageHandler(routingKey string, handler TopicMessageHandler) {
	queue, err := ctx.channel.QueueDeclare(
		"",    //name
		false, //durable
		false, //delete when unused
		false, //exclusive
		false, //no-wait
		nil,   //arguments
	)
	if err != nil {
		ctx.cfg.logger.Fatal().Err(err).Msg("failed to declare a queue")
	}

	logger := ctx.cfg.logger.With().Str("queue", queue.Name).Logger()

	logger.Info().Msg("declared topic subscription queue")

	err = ctx.channel.QueueBind(
		queue.Name,
		routingKey,
		topicExchange,
		false,
		nil,
	)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to bind to queue")
	}

	logger.Info().Msg("successfully bound to queue")

	messagesFromQueue, err := ctx.channel.Consume(
		queue.Name, //queue
		"",         //consumer
		true,       //auto ack
		true,       //exclusive
		false,      //no local
		false,      //no-wait
		nil,        //args
	)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to register a consumer with queue")
	}

	logger.Info().Msg("successfully registered as a consumer")

	go func() {
		for msg := range messagesFromQueue {
			ctx := tracing.ExtractAMQPHeaders(context.Background(), msg.Headers)
			ctx, span := tracer.Start(ctx, queue.Name+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

			traceID := span.SpanContext().TraceID()
			if traceID.IsValid() {
				logger = logger.With().Str("traceID", traceID.String()).Logger()
			}

			handler(ctx, msg, logger)
			span.End()
		}
	}()
}

func createMessageQueueChannel(ctx *rabbitMQContext) (*rabbitMQContext, error) {
	connectionString := fmt.Sprintf("amqp://%s:%s@%s:5672/", ctx.cfg.User, ctx.cfg.Password, ctx.cfg.Host)
	conn, err := amqp.Dial(connectionString)
	if err != nil {
		return nil, &Error{"unable to connect to message queue!", err}
	}

	amqpChannel, err := conn.Channel()

	if err != nil {
		return nil, &Error{"unable to create an amqp channel to message queue!", err}
	}

	ctx.connection = conn
	ctx.connection.NotifyClose(ctx.connectionClosedError)
	ctx.channel = amqpChannel

	go func() {
		for evt := range ctx.connectionClosedError {
			ctx.cfg.logger.Fatal().Err(evt).Msg("connection error")
		}
	}()

	return ctx, nil
}

func createCommandExchange(ctx *rabbitMQContext) error {
	err := ctx.channel.ExchangeDeclare(commandExchange, amqp.ExchangeDirect, false, false, false, false, nil)

	if err != nil {
		err = &Error{"unable to declare command exchange " + commandExchange + "!", err}
	}

	return err
}

func createTopicExchange(ctx *rabbitMQContext) error {
	err := ctx.channel.ExchangeDeclare(topicExchange, amqp.ExchangeTopic, false, false, false, false, nil)

	if err != nil {
		err = &Error{"unable to declare topic exchange " + topicExchange + "!", err}
	}

	return err
}

func createCommandAndResponseQueues(rmq *rabbitMQContext) error {
	err := createCommandExchange(rmq)
	if err != nil {
		return err
	}

	serviceName := rmq.serviceName()

	commandQueue, err := rmq.channel.QueueDeclare(serviceName, false, false, false, false, nil)
	if err != nil {
		return &Error{"failed to declare command queue for " + serviceName + "!", err}
	}

	cmdlog := rmq.cfg.logger.With().Str("queue", commandQueue.Name).Logger()
	cmdlog.Info().Msg("declared command queue")

	err = rmq.channel.QueueBind(commandQueue.Name, serviceName, commandExchange, false, nil)
	if err != nil {
		return &Error{"failed to bind command queue " + commandQueue.Name + " to exchange " + commandExchange + "!", err}
	}

	cmdlog.Info().Msg("bound command queue to command exchange")

	responseQueue, err := rmq.channel.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return &Error{"failed to declare response queue for " + serviceName + "!", err}
	}

	resplog := rmq.cfg.logger.With().Str("queue", responseQueue.Name).Logger()
	resplog.Info().Msg("declared response queue")

	err = rmq.channel.QueueBind(responseQueue.Name, responseQueue.Name, commandExchange, false, nil)
	if err != nil {
		msg := fmt.Sprintf("failed to bind response queue %s to exchange %s!", responseQueue.Name, commandExchange)
		return &Error{msg, err}
	}

	resplog.Info().Msg("bound response queue to command exchange")

	commands, err := rmq.channel.Consume(commandQueue.Name, "command-consumer", false, false, false, false, nil)
	if err != nil {
		msg := fmt.Sprintf("unable to start consuming commands from %s!", commandQueue.Name)
		return &Error{msg, err}
	}

	rmq.RegisterCommandHandler(PingCommandContentType, NewPingCommandHandler(rmq))

	go func() {
		for cmd := range commands {
			sublog := cmdlog

			ctx := tracing.ExtractAMQPHeaders(context.Background(), cmd.Headers)
			ctx, span := tracer.Start(ctx, commandQueue.Name+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

			traceID := span.SpanContext().TraceID()
			if traceID.IsValid() {
				sublog = sublog.With().Str("traceID", traceID.String()).Logger()
			}

			sublog.Info().Str("body", string(cmd.Body)).Msg("received command")

			handler, ok := rmq.commandHandlers[cmd.ContentType]
			if ok {
				handler(ctx, newAMQPDeliveryWrapper(rmq, &cmd), sublog)
			} else {
				err := fmt.Errorf("no handler registered for command with content type %s", cmd.ContentType)
				span.RecordError(err)
				sublog.Warn().Err(err).Msg("no handler")
			}

			span.End()

			cmd.Ack(true)
		}
	}()

	responses, err := rmq.channel.Consume(responseQueue.Name, "response-consumer", false, false, false, false, nil)
	if err != nil {
		msg := fmt.Sprintf("unable to start consuming responses from %s!", responseQueue.Name)
		return &Error{msg, err}
	}

	rmq.responseQueueName = responseQueue.Name

	err = rmq.NoteToSelf(context.Background(), NewPingCommand())
	if err != nil {
		return &Error{"failed to publish a ping command to ourselves!", err}
	}

	go func() {
		for response := range responses {
			ctx := tracing.ExtractAMQPHeaders(context.Background(), response.Headers)
			ctx, span := tracer.Start(ctx, rmq.responseQueueName+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

			traceID := span.SpanContext().TraceID()
			if traceID.IsValid() {
				resplog = resplog.With().Str("traceID", traceID.String()).Logger()
			}

			// TODO: Ability to dispatch response to an application supplied response handler
			resplog.Info().Str("body", string(response.Body)).Msg("received response")
			response.Ack(true)

			span.End()
		}
	}()

	return nil
}

package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/diwise/messaging-golang/pkg/messaging/tracing"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config is an encapsulating context that wraps configuration information
// used by the initialization methods of the messaging library
type Config struct {
	ServiceName string
	Host        string
	Port        uint64
	VirtualHost string
	User        string
	Password    string
	initTimeout time.Duration
	logger      *slog.Logger
}

// MsgContext encapsulates the underlying messaging primitives, as well as
// their associated configuration
type MsgContext interface {
	NoteToSelf(ctx context.Context, command CommandMessage) error
	SendCommandTo(ctx context.Context, command CommandMessage, key string) error
	SendResponseTo(ctx context.Context, response CommandMessage, key string) error
	PublishOnTopic(ctx context.Context, message TopicMessage) error

	Start()
	Close()

	RegisterCommandHandler(contentType string, handler CommandHandler) error
	RegisterTopicMessageHandler(routingKey string, handler TopicMessageHandler) error
}

// action is the type that we enqueue on our internal queue
type action func()

type rabbitMQContext struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	cfg        Config

	queue chan action

	commandHandlers map[string]CommandHandler

	commandQueueName string
	commandChannel   <-chan amqp.Delivery
	commandLogger    *slog.Logger

	responseQueueName string
	responseChannel   <-chan amqp.Delivery
	responseLogger    *slog.Logger

	connectionClosedError chan *amqp.Error

	keepRunning *atomic.Bool
	wg          sync.WaitGroup
}

// CommandMessage is an interface used when sending commands
type CommandMessage interface {
	ContentType() string
}

// CommandMessageWrapper is used to wrap an incoming command message
// so that we can hide the details of the actual messaging technology
type CommandMessageWrapper interface {
	Body() []byte
	Context() context.Context
	RespondWith(context.Context, CommandMessage) error
}

var tracer = otel.Tracer("messaging-rmq")

func (rmq *rabbitMQContext) oncommand(cmd *amqp.Delivery) {
	sublog := rmq.commandLogger

	ctx := tracing.ExtractAMQPHeaders(context.Background(), cmd.Headers)
	ctx, span := tracer.Start(ctx, rmq.commandQueueName+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

	traceID := span.SpanContext().TraceID()
	if traceID.IsValid() {
		sublog = sublog.With(slog.String("traceID", traceID.String()))
	}

	sublog.Info("received command", "body", string(cmd.Body))

	handler, ok := rmq.commandHandlers[cmd.ContentType]
	if ok {
		handler(ctx, newAMQPDeliveryWrapper(rmq, cmd), sublog)
	} else {
		err := fmt.Errorf("no handler registered for command with content type %s", cmd.ContentType)
		span.RecordError(err)
		sublog.Warn("no handler", "err", err.Error())
	}

	span.End()

	cmd.Ack(false)
}

func (rmq *rabbitMQContext) onresponse(response *amqp.Delivery) {
	ctx := tracing.ExtractAMQPHeaders(context.Background(), response.Headers)
	_, span := tracer.Start(ctx, rmq.responseQueueName+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

	resplog := rmq.responseLogger

	traceID := span.SpanContext().TraceID()
	if traceID.IsValid() {
		resplog = resplog.With(slog.String("traceID", traceID.String()))
	}

	// TODO: Ability to dispatch response to an application supplied response handler
	resplog.Info("received response", "body", string(response.Body))
	response.Ack(false)

	span.End()
}

func (rmq *rabbitMQContext) run() {
	// Increment the waitgroup and decrement on exit so that others can
	// know if messages are being processed or not
	rmq.wg.Add(1)
	defer rmq.wg.Done()

	// use atomic swap to avoid startup races
	alreadyStarted := rmq.keepRunning.Swap(true)
	if alreadyStarted {
		rmq.cfg.logger.Error("attempt to start the messaging goroutine multiple times")
		return
	}

	for rmq.keepRunning.Load() {
		select {
		case action := <-rmq.queue:
			{
				if action != nil {
					action()
				}
			}
		case cmd := <-rmq.commandChannel:
			{
				rmq.oncommand(&cmd)
			}
		case response := <-rmq.responseChannel:
			{
				rmq.onresponse(&response)
			}
		case cce := <-rmq.connectionClosedError:
			{
				msg := "connection error"
				rmq.cfg.logger.Error(msg, "err", cce.Error())
				panic(msg)
			}
		}
	}

	rmq.cfg.logger.Info("exiting messaging loop")
}

// waitOnChannel is a helper method that waits on a completion channel
// with a configurable timeout
func waitOnChannel(ch chan error, timeout time.Duration) (error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// seconds converts a float duration to a time.Duration in seconds
func seconds(s float64) time.Duration {
	return time.Duration(s) * time.Second
}

// NoteToSelf enqueues a command to the same routing key as the calling service
// which means that the sender or one of its replicas will receive the command
func (rmq *rabbitMQContext) NoteToSelf(ctx context.Context, command CommandMessage) error {
	return rmq.SendCommandTo(ctx, command, rmq.serviceName())
}

// SendCommandTo enqueues a command to the given routing key via the command exchange
func (rmq *rabbitMQContext) SendCommandTo(ctx context.Context, command CommandMessage, key string) error {
	var err error

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

	messageBytes, err := json.Marshal(command)
	if err != nil {
		err = fmt.Errorf("unable to marshal command to json: (%w)", err)
		return err
	}

	completion := make(chan error, 1)

	// Enqueue this action on the command queue
	rmq.queue <- func() {
		// Send the publish response to the completion channel
		completion <- rmq.channel.Publish(commandExchange, key, true, false, amqp.Publishing{
			Headers:     tracing.InjectAMQPHeaders(ctx),
			ContentType: command.ContentType(),
			ReplyTo:     rmq.responseQueueName,
			Body:        messageBytes,
		})
	}

	// Wait until the action has been processed (command has been sent)
	var tmo error
	if err, tmo = waitOnChannel(completion, seconds(5.0)); tmo != nil {
		return tmo
	}

	if err != nil {
		err = fmt.Errorf("failed to publish command to %s: (%w)", key, err)
		return err
	}

	return nil
}

// SendResponseTo enqueues a response to a given routing key via the command exchange
func (rmq *rabbitMQContext) SendResponseTo(ctx context.Context, response CommandMessage, key string) error {
	var err error

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

	messageBytes, err := json.Marshal(response)
	if err != nil {
		err = fmt.Errorf("unable to marshal response to json: (%w)", err)
		return err
	}

	completion := make(chan error, 1)

	rmq.queue <- func() {
		completion <- rmq.channel.Publish(commandExchange, key, true, false, amqp.Publishing{
			Headers:     tracing.InjectAMQPHeaders(ctx),
			ContentType: response.ContentType(),
			Body:        messageBytes,
		})
	}

	// Wait until the action has been processed (response has been sent)
	var tmo error
	if err, tmo = waitOnChannel(completion, seconds(5.0)); tmo != nil {
		return tmo
	}

	if err != nil {
		err = fmt.Errorf("failed to publish a response to %s (%w)", key, err)
		return err
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

	messageBytes, err := json.Marshal(message)
	if err != nil {
		err = fmt.Errorf("unable to marshal topic message to json: (%w)", err)
		return err
	}

	completion := make(chan error, 1)

	rmq.queue <- func() {
		completion <- rmq.channel.Publish(topicExchange, message.TopicName(), false, false,
			amqp.Publishing{
				Headers:     tracing.InjectAMQPHeaders(ctx),
				ContentType: message.ContentType(),
				Body:        messageBytes,
			})
	}

	// Wait until the action has been processed (topic message has been sent)
	var tmo error
	if err, tmo = waitOnChannel(completion, seconds(5.0)); tmo != nil {
		return tmo
	}

	return err
}

// Close is a wrapper method to close both the underlying AMQP
// connection as well as the channel after shuttng down the messaging worker
func (rmq *rabbitMQContext) Close() {
	rmq.queue <- func() {
		rmq.keepRunning.Store(false)
	}

	rmq.wg.Wait()

	rmq.channel.Close()
	rmq.connection.Close()
}

func (rmq *rabbitMQContext) Start() {
	go rmq.run()
}

// CommandHandler is a callback type to be used for dispatching incoming commands
type CommandHandler func(context.Context, CommandMessageWrapper, *slog.Logger) error

// RegisterCommandHandler registers a handler to be called when a command with a given
// content type is received
func (rmq *rabbitMQContext) RegisterCommandHandler(contentType string, handler CommandHandler) error {

	completion := make(chan error, 1)

	rmq.queue <- func() {
		if rmq.commandHandlers == nil {
			rmq.commandHandlers = map[string]CommandHandler{}
		}

		rmq.commandHandlers[contentType] = handler

		completion <- nil
	}

	// Wait until the action has been processed (handler has been registered)
	err, tmo := waitOnChannel(completion, seconds(5.0))
	if tmo != nil {
		return tmo
	}

	return err
}

func (rmq *rabbitMQContext) serviceName() string {
	return rmq.cfg.ServiceName
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
func LoadConfiguration(ctx context.Context, serviceName string, log *slog.Logger) Config {
	rabbitMQHostEnvVar := "RABBITMQ_HOST"
	rabbitMQHost := os.Getenv(rabbitMQHostEnvVar)
	rabbitMQPort := getEnvironmentVariableOrDefault("RABBITMQ_PORT", "5672")
	rabbitMQTenant := getEnvironmentVariableOrDefault("RABBITMQ_VHOST", "/")
	rabbitMQUser := getEnvironmentVariableOrDefault("RABBITMQ_USER", "user")
	rabbitMQPass := getEnvironmentVariableOrDefault("RABBITMQ_PASS", "bitnami")
	rabbitMQDisabled := getEnvironmentVariableOrDefault("RABBITMQ_DISABLED", "false")

	if rabbitMQDisabled != "true" {
		if rabbitMQHost == "" {
			msg := fmt.Sprintf("Rabbit MQ host missing. Please set " + rabbitMQHostEnvVar + " to a valid host name or IP.")
			log.Error(msg)
			panic(msg)
		}

		port, err := strconv.ParseUint(rabbitMQPort, 10, 32)
		if err != nil {
			msg := fmt.Sprintf("Rabbit MQ port number must be numerical (not %s).", rabbitMQPort)
			log.Error(msg)
			panic(msg)
		}

		tmostr := getEnvironmentVariableOrDefault("RABBITMQ_INIT_TIMEOUT", "10")
		timeout, err := strconv.ParseInt(tmostr, 10, 64)
		if err != nil {
			msg := fmt.Sprintf("unable to parse messaging timeout value \"%s\" (%s)", tmostr, err.Error())
			log.Error(msg)
			panic(msg)
		}

		return Config{
			ServiceName: serviceName,
			Host:        rabbitMQHost,
			Port:        port,
			VirtualHost: rabbitMQTenant,
			User:        rabbitMQUser,
			Password:    rabbitMQPass,
			initTimeout: time.Duration(timeout),
			logger:      log,
		}
	}

	return Config{
		ServiceName: serviceName,
		Host:        "",
		VirtualHost: "",
		User:        "",
		Password:    "",
		initTimeout: time.Duration(0),
		logger:      log,
	}
}

// Initialize takes a Config parameter and initializes a connection,
// channel, topic exchange, command exchange and service specific
// command and response queues. Retries every 2 seconds until successfull.
func Initialize(ctx context.Context, cfg Config) (MsgContext, error) {

	if cfg.Host == "" {
		cfg.logger.Info("host name empty, returning mocked context instead.")
		return &MsgContextMock{}, nil
	}

	if cfg.initTimeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.initTimeout*time.Second)
		defer cancel()
	}

	initComplete := make(chan MsgContext, 1)
	go do_init(ctx, cfg, initComplete)

	select {
	case msgctx := <-initComplete:
		return msgctx, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func do_init(ctx context.Context, cfg Config, initComplete chan MsgContext) {
	var connClosedError = make(chan *amqp.Error)
	var msgctx *rabbitMQContext
	var err error

	for {

		time.Sleep(2 * time.Second)

		msgctx, err = createMessageQueueChannel(ctx, &rabbitMQContext{
			cfg:                   cfg,
			connectionClosedError: connClosedError,
		})
		if err != nil {
			cfg.logger.Error("failed to create message queue channel", "err", err.Error())
			continue
		}

		err = createTopicExchange(ctx, msgctx)
		if err != nil {
			cfg.logger.Error("failed to create topic exchange", "err", err.Error())
			continue
		}

		err = createCommandAndResponseQueues(ctx, msgctx)
		if err != nil {
			cfg.logger.Error("failed to create cmd and response queues", "err", err.Error())
			continue
		}

		msgctx.queue = make(chan action, 32)
		msgctx.keepRunning = &atomic.Bool{}

		initComplete <- msgctx
		break
	}
}

// TopicMessageHandler is a callback type that should be passed
// to RegisterTopicMessageHandler to receive messages from topics.
type TopicMessageHandler func(context.Context, amqp.Delivery, *slog.Logger)

type TopicMessageFilter func() bool

func ContentType(content string) TopicMessageFilter {
	return func() bool { return true }
}

// RegisterTopicMessageHandler creates a subscription queue that is bound
// to the topic exchange with the provided routing key, starts a consumer
// for that queue and hands off any received messages to the provided
// TopicMessageHandler
func (msgctx *rabbitMQContext) RegisterTopicMessageHandler(routingKey string, handler TopicMessageHandler) error {

	completion := make(chan error, 1)

	msgctx.queue <- func() {
		registerTMH(msgctx, routingKey, handler, completion)
	}

	// Wait until the action has been processed (message handler registered)
	var tmo error
	err, tmo := waitOnChannel(completion, seconds(5.0))

	if tmo != nil {
		msg := fmt.Sprintf("failed to register topic message handler: %s", tmo.Error())
		msgctx.cfg.logger.Error(msg)
		panic(msg)
	}

	return err
}

func registerTMH(msgctx *rabbitMQContext, routingKey string, handler TopicMessageHandler, registerComplete chan error) {
	queue, err := msgctx.channel.QueueDeclare(
		"",    //name
		false, //durable
		true,  //delete when unused
		false, //exclusive
		false, //no-wait
		nil,   //arguments
	)
	if err != nil {
		msgctx.cfg.logger.Error("failed to declare a queue", "err", err.Error())
		os.Exit(1)
	}

	logger := msgctx.cfg.logger.With(
		slog.String("queue", queue.Name),
		slog.String("key", routingKey),
	)

	logger.Info("declared topic subscription queue")

	err = msgctx.channel.QueueBind(
		queue.Name,
		routingKey,
		topicExchange,
		false,
		nil,
	)
	if err != nil {
		msg := "failed to bind to queue"
		logger.Error(msg, "err", err.Error())
		panic(err)
	}

	logger.Info("successfully bound to queue")

	messagesFromQueue, err := msgctx.channel.Consume(
		queue.Name, //queue
		"",         //consumer
		false,      //auto ack
		true,       //exclusive
		false,      //no local
		false,      //no-wait
		nil,        //args
	)
	if err != nil {
		msg := "failed to register a consumer with queue"
		logger.Error(msg, "err", err.Error())
		panic(msg)
	}

	logger.Info("successfully registered as a consumer")

	go func() {
		for msg := range messagesFromQueue {

			msgHandled := make(chan struct{}, 1)

			msgctx.queue <- func() {
				sublog := logger
				ctx := tracing.ExtractAMQPHeaders(context.Background(), msg.Headers)
				ctx, span := tracer.Start(ctx, queue.Name+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

				traceID := span.SpanContext().TraceID()
				if traceID.IsValid() {
					sublog = sublog.With(slog.String("traceID", traceID.String()))
				}

				handler(ctx, msg, sublog)

				err = msg.Ack(false)
				if err != nil {
					sublog.Error("failed to ack message delivery", "err", err.Error())
					span.RecordError(err)
				}

				span.End()

				msgHandled <- struct{}{}
			}

			<-msgHandled
		}

		logger.Error("topic message queue was closed")
		os.Exit(1)
	}()

	registerComplete <- nil
}

func createMessageQueueChannel(ctx context.Context, msgctx *rabbitMQContext) (*rabbitMQContext, error) {
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

func createCommandExchange(ctx context.Context, msgctx *rabbitMQContext) error {
	err := msgctx.channel.ExchangeDeclare(commandExchange, amqp.ExchangeDirect, false, false, false, false, nil)

	if err != nil {
		return fmt.Errorf("unable to declare command exchange %s: (%w)", commandExchange, err)
	}

	return nil
}

func createTopicExchange(ctx context.Context, msgctx *rabbitMQContext) error {
	err := msgctx.channel.ExchangeDeclare(topicExchange, amqp.ExchangeTopic, false, false, false, false, nil)

	if err != nil {
		return fmt.Errorf("unable to declare topic exchange %s: (%w)", topicExchange, err)
	}

	return nil
}

func createCommandAndResponseQueues(ctx context.Context, msgctx *rabbitMQContext) error {
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
	msgctx.commandChannel, err = msgctx.channel.Consume(commandQueue.Name, "command-consumer", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("unable to start consuming commands from %s: (%w)", commandQueue.Name, err)
	}

	msgctx.commandLogger = cmdlog

	go msgctx.RegisterCommandHandler(PingCommandContentType, NewPingCommandHandler(msgctx))

	msgctx.responseQueueName = responseQueue.Name
	msgctx.responseChannel, err = msgctx.channel.Consume(responseQueue.Name, "response-consumer", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("unable to start consuming responses from %s: (%w)", responseQueue.Name, err)
	}

	go func() {
		err := msgctx.NoteToSelf(context.Background(), NewPingCommand())
		if err != nil {
			cmdlog.Error("failed to publish a ping command to ourselves!", "err", err.Error())
		}
	}()

	return nil
}

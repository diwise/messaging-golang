package messaging

import (
	"context"
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

type Acknowledgeable interface {
	Ack(bool) error
}

type Incoming interface {
	Headers() map[string]interface{}
}

type Message interface {
	Body() []byte
	ContentType() string
}

type Command interface {
	Message
}

type Response interface {
	Message
}

type IncomingResponse interface {
	Incoming
	Response
}

type AcknowledgeableIncomingResponse interface {
	Acknowledgeable
	IncomingResponse
}

// Command is an interface used when handling commands
type IncomingCommand interface {
	Incoming
	Command
	Context() context.Context
	RespondWith(context.Context, Response) error
}

type AcknowledgeableIncomingCommand interface {
	Acknowledgeable
	IncomingCommand
}

// TopicMessage is an interface used when sending messages to make sure
// that messages are sent to the correct topic with correct content type
type TopicMessage interface {
	Message
	TopicName() string
}

type IncomingTopicMessage interface {
	Incoming
	TopicMessage
}

type AcknowledgeableIncomingTopicMessage interface {
	Acknowledgeable
	IncomingTopicMessage
}

// MsgContext encapsulates the underlying messaging primitives, as well as
// their associated configuration
type MsgContext interface {
	NoteToSelf(ctx context.Context, command Command) error
	SendCommandTo(ctx context.Context, command Command, key string) error
	SendResponseTo(ctx context.Context, response Response, key string) error
	PublishOnTopic(ctx context.Context, message TopicMessage) error

	Start()
	Close()

	RegisterCommandHandler(filter MessageFilter, handler CommandHandler) error
	RegisterTopicMessageHandler(routingKey string, handler TopicMessageHandler) error
	RegisterTopicMessageHandlerWithFilter(routingKey string, handler TopicMessageHandler, filter MessageFilter) error
}

// action is the type that we enqueue on our internal queue
type action func()

type cmdFilterHandlerPair struct {
	wants MessageFilter
	fn    CommandHandler
}

type tmhFilterHandlerPair struct {
	wants MessageFilter
	fn    TopicMessageHandler
}

type messagingContext struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	cfg        Config

	queue chan action

	commandHandlers []cmdFilterHandlerPair

	commandQueueName string
	commandChannel   chan AcknowledgeableIncomingCommand
	commandLogger    *slog.Logger

	responseQueueName string
	responseChannel   chan AcknowledgeableIncomingResponse
	responseLogger    *slog.Logger

	topicSubscriptions   map[string]struct{}
	topicMessageHandlers map[string][]tmhFilterHandlerPair

	connectionClosedError chan *amqp.Error

	keepRunning *atomic.Bool
	wg          sync.WaitGroup
}

var tracer = otel.Tracer("messaging-rmq")

func (rmq *messagingContext) oncommand(cmd AcknowledgeableIncomingCommand) {
	sublog := rmq.commandLogger

	ctx := tracing.ExtractAMQPHeaders(context.Background(), cmd.Headers())
	ctx, span := tracer.Start(ctx, rmq.commandQueueName+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

	traceID := span.SpanContext().TraceID()
	if traceID.IsValid() {
		sublog = sublog.With(slog.String("traceID", traceID.String()))
	}

	sublog.Info("received command", "content-type", cmd.ContentType, "body", string(cmd.Body()))

	wg := sync.WaitGroup{}
	matchingHandlers := 0

	for _, handler := range rmq.commandHandlers {
		if handler.wants(cmd) {
			wg.Add(1)
			matchingHandlers++

			go func(handle CommandHandler) {
				handle(ctx, cmd, sublog)
				wg.Done()
			}(handler.fn)

			// allow at most one handler to receive each command
			break
		}
	}

	if matchingHandlers == 0 {
		err := fmt.Errorf("no handler registered for command")
		span.RecordError(err)
		sublog.Warn("no handler", "err", err.Error())
	}

	go func() {
		wg.Wait()

		span.End()
		cmd.Ack(false)
	}()
}

func (rmq *messagingContext) onresponse(response AcknowledgeableIncomingResponse) {
	ctx := tracing.ExtractAMQPHeaders(context.Background(), response.Headers())
	_, span := tracer.Start(ctx, rmq.responseQueueName+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

	resplog := rmq.responseLogger

	traceID := span.SpanContext().TraceID()
	if traceID.IsValid() {
		resplog = resplog.With(slog.String("traceID", traceID.String()))
	}

	// TODO: Ability to dispatch response to an application supplied response handler
	resplog.Info("received response", "body", string(response.Body()))
	response.Ack(false)

	span.End()
}

func (rmq *messagingContext) onmessage(msg AcknowledgeableIncomingTopicMessage, queueName, routingKey string, logger *slog.Logger) {
	sublog := logger
	ctx := tracing.ExtractAMQPHeaders(context.Background(), msg.Headers())
	ctx, span := tracer.Start(ctx, queueName+" receive", trace.WithSpanKind(trace.SpanKindConsumer))

	traceID := span.SpanContext().TraceID()
	if traceID.IsValid() {
		sublog = sublog.With(slog.String("traceID", traceID.String()))
	}

	handlers := rmq.topicMessageHandlers[routingKey]

	// Use a waitgroup to spawn a goroutine for each matching topic
	// message handler and to be able to wait for their completion
	wg := sync.WaitGroup{}
	for _, handler := range handlers {
		if handler.wants(msg) {
			wg.Add(1)
			go func(handle TopicMessageHandler) {
				handle(ctx, msg, sublog)
				wg.Done()
			}(handler.fn)
		}
	}

	// Spawn a goroutine to wait for the completion of all handlers so that we dont
	// block the messaging loop and cause a deadlock if any of them wants to send something
	go func() {
		wg.Wait()

		err := msg.Ack(false)
		if err != nil {
			sublog.Error("failed to ack message delivery", "err", err.Error())
			span.RecordError(err)
		}

		span.End()
	}()
}

func (rmq *messagingContext) run() {
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
				rmq.oncommand(cmd)
			}
		case response := <-rmq.responseChannel:
			{
				rmq.onresponse(response)
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
func (rmq *messagingContext) NoteToSelf(ctx context.Context, command Command) error {
	return rmq.SendCommandTo(ctx, command, rmq.serviceName())
}

// SendCommandTo enqueues a command to the given routing key via the command exchange
func (rmq *messagingContext) SendCommandTo(ctx context.Context, command Command, key string) error {
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

	completion := make(chan error, 1)

	// Enqueue this action on the command queue
	rmq.queue <- func() {
		// Send the publish response to the completion channel
		completion <- rmq.channel.Publish(commandExchange, key, true, false, amqp.Publishing{
			Headers:     tracing.InjectAMQPHeaders(ctx),
			ContentType: command.ContentType(),
			ReplyTo:     rmq.responseQueueName,
			Body:        command.Body(),
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
func (rmq *messagingContext) SendResponseTo(ctx context.Context, response Response, key string) error {
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

	completion := make(chan error, 1)

	rmq.queue <- func() {
		completion <- rmq.channel.Publish(commandExchange, key, true, false, amqp.Publishing{
			Headers:     tracing.InjectAMQPHeaders(ctx),
			ContentType: response.ContentType(),
			Body:        response.Body(),
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

// PublishOnTopic takes a TopicMessage, reads its TopicName property,
// and publishes it to the correct topic with the correct content type
func (rmq *messagingContext) PublishOnTopic(ctx context.Context, message TopicMessage) error {
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

	completion := make(chan error, 1)

	rmq.queue <- func() {
		completion <- rmq.channel.Publish(topicExchange, message.TopicName(), false, false,
			amqp.Publishing{
				Headers:     tracing.InjectAMQPHeaders(ctx),
				ContentType: message.ContentType(),
				Body:        message.Body(),
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
func (rmq *messagingContext) Close() {
	rmq.queue <- func() {
		rmq.keepRunning.Store(false)
	}

	rmq.wg.Wait()

	rmq.channel.Close()
	rmq.connection.Close()
}

func (rmq *messagingContext) Start() {
	go rmq.run()
}

// CommandHandler is a callback type to be used for dispatching incoming commands
type CommandHandler func(context.Context, IncomingCommand, *slog.Logger) error

// RegisterCommandHandler registers a handler to be called when a command that matches
// the specified filter is received
func (rmq *messagingContext) RegisterCommandHandler(filter MessageFilter, handler CommandHandler) error {

	completion := make(chan error, 1)

	rmq.queue <- func() {
		if rmq.commandHandlers == nil {
			rmq.commandHandlers = []cmdFilterHandlerPair{}
		}

		rmq.commandHandlers = append(rmq.commandHandlers, cmdFilterHandlerPair{filter, handler})

		completion <- nil
	}

	// Wait until the action has been processed (handler has been registered)
	err, tmo := waitOnChannel(completion, seconds(5.0))
	if tmo != nil {
		return tmo
	}

	return err
}

func (rmq *messagingContext) serviceName() string {
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
	var msgctx *messagingContext
	var err error

	for {

		time.Sleep(2 * time.Second)

		msgctx, err = createMessageQueueChannel(ctx, &messagingContext{
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
		msgctx.topicMessageHandlers = map[string][]tmhFilterHandlerPair{}
		msgctx.topicSubscriptions = map[string]struct{}{}

		initComplete <- msgctx
		break
	}
}

// TopicMessageHandler is a callback type that should be passed
// to RegisterTopicMessageHandler to receive messages from topics.
type TopicMessageHandler func(context.Context, IncomingTopicMessage, *slog.Logger)

// MessageFilter allows a subscriber to supply a filter function that
// decides if a received message should be delivered to the handler or not
type MessageFilter func(Message) bool

// MatchContentType returns a topic message filter that returns true
// for all messages where the content type matches the supplied content type
// case insensitive
func MatchContentType(contentType string) MessageFilter {
	lowerCaseMatch := strings.ToLower(contentType)
	matchLength := len(lowerCaseMatch)

	return func(m Message) bool {
		ct := m.ContentType()
		if len(ct) != matchLength {
			return false
		}

		return strings.Compare(lowerCaseMatch, strings.ToLower(ct)) == 0
	}
}

// RegisterTopicMessageHandler creates a subscription queue that is bound
// to the topic exchange with the provided routing key, starts a consumer
// for that queue and hands off any received messages to the provided
// TopicMessageHandler
func (msgctx *messagingContext) RegisterTopicMessageHandler(routingKey string, handler TopicMessageHandler) error {
	return msgctx.RegisterTopicMessageHandlerWithFilter(routingKey, handler, func(Message) bool { return true })
}

func (msgctx *messagingContext) RegisterTopicMessageHandlerWithFilter(routingKey string, handler TopicMessageHandler, filter MessageFilter) error {

	completion := make(chan error, 1)

	msgctx.queue <- func() {
		msgctx.topicMessageHandlers[routingKey] = append(
			msgctx.topicMessageHandlers[routingKey],
			tmhFilterHandlerPair{filter, handler},
		)
		registerTMH(msgctx, routingKey, completion)
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

func registerTMH(msgctx *messagingContext, routingKey string, registerComplete chan error) {

	// Do not create more than one listener queue and consumer per routing key
	if _, exists := msgctx.topicSubscriptions[routingKey]; exists {
		registerComplete <- nil
	}

	queue, err := msgctx.channel.QueueDeclare(
		"",    //name
		false, //durable
		true,  //delete when unused
		false, //exclusive
		false, //no-wait
		nil,   //arguments
	)
	if err != nil {
		registerComplete <- fmt.Errorf("failed to declare a queue: (%w)", err)
		return
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
		registerComplete <- fmt.Errorf("failed to bind to queue %s: (%w)", queue.Name, err)
		return
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
		registerComplete <- fmt.Errorf("failed to register a consumer with queue %s: (%w)", queue.Name, err)
		return
	}

	logger.Info("successfully registered as a consumer")

	// Save a token for this routing key, so that we know that we are already
	// registered with a listener for it
	msgctx.topicSubscriptions[routingKey] = struct{}{}

	go func() {
		for msg := range messagesFromQueue {

			msgHandled := make(chan struct{}, 1)

			msgctx.queue <- func() {
				wrapper := newAMQPDeliveryWrapper(msgctx, &msg)
				msgctx.onmessage(wrapper, queue.Name, routingKey, logger)
				msgHandled <- struct{}{}
			}

			<-msgHandled
		}

		logger.Error("topic message queue was closed")
		os.Exit(1)
	}()

	registerComplete <- nil
}

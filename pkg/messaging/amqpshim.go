package messaging

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
)

type amqpDeliveryWrapper struct {
	rmq MsgContext
	d   *amqp.Delivery
}

func newAMQPDeliveryWrapper(ctx MsgContext, d *amqp.Delivery) *amqpDeliveryWrapper {
	return &amqpDeliveryWrapper{rmq: ctx, d: d}
}

func (w *amqpDeliveryWrapper) Body() []byte {
	return w.d.Body
}

func (w *amqpDeliveryWrapper) ContentType() string {
	return w.d.ContentType
}

func (w *amqpDeliveryWrapper) Context() context.Context {
	// TODO: Extract trace context from headers
	return ExtractAMQPHeaders(context.Background(), w.d.Headers)
}

func (w *amqpDeliveryWrapper) RespondWith(ctx context.Context, response CommandMessage) error {
	return w.rmq.SendResponseTo(ctx, response, w.d.ReplyTo)
}

type AmqpHeadersCarrier map[string]interface{}

func (a AmqpHeadersCarrier) Get(key string) string {
	v, ok := a[key]
	if !ok {
		return ""
	}
	return v.(string)
}

func (a AmqpHeadersCarrier) Set(key string, value string) {
	a[key] = value
}

func (a AmqpHeadersCarrier) Keys() []string {
	i := 0
	r := make([]string, len(a))

	for k := range a {
		r[i] = k
		i++
	}

	return r
}

// InjectAMQPHeaders injects the tracing from the context into the header map
func InjectAMQPHeaders(ctx context.Context) map[string]interface{} {
	h := make(AmqpHeadersCarrier)
	otel.GetTextMapPropagator().Inject(ctx, h)
	return h
}

// ExtractAMQPHeaders extracts the tracing from the header and puts it into the context
func ExtractAMQPHeaders(ctx context.Context, headers map[string]interface{}) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, AmqpHeadersCarrier(headers))
}

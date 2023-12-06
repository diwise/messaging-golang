package messaging

import (
	"context"

	"github.com/diwise/messaging-golang/pkg/messaging/tracing"
	amqp "github.com/rabbitmq/amqp091-go"
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
	return tracing.ExtractAMQPHeaders(context.Background(), w.d.Headers)
}

func (w *amqpDeliveryWrapper) RespondWith(ctx context.Context, response Response) error {
	return w.rmq.SendResponseTo(ctx, response, w.d.ReplyTo)
}

func (w *amqpDeliveryWrapper) TopicName() string {
	return w.d.RoutingKey
}

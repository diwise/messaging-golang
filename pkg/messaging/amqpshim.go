package messaging

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

type amqpDeliveryWrapper struct {
	ctx Context
	d   *amqp.Delivery
}

func newAMQPDeliveryWrapper(ctx Context, d *amqp.Delivery) *amqpDeliveryWrapper {
	return &amqpDeliveryWrapper{ctx: ctx, d: d}
}

func (w *amqpDeliveryWrapper) Body() []byte {
	return w.d.Body
}

func (w *amqpDeliveryWrapper) ContentType() string {
	return w.d.ContentType
}

func (w *amqpDeliveryWrapper) RespondWith(response CommandMessage) error {
	return w.ctx.SendResponseTo(response, w.d.ReplyTo)
}

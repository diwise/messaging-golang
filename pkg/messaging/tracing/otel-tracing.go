package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
)

type headersCarrier map[string]interface{}

func (a headersCarrier) Get(key string) string {
	v, ok := a[key]
	if !ok {
		return ""
	}
	return v.(string)
}

func (a headersCarrier) Set(key string, value string) {
	a[key] = value
}

func (a headersCarrier) Keys() []string {
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
	h := make(headersCarrier)
	otel.GetTextMapPropagator().Inject(ctx, h)
	return h
}

// ExtractAMQPHeaders extracts the tracing from the header and puts it into the context
func ExtractAMQPHeaders(ctx context.Context, headers map[string]interface{}) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, headersCarrier(headers))
}

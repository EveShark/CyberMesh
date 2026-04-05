package observability

import (
	"context"
	"strings"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type textCarrier map[string]string

func (c textCarrier) Get(key string) string {
	return c[strings.ToLower(key)]
}

func (c textCarrier) Set(key, value string) {
	c[strings.ToLower(key)] = value
}

func (c textCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

func ExtractContextFromSaramaHeaders(ctx context.Context, headers []*sarama.RecordHeader) context.Context {
	carrier := make(textCarrier)
	for _, h := range headers {
		if h == nil || len(h.Key) == 0 {
			continue
		}
		carrier.Set(string(h.Key), string(h.Value))
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func InjectContextToSaramaHeaders(ctx context.Context, headers []sarama.RecordHeader) []sarama.RecordHeader {
	carrier := make(textCarrier)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return headers
	}

	out := make([]sarama.RecordHeader, 0, len(headers)+len(carrier))
	out = append(out, headers...)
	existing := map[string]int{}
	for i := range out {
		existing[strings.ToLower(string(out[i].Key))] = i
	}
	for key, value := range carrier {
		if idx, ok := existing[key]; ok {
			out[idx].Value = []byte(value)
			continue
		}
		out = append(out, sarama.RecordHeader{Key: []byte(key), Value: []byte(value)})
	}
	return out
}

func CurrentPropagator() propagation.TextMapPropagator {
	return otel.GetTextMapPropagator()
}

package observability

import (
	"context"
	"net/http"
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

func ExtractContextFromHTTPHeaders(ctx context.Context, headers http.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
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

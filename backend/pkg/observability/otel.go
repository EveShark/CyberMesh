package observability

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
	"go.opentelemetry.io/otel/trace"
)

const envEnabled = "OTEL_ENABLED"

// InitFromEnv initializes OpenTelemetry tracing from environment variables.
// It is intentionally fail-open: when misconfigured, callers can log the error and continue.
func InitFromEnv(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv(envEnabled)), "true")
	if !enabled {
		return func(context.Context) error { return nil }, nil
	}

	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return nil, fmt.Errorf("otel enabled but OTEL_EXPORTER_OTLP_ENDPOINT is empty")
	}
	parsedEndpoint, insecure, err := parseOTLPEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	sampleRatio := 0.1
	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed >= 0 && parsed <= 1 {
			sampleRatio = parsed
		}
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(parsedEndpoint.Host),
		otlptracehttp.WithURLPath(parsedEndpoint.Path),
		otlptracehttp.WithTimeout(5 * time.Second),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("telemetry.sdk.language", "go"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

func parseOTLPEndpoint(raw string) (*url.URL, bool, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return nil, false, fmt.Errorf("otel endpoint is empty")
	}
	if !strings.Contains(normalized, "://") {
		normalized = "http://" + normalized
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, false, fmt.Errorf("invalid OTEL_EXPORTER_OTLP_ENDPOINT %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, false, fmt.Errorf("unsupported OTEL endpoint scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, false, fmt.Errorf("OTEL endpoint host is empty in %q", raw)
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1/traces"
	}
	return parsed, parsed.Scheme == "http", nil
}

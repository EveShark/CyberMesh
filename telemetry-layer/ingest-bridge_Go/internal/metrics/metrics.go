package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	EventsTotal     prometheus.Counter
	EventsInvalid   prometheus.Counter
	AggregatesTotal prometheus.Counter
	DLQTotal        prometheus.Counter
}

func New() *Metrics {
	return &Metrics{
		EventsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "telemetry_bridge_events_total",
			Help: "Total events received from Hubble",
		}),
		EventsInvalid: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "telemetry_bridge_events_invalid_total",
			Help: "Invalid events or parse errors",
		}),
		AggregatesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "telemetry_bridge_aggregates_total",
			Help: "Aggregated flow records emitted",
		}),
		DLQTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "telemetry_bridge_dlq_total",
			Help: "DLQ events emitted",
		}),
	}
}

func (m *Metrics) Register() {
	prometheus.MustRegister(m.EventsTotal, m.EventsInvalid, m.AggregatesTotal, m.DLQTotal)
}

func StartServer(addr string) {
	http.Handle("/metrics", promhttp.Handler())
	_ = http.ListenAndServe(addr, nil)
}

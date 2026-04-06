package spectra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/adapter"
	"cybermesh/telemetry-layer/adapters/internal/codec"
	"cybermesh/telemetry-layer/adapters/internal/kafka"
	"cybermesh/telemetry-layer/adapters/internal/model"
	"cybermesh/telemetry-layer/adapters/internal/observability"
	"cybermesh/telemetry-layer/adapters/internal/telemetrymetrics"
	"cybermesh/telemetry-layer/adapters/utils"
	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Service struct {
	cfg     Config
	prod    producer
	logger  *utils.Logger
	deduper *deduper
	client  *http.Client
}

type producer interface {
	SendToTopic(topic, key string, payload []byte, headers ...sarama.RecordHeader) error
}

func NewService(cfg Config, prod *kafka.Producer, logger *utils.Logger) *Service {
	return NewServiceWithProducer(cfg, prod, logger)
}

func NewServiceWithProducer(cfg Config, prod producer, logger *utils.Logger) *Service {
	return &Service{
		cfg:     cfg,
		prod:    prod,
		logger:  logger,
		deduper: newDeduper(cfg.DedupeTTL),
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *Service) Run(ctx context.Context) error {
	if s.prod == nil {
		return errors.New("producer required")
	}
	if s.logger == nil {
		return errors.New("logger required")
	}
	if strings.TrimSpace(s.cfg.ListenAddr) == "" {
		s.cfg.ListenAddr = ":8087"
	}
	if strings.TrimSpace(s.cfg.WebhookPath) == "" {
		s.cfg.WebhookPath = "/ingest/app-events"
	}
	if s.cfg.ReplayWindow <= 0 {
		s.cfg.ReplayWindow = 5 * time.Minute
	}
	if s.cfg.FlowEncoding == "" {
		s.cfg.FlowEncoding = "json"
	}
	if s.cfg.DeepFlowEncoding == "" {
		s.cfg.DeepFlowEncoding = "json"
	}
	if strings.TrimSpace(s.cfg.FlowTopic) == "" {
		s.cfg.FlowTopic = "telemetry.flow.v1"
	}
	if strings.TrimSpace(s.cfg.DeepFlowTopic) == "" {
		s.cfg.DeepFlowTopic = "telemetry.deepflow.v1"
	}
	if s.cfg.WebhookRequireSignature && strings.TrimSpace(s.cfg.WebhookSecret) == "" {
		return errors.New("WEBHOOK_SECRET is required when webhook signature is required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.WebhookPath, s.handleWebhook)
	// Backward compatibility during rename rollout.
	if s.cfg.WebhookPath != "/ingest/spectra" {
		mux.HandleFunc("/ingest/spectra", s.handleWebhook)
	}
	if s.cfg.WebhookPath != "/ingest/app-events" {
		mux.HandleFunc("/ingest/app-events", s.handleWebhook)
	}
	mux.Handle("/metrics", telemetrymetrics.Global().Handler())

	srv := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("app-events adapter listening",
			utils.ZapString("addr", s.cfg.ListenAddr),
			utils.ZapString("path", s.cfg.WebhookPath))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if strings.TrimSpace(s.cfg.PollURL) != "" {
		go s.runPoller(ctx)
	}

	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Service) handleWebhook(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	parentCtx := observability.ExtractContextFromHTTPHeaders(r.Context(), r.Header)
	ctx, span := observability.Tracer("telemetry/app-events-adapter").Start(parentCtx, "app_events.handle_webhook")
	defer span.End()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		span.SetStatus(codes.Error, "method not allowed")
		return
	}
	span.SetAttributes(
		attribute.String("http.method", r.Method),
		attribute.String("http.path", r.URL.Path),
	)
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		telemetrymetrics.Global().IncCounter("spectra_ingest_reject_total")
		http.Error(w, "bad request", http.StatusBadRequest)
		span.RecordError(err)
		span.SetStatus(codes.Error, "read body failed")
		return
	}
	span.SetAttributes(attribute.Int("app_events.payload_bytes", len(body)))
	if s.cfg.WebhookRequireSignature || strings.TrimSpace(s.cfg.WebhookSecret) != "" {
		sigHeader := r.Header.Get(headerOrDefault(s.cfg.SignatureHeader, "X-CM-Signature"))
		tsHeader := r.Header.Get(headerOrDefault(s.cfg.SignatureTimestampHeader, "X-CM-Timestamp"))
		if err := verifySignature(s.cfg.WebhookSecret, body, sigHeader, tsHeader, s.cfg.ReplayWindow); err != nil {
			telemetrymetrics.Global().IncCounter("spectra_ingest_reject_total")
			telemetrymetrics.Global().IncCounter("spectra_reject_signature_total")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			span.RecordError(err)
			span.SetStatus(codes.Error, "signature verification failed")
			return
		}
	}
	envs, err := parseEnvelopes(body)
	if err != nil {
		telemetrymetrics.Global().IncCounter("spectra_ingest_reject_total")
		telemetrymetrics.Global().IncCounter("spectra_reject_json_total")
		http.Error(w, "invalid json", http.StatusBadRequest)
		span.RecordError(err)
		span.SetStatus(codes.Error, "json parse failed")
		return
	}
	accepted := 0
	for _, raw := range envs {
		if err := s.processEnvelope(ctx, raw); err != nil {
			telemetrymetrics.Global().IncCounter("spectra_ingest_reject_total")
			continue
		}
		accepted++
	}
	telemetrymetrics.Global().RecordOperation("spectra_ingest", "ok", time.Since(start))
	span.SetAttributes(
		attribute.Int("app_events.received", len(envs)),
		attribute.Int("app_events.accepted", accepted),
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"accepted": accepted,
		"received": len(envs),
	})
}

func (s *Service) runPoller(ctx context.Context) {
	interval := s.cfg.PollInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollOnce(ctx)
		}
	}
}

func (s *Service) pollOnce(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.PollURL, nil)
	if err != nil {
		s.logger.Warn("spectra poll build request failed", utils.ZapError(err))
		return
	}
	if strings.TrimSpace(s.cfg.PollAuthBearer) != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.PollAuthBearer)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Warn("spectra poll failed", utils.ZapError(err))
		telemetrymetrics.Global().IncCounter("spectra_poll_error_total")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		telemetrymetrics.Global().IncCounter("spectra_poll_error_total")
		s.logger.Warn("spectra poll bad status", utils.ZapInt("status", resp.StatusCode))
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		telemetrymetrics.Global().IncCounter("spectra_poll_error_total")
		return
	}
	envs, err := parseEnvelopes(body)
	if err != nil {
		telemetrymetrics.Global().IncCounter("spectra_poll_error_total")
		return
	}
	for _, raw := range envs {
		if err := s.processEnvelope(ctx, raw); err != nil {
			continue
		}
	}
	telemetrymetrics.Global().IncCounter("spectra_poll_success_total")
}

func parseEnvelopes(body []byte) ([]Envelope, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, errors.New("empty payload")
	}
	if body[0] == '[' {
		var arr []Envelope
		if err := json.Unmarshal(body, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var single Envelope
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, err
	}
	return []Envelope{single}, nil
}

func (s *Service) processEnvelope(ctx context.Context, raw Envelope) error {
	processStart := time.Now()
	processStatus := "ok"
	processOperation := "spectra_process_other"
	ctx, span := observability.Tracer("telemetry/app-events-adapter").Start(ctx, "app_events.process_envelope")
	defer func() {
		telemetrymetrics.Global().RecordOperation(processOperation, processStatus, time.Since(processStart))
		span.SetAttributes(
			attribute.String("app_events.operation", processOperation),
			attribute.String("app_events.status", processStatus),
		)
		span.End()
	}()

	env, err := normalize(raw, s.cfg)
	if err != nil {
		processStatus = "error"
		s.recordRejectReason(err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "normalize failed")
		return err
	}
	processOperation = operationForEnvelope(env)
	span.SetAttributes(
		attribute.String("app_events.modality", env.Modality),
		attribute.String("app_events.event_category", env.EventCategory),
		attribute.String("app_events.event_name", env.EventName),
		attribute.String("app_events.trace_id", env.TraceID),
		attribute.String("app_events.source_event_id", env.SourceEventID),
		attribute.String("app_events.tenant_id", env.TenantID),
	)

	now := time.Now()
	if !s.deduper.firstSeen(dedupeID(env), now) {
		processStatus = "duplicate"
		telemetrymetrics.Global().IncCounter("spectra_replay_drop_total")
		span.SetStatus(codes.Error, "duplicate source_event_id")
		return errors.New("duplicate source_event_id")
	}
	if env.EventCategory == "request" {
		if err := s.publishFlow(ctx, env); err != nil {
			processStatus = "error"
			telemetrymetrics.Global().IncCounter("spectra_publish_error_total")
			span.RecordError(err)
			span.SetStatus(codes.Error, "publish flow failed")
			return err
		}
		if s.cfg.RequestMirrorDeepFlow {
			if err := s.publishDeepFlow(ctx, env, "request_observed"); err != nil {
				processStatus = "error"
				telemetrymetrics.Global().IncCounter("spectra_publish_error_total")
				span.RecordError(err)
				span.SetStatus(codes.Error, "publish deepflow mirror failed")
				return err
			}
		}
		return nil
	}
	if env.Modality != "" {
		if err := s.publishModalityEvent(ctx, env); err != nil {
			processStatus = "error"
			telemetrymetrics.Global().IncCounter("spectra_publish_error_total")
			span.RecordError(err)
			span.SetStatus(codes.Error, "publish modality event failed")
			return err
		}
		return nil
	}
	if err := s.publishDeepFlow(ctx, env, env.EventName); err != nil {
		processStatus = "error"
		telemetrymetrics.Global().IncCounter("spectra_publish_error_total")
		span.RecordError(err)
		span.SetStatus(codes.Error, "publish deepflow failed")
		return err
	}
	return nil
}

func (s *Service) recordRejectReason(err error) {
	switch {
	case errors.Is(err, errMissingAppID),
		errors.Is(err, errMissingSourceType),
		errors.Is(err, errMissingCategory),
		errors.Is(err, errMissingEventName),
		errors.Is(err, errMissingTraceID),
		errors.Is(err, errRequestNeedsNet),
		errors.Is(err, errInvalidNetworkIP),
		errors.Is(err, errInvalidNetworkPort):
		telemetrymetrics.Global().IncCounter("spectra_reject_missing_field_total")
	case errors.Is(err, errInvalidModality), errors.Is(err, errMissingModality), errors.Is(err, errMissingFeatures):
		telemetrymetrics.Global().IncCounter("spectra_reject_modality_total")
	case errors.Is(err, errMissingTenantID):
		telemetrymetrics.Global().IncCounter("spectra_reject_tenant_total")
	case errors.Is(err, errMissingEventID):
		telemetrymetrics.Global().IncCounter("spectra_reject_source_event_id_total")
	default:
		telemetrymetrics.Global().IncCounter("spectra_reject_other_total")
	}
}

func (s *Service) publishFlow(ctx context.Context, env Envelope) error {
	start := time.Now()
	status := "ok"
	ctx, span := observability.Tracer("telemetry/app-events-adapter").Start(ctx, "app_events.publish_flow")
	defer func() {
		telemetrymetrics.Global().RecordOperation("spectra_publish_flow", status, time.Since(start))
		span.SetAttributes(attribute.String("app_events.status", status))
		span.End()
	}()

	rec := adapter.Record{
		Timestamp:       env.TimestampMs / 1000,
		SourceEventTsMs: env.TimestampMs,
		TenantID:        env.TenantID,
		TraceID:         env.TraceID,
		SourceEventID:   env.SourceEventID,
		SrcIP:           env.Network.SrcIP,
		DstIP:           env.Network.DstIP,
		SrcPort:         env.Network.SrcPort,
		DstPort:         env.Network.DstPort,
		Proto:           env.Network.Proto,
		BytesFwd:        env.Network.BytesFwd,
		BytesBwd:        env.Network.BytesBwd,
		PktsFwd:         env.Network.PktsFwd,
		PktsBwd:         env.Network.PktsBwd,
		DurationMS:      env.Network.DurationMS,
		SourceType:      env.SourceType,
		SourceID:        nonEmpty(env.SourceID, env.AppID),
		MetricsKnown:    true,
	}
	flow, err := adapter.ToFlow(rec, 10)
	if err != nil {
		return err
	}
	payload, err := codec.EncodeFlowAggregate(flow, s.cfg.FlowEncoding)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "encode flow failed")
		return err
	}
	headers := observability.InjectContextToSaramaHeaders(ctx, nil)
	if err := s.prod.SendToTopic(s.cfg.FlowTopic, flow.FlowID, payload, headers...); err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, "kafka send flow failed")
		return err
	}
	telemetrymetrics.Global().IncCounter("spectra_publish_flow_total")
	recordIngestToPublishLatency(env.TimestampMs, "spectra_ingest_to_publish_flow")
	return nil
}

func (s *Service) publishDeepFlow(ctx context.Context, env Envelope, alertType string) error {
	start := time.Now()
	status := "ok"
	ctx, span := observability.Tracer("telemetry/app-events-adapter").Start(ctx, "app_events.publish_deepflow")
	defer func() {
		telemetrymetrics.Global().RecordOperation("spectra_publish_deepflow", status, time.Since(start))
		span.SetAttributes(attribute.String("app_events.status", status))
		span.End()
	}()

	meta := make(map[string]string, len(env.Metadata)+8)
	for k, v := range env.Metadata {
		meta[k] = v
	}
	meta["app_id"] = env.AppID
	meta["env"] = env.Env
	meta["tenant_id"] = env.TenantID
	meta["user_id"] = env.UserID
	meta["request_id"] = env.RequestID
	meta["trace_id"] = env.TraceID
	meta["source_event_id"] = env.SourceEventID
	meta["event_name"] = env.EventName

	ev := model.DeepFlowEvent{
		Schema:        "deepflow.v1",
		Timestamp:     env.TimestampMs / 1000,
		TenantID:      env.TenantID,
		SensorID:      nonEmpty(env.SourceID, env.AppID),
		SourceType:    env.SourceType,
		AlertType:     alertType,
		AlertCategory: env.EventCategory,
		Severity:      "info",
		Signature:     env.EventName,
		SignatureID:   env.SourceEventID,
		Metadata:      meta,
		FlowID:        nonEmpty(env.TraceID, env.SourceEventID),
	}
	if env.Network != nil {
		ev.SrcIP = env.Network.SrcIP
		ev.DstIP = env.Network.DstIP
		ev.SrcPort = env.Network.SrcPort
		ev.DstPort = env.Network.DstPort
		ev.Proto = env.Network.Proto
	}
	payload, err := codec.EncodeDeepFlow(ev, s.cfg.DeepFlowEncoding)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "encode deepflow failed")
		return err
	}
	key := nonEmpty(env.TraceID, env.SourceEventID)
	headers := observability.InjectContextToSaramaHeaders(ctx, nil)
	if err := s.prod.SendToTopic(s.cfg.DeepFlowTopic, key, payload, headers...); err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, "kafka send deepflow failed")
		return err
	}
	telemetrymetrics.Global().IncCounter("spectra_publish_deepflow_total")
	recordIngestToPublishLatency(env.TimestampMs, "spectra_ingest_to_publish_deepflow")
	return nil
}

func (s *Service) publishModalityEvent(ctx context.Context, env Envelope) error {
	start := time.Now()
	status := "ok"
	modalityToken := sanitizeMetricToken(env.Modality)
	ctx, span := observability.Tracer("telemetry/app-events-adapter").Start(ctx, "app_events.publish_modality")
	defer func() {
		telemetrymetrics.Global().RecordOperation("spectra_publish_modality_"+modalityToken, status, time.Since(start))
		span.SetAttributes(
			attribute.String("app_events.status", status),
			attribute.String("app_events.modality", env.Modality),
		)
		span.End()
	}()

	features := buildModalityFeatures(env)
	payload := map[string]any{
		"schema":          "app_event.v1",
		"ts":              env.TimestampMs / 1000,
		"tenant_id":       env.TenantID,
		"sensor_id":       nonEmpty(env.SourceID, env.AppID),
		"source_type":     env.SourceType,
		"modality":        env.Modality,
		"event_name":      env.EventName,
		"event_category":  env.EventCategory,
		"request_id":      env.RequestID,
		"trace_id":        env.TraceID,
		"source_event_id": env.SourceEventID,
		"metadata":        env.Metadata,
		"features":        features,
		"labels":          env.Labels,
	}
	if len(env.Attributes) > 0 {
		payload["attributes"] = env.Attributes
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, "marshal modality failed")
		return fmt.Errorf("marshal modality payload: %w", err)
	}
	key := nonEmpty(env.TraceID, env.SourceEventID)
	headers := observability.InjectContextToSaramaHeaders(ctx, nil)
	if err := s.prod.SendToTopic(s.cfg.DeepFlowTopic, key, raw, headers...); err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, "kafka send modality failed")
		return err
	}
	telemetrymetrics.Global().IncCounter("spectra_publish_modality_total")
	telemetrymetrics.Global().IncCounter("spectra_publish_modality_" + modalityToken + "_total")
	recordIngestToPublishLatency(env.TimestampMs, "spectra_ingest_to_publish_modality_"+modalityToken)
	return nil
}

func operationForEnvelope(env Envelope) string {
	if env.EventCategory == "request" {
		return "spectra_process_request"
	}
	if env.Modality != "" {
		return "spectra_process_modality_" + sanitizeMetricToken(env.Modality)
	}
	return "spectra_process_deepflow"
}

func sanitizeMetricToken(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return "unknown"
	}
	replaced := strings.ReplaceAll(value, "-", "_")
	replaced = strings.ReplaceAll(replaced, ".", "_")
	replaced = strings.ReplaceAll(replaced, " ", "_")
	switch replaced {
	case "action_event", "mcp_runtime", "exfil_event", "resilience_event", "request":
		return replaced
	default:
		return "unknown"
	}
}

func recordIngestToPublishLatency(timestampMs int64, operation string) {
	if timestampMs <= 0 {
		return
	}
	nowMs := time.Now().UnixMilli()
	if nowMs < timestampMs {
		return
	}
	lagMs := nowMs - timestampMs
	// Bound to 1 hour to avoid metric pollution from malformed historic timestamps.
	if lagMs > 3600_000 {
		return
	}
	telemetrymetrics.Global().RecordOperation(operation, "ok", time.Duration(lagMs)*time.Millisecond)
}

func buildModalityFeatures(env Envelope) map[string]any {
	features := make(map[string]any, len(env.Features)+10)
	for k, v := range env.Features {
		features[k] = v
	}
	switch env.Modality {
	case "action_event":
		setDefaultFeature(features, "event_name", env.EventName)
		setDefaultFeature(features, "event_category", []string{env.EventCategory})
		if env.UserID != "" {
			setDefaultFeature(features, "actor_id", env.UserID)
		}
		if env.RequestID != "" {
			setDefaultFeature(features, "session_id", env.RequestID)
		}
		if len(env.Attributes) > 0 {
			setDefaultFeature(features, "attributes", env.Attributes)
		}
	case "mcp_runtime":
		setDefaultFeature(features, "request_id", env.RequestID)
		if len(env.Attributes) > 0 {
			setDefaultFeature(features, "attributes", env.Attributes)
		}
	case "exfil_event":
		if len(env.Attributes) > 0 {
			setDefaultFeature(features, "attributes", env.Attributes)
		}
	case "resilience_event":
		if len(env.Attributes) > 0 {
			setDefaultFeature(features, "attributes", env.Attributes)
		}
	}
	return features
}

func setDefaultFeature(features map[string]any, key string, value any) {
	if _, ok := features[key]; ok {
		return
	}
	features[key] = value
}

func nonEmpty(a, b string) string {
	a = strings.TrimSpace(a)
	if a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

func headerOrDefault(v, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}

func dedupeID(env Envelope) string {
	return strings.Join([]string{
		strings.TrimSpace(env.SourceType),
		strings.TrimSpace(env.SourceID),
		strings.TrimSpace(env.SourceEventID),
	}, "|")
}

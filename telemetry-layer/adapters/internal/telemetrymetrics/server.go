package telemetrymetrics

import (
	"context"
	"net/http"
	"time"

	"cybermesh/telemetry-layer/adapters/utils"
)

func StartServer(ctx context.Context, addr string, logger *utils.Logger) {
	if addr == "" {
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", Global().Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if logger != nil {
			logger.Info("telemetry metrics server listening", utils.ZapString("addr", addr))
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed && logger != nil {
			logger.Warn("telemetry metrics server stopped", utils.ZapError(err))
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
}

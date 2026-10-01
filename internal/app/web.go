package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	readinessTimeout = 5 * time.Second
	shutdownTimeout  = 20 * time.Second
)

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", a.handleWebhook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		a.respond(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", a.handleReady)
	mux.Handle("GET /metrics", a.metrics.Handler())
	return mux
}

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		a.respond(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		a.logger.Warn("readiness check failed", "error", err.Error())
		a.respond(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	a.respond(w, http.StatusOK, "ready")
}

func (a *App) respond(w http.ResponseWriter, status int, body string) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, body+"\n"); err != nil {
		a.logger.Debug("write response failed", "error", err.Error())
	}
}

func (a *App) ServeHTTP(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("app: listen on %s: %w", addr, err)
	}
	return a.serveListener(ctx, ln)
}

func (a *App) serveListener(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- srv.Serve(ln)
	}()
	a.logger.Info("http server listening", "addr", ln.Addr().String())
	select {
	case err := <-errc:
		return fmt.Errorf("app: http server: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("app: http shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("app: http server: %w", err)
	}
	return nil
}

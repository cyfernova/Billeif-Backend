package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"invoice-backend/internal/voice/audio"
	voiceruntime "invoice-backend/internal/voice/runtime"
)

const (
	runtimeAddress  = "0.0.0.0:8080"
	shutdownTimeout = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		// The runtime deliberately avoids logging invocation, authorization, and
		// provider errors. Platform telemetry still records the exit status.
		slog.Error("voice runtime stopped unexpectedly")
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if err := audio.VerifyLibopus(); err != nil {
		return err
	}
	application, httpServer := newRuntimeApplication()
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		shutdownErr := shutdown(application, httpServer)
		if errors.Is(err, http.ErrServerClosed) {
			return shutdownErr
		}
		return errors.Join(err, shutdownErr)
	case <-ctx.Done():
		return shutdown(application, httpServer)
	}
}

func newRuntimeApplication() (*voiceruntime.Server, *http.Server) {
	application := voiceruntime.NewServer(voiceruntime.Config{
		RuntimeID: os.Getenv("RUNTIME_ID"),
		AWSRegion: os.Getenv("AWS_REGION"),
	}, voiceruntime.InvocationHandlerFunc(func(context.Context, voiceruntime.InvocationRequest) (voiceruntime.InvocationResponse, error) {
		return voiceruntime.InvocationResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       json.RawMessage(`{"error":"runtime signaling is not configured"}`),
		}, nil
	}))

	return application, &http.Server{
		Addr:              runtimeAddress,
		Handler:           application,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func shutdown(application *voiceruntime.Server, httpServer *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	runtimeDone := make(chan error, 1)
	go func() {
		runtimeDone <- application.Shutdown(ctx)
	}()

	httpError := httpServer.Shutdown(ctx)
	if ctx.Err() != nil {
		httpError = errors.Join(httpError, httpServer.Close())
	}
	runtimeError := <-runtimeDone
	return errors.Join(runtimeError, httpError)
}

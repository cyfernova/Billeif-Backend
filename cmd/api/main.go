package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"invoice-backend/internal/app"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	ctx := context.Background()
	rt, err := app.Initialize(ctx, app.InitializeOptions{EnableWorker: true})
	if err != nil {
		panic(fmt.Sprintf("failed to initialize runtime: %v", err))
	}
	defer rt.Close()

	if rt.Worker != nil {
		go rt.Worker.Start(ctx)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", rt.Config.Server.Port),
		Handler:      rt.Router,
		ReadTimeout:  time.Duration(rt.Config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(rt.Config.Server.WriteTimeout) * time.Second,
	}

	go func() {
		if rt.Config.Server.SSLEnabled {
			rt.Log.Info("starting https server", "port", rt.Config.Server.Port)
			if err := srv.ListenAndServeTLS(rt.Config.Server.SSLCertPath, rt.Config.Server.SSLKeyPath); err != nil && err != http.ErrServerClosed {
				rt.Log.Fatal("failed to start server", "error", err)
			}
		} else {
			rt.Log.Info("starting http server", "port", rt.Config.Server.Port)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				rt.Log.Fatal("failed to start server", "error", err)
			}
		}
	}()

	metricsSrv := &http.Server{
		Addr:    "127.0.0.1:9090",
		Handler: promhttp.Handler(),
	}
	go func() {
		rt.Log.Info("starting metrics server", "port", 9090)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			rt.Log.Error("metrics server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	rt.Log.Info("shutdown initiated")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		rt.Log.Error("server forced to shutdown", "error", err)
	}
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		rt.Log.Error("metrics server forced to shutdown", "error", err)
	}

	rt.Log.Info("shutdown complete")
}

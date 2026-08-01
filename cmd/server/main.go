package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
)

func main() {
	ctx := context.Background()
	rt, err := app.Initialize(ctx, app.InitializeOptions{EnableWorker: false, Profile: config.ProfileHTTP})
	if err != nil {
		panic(fmt.Sprintf("failed to initialize runtime: %v", err))
	}

	port := rt.Config.Server.Port
	if port == 0 {
		port = 8080
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      rt.Router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	rt.Log.Info("starting HTTP server", "port", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("failed to start server: %v", err))
	}
}

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"time-leak/config"
	"time-leak/internal/handler"
	"time-leak/internal/repository"
	"time-leak/internal/service"
	"time-leak/traits/database"
	"time-leak/traits/logger"

	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	zapLogger, err := logger.NewLogger()
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		zapLogger.Error("error init config", zap.Error(err))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-stop
		zapLogger.Info("shutting down gracefully...")
		cancel()
	}()

	db, err := database.InitDatabase(cfg, zapLogger)
	if err != nil {
		zapLogger.Error("error opening sqlite", zap.Error(err), zap.String("db_path", cfg.DBPath))
		return
	}
	defer db.Close()

	repos := repository.NewRepositories(db)
	services := service.NewServices(ctx, cfg, repos, zapLogger)
	h := handler.New(services, cfg, zapLogger)

	mux := http.NewServeMux()
	h.Register(mux)

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zapLogger.Error("error shutting down http server", zap.Error(err))
		}
	}()

	zapLogger.Info("http server starting", zap.String("addr", cfg.Addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		zapLogger.Error("http server failed", zap.Error(err))
	}
}

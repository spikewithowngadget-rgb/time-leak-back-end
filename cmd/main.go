package main

import (
	"context"
	"database/sql"
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
	"golang.org/x/crypto/bcrypt"
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

	if err := os.MkdirAll(cfg.NoteFilesPath, 0o755); err != nil {
		zapLogger.Error("error creating note files directory", zap.Error(err), zap.String("path", cfg.NoteFilesPath))
		return
	}

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

	// Seed the Swagger example test user so that the /api/v1/auth/login example
	// in Swagger UI works out of the box.
	if hash, err := bcrypt.GenerateFromPassword([]byte("StrongPass123!"), bcrypt.DefaultCost); err == nil {
		if err := repos.Auth.SeedUserIfNotExists(ctx, "wa_77015556677@otp.local", "+77015556677", string(hash), "en"); err != nil {
			zapLogger.Warn("failed to seed swagger test user", zap.Error(err))
		} else {
			zapLogger.Info("swagger test user ensured", zap.String("phone", "+77015556677"))
		}
	}

	ensureStaticTestingPhoneBindings(ctx, repos.Auth, zapLogger)

	h := handler.New(services, cfg, zapLogger)

	mux := http.NewServeMux()
	h.Register(mux)

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler.WithCORS(mux),
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

func ensureStaticTestingPhoneBindings(ctx context.Context, repo *repository.Repository, log *zap.Logger) {
	for _, contact := range service.StaticTestingPhoneContacts() {
		if contact.Email == "" {
			continue
		}

		user, err := repo.GetUserByEmail(ctx, contact.Email)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			log.Warn(
				"failed to load testing contact user by email",
				zap.String("email", contact.Email),
				zap.String("phone", contact.Phone),
				zap.Error(err),
			)
			continue
		}

		if user.Phone == contact.Phone {
			continue
		}
		if user.Phone != "" && user.Phone != contact.Phone {
			log.Warn(
				"skipping testing phone binding because user already has another phone",
				zap.String("email", contact.Email),
				zap.String("existing_phone", user.Phone),
				zap.String("testing_phone", contact.Phone),
			)
			continue
		}

		if err := repo.UpdateUserPhone(ctx, user.UserID, contact.Phone); err != nil {
			log.Warn(
				"failed to bind testing phone to existing user",
				zap.String("email", contact.Email),
				zap.String("phone", contact.Phone),
				zap.Error(err),
			)
			continue
		}

		log.Info(
			"testing phone bound to existing user",
			zap.String("email", contact.Email),
			zap.String("phone", contact.Phone),
		)
	}
}

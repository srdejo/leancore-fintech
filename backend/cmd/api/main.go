// Command api levanta el backend del ledger de crédito: aplica migraciones,
// compone los adaptadores y sirve la API REST.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	httpapi "leancore-fintech/backend/internal/adapters/in/http"
	"leancore-fintech/backend/internal/adapters/out/clock"
	"leancore-fintech/backend/internal/adapters/out/ids"
	"leancore-fintech/backend/internal/adapters/out/postgres"
	"leancore-fintech/backend/internal/application/usecases"
)

type config struct {
	databaseURL string
	httpAddr    string
	timezone    string
}

func loadConfig() (config, error) {
	cfg := config{
		databaseURL: os.Getenv("DATABASE_URL"),
		httpAddr:    envOr("HTTP_ADDR", ":8081"),
		timezone:    envOr("APP_TIMEZONE", "America/Bogota"),
	}
	if cfg.databaseURL == "" {
		return cfg, errors.New("DATABASE_URL es obligatorio")
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fallo al iniciar", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := waitAndMigrate(ctx, cfg.databaseURL, log); err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		return fmt.Errorf("pool: %w", err)
	}
	defer pool.Close()

	clk, err := clock.NewSystem(cfg.timezone)
	if err != nil {
		return fmt.Errorf("zona horaria %q: %w", cfg.timezone, err)
	}
	svc := usecases.NewService(postgres.NewRepository(pool), clk, ids.Generator{})

	srv := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           httpapi.NewHandler(svc, log),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("API escuchando", "addr", cfg.httpAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("apagando")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

// waitAndMigrate reintenta mientras la base arranca y aplica las migraciones.
func waitAndMigrate(ctx context.Context, dsn string, log *slog.Logger) error {
	var err error
	for attempt := 1; attempt <= 30; attempt++ {
		if err = postgres.Migrate(dsn); err == nil {
			log.Info("migraciones aplicadas")
			return nil
		}
		log.Warn("base de datos no disponible, reintentando", "intento", attempt, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("migraciones: %w", err)
}

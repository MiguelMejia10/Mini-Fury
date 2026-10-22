package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"mini-operador-ti/platform-api/internal/platform"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server_stopped", "error", err)
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	pool := 10
	if raw := os.Getenv("DB_MAX_CONNECTIONS"); raw != "" {
		pool, err = strconv.Atoi(raw)
		if err != nil || pool < 2 || pool > 100 {
			return errors.New("DB_MAX_CONNECTIONS must be between 2 and 100")
		}
	}
	db.SetMaxOpenConns(pool)
	db.SetMaxIdleConns(pool / 2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(root, 45*time.Second)
	defer cancel()
	for {
		if err = db.PingContext(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("database unavailable at startup")
		case <-time.After(time.Second):
		}
	}
	if err = platform.Migrate(ctx, db); err != nil {
		return err
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	origins := identity.Origins(os.Getenv("ALLOWED_ORIGINS"))
	if len(origins) == 0 {
		return errors.New("ALLOWED_ORIGINS is required")
	}
	repo := &identity.Postgres{DB: db}
	service, err := identity.NewService(repo)
	if err != nil {
		return err
	}
	auth := &identity.HTTP{Service: service, Log: logger, Origins: origins, Secure: os.Getenv("COOKIE_SECURE") != "false"}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-root.Done():
				return
			case <-ticker.C:
				cleanup, c := context.WithTimeout(root, 5*time.Second)
				if e := repo.Cleanup(cleanup); e != nil {
					logger.Error("identity_cleanup_failed", "error", e)
				}
				c()
			}
		}
	}()
	api := &platform.API{Store: &platform.PostgresStore{DB: db}, Log: logger, Auth: auth}
	server := &http.Server{Addr: addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan error, 1)
	go func() { logger.Info("server_started", "address", addr); done <- server.ListenAndServe() }()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-root.Done():
		shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		return server.Shutdown(shutdown)
	}
}

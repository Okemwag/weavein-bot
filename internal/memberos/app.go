package memberos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"memberos/internal/store"
	"memberos/internal/telegram"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	db, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		return err
	}
	a := &app{cfg: cfg, db: db, telegram: telegram.New(cfg.botToken, &http.Client{Timeout: 5 * time.Second})}
	if len(os.Args) > 1 && os.Args[1] == "provision" {
		return a.provision(ctx)
	}
	if len(os.Args) > 1 && os.Args[1] == "provision-managed" {
		return a.provisionManaged(ctx)
	}
	go a.processUpdates(ctx)
	go a.accessService().RunJobs(ctx)
	go a.accessService().RunExpirations(ctx)
	server := &http.Server{Addr: cfg.address, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	slog.Info("memberos listening", "address", cfg.address)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

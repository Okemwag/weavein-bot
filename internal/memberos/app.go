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
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Run() error {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		return migrateOnly()
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	poolConfig, err := pgxpool.ParseConfig(cfg.databaseURL)
	if err != nil {
		return fmt.Errorf("postgres configuration: %w", err)
	}
	// Seven workers share this pool with HTTP traffic, so pgx's default of
	// max(4, CPUs) is too small. pool_max_conns in the URL still takes priority.
	if !strings.Contains(cfg.databaseURL, "pool_max_conns") {
		poolConfig.MaxConns = 20
	}
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
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
	var workers sync.WaitGroup
	startWorker := func(run func(context.Context)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			superviseWorker(ctx, run)
		}()
	}
	startWorker(a.processUpdates)
	startWorker(a.accessService().RunJobs)
	startWorker(a.accessService().RunExpirations)
	startWorker(a.accessService().RunReconciliation)
	startWorker(a.sendNotifications)
	startWorker(a.runReminderSchedule)
	startWorker(a.runOperationsAlerts)
	// WriteTimeout allows for Stars reconciliation and CSV exports, which walk many pages.
	server := &http.Server{Addr: cfg.address, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	slog.Info("memberos listening", "address", cfg.address)
	err = server.ListenAndServe()
	stop()
	// ListenAndServe returns as soon as Shutdown begins, while requests may still be running.
	<-shutdownDone
	workers.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// superviseWorker restarts a worker after a panic, so one bad row cannot stop
// the payment inbox or take the HTTP server down with it.
func superviseWorker(ctx context.Context, run func(context.Context)) {
	for ctx.Err() == nil {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Error("worker panicked; restarting", "panic", recovered, "stack", string(debug.Stack()))
				}
			}()
			run(ctx)
		}()
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// migrateOnly applies pending migrations and exits. It needs only DATABASE_URL,
// so a deploy can run it as a release step before starting the service.
func migrateOnly() error {
	url := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("postgres configuration: %w", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		return err
	}
	slog.Info("migrations applied")
	return nil
}

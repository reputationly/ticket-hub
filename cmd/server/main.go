package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/reputationly/ticket-hub/internal/api"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/config"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/notifier"
	"github.com/reputationly/ticket-hub/internal/queue"
	"github.com/reputationly/ticket-hub/internal/sender"
)

func main() {
	if err := run(); err != nil {
		slog.Error("服务启动或运行失败，请检查配置与依赖")
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		slog.Error(err.Error())
		return err
	}
	d, err := db.Open(cfg.DBType, cfg.DBDSN)
	if err != nil {
		return err
	}
	sql, err := d.DB()
	if err != nil {
		return err
	}
	defer sql.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := cache.New(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer store.Close()
	if err = db.Bootstrap(d, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		return err
	}
	cipher, err := crypto.New(cfg.Secret)
	if err != nil {
		return err
	}
	var q queue.Queue = queue.NewMemory()
	if cfg.RedisURL != "" {
		q, err = queue.NewRedis(ctx, cfg.RedisURL, "ticket-hub:notify")
		if err != nil {
			return err
		}
	}
	defer q.Close()
	notify := notifier.New(d, store, cipher, q, sender.New(d, store, sender.Options{}))
	server := &http.Server{Addr: cfg.Address, Handler: api.New(d, store, cfg, notify), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); notify.Run(workerCtx) }()
	defer func() { stopWorker(); <-workerDone }()
	stop, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	result := make(chan error, 1)
	go func() { slog.Info("服务已启动", "address", cfg.Address); result <- server.ListenAndServe() }()
	select {
	case err = <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stop.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

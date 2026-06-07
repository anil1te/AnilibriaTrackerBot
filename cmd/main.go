package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"anilibria-bot/internal/anilibria"
	"anilibria-bot/internal/config"
	"anilibria-bot/internal/db"
	"anilibria-bot/internal/qbittorrent"
	"anilibria-bot/internal/telegram"
	"anilibria-bot/internal/worker"
)

func main() {
	slog.Info("Starting Anilibria Bot...")

	cfg := config.Load()
	if cfg.TelegramToken == "" {
		slog.Error("TELEGRAM_TOKEN is required")
		os.Exit(1)
	}

	database, err := db.Init(cfg.DBPath)
	if err != nil {
		slog.Error("Database init failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize API clients
	aniClient := anilibria.New()
	qbClient, err := qbittorrent.New(cfg)
	if err != nil {
		slog.Error("Failed to init qBittorrent client", "error", err)
		os.Exit(1)
	}

	bot, err := telegram.New(cfg, database, aniClient, qbClient)
	if err != nil {
		slog.Error("Telegram bot init failed", "error", err)
		os.Exit(1)
	}

	// Initialize and start worker
	w := worker.New(aniClient, qbClient, database, bot)
	go w.Start(ctx, 10*time.Minute) // Check every 10 minutes

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		slog.Info("Received shutdown signal")
		cancel()
	}()

	// Start bot blocking
	bot.Start(ctx)

	slog.Info("Shutdown complete")
}

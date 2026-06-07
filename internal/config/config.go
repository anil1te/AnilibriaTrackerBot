package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken        string
	AdminIDs             []int64
	DBPath               string
	QbittorrentURL       string
	QbittorrentUsername  string
	QbittorrentPassword  string
}

func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		slog.Warn("No .env file found, reading from environment variables")
	}

	adminIDsStr := os.Getenv("ADMIN_IDS")
	var adminIDs []int64
	for _, idStr := range strings.Split(adminIDsStr, ",") {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err == nil {
			adminIDs = append(adminIDs, id)
		}
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "anilibria.db"
	}

	return &Config{
		TelegramToken:       os.Getenv("TELEGRAM_TOKEN"),
		AdminIDs:            adminIDs,
		DBPath:              dbPath,
		QbittorrentURL:      os.Getenv("QBITTORRENT_URL"),
		QbittorrentUsername: os.Getenv("QBITTORRENT_USERNAME"),
		QbittorrentPassword: os.Getenv("QBITTORRENT_PASSWORD"),
	}
}

func (c *Config) IsAdmin(id int64) bool {
	if len(c.AdminIDs) == 0 {
		return true // If no admins configured, everyone is admin (for testing)
	}
	for _, adminID := range c.AdminIDs {
		if adminID == id {
			return true
		}
	}
	return false
}

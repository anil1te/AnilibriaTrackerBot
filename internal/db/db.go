package db

import (
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
}

func Init(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	d := &Database{db: db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	slog.Info("Database initialized successfully", "path", dbPath)
	return d, nil
}

func (d *Database) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY
	);
	
	CREATE TABLE IF NOT EXISTS subscriptions (
		user_id INTEGER,
		title_id INTEGER,
		PRIMARY KEY (user_id, title_id)
	);

	CREATE TABLE IF NOT EXISTS history (
		title_id INTEGER,
		episode INTEGER,
		downloaded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (title_id, episode)
	);

	CREATE TABLE IF NOT EXISTS settings (
		user_id INTEGER PRIMARY KEY,
		auto_download BOOLEAN DEFAULT 1
	);
	`
	_, err := d.db.Exec(query)
	return err
}

func (d *Database) GetSubscriptions() ([]int, error) {
	rows, err := d.db.Query("SELECT DISTINCT title_id FROM subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (d *Database) GetUsersSubscribedTo(titleID int) ([]int64, error) {
	rows, err := d.db.Query("SELECT user_id FROM subscriptions WHERE title_id = ?", titleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []int64
	for rows.Next() {
		var user int64
		if err := rows.Scan(&user); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (d *Database) IsEpisodeProcessed(titleID, episode int) (bool, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM history WHERE title_id = ? AND episode = ?", titleID, episode).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *Database) MarkEpisodeProcessed(titleID, episode int) error {
	_, err := d.db.Exec("INSERT OR IGNORE INTO history (title_id, episode) VALUES (?, ?)", titleID, episode)
	return err
}

func (d *Database) Subscribe(userID int64, titleID int) error {
	_, err := d.db.Exec("INSERT OR IGNORE INTO subscriptions (user_id, title_id) VALUES (?, ?)", userID, titleID)
	return err
}

func (d *Database) Unsubscribe(userID int64, titleID int) error {
	_, err := d.db.Exec("DELETE FROM subscriptions WHERE user_id = ? AND title_id = ?", userID, titleID)
	return err
}

func (d *Database) GetUserSubscriptions(userID int64) ([]int, error) {
	rows, err := d.db.Query("SELECT title_id FROM subscriptions WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (d *Database) GetAutoDownload(userID int64) (bool, error) {
	var autoDownload bool
	err := d.db.QueryRow("SELECT auto_download FROM settings WHERE user_id = ?", userID).Scan(&autoDownload)
	if err == sql.ErrNoRows {
		return true, nil // Default is true
	}
	if err != nil {
		return true, err
	}
	return autoDownload, nil
}

func (d *Database) SetAutoDownload(userID int64, enabled bool) error {
	_, err := d.db.Exec("INSERT INTO settings (user_id, auto_download) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET auto_download=excluded.auto_download", userID, enabled)
	return err
}

func (d *Database) IsAnyUserAutoDownloadEnabled(titleID int) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM subscriptions s
		LEFT JOIN settings set ON s.user_id = set.user_id
		WHERE s.title_id = ? AND COALESCE(set.auto_download, 1) = 1
	`
	var count int
	err := d.db.QueryRow(query, titleID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

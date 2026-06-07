package worker

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"anilibria-bot/internal/anilibria"
	"anilibria-bot/internal/db"
	"anilibria-bot/internal/qbittorrent"
	"anilibria-bot/internal/telegram"
)

type Worker struct {
	aniClient *anilibria.Client
	qbClient  *qbittorrent.Client
	database  *db.Database
	bot       *telegram.Bot
}

func New(ani *anilibria.Client, qb *qbittorrent.Client, database *db.Database, bot *telegram.Bot) *Worker {
	return &Worker{
		aniClient: ani,
		qbClient:  qb,
		database:  database,
		bot:       bot,
	}
}

func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial login to qBittorrent
	if err := w.qbClient.Login(); err != nil {
		slog.Error("Failed to login to qBittorrent on startup", "error", err)
	} else {
		slog.Info("Successfully logged into qBittorrent")
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping worker...")
			return
		case <-ticker.C:
			w.processUpdates()
		}
	}
}

func (w *Worker) processUpdates() {
	slog.Debug("Checking for updates...")
	
	releases, err := w.aniClient.GetLatestReleases()
	if err != nil {
		slog.Error("Failed to fetch latest releases", "error", err)
		return
	}

	subs, err := w.database.GetSubscriptions()
	if err != nil {
		slog.Error("Failed to fetch subscriptions", "error", err)
		return
	}

	subMap := make(map[int]bool)
	for _, id := range subs {
		subMap[id] = true
	}

	for _, release := range releases {
		// Only process if someone is subscribed to this release
		if !subMap[release.ID] {
			continue
		}

		if release.LatestEpisode == nil {
			continue
		}
		
		epNum := release.LatestEpisode.Ordinal

		processed, err := w.database.IsEpisodeProcessed(release.ID, epNum)
		if err != nil || processed {
			continue
		}

		slog.Info("Found new episode for subscribed release", "title", release.Name.Main, "episode", epNum)

		// Check if any subscribed user wants auto-download
		autoDownloadEnabled, _ := w.database.IsAnyUserAutoDownloadEnabled(release.ID)

		if autoDownloadEnabled {
			// Global check if the torrent client is alive before downloading
			if err := w.qbClient.CheckAlive(); err != nil {
				slog.Error("qBittorrent is offline, skipping auto-download", "error", err)
			} else {
				// Download logic
				if err := w.downloadRelease(release, epNum); err != nil {
					slog.Error("Failed to download release", "title", release.Name.Main, "error", err)
					continue
				}
			}
		}

		// Mark as processed
		if err := w.database.MarkEpisodeProcessed(release.ID, epNum); err != nil {
			slog.Error("Failed to mark episode as processed", "error", err)
			continue
		}

		// Notify users
		w.notifyUsers(release)
	}
}

func (w *Worker) downloadRelease(release anilibria.Release, targetEpisode int) error {
	torrents, err := w.aniClient.GetTorrents(release.ID)
	if err != nil || len(torrents) == 0 {
		return fmt.Errorf("failed to fetch torrents: %w", err)
	}

	// Simplistic choice: pick the first one (you could implement quality matching like 1080p here)
	var bestTorrent *anilibria.Torrent
	for _, t := range torrents {
		if t.Quality.Value == "1080p" {
			bestTorrent = &t
			break
		}
	}
	
	if bestTorrent == nil {
		bestTorrent = &torrents[0]
	}

	// Login check/retry
	if err := w.qbClient.AddTorrentPaused(bestTorrent.Magnet, "", "Anime"); err != nil {
		slog.Warn("AddTorrentPaused failed, attempting re-login", "error", err)
		if loginErr := w.qbClient.Login(); loginErr != nil {
			return err
		}
		if retryErr := w.qbClient.AddTorrentPaused(bestTorrent.Magnet, "", "Anime"); retryErr != nil {
			return retryErr
		}
	}

	// Background process to filter files
	go func() {
		// Try for up to 60 seconds to get metadata
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			files, err := w.qbClient.GetFiles(bestTorrent.Hash)
			if err == nil && len(files) > 0 {
				var toDownload []int
				var toSkip []int

				// Regex to find episode number (e.g. - 01.mkv, _1_, etc)
				epStr := fmt.Sprintf("%d", targetEpisode)
				epStr0 := fmt.Sprintf("%02d", targetEpisode)
				// Basic matching: file name contains episode number surrounded by non-word chars
				re := regexp.MustCompile(`(?:^|[^a-zA-Z0-9])0*` + regexp.QuoteMeta(epStr) + `(?:[^a-zA-Z0-9]|$)`)

				for _, f := range files {
					if re.MatchString(f.Name) || strings.Contains(f.Name, " "+epStr0+".") || strings.Contains(f.Name, " "+epStr+".") || strings.Contains(f.Name, "- "+epStr0+" ") || strings.Contains(f.Name, "- "+epStr+" ") {
						toDownload = append(toDownload, f.Index)
					} else {
						toSkip = append(toSkip, f.Index)
					}
				}

				if len(toDownload) == 0 {
					// Fallback: if we can't figure out which file it is, just download everything or the largest file?
					// For safety, let's just resume and download everything if we can't match it.
					slog.Warn("Could not identify specific episode file, downloading all", "release", release.ID, "episode", targetEpisode)
				} else {
					if len(toSkip) > 0 {
						w.qbClient.SetFilePriorities(bestTorrent.Hash, toSkip, 0)
					}
					// Optional: force priority 1 for selected files
					// w.qbClient.SetFilePriorities(bestTorrent.Hash, toDownload, 1)
				}

				// Resume torrent
				w.qbClient.ResumeTorrent(bestTorrent.Hash)
				return
			}
		}
		slog.Error("Failed to fetch metadata for torrent in time", "hash", bestTorrent.Hash)
		// Try to resume anyway
		w.qbClient.ResumeTorrent(bestTorrent.Hash)
	}()

	return nil
}

func (w *Worker) notifyUsers(release anilibria.Release) {
	users, err := w.database.GetUsersSubscribedTo(release.ID)
	if err != nil {
		slog.Error("Failed to get subscribed users", "error", err)
		return
	}

	msgText := fmt.Sprintf("🎉 *Новая серия!*\n📺 %s\n✨ Эпизод: %d\n\n📥 Загрузка торрента начата.", release.Name.Main, release.LatestEpisode.Ordinal)

	var photoURL string
	if release.Poster.Src != "" {
		photoURL = "https://www.anilibria.top" + release.Poster.Src
	}

	for _, userID := range users {
		w.bot.SendNotification(userID, msgText, photoURL)
	}
}

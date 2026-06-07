package anilibria

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://anilibria.top/api/v1"

type Client struct {
	httpClient *http.Client
}

func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type Release struct {
	ID    int `json:"id"`
	Alias string `json:"alias"`
	Name  struct {
		Main    string `json:"main"`
		English string `json:"english"`
	} `json:"name"`
	Poster struct {
		Src string `json:"src"`
	} `json:"poster"`
	PublishDay struct {
		Value       int    `json:"value"`
		Description string `json:"description"`
	} `json:"publish_day"`
	LatestEpisode *LatestEpisode `json:"latest_episode"`
	FreshAt       string         `json:"fresh_at"`
	EpisodesTotal int            `json:"episodes_total"`
}

type LatestEpisode struct {
	ID        string `json:"id"`
	Ordinal   int    `json:"ordinal"` // Episode number
	UpdatedAt string `json:"updated_at"`
}

type Torrent struct {
	ID       int    `json:"id"`
	Hash     string `json:"hash"`
	Magnet   string `json:"magnet"`
	Size     int64  `json:"size"`
	Quality  struct {
		Value string `json:"value"`
	} `json:"quality"`
	Description string   `json:"description"` // Often contains episode range, e.g., "1-9"
	Filename    string   `json:"filename"`
	Release     *Release `json:"release"`
}

// GetLatestReleases fetches the latest updated releases from Anilibria
func (c *Client) GetLatestReleases() ([]Release, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/anime/releases/latest", baseURL))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w", err)
	}

	return releases, nil
}

// GetTorrents fetches torrents for a specific release
func (c *Client) GetTorrents(releaseID int) ([]Torrent, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/anime/torrents/release/%d", baseURL, releaseID))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var torrents []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w", err)
	}

	return torrents, nil
}

// SearchReleases searches for an anime title
func (c *Client) SearchReleases(query string) ([]Release, error) {
	u := fmt.Sprintf("%s/app/search/releases?query=%s", baseURL, url.QueryEscape(query))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w", err)
	}

	return releases, nil
}

type ScheduleNow struct {
	Today []ScheduleItem `json:"today"`
}

type ScheduleItem struct {
	Release Release `json:"release"`
}

// GetScheduleToday fetches the releases scheduled for today
func (c *Client) GetScheduleToday() ([]Release, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/anime/schedule/now", baseURL))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var sched ScheduleNow
	if err := json.NewDecoder(resp.Body).Decode(&sched); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w", err)
	}

	var releases []Release
	for _, item := range sched.Today {
		releases = append(releases, item.Release)
	}

	return releases, nil
}

// GetScheduleWeek fetches the releases scheduled for the entire week
func (c *Client) GetScheduleWeek() ([]Release, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/anime/schedule/week", baseURL))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var items []ScheduleItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w", err)
	}

	var releases []Release
	for _, item := range items {
		releases = append(releases, item.Release)
	}

	return releases, nil
}

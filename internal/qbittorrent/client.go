package qbittorrent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"anilibria-bot/internal/config"
)

type Client struct {
	cfg        *config.Config
	httpClient *http.Client
}

func New(cfg *config.Config) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to init cookie jar: %w", err)
	}

	c := &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Jar:     jar,
		},
	}

	return c, nil
}

// CheckAlive pings the qBittorrent WebUI to see if it's reachable
func (c *Client) CheckAlive() error {
	req, err := http.NewRequest("GET", c.cfg.QbittorrentURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create alive check request: %w", err)
	}

	// Use a shorter timeout for alive check
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("qbittorrent web interface is unreachable: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// Login authenticates with the qBittorrent WebUI and stores the SID cookie
func (c *Client) Login() error {
	loginURL := fmt.Sprintf("%s/api/v2/auth/login", c.cfg.QbittorrentURL)

	data := url.Values{}
	data.Set("username", c.cfg.QbittorrentUsername)
	data.Set("password", c.cfg.QbittorrentPassword)

	req, err := http.NewRequest("POST", loginURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Referer", c.cfg.QbittorrentURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("login failed, unauthorized (check login/password)")
	}

	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if string(body) == "Fails." {
			return fmt.Errorf("login failed: wrong credentials")
		}
	} else if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed, status code: %d", resp.StatusCode)
	}

	return nil
}

// AddTorrent sends a magnet link to qBittorrent to be downloaded
func (c *Client) AddTorrent(magnetLink, savePath, category string) error {
	addURL := fmt.Sprintf("%s/api/v2/torrents/add", c.cfg.QbittorrentURL)

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	if err := w.WriteField("urls", magnetLink); err != nil {
		return err
	}
	
	// Optional: add path and category if provided
	if savePath != "" {
		if err := w.WriteField("savepath", savePath); err != nil {
			return err
		}
	}
	if category != "" {
		if err := w.WriteField("category", category); err != nil {
			return err
		}
	}

	w.Close()

	req, err := http.NewRequest("POST", addURL, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("add torrent request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("add torrent failed, status code: %d", resp.StatusCode)
	}

	return nil
}

// AddTorrentPaused adds a torrent but starts it in paused mode
func (c *Client) AddTorrentPaused(magnetLink, savePath, category string) error {
	addURL := fmt.Sprintf("%s/api/v2/torrents/add", c.cfg.QbittorrentURL)

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	if err := w.WriteField("urls", magnetLink); err != nil {
		return err
	}
	
	if savePath != "" {
		if err := w.WriteField("savepath", savePath); err != nil {
			return err
		}
	}
	if category != "" {
		if err := w.WriteField("category", category); err != nil {
			return err
		}
	}
	// Pause the torrent
	if err := w.WriteField("paused", "true"); err != nil {
		return err
	}

	w.Close()

	req, err := http.NewRequest("POST", addURL, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("add torrent request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("add torrent failed, status code: %d", resp.StatusCode)
	}

	return nil
}

// ResumeTorrent resumes a paused torrent
func (c *Client) ResumeTorrent(hash string) error {
	u := fmt.Sprintf("%s/api/v2/torrents/resume", c.cfg.QbittorrentURL)
	data := url.Values{}
	data.Set("hashes", hash)

	req, err := http.NewRequest("POST", u, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to resume torrent, status: %d", resp.StatusCode)
	}

	return nil
}

// PauseTorrent pauses a running torrent
func (c *Client) PauseTorrent(hash string) error {
	u := fmt.Sprintf("%s/api/v2/torrents/pause", c.cfg.QbittorrentURL)
	data := url.Values{}
	data.Set("hashes", hash)

	req, err := http.NewRequest("POST", u, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to pause torrent, status: %d", resp.StatusCode)
	}

	return nil
}

// DeleteTorrent deletes a torrent, optionally deleting its files
func (c *Client) DeleteTorrent(hash string, deleteFiles bool) error {
	u := fmt.Sprintf("%s/api/v2/torrents/delete", c.cfg.QbittorrentURL)
	data := url.Values{}
	data.Set("hashes", hash)
	if deleteFiles {
		data.Set("deleteFiles", "true")
	} else {
		data.Set("deleteFiles", "false")
	}

	req, err := http.NewRequest("POST", u, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete torrent, status: %d", resp.StatusCode)
	}

	return nil
}

type TorrentInfo struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	Progress   float64 `json:"progress"`
	Eta        int64   `json:"eta"`
	State      string  `json:"state"`
	Downloaded int64   `json:"downloaded"`
}

// GetTorrentsList fetches the list of torrents, optionally filtered by category
func (c *Client) GetTorrentsList(category string) ([]TorrentInfo, error) {
	u := fmt.Sprintf("%s/api/v2/torrents/info", c.cfg.QbittorrentURL)
	if category != "" {
		u += "?category=" + url.QueryEscape(category)
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get torrents list, status: %d", resp.StatusCode)
	}

	var torrents []TorrentInfo
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, err
	}

	return torrents, nil
}

type TorrentFile struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Priority int    `json:"priority"`
}

// GetFiles fetches the files of a torrent by hash
func (c *Client) GetFiles(hash string) ([]TorrentFile, error) {
	u := fmt.Sprintf("%s/api/v2/torrents/files?hash=%s", c.cfg.QbittorrentURL, hash)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("torrent not found or metadata not loaded")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get files, status: %d", resp.StatusCode)
	}

	var files []TorrentFile
	// qbittorrent returns an array of file objects, but some versions omit 'index'.
	// We can assign index manually just in case.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Try to unmarshal into a temporary slice of maps to ensure we get indices
	var rawFiles []map[string]interface{}
	if err := json.Unmarshal(body, &rawFiles); err != nil {
		return nil, err
	}

	for i, rf := range rawFiles {
		name, _ := rf["name"].(string)
		sizeFloat, _ := rf["size"].(float64)
		prioFloat, _ := rf["priority"].(float64)
		idxFloat, ok := rf["index"].(float64)
		idx := i
		if ok {
			idx = int(idxFloat)
		}

		files = append(files, TorrentFile{
			Index:    idx,
			Name:     name,
			Size:     int64(sizeFloat),
			Priority: int(prioFloat),
		})
	}

	return files, nil
}

// SetFilePriorities sets the priority of specified files
func (c *Client) SetFilePriorities(hash string, fileIndices []int, priority int) error {
	if len(fileIndices) == 0 {
		return nil
	}

	u := fmt.Sprintf("%s/api/v2/torrents/filePrio", c.cfg.QbittorrentURL)
	
	// Join indices with "|"
	var strIndices []string
	for _, idx := range fileIndices {
		strIndices = append(strIndices, fmt.Sprintf("%d", idx))
	}
	idStr := strings.Join(strIndices, "|")

	data := url.Values{}
	data.Set("hash", hash)
	data.Set("id", idStr)
	data.Set("priority", fmt.Sprintf("%d", priority))

	req, err := http.NewRequest("POST", u, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to set priorities, status: %d", resp.StatusCode)
	}

	return nil
}

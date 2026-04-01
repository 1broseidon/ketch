package crawl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/1broseidon/ketch/internal/appdirs"
)

// CrawlStatus tracks the progress of a background crawl.
type CrawlStatus struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Seed      string    `json:"seed"`
	Status    string    `json:"status"` // "starting", "running", "completed", "failed", "stopped"
	Pages     int       `json:"pages"`
	New       int       `json:"new"`
	Changed   int       `json:"changed"`
	Unchanged int       `json:"unchanged"`
	Errors    int       `json:"errors"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
}

// StatusDir returns the directory for crawl status files.
func StatusDir() (string, error) {
	return appdirs.StatusDir()
}

// StatusPath returns the file path for a crawl status by ID.
func StatusPath(id string) (string, error) {
	dir, err := StatusDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}

// GenerateCrawlID returns a unique crawl identifier like "c_a1b2c3d4".
func GenerateCrawlID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "c_" + hex.EncodeToString(b)
}

// WriteStatus atomically writes a crawl status file.
func WriteStatus(s *CrawlStatus) error {
	path, err := StatusPath(s.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadStatus reads a crawl status file by ID.
func ReadStatus(id string) (*CrawlStatus, error) {
	path, err := StatusPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s CrawlStatus
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListStatuses returns all crawl statuses, newest first.
func ListStatuses() ([]*CrawlStatus, error) {
	dir, err := StatusDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var statuses []*CrawlStatus
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		s, err := ReadStatus(id)
		if err != nil {
			continue
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

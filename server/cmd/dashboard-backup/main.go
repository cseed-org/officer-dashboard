//go:build linux

// dashboard-backup is the static container backup and stopped-volume maintenance tool.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"filippo.io/age"
	sqlite "github.com/mattn/go-sqlite3"
)

const chunkSize = 1900000

type snapshot struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Chunks    int    `json:"chunks"`
}
type usage struct {
	Bytes      int64 `json:"bytes"`
	Writes     int   `json:"writes"`
	Uploads    int   `json:"uploads"`
	ByteLimit  int64 `json:"byte_limit"`
	WriteLimit int   `json:"write_limit"`
}
type sink struct {
	origin string
	client *http.Client
}

func newSink() (*sink, error) {
	origin := strings.TrimRight(setting("BACKUP_SINK_URL"), "/")
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "host.docker.internal"))) {
		return nil, errors.New("sink requires HTTPS")
	}
	if setting("BACKUP_BEARER_SECRET") == "" {
		return nil, errors.New("missing backup bearer")
	}
	return &sink{origin, &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *sink) request(path, method string, body any, result any) error {
	var data []byte
	if b, ok := body.([]byte); ok {
		data = b
	} else if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, s.origin+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+setting("BACKUP_BEARER_SECRET"))
	req.Header.Set("CF-Access-Client-Id", setting("BACKUP_ACCESS_CLIENT_ID"))
	req.Header.Set("CF-Access-Client-Secret", setting("BACKUP_ACCESS_CLIENT_SECRET"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New("backup sink request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("backup sink returned status %d", resp.StatusCode)
	}
	if target, ok := result.(*[]byte); ok {
		*target, err = io.ReadAll(io.LimitReader(resp.Body, chunkSize+1))
		if len(*target) > chunkSize {
			return errors.New("oversize downloaded chunk")
		}
		return err
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 100000)).Decode(result)
	}
	return nil
}
func (s *sink) list() ([]snapshot, error) {
	var result []snapshot
	err := s.request("/snapshots", "GET", nil, &result)
	return result, err
}
func notify(message string) {
	fmt.Fprintln(os.Stderr, message)
	if endpoint := setting("DISCORD_WEBHOOK_URL"); endpoint != "" {
		body, _ := json.Marshal(map[string]string{"content": message})
		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
		if err == nil {
			resp.Body.Close()
		}
	}
}
func digest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	return hex.EncodeToString(hash.Sum(nil)), size, err
}
func (s *sink) download(m snapshot, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	var total int64
	for n := 0; n < m.Chunks; n++ {
		var body []byte
		err = s.request(fmt.Sprintf("/snapshots/%s/chunks/%d", m.ID, n), "GET", nil, &body)
		if err != nil {
			break
		}
		var size int
		size, err = io.MultiWriter(f, hash).Write(body)
		total += int64(size)
		if err != nil {
			break
		}
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil && (total != m.Size || hex.EncodeToString(hash.Sum(nil)) != m.SHA256) {
		err = errors.New("ciphertext checksum mismatch")
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}
func (s *sink) upload(path, kind string) (snapshot, error) {
	sha, size, err := digest(path)
	if err != nil {
		return snapshot{}, err
	}
	if size < 1 || size > 13*chunkSize {
		return snapshot{}, errors.New("snapshot exceeds 24.7 MB; upload skipped")
	}
	chunks := int((size + chunkSize - 1) / chunkSize)
	allocation := 2 * ((size+4095)/4096*4096 + int64(chunks)*8192 + 16384)
	var u usage
	if err = s.request("/prune", "POST", map[string]int{"hourly": 48}, nil); err != nil {
		return snapshot{}, err
	}
	if err = s.request("/usage", "GET", nil, &u); err != nil {
		return snapshot{}, err
	}
	if u.Bytes+allocation > 350000000 || allocation*82 > 450000000 {
		notify("Backup storage pressure: falling back to 24 hourly snapshots")
		if err = s.request("/prune", "POST", map[string]int{"hourly": 24}, &u); err != nil {
			return snapshot{}, err
		}
	}
	writes := 100 + 20*chunks
	if u.Bytes+allocation >= 350000000 || u.Writes+writes+50000 >= 70000 {
		notify("Backup usage reached 70% of a free-tier limit")
	}
	if u.Bytes+allocation > u.ByteLimit || u.Writes+writes > u.WriteLimit || u.Uploads >= 200 {
		return snapshot{}, errors.New("backup budget exhausted; upload skipped")
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return snapshot{}, err
	}
	m := snapshot{ID: hex.EncodeToString(id), Kind: kind, SHA256: sha, Size: size, Chunks: chunks}
	if err = s.request("/snapshots", "POST", m, nil); err != nil {
		return m, err
	}
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	for n := 0; n < chunks; n++ {
		data, readErr := io.ReadAll(io.LimitReader(f, chunkSize))
		if readErr != nil {
			return m, readErr
		}
		if err = s.request(fmt.Sprintf("/snapshots/%s/chunks/%d", m.ID, n), "PUT", data, nil); err != nil {
			return m, err
		}
	}
	var committed snapshot
	if err = s.request("/snapshots/"+m.ID+"/commit", "POST", nil, &committed); err != nil {
		return m, err
	}
	if committed.SHA256 != sha {
		return m, errors.New("remote checksum mismatch")
	}
	verify := path + ".verify"
	defer os.Remove(verify)
	return m, s.download(m, verify)
}
func dbPath() string {
	if p := setting("DB_PATH"); p != "" {
		return p
	}
	return "/data/dashboard.db"
}
func openDB(path string, readOnly bool) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"_busy_timeout": {"5000"}}
	if readOnly {
		q.Set("mode", "ro")
	}
	u.RawQuery = q.Encode()
	return sql.Open("sqlite3", u.String())
}
func integrity(path string) error {
	db, err := openDB(path, true)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return errors.New("SQLite integrity check failed")
	}
	return nil
}
func copyDatabase(source, target string) error {
	src, err := openDB(source, true)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := openDB(target, false)
	if err != nil {
		return err
	}
	defer dst.Close()
	ctx := context.Background()
	sc, err := src.Conn(ctx)
	if err != nil {
		return err
	}
	defer sc.Close()
	dc, err := dst.Conn(ctx)
	if err != nil {
		return err
	}
	defer dc.Close()
	err = dc.Raw(func(d any) error {
		return sc.Raw(func(s any) error {
			backup, err := d.(*sqlite.SQLiteConn).Backup("main", s.(*sqlite.SQLiteConn), "main")
			if err != nil {
				return err
			}
			for {
				done, stepErr := backup.Step(256)
				if stepErr != nil {
					backup.Finish()
					return stepErr
				}
				if done {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			return backup.Finish()
		})
	})
	// Release the borrowed connection before querying through the pool.
	dc.Close()
	sc.Close()
	if err != nil {
		return err
	}
	return integrity(target)
}
func encrypt(source, target string) error {
	recipient, err := age.ParseX25519Recipient(setting("BACKUP_AGE_RECIPIENT"))
	if err != nil {
		return errors.New("invalid public age recipient")
	}
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	sealed, err := age.Encrypt(out, recipient)
	if err != nil {
		return err
	}
	zipped := gzip.NewWriter(sealed)
	if _, err = io.Copy(zipped, src); err != nil {
		return err
	}
	if err = zipped.Close(); err != nil {
		return err
	}
	if err = sealed.Close(); err != nil {
		return err
	}
	return out.Sync()
}
func backup() (snapshot, error) {
	work, err := os.MkdirTemp(filepath.Dir(dbPath()), "backup-")
	if err != nil {
		return snapshot{}, err
	}
	defer os.RemoveAll(work)
	if err = copyDatabase(dbPath(), filepath.Join(work, "snapshot.db")); err != nil {
		return snapshot{}, err
	}
	encrypted := filepath.Join(work, "snapshot.age")
	if err = encrypt(filepath.Join(work, "snapshot.db"), encrypted); err != nil {
		return snapshot{}, err
	}
	s, err := newSink()
	if err != nil {
		return snapshot{}, err
	}
	return s.upload(encrypted, "hourly")
}
func membership() error {
	account, database := setting("BACKUP_MEMBERSHIP_ACCOUNT_ID"), setting("BACKUP_MEMBERSHIP_DATABASE_ID")
	endpoint := "https://api.cloudflare.com/client/v4/accounts/" + url.PathEscape(account) + "/d1/database/" + url.PathEscape(database) + "/export"
	client := &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	bookmark := ""
	for attempt := 0; attempt < 60; attempt++ {
		payload := map[string]string{"output_format": "polling"}
		if bookmark != "" {
			payload["current_bookmark"] = bookmark
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+setting("BACKUP_MEMBERSHIP_API_TOKEN"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return errors.New("membership export request failed")
		}
		var r struct {
			Success bool `json:"success"`
			Result  struct {
				Status   string `json:"status"`
				Bookmark string `json:"at_bookmark"`
				Result   struct {
					URL string `json:"signed_url"`
				} `json:"result"`
			} `json:"result"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 100000)).Decode(&r)
		resp.Body.Close()
		if err != nil || !r.Success || r.Result.Status == "error" {
			return errors.New("membership export rejected")
		}
		if r.Result.Status == "complete" {
			u, err := url.Parse(r.Result.Result.URL)
			if err != nil || u.Scheme != "https" {
				return errors.New("invalid export URL")
			}
			work, err := os.MkdirTemp(filepath.Dir(dbPath()), "membership-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(work)
			resp, err = client.Get(u.String())
			if err != nil {
				return errors.New("membership download failed")
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return errors.New("membership download rejected")
			}
			path := filepath.Join(work, "membership.sql")
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			size, err := io.Copy(f, io.LimitReader(resp.Body, 500000001))
			f.Close()
			if err != nil {
				return err
			}
			if size > 500000000 {
				return errors.New("membership export too large")
			}
			encrypted := filepath.Join(work, "membership.age")
			if err = encrypt(path, encrypted); err != nil {
				return err
			}
			s, err := newSink()
			if err != nil {
				return err
			}
			_, err = s.upload(encrypted, "d1-weekly")
			return err
		}
		bookmark = r.Result.Bookmark
		time.Sleep(5 * time.Second)
	}
	return errors.New("membership export timed out")
}
func scheduled() error {
	if _, err := os.Stat(dbPath()); os.IsNotExist(err) {
		output := dbPath() + ".recovery.age"
		if _, err := os.Stat(output); err == nil {
			return nil
		}
		s, err := newSink()
		if err != nil {
			return err
		}
		all, err := s.list()
		if err != nil {
			return err
		}
		for _, m := range all {
			if m.Kind != "d1-weekly" {
				return s.download(m, output)
			}
		}
		return errors.New("no recovery snapshot available")
	}
	m, err := backup()
	if err != nil {
		return err
	}
	s, err := newSink()
	if err != nil {
		return err
	}
	all, err := s.list()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	day := now.Format("2006-01-02")
	weekday := (int(now.Weekday()) + 6) % 7
	week := now.AddDate(0, 0, -weekday).Format("2006-01-02")
	daily, weekly := false, false
	for _, m := range all {
		if m.Kind == "daily" && strings.HasPrefix(m.CreatedAt, day) {
			daily = true
		}
		if m.Kind == "d1-weekly" && m.CreatedAt >= week {
			weekly = true
		}
	}
	if !daily {
		if err = s.request("/snapshots/"+m.ID+"/promote", "POST", nil, nil); err != nil {
			return err
		}
	}
	if !weekly && setting("BACKUP_MEMBERSHIP_API_TOKEN") != "" {
		return membership()
	}
	return nil
}
func locked(fn func() error) error {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(dbPath()), ".backup.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	return fn()
}
func run(args []string) error {
	if err := loadSecretFiles(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("usage: dashboard-backup backup|loop|download|checkpoint|rollback|cleanup|membership")
	}
	switch args[0] {
	case "loop":
		for {
			if err := locked(scheduled); err != nil {
				notify("Backup failed or skipped; inspect sink usage and configuration")
			}
			time.Sleep(time.Until(time.Now().Truncate(time.Hour).Add(time.Hour)))
		}
	case "backup":
		return locked(func() error {
			m, err := backup()
			if err == nil {
				fmt.Println(m.ID)
			}
			return err
		})
	case "membership":
		return locked(membership)
	case "download":
		flags := flag.NewFlagSet("download", flag.ContinueOnError)
		output := flags.String("output", "", "new ciphertext destination")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *output == "" {
			return errors.New("output required")
		}
		return locked(func() error {
			s, err := newSink()
			if err != nil {
				return err
			}
			all, err := s.list()
			if err != nil {
				return err
			}
			for _, m := range all {
				if m.Kind != "d1-weekly" {
					return s.download(m, *output)
				}
			}
			return errors.New("no committed SQLite snapshot")
		})
	case "checkpoint":
		if err := copyDatabase(dbPath(), filepath.Join(filepath.Dir(dbPath()), ".update-before.db")); err != nil {
			return err
		}
		return copyDatabase(dbPath(), filepath.Join(filepath.Dir(dbPath()), ".migration-check.db"))
	case "rollback":
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.Remove(dbPath() + suffix); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return os.Rename(filepath.Join(filepath.Dir(dbPath()), ".update-before.db"), dbPath())
	case "cleanup":
		for _, name := range []string{".update-before.db", ".migration-check.db", ".migration-check.db-wal", ".migration-check.db-shm"} {
			if err := os.Remove(filepath.Join(filepath.Dir(dbPath()), name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}
	return errors.New("unknown backup command")
}
func main() {
	syscall.Umask(0077)
	if err := run(os.Args[1:]); err != nil {
		notify("Backup operation failed; check configuration, budget and snapshot availability")
		os.Exit(1)
	}
}

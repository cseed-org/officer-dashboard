package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Alerter posts short operational alerts (storage full, Drive disconnected) as plain
// Discord webhook `content`, at most one per key per Every (an hour by default). With no
// webhook it logs instead. Messages must carry no names, emails or tokens.
type Alerter struct {
	URL   string // Discord webhook; "" logs only
	HTTP  *http.Client
	Log   *slog.Logger
	Now   func() time.Time
	Every time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

// NewAlerter builds an alerter for a webhook URL ("" for console only).
func NewAlerter(url string, log *slog.Logger) *Alerter {
	return &Alerter{URL: url, HTTP: &http.Client{Timeout: 5 * time.Second}, Log: log}
}

// Alert sends msg unless an alert with the same key went out within Every. It reports
// whether this call sent (or logged) the alert. A nil Alerter does nothing.
func (a *Alerter) Alert(ctx context.Context, key, msg string) bool {
	if a == nil {
		return false
	}
	now, every := time.Now, a.Every
	if a.Now != nil {
		now = a.Now
	}
	if every <= 0 {
		every = time.Hour
	}
	a.mu.Lock()
	if a.last == nil {
		a.last = map[string]time.Time{}
	}
	if t, ok := a.last[key]; ok && now().Sub(t) < every {
		a.mu.Unlock()
		return false
	}
	a.last[key] = now()
	a.mu.Unlock()
	log := a.Log
	if log == nil {
		log = slog.Default()
	}
	log.Warn("ops alert", "key", key, "message", msg)
	if a.URL == "" {
		return true
	}
	payload, _ := json.Marshal(map[string]any{"content": trunc(msg, 1900), "allowed_mentions": map[string]any{"parse": []string{}}})
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodPost, a.URL, bytes.NewReader(payload))
	if err != nil {
		return true
	}
	req.Header.Set("Content-Type", "application/json")
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		log.Warn("ops alert delivery failed", "key", key)
		return true
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		log.Warn("ops alert delivery failed", "key", key, "status", res.StatusCode)
	}
	return true
}

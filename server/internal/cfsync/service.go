// Package cfsync runs bounded, serialized daily Cloudflare pulls.
package cfsync

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/store"
	"sync"
	"time"
)

var ErrBusy = errors.New("a Cloudflare operation is already running")

type Service struct {
	Client *cloudflare.Client
	DB     *store.DB
	Log    *slog.Logger
	mu     sync.Mutex
}

func (s *Service) Lock() bool { return s.mu.TryLock() }
func (s *Service) Unlock()    { s.mu.Unlock() }
func (s *Service) Run(ctx context.Context, actor *string) error {
	if !s.Lock() {
		return ErrBusy
	}
	defer s.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	now := time.Now().UTC()
	var failures []error
	if !s.Client.D1Connected() {
		log.Info("Cloudflare members sync skipped: Not connected")
	} else {
		people, submissions, err := s.Client.Pull(ctx)
		if err == nil {
			err = s.DB.Write(ctx, func(q *store.Queries) error {
				if e := q.ReplaceMembers(ctx, people, submissions, now.UnixMilli()); e != nil {
					return e
				}
				return q.InsertAudit(ctx, store.AuditEntry{ID: auth.UUID(), ActorUserID: actor, Action: "members.synced", EntityType: "members", AfterJSON: jsonText(map[string]int{"people": len(people), "submissions": len(submissions)}), CreatedAt: store.Ms(now.UnixMilli())})
			})
		}
		if err != nil {
			failures = append(failures, err)
			s.recordError(ctx, "members", err)
		}
	}
	for _, kind := range []string{"web-analytics", "traffic-security", "performance", "workers-d1"} {
		if !s.Client.AnalyticsConnectedFor(kind) {
			log.Info("Cloudflare analytics sync skipped: Not connected", "widget", kind)
			continue
		}
		variables := map[string]any{"start": now.Add(-24 * time.Hour).Format(time.RFC3339), "end": now.Format(time.RFC3339)}
		if kind == "traffic-security" {
			variables["zone"] = s.Client.Config.ZoneID
		} else {
			variables["account"] = s.Client.Config.AccountID
		}
		if kind == "web-analytics" {
			variables["start"] = now.Add(-7 * 24 * time.Hour).Format(time.RFC3339)
		}
		if kind == "workers-d1" {
			variables["day"] = now.Add(-24 * time.Hour).Format("2006-01-02")
		}
		data, err := s.Client.GraphQL(ctx, analyticsQuery(kind), variables)
		if err == nil {
			err = s.DB.Write(ctx, func(q *store.Queries) error {
				if e := q.SaveSnapshot(ctx, kind, now.Format("2006-01-02"), data, now.UnixMilli()); e != nil {
					return e
				}
				return q.InsertAudit(ctx, store.AuditEntry{ID: auth.UUID(), ActorUserID: actor, Action: "cloudflare.synced", EntityType: "analytics", EntityID: &kind, CreatedAt: store.Ms(now.UnixMilli())})
			})
		}
		if err != nil {
			failures = append(failures, err)
			s.recordError(ctx, kind, err)
		}
	}
	return errors.Join(failures...)
}
func jsonText(v any) *string { b, _ := json.Marshal(v); s := string(b); return &s }
func (s *Service) recordError(ctx context.Context, kind string, err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if s.Log != nil {
		s.Log.Warn("Cloudflare sync failed", "kind", kind, "error", err)
	}
	_ = s.DB.Write(ctx, func(q *store.Queries) error {
		if e := q.SyncState(ctx, kind, nil, err.Error()); e != nil {
			return e
		}
		return q.InsertAudit(ctx, store.AuditEntry{ID: auth.UUID(), Action: "cloudflare.sync_failed", EntityType: "cloudflare", EntityID: &kind, AfterJSON: jsonText(map[string]string{"error": err.Error()}), CreatedAt: store.Ms(time.Now().UnixMilli())})
	})
}

// Daily also catches up at startup; cancellation stops timers and in-flight requests.
func (s *Service) Daily(ctx context.Context) {
	for {
		err := s.Run(ctx, nil)
		delay := 24 * time.Hour
		if errors.Is(err, ErrBusy) {
			delay = time.Minute
			if s.Log != nil {
				s.Log.Info("Cloudflare daily sync busy; retrying in one minute")
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Separate queries isolate plan/permission failures to their widget.
func analyticsQuery(kind string) string {
	switch kind {
	case "web-analytics":
		return `query($account: string!, $start: Time!, $end: Time!) { viewer { accounts(filter: {accountTag: $account}) { rumPageloadEventsAdaptiveGroups(limit: 1, filter: {datetime_geq: $start, datetime_lt: $end}) { count sum { visits } }
topPages: rumPageloadEventsAdaptiveGroups(limit: 3, orderBy: [count_DESC], filter: {datetime_geq: $start, datetime_lt: $end}) { count dimensions { requestPath } }
referrers: rumPageloadEventsAdaptiveGroups(limit: 3, orderBy: [count_DESC], filter: {datetime_geq: $start, datetime_lt: $end}) { count dimensions { refererHost } }
countries: rumPageloadEventsAdaptiveGroups(limit: 3, orderBy: [count_DESC], filter: {datetime_geq: $start, datetime_lt: $end}) { count dimensions { countryName } } } } }`
	case "performance":
		return `query($account: string!, $start: Time!, $end: Time!) { viewer { accounts(filter: {accountTag: $account}) { rumWebVitalsEventsAdaptiveGroups(limit: 1, filter: {datetime_geq: $start, datetime_lt: $end}) { count quantiles { largestContentfulPaintP75 interactionToNextPaintP75 cumulativeLayoutShiftP75 } } } } }`
	case "traffic-security":
		return `query($zone: string!, $start: Time!, $end: Time!) { viewer { zones(filter: {zoneTag: $zone}) { httpRequestsAdaptiveGroups(limit: 1, filter: {datetime_geq: $start, datetime_lt: $end}) { count sum { edgeResponseBytes } } cached: httpRequestsAdaptiveGroups(limit:1,filter:{datetime_geq:$start,datetime_lt:$end,cacheStatus:"hit"}){count} firewallEventsAdaptiveGroups(limit: 10, filter: {datetime_geq: $start, datetime_lt: $end}) { count dimensions { action } } } } }`
	default:
		return `query($account: string!, $start: Time!, $end: Time!, $day: Date!) { viewer { accounts(filter: {accountTag: $account}) { workersInvocationsAdaptive(limit: 1, filter: {datetime_geq: $start, datetime_lt: $end}) { sum { requests errors } quantiles {cpuTimeP50} } d1AnalyticsAdaptiveGroups(limit: 1, filter: {date_geq: $day, date_leq: $day}) { sum { readQueries writeQueries rowsRead rowsWritten } } d1StorageAdaptiveGroups(limit:1,filter:{date_geq:$day,date_leq:$day}) {max {databaseSizeBytes}} } } }`
	}
}
